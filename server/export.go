// export.go — Data collection for reports, OEE, and Excel/CSV export.
package server

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

// --- CSV/XLSX writing helpers ---

func writeCSV(w http.ResponseWriter, filename string, headers []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	cw := csv.NewWriter(w)
	cw.Write(headers)
	for _, row := range rows {
		cw.Write(row)
	}
	cw.Flush()
}

type xlsxSheet struct {
	Name    string
	Headers []string
	Rows    [][]string
}

func writeXLSX(w http.ResponseWriter, filename string, sheets []xlsxSheet) {
	f := excelize.NewFile()
	defer f.Close()

	for i, sheet := range sheets {
		name := sheet.Name
		if i == 0 {
			f.SetSheetName("Sheet1", name)
		} else {
			f.NewSheet(name)
		}
		for col, h := range sheet.Headers {
			cell, _ := excelize.CoordinatesToCellName(col+1, 1)
			f.SetCellValue(name, cell, h)
		}
		for rowIdx, row := range sheet.Rows {
			for col, val := range row {
				cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx+2)
				// Try to set as number for better Excel handling
				if n, err := strconv.ParseFloat(val, 64); err == nil {
					f.SetCellValue(name, cell, n)
				} else {
					f.SetCellValue(name, cell, val)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if _, err := f.WriteTo(w); err != nil {
		log.Printf("export xlsx: %v", err)
	}
}

// --- Flat-row converters ---

func fmtFloat(f float64) string    { return strconv.FormatFloat(f, 'f', 4, 64) }
func fmtInt(i int) string           { return strconv.Itoa(i) }
func fmtBool(b bool) string         { if b { return "1" }; return "0" }

var hourlyCountsHeaders = []string{
	"count_date", "screen_id", "shape_id", "shape_name", "shape_type",
	"shift_name", "job_style", "style_name", "shift_hour",
	"actual", "planned", "style_minutes", "is_early", "is_overtime",
}

func flattenHourlyCounts(countRows []HourlyCountRow, infos map[string]*shapeLayoutInfo) ([]string, [][]string) {
	var rows [][]string
	for _, cr := range countRows {
		shapeName := ""
		shapeType := ""
		styleName := ""
		if si, ok := infos[cr.ShapeID]; ok {
			shapeName = si.Name
			shapeType = si.Type
			styleName = si.StyleNames[cr.JobStyle]
		}
		if styleName == "" && cr.JobStyle != "" {
			styleName = "Style" + cr.JobStyle
		}
		rows = append(rows, []string{
			cr.CountDate, cr.ScreenID, cr.ShapeID, shapeName, shapeType,
			cr.ShiftName, cr.JobStyle, styleName, fmtInt(cr.ShiftHour),
			fmtInt(cr.Delta), fmtInt(cr.Planned), fmtInt(cr.StyleMinutes),
			fmtBool(cr.IsEarly), fmtBool(cr.IsOvertime),
		})
	}
	return hourlyCountsHeaders, rows
}

var shiftSummaryHeaders = []string{
	"count_date", "screen_id", "shape_id", "shape_name", "shape_type",
	"shift_name", "job_style", "style_name",
	"actual", "planned", "early", "overtime",
	"availability", "performance", "quality", "oee",
	"downtime_seconds", "downtime_count",
	"operator_entry_seconds", "operator_overcycle_seconds",
	"machine_overcycle_seconds", "starved_blocked_seconds",
	"tool_change_seconds", "tool_change_count",
	"red_rabbit_seconds", "scrap_count",
	"andon_prod_seconds", "andon_maint_seconds", "andon_logistics_seconds",
	"andon_quality_seconds", "andon_hr_seconds", "andon_emergency_seconds",
	"andon_tooling_seconds", "andon_engineering_seconds",
	"andon_controls_seconds", "andon_it_seconds",
	"total_work_seconds", "style_minutes", "computed_at",
}

func flattenShiftSummary(summaryRows []ShiftSummaryRow) ([]string, [][]string) {
	var rows [][]string
	for _, r := range summaryRows {
		rows = append(rows, []string{
			r.CountDate, r.ScreenID, r.ShapeID, r.ShapeName, r.ShapeType,
			r.ShiftName, r.JobStyle, r.StyleName,
			fmtInt(r.Actual), fmtInt(r.Planned), fmtInt(r.Early), fmtInt(r.Overtime),
			fmtFloat(r.Availability), fmtFloat(r.Performance), fmtFloat(r.Quality), fmtFloat(r.OEE),
			fmtFloat(r.DowntimeSeconds), fmtInt(r.DowntimeCount),
			fmtFloat(r.OperatorEntrySeconds), fmtFloat(r.OperatorOvercycleSeconds),
			fmtFloat(r.MachineOvercycleSeconds), fmtFloat(r.StarvedBlockedSeconds),
			fmtFloat(r.ToolChangeSeconds), fmtInt(r.ToolChangeCount),
			fmtFloat(r.RedRabbitSeconds), fmtInt(r.ScrapCount),
			fmtFloat(r.AndonProdSeconds), fmtFloat(r.AndonMaintSeconds), fmtFloat(r.AndonLogisticsSeconds),
			fmtFloat(r.AndonQualitySeconds), fmtFloat(r.AndonHRSeconds), fmtFloat(r.AndonEmergencySeconds),
			fmtFloat(r.AndonToolingSeconds), fmtFloat(r.AndonEngineeringSeconds),
			fmtFloat(r.AndonControlsSeconds), fmtFloat(r.AndonITSeconds),
			fmtFloat(r.TotalWorkSeconds), fmtInt(r.StyleMinutes), r.ComputedAt,
		})
	}
	return shiftSummaryHeaders, rows
}

func flattenDurationReport(ops []reportOperation) ([]string, [][]string) {
	headers := []string{"shape_id", "shape_name", "shift_name", "shift_hour", "seconds", "count", "total_seconds", "total_count"}
	var rows [][]string
	for _, op := range ops {
		for _, sh := range op.Shifts {
			if len(sh.Hours) == 0 {
				rows = append(rows, []string{
					op.ShapeID, op.Name, sh.Name, "",
					fmtFloat(sh.TotalSeconds), fmtInt(sh.TotalCount),
					fmtFloat(sh.TotalSeconds), fmtInt(sh.TotalCount),
				})
				continue
			}
			for _, hr := range sh.Hours {
				rows = append(rows, []string{
					op.ShapeID, op.Name, sh.Name, fmtInt(hr.ShiftHour),
					fmtFloat(hr.Seconds), fmtInt(hr.Count),
					fmtFloat(sh.TotalSeconds), fmtInt(sh.TotalCount),
				})
			}
		}
	}
	return headers, rows
}

func flattenOEEReport(ops []oeeOperation) ([]string, [][]string) {
	headers := []string{"shape_id", "shape_name", "shift_name",
		"availability", "performance", "quality", "oee",
		"downtime_seconds", "actual", "planned", "scrapped"}
	var rows [][]string
	for _, op := range ops {
		for _, sh := range op.Shifts {
			rows = append(rows, []string{
				op.ShapeID, op.Name, sh.Name,
				fmtFloat(sh.Availability), fmtFloat(sh.Performance), fmtFloat(sh.Quality), fmtFloat(sh.OEE),
				fmtFloat(sh.DowntimeSeconds), fmtInt(sh.Actual), fmtInt(sh.Planned), fmtInt(sh.Scrapped),
			})
		}
	}
	return headers, rows
}

func flattenAndonReport(ops []andonOperation) ([]string, [][]string) {
	headers := []string{"shape_id", "shape_name", "shift_name", "andon_type", "andon_label", "seconds"}
	var rows [][]string
	for _, op := range ops {
		for _, sh := range op.Shifts {
			for _, a := range sh.Andons {
				rows = append(rows, []string{
					op.ShapeID, op.Name, sh.Name, a.Type, a.Label, fmtFloat(a.Seconds),
				})
			}
		}
	}
	return headers, rows
}

func flattenTrendReport(resp trendReportResponse) ([]string, [][]string) {
	headers := []string{"date", "shift", "style", "value"}
	var rows [][]string
	for _, ds := range resp.Datasets {
		for i, val := range ds.Data {
			label := ""
			if i < len(resp.Labels) {
				label = resp.Labels[i]
			}
			rows = append(rows, []string{label, ds.Shift, ds.Style, fmtFloat(val)})
		}
	}
	return headers, rows
}

// --- Export HTTP handlers ---

func (a *API) ExportHourlyCountsCSV(w http.ResponseWriter, r *http.Request) {
	headers, rows := a.collectHourlyCounts(r)
	writeCSV(w, "hourly-counts.csv", headers, rows)
}

func (a *API) ExportHourlyCountsXLSX(w http.ResponseWriter, r *http.Request) {
	headers, rows := a.collectHourlyCounts(r)
	writeXLSX(w, "hourly-counts.xlsx", []xlsxSheet{{Name: "Hourly Counts", Headers: headers, Rows: rows}})
}

func (a *API) collectHourlyCounts(r *http.Request) ([]string, [][]string) {
	screenID := r.URL.Query().Get("screen")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	shape := r.URL.Query().Get("shape")

	today := time.Now().Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = today
	}

	infos := buildShapeInfosFromStore(a.Store, screenID)

	fromDate, _ := time.Parse("2006-01-02", from)
	toDate, _ := time.Parse("2006-01-02", to)

	var allRows [][]string
	var headers []string
	for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		countRows, err := a.EventLog.QueryHourlyCounts(screenID, date, shape, "")
		if err != nil {
			continue
		}
		h, rows := flattenHourlyCounts(countRows, infos)
		headers = h
		allRows = append(allRows, rows...)
	}
	if headers == nil {
		headers = hourlyCountsHeaders
	}
	return headers, allRows
}

func (a *API) ExportShiftSummaryCSV(w http.ResponseWriter, r *http.Request) {
	headers, rows := a.collectShiftSummary(r)
	writeCSV(w, "shift-summary.csv", headers, rows)
}

func (a *API) ExportShiftSummaryXLSX(w http.ResponseWriter, r *http.Request) {
	headers, rows := a.collectShiftSummary(r)
	writeXLSX(w, "shift-summary.xlsx", []xlsxSheet{{Name: "Shift Summary", Headers: headers, Rows: rows}})
}

func (a *API) collectShiftSummary(r *http.Request) ([]string, [][]string) {
	screenID := r.URL.Query().Get("screen")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	shift := r.URL.Query().Get("shift")
	shape := r.URL.Query().Get("shape")

	today := time.Now().Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = today
	}

	summaryRows, err := a.EventLog.QueryShiftSummary(screenID, from, to, shift, shape)
	if err != nil {
		return shiftSummaryHeaders, nil
	}
	return flattenShiftSummary(summaryRows)
}

func (a *API) ExportReportsCSV(w http.ResponseWriter, r *http.Request) {
	headers, rows := a.collectReportExport(r)
	writeCSV(w, "reports.csv", headers, rows)
}

func (a *API) ExportReportsXLSX(w http.ResponseWriter, r *http.Request) {
	reportType := r.URL.Query().Get("report_type")
	if reportType == "all" {
		a.exportAllReportsXLSX(w, r)
		return
	}
	headers, rows := a.collectReportExport(r)
	name := reportType
	if name == "" {
		name = "Report"
	}
	writeXLSX(w, "reports.xlsx", []xlsxSheet{{Name: name, Headers: headers, Rows: rows}})
}

func (a *API) exportAllReportsXLSX(w http.ResponseWriter, r *http.Request) {
	screenID := r.URL.Query().Get("screen")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	shiftFilter := r.URL.Query().Get("shift")
	shapeFilter := r.URL.Query().Get("shape")

	today := time.Now().Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = today
	}

	types := []string{"downtime", "operator_entry", "operator_overcycle", "machine_overcycle", "starved_blocked", "red_rabbit", "tool_change", "oee", "andon_response"}
	var sheets []xlsxSheet
	for _, rt := range types {
		headers, rows := a.collectReportForType(screenID, from, to, shiftFilter, shapeFilter, rt)
		sheets = append(sheets, xlsxSheet{Name: rt, Headers: headers, Rows: rows})
	}
	writeXLSX(w, "reports-all.xlsx", sheets)
}

func (a *API) collectReportExport(r *http.Request) ([]string, [][]string) {
	screenID := r.URL.Query().Get("screen")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	reportType := r.URL.Query().Get("report_type")
	shiftFilter := r.URL.Query().Get("shift")
	shapeFilter := r.URL.Query().Get("shape")

	today := time.Now().Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = today
	}

	return a.collectReportForType(screenID, from, to, shiftFilter, shapeFilter, reportType)
}

func (a *API) collectReportForType(screenID, from, to, shiftFilter, shapeFilter, reportType string) ([]string, [][]string) {
	fromDate, _ := time.Parse("2006-01-02", from)
	toDate, _ := time.Parse("2006-01-02", to)

	var allHeaders []string
	var allRows [][]string

	for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		headers, rows := a.runReportForExport(screenID, date, shiftFilter, shapeFilter, reportType)
		allHeaders = headers
		// Prepend date column to each row
		for _, row := range rows {
			allRows = append(allRows, append([]string{date}, row...))
		}
	}

	if allHeaders != nil {
		allHeaders = append([]string{"count_date"}, allHeaders...)
	}
	return allHeaders, allRows
}

func (a *API) runReportForExport(screenID, date, shiftFilter, shapeFilter, reportType string) ([]string, [][]string) {
	switch reportType {
	case "downtime", "operator_entry", "operator_overcycle", "machine_overcycle", "starved_blocked", "red_rabbit", "tool_change":
		ops := a.collectDurationReport(screenID, date, shiftFilter, shapeFilter, reportType)
		return flattenDurationReport(ops)

	case "oee":
		ops := a.collectOEEReport(screenID, date, shiftFilter, shapeFilter)
		return flattenOEEReport(ops)

	case "andon_response":
		ops := a.collectAndonReport(screenID, date, shiftFilter, shapeFilter)
		return flattenAndonReport(ops)

	case "production_trend":
		resp := a.collectProductionTrend(screenID, shapeFilter, "30", "")
		return flattenTrendReport(resp)

	default:
		return []string{"error"}, [][]string{{"unsupported report type: " + reportType}}
	}
}

// Report collectors that return data without writing HTTP responses.

func (a *API) collectDurationReport(screenID, date, shiftFilter, shapeFilter, reportType string) []reportOperation {
	var ops []reportOperation

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {
			var dr durationResult
			switch reportType {
			case "downtime":
				dr = walkDowntime(rows, sess, start, end, seed)
			case "operator_entry":
				dr = walkPairedIntervals(rows, BitClearToEnter, BitLCBroken, 0, nil, sess, start, end, seed, seedStyle)
			case "operator_overcycle":
				sc, ok := a.Store.GetScreen(screenID)
				manTime := 0.0
				var resolver targetResolver
				if ok {
					cfg := sc.CellConfig[si.ID]
					_, manTime, _ = effectiveTimes(cfg)
					resolver = buildTargetResolver(cfg, si.LayoutStyles)
				}
				dr = walkPairedIntervals(rows, BitClearToEnter, BitInCycle, manTime, resolver, sess, start, end, seed, seedStyle)
			case "machine_overcycle":
				sc, ok := a.Store.GetScreen(screenID)
				machineTime := 0.0
				var resolver targetResolver
				if ok {
					cfg := sc.CellConfig[si.ID]
					_, _, machineTime = effectiveTimes(cfg)
					resolver = buildTargetResolver(cfg, si.LayoutStyles)
				}
				dr = walkMachineOvercycle(rows, machineTime, resolver, sess, start, end, seed, seedStyle)
			case "starved_blocked":
				sc, ok := a.Store.GetScreen(screenID)
				machineTime := 0.0
				var resolver targetResolver
				if ok {
					cfg := sc.CellConfig[si.ID]
					_, _, machineTime = effectiveTimes(cfg)
					resolver = buildTargetResolver(cfg, si.LayoutStyles)
				}
				dr = walkStarvedBlocked(rows, machineTime, resolver, sess, start, end, seed, seedStyle)
			case "red_rabbit":
				dr = walkDuration(rows, func(s int32) bool { return s&(1<<BitRedRabbit) != 0 }, sess, start, end, seed)
			case "tool_change":
				dr = walkDuration(rows, func(s int32) bool { return s&(1<<BitToolChangeActive) != 0 }, sess, start, end, seed)
			}

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}
			addShiftToOps(&ops, si, sr)
		})

	return ops
}

func (a *API) collectOEEReport(screenID, date, shiftFilter, shapeFilter string) []oeeOperation {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	var ops []oeeOperation
	shapeInfos := a.buildShapeInfos(screenID)
	settings := a.Store.GetSettings()

	ordered := orderedShapes(shapeInfos, shapeFilter)

	for _, si := range ordered {
		op := oeeOperation{ShapeID: si.ID, Name: si.Name}

		for _, shift := range settings.Shifts {
			if shiftFilter != "" && shift.Name != shiftFilter {
				continue
			}
			start, end, err := shiftTimeRange(date, shift)
			if err != nil {
				continue
			}
			sess := buildReportSession(shift)
			rows, seed, _, err := a.EventLog.queryStateRows(si.ID, start, end)
			if err != nil {
				continue
			}

			totalWorkSec := elapsedWorkSeconds(sess, start, end, lastEventTimestamp(rows))
			if totalWorkSec == 0 {
				totalWorkSec = 8 * 3600
			}
			dr := walkDowntime(rows, sess, start, end, seed)

			countRows, _ := a.EventLog.QueryHourlyCounts(screenID, date, si.ID, "")
			var actual, planned int
			for _, cr := range countRows {
				if cr.ShiftName == shift.Name {
					actual += cr.Delta
					planned += cr.Planned
				}
			}

			scrapDR := walkDuration(rows, func(s int32) bool { return s&(1<<BitPartKicked) != 0 }, sess, start, end, seed)
			scrapped := scrapDR.RisingEdges
			avail, perf, qual, oee := computeOEEValues(totalWorkSec, dr.TotalSeconds, actual, planned, scrapped)

			op.Shifts = append(op.Shifts, oeeShiftResult{
				Name:            shift.Name,
				TimeRange:       shift.Start + " - " + shift.End,
				Availability:    avail,
				Performance:     perf,
				Quality:         qual,
				OEE:             oee,
				DowntimeSeconds: dr.TotalSeconds,
				Actual:          actual,
				Planned:         planned,
				Scrapped:        scrapped,
			})
		}
		ops = append(ops, op)
	}
	return ops
}

func (a *API) collectAndonReport(screenID, date, shiftFilter, shapeFilter string) []andonOperation {
	andonBits := make([]int, len(AndonDefs))
	for i, ad := range AndonDefs {
		andonBits[i] = ad.Bit
	}

	var ops []andonOperation

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {
			results := walkMultiDuration(rows, andonBits, sess, start, end, seed)
			var andons []andonEntry
			var total float64
			for i, dr := range results {
				if dr.TotalSeconds > 0 {
					andons = append(andons, andonEntry{
						Type:    AndonDefs[i].Type,
						Label:   AndonDefs[i].Label,
						Seconds: dr.TotalSeconds,
					})
					total += dr.TotalSeconds
				}
			}
			sr := andonShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Andons:       andons,
				TotalSeconds: total,
			}
			addAndonShiftToOps(&ops, si, sr)
		})

	return ops
}

func (a *API) collectProductionTrend(screenID, shapeFilter, daysStr, styleFilter string) trendReportResponse {
	days := parseDays(daysStr)
	settings := a.Store.GetSettings()
	today := time.Now()

	var labels []string
	dataMap := make(map[string][]float64)
	for i := days - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		date := d.Format("2006-01-02")
		labels = append(labels, d.Format("Jan 2"))

		shapeInfos := a.buildShapeInfos(screenID)
		for _, shift := range settings.Shifts {
			for _, si := range shapeInfos {
				if shapeFilter != "" && si.ID != shapeFilter {
					continue
				}
				countRows, _ := a.EventLog.QueryHourlyCounts(screenID, date, si.ID, "")
				byStyle := make(map[string]int)
				for _, cr := range countRows {
					if cr.ShiftName != shift.Name {
						continue
					}
					sn := si.StyleNames[cr.JobStyle]
					if sn == "" {
						sn = "Default"
					}
					if styleFilter != "" && sn != styleFilter && cr.JobStyle != styleFilter {
						continue
					}
					byStyle[sn] += cr.Delta
				}
				for sn, actual := range byStyle {
					key := shift.Name + "|" + sn
					if _, ok := dataMap[key]; !ok {
						dataMap[key] = make([]float64, len(labels)-1)
					}
					for len(dataMap[key]) < len(labels)-1 {
						dataMap[key] = append(dataMap[key], 0)
					}
					dataMap[key] = append(dataMap[key], float64(actual))
				}
			}
		}
		// Pad all datasets to current length
		for k := range dataMap {
			for len(dataMap[k]) < len(labels) {
				dataMap[k] = append(dataMap[k], 0)
			}
		}
	}

	var datasets []trendDataset
	for key, data := range dataMap {
		parts := splitFirst(key, "|")
		datasets = append(datasets, trendDataset{Shift: parts[0], Style: parts[1], Data: data})
	}

	return trendReportResponse{ReportType: "production_trend", Labels: labels, Datasets: datasets}
}

