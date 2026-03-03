// api_reports.go — GET /api/reports endpoint with daily and trend report handlers.
package server

import (
	"fmt"
	"net/http"
	"slices"
	"time"
)

// GetReports dispatches to the appropriate report handler.
func (a *API) GetReports(w http.ResponseWriter, r *http.Request) {
	reportType := r.URL.Query().Get("report_type")
	screenID := r.URL.Query().Get("screen")
	date := r.URL.Query().Get("date")
	shift := r.URL.Query().Get("shift")
	shape := r.URL.Query().Get("shape")
	days := r.URL.Query().Get("days")
	style := r.URL.Query().Get("style")

	if screenID == "" {
		writeError(w, http.StatusBadRequest, "screen parameter required")
		return
	}
	if a.EventLog == nil {
		writeError(w, http.StatusServiceUnavailable, "event log not configured")
		return
	}

	switch reportType {
	case "downtime":
		a.reportDowntime(w, screenID, date, shift, shape)
	case "operator_entry":
		a.reportOperatorEntry(w, screenID, date, shift, shape)
	case "operator_overcycle":
		a.reportOperatorOvercycle(w, screenID, date, shift, shape)
	case "machine_overcycle":
		a.reportMachineOvercycle(w, screenID, date, shift, shape)
	case "starved_blocked":
		a.reportStarvedBlocked(w, screenID, date, shift, shape)
	case "red_rabbit":
		a.reportRedRabbit(w, screenID, date, shift, shape)
	case "andon_response":
		a.reportAndonResponse(w, screenID, date, shift, shape)
	case "tool_change":
		a.reportToolChange(w, screenID, date, shift, shape)
	case "oee":
		a.reportOEE(w, screenID, date, shift, shape)
	case "production_trend":
		a.reportProductionTrend(w, screenID, shape, days, style)
	case "tool_change_trend":
		a.reportToolChangeTrend(w, screenID, shape, days)
	case "fault_trend":
		a.reportFaultTrend(w, screenID, shape, days)
	default:
		writeError(w, http.StatusBadRequest, "unknown report_type")
	}
}

// --- Report response types ---

type reportHourBucket struct {
	ShiftHour int     `json:"shift_hour"`
	Seconds   float64 `json:"seconds"`
	Count     int     `json:"count,omitempty"`
}

type reportShiftResult struct {
	Name         string             `json:"name"`
	TimeRange    string             `json:"time_range"`
	Hours        []reportHourBucket `json:"hours,omitempty"`
	TotalSeconds float64            `json:"total_seconds"`
	TotalCount   int                `json:"total_count,omitempty"`
}

type reportOperation struct {
	ShapeID string              `json:"shape_id"`
	Name    string              `json:"name"`
	Shifts  []reportShiftResult `json:"shifts"`
}

type reportHourColumn struct {
	ShiftHour int    `json:"shift_hour"`
	Index     int    `json:"index"`
	Label     string `json:"label"`
	StartMin  int    `json:"start_min"`
}

type hourlyReportResponse struct {
	ReportType string             `json:"report_type"`
	Operations []reportOperation  `json:"operations"`
	Hours      []reportHourColumn `json:"hours,omitempty"`
}

type andonEntry struct {
	Type    string  `json:"type"`
	Label   string  `json:"label"`
	Seconds float64 `json:"seconds"`
}

type andonShiftResult struct {
	Name         string       `json:"name"`
	TimeRange    string       `json:"time_range"`
	Andons       []andonEntry `json:"andons"`
	TotalSeconds float64      `json:"total_seconds"`
}

type andonOperation struct {
	ShapeID string             `json:"shape_id"`
	Name    string             `json:"name"`
	Shifts  []andonShiftResult `json:"shifts"`
}

type andonReportResponse struct {
	ReportType string           `json:"report_type"`
	Operations []andonOperation `json:"operations"`
}

type oeeShiftResult struct {
	Name             string  `json:"name"`
	TimeRange        string  `json:"time_range"`
	Availability     float64 `json:"availability"`
	Performance      float64 `json:"performance"`
	Quality          float64 `json:"quality"`
	OEE              float64 `json:"oee"`
	DowntimeSeconds  float64 `json:"downtime_seconds"`
	Actual           int     `json:"actual"`
	Planned          int     `json:"planned"`
	Scrapped         int     `json:"scrapped"`
}

type oeeOperation struct {
	ShapeID string           `json:"shape_id"`
	Name    string           `json:"name"`
	Shifts  []oeeShiftResult `json:"shifts"`
}

type oeeReportResponse struct {
	ReportType string         `json:"report_type"`
	Operations []oeeOperation `json:"operations"`
}

type trendDataset struct {
	Shift string    `json:"shift"`
	Style string    `json:"style,omitempty"`
	Data  []float64 `json:"data"`
}

type trendReportResponse struct {
	ReportType string         `json:"report_type"`
	Labels     []string       `json:"labels"`
	Datasets   []trendDataset `json:"datasets"`
}

// --- Report helpers ---

// effectiveTimes returns takt, manTime, machineTime with fallbacks matching datagen defaults.
func effectiveTimes(cfg CellParams) (takt, manTime, machineTime float64) {
	takt = cfg.TaktTime
	if takt <= 0 {
		takt = 30
	}
	manTime = cfg.ManTime
	if manTime <= 0 {
		manTime = takt * 0.4
	}
	machineTime = cfg.MachineTime
	if machineTime <= 0 {
		machineTime = takt * 0.5
	}
	return
}

func buildReportSession(shift Shift) *reportSession {
	startMin, _ := parseHHMM(shift.Start)
	endMin, _ := parseHHMM(shift.End)
	return &reportSession{
		shiftStart: startMin,
		overnight:  endMin <= startMin,
		minutes:    shift.Minutes,
	}
}

// addShiftToOps appends a shift result to the matching operation, or creates a new one.
func addShiftToOps(ops *[]reportOperation, si *shapeLayoutInfo, sr reportShiftResult) {
	for i := range *ops {
		if (*ops)[i].ShapeID == si.ID {
			(*ops)[i].Shifts = append((*ops)[i].Shifts, sr)
			return
		}
	}
	*ops = append(*ops, reportOperation{ShapeID: si.ID, Name: si.Name, Shifts: []reportShiftResult{sr}})
}

// addAndonShiftToOps appends an andon shift result to the matching operation, or creates a new one.
func addAndonShiftToOps(ops *[]andonOperation, si *shapeLayoutInfo, sr andonShiftResult) {
	for i := range *ops {
		if (*ops)[i].ShapeID == si.ID {
			(*ops)[i].Shifts = append((*ops)[i].Shifts, sr)
			return
		}
	}
	*ops = append(*ops, andonOperation{ShapeID: si.ID, Name: si.Name, Shifts: []andonShiftResult{sr}})
}

// buildHourColumns builds hour column headers for a shift.
func buildHourColumns(shift Shift) []reportHourColumn {
	startMin, _ := parseHHMM(shift.Start)
	endMin, _ := parseHHMM(shift.End)
	duration := endMin - startMin
	if duration <= 0 {
		duration += 1440
	}
	n := duration / 60
	if duration%60 > 0 {
		n++
	}
	if n < 8 {
		n = 8
	}
	cols := make([]reportHourColumn, n)
	for i := 0; i < n; i++ {
		sm := (startMin + i*60) % 1440
		cols[i] = reportHourColumn{
			ShiftHour: i + 1,
			Index:     i + 1,
			Label:     fmt.Sprintf("%d:%02d", sm/60, sm%60),
			StartMin:  sm,
		}
	}
	return cols
}

// resultToHourBuckets converts a durationResult to sorted hour buckets.
func resultToHourBuckets(dr durationResult) []reportHourBucket {
	var buckets []reportHourBucket
	for h, secs := range dr.ByHour {
		buckets = append(buckets, reportHourBucket{
			ShiftHour: h,
			Seconds:   secs,
			Count:     dr.EdgesByHour[h],
		})
	}
	slices.SortFunc(buckets, func(a, b reportHourBucket) int { return a.ShiftHour - b.ShiftHour })
	return buckets
}

// iterateShapesShifts calls fn for each shape × shift matching the filters.
func (a *API) iterateShapesShifts(screenID, date, shiftFilter, shapeFilter string,
	fn func(si *shapeLayoutInfo, shift Shift, sess *reportSession, shiftStart, shiftEnd time.Time, rows []stateRow, seed int32, seedStyle int32)) {

	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	shapeInfos := a.buildShapeInfos(screenID)
	settings := a.Store.GetSettings()

	ordered := orderedShapes(shapeInfos, shapeFilter)

	for _, si := range ordered {
		for _, shift := range settings.Shifts {
			if shiftFilter != "" && shift.Name != shiftFilter {
				continue
			}
			start, end, err := shiftTimeRange(date, shift)
			if err != nil {
				continue
			}
			sess := buildReportSession(shift)
			rows, seed, seedStyle, err := a.EventLog.queryStateRows(si.ID, start, end)
			if err != nil {
				continue
			}
			fn(si, shift, sess, start, end, rows, seed, seedStyle)
		}
	}
}

// --- Daily Report Implementations ---

func (a *API) reportDowntime(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation
	var hourCols []reportHourColumn

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {
			dr := walkDowntime(rows, sess, start, end, seed)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}

			addShiftToOps(&ops, si, sr)

			if len(hourCols) == 0 {
				hourCols = buildHourColumns(shift)
			}
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "downtime",
		Operations: ops,
		Hours:      hourCols,
	})
}

func (a *API) reportOperatorEntry(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation
	var hourCols []reportHourColumn

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			// clearToEnter(bit6) rising → lcBroken(bit7) rising, no subtract
			dr := walkPairedIntervals(rows, BitClearToEnter, BitLCBroken, 0, nil, sess, start, end, seed, seedStyle)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}

			addShiftToOps(&ops, si, sr)
			if len(hourCols) == 0 {
				hourCols = buildHourColumns(shift)
			}
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "operator_entry",
		Operations: ops,
		Hours:      hourCols,
	})
}

func (a *API) reportOperatorOvercycle(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation
	var hourCols []reportHourColumn

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			sc, ok := a.Store.GetScreen(screenID)
			manTime := 0.0
			var resolver targetResolver
			if ok {
				cfg := sc.CellConfig[si.ID]
				_, manTime, _ = effectiveTimes(cfg)
				resolver = buildTargetResolver(cfg, si.LayoutStyles)
			}
			// clearToEnter(bit6) rising → inCycle(bit4) rising, subtract ManTime
			dr := walkPairedIntervals(rows, BitClearToEnter, BitInCycle, manTime, resolver, sess, start, end, seed, seedStyle)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}

			addShiftToOps(&ops, si, sr)
			if len(hourCols) == 0 {
				hourCols = buildHourColumns(shift)
			}
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "operator_overcycle",
		Operations: ops,
		Hours:      hourCols,
	})
}

func (a *API) reportMachineOvercycle(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation
	var hourCols []reportHourColumn

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			sc, ok := a.Store.GetScreen(screenID)
			machineTime := 0.0
			var resolver targetResolver
			if ok {
				cfg := sc.CellConfig[si.ID]
				_, _, machineTime = effectiveTimes(cfg)
				resolver = buildTargetResolver(cfg, si.LayoutStyles)
			}
			dr := walkMachineOvercycle(rows, machineTime, resolver, sess, start, end, seed, seedStyle)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}

			addShiftToOps(&ops, si, sr)
			if len(hourCols) == 0 {
				hourCols = buildHourColumns(shift)
			}
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "machine_overcycle",
		Operations: ops,
		Hours:      hourCols,
	})
}

func (a *API) reportStarvedBlocked(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation
	var hourCols []reportHourColumn

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			sc, ok := a.Store.GetScreen(screenID)
			machineTime := 0.0
			var resolver targetResolver
			if ok {
				cfg := sc.CellConfig[si.ID]
				_, _, machineTime = effectiveTimes(cfg)
				resolver = buildTargetResolver(cfg, si.LayoutStyles)
			}
			dr := walkStarvedBlocked(rows, machineTime, resolver, sess, start, end, seed, seedStyle)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				Hours:        resultToHourBuckets(dr),
				TotalSeconds: dr.TotalSeconds,
			}

			addShiftToOps(&ops, si, sr)
			if len(hourCols) == 0 {
				hourCols = buildHourColumns(shift)
			}
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "starved_blocked",
		Operations: ops,
		Hours:      hourCols,
	})
}

func (a *API) reportRedRabbit(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			dr := walkDuration(rows, func(s int32) bool { return s&(1<<BitRedRabbit) != 0 }, sess, start, end, seed)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				TotalSeconds: dr.TotalSeconds,
			}

			addShiftToOps(&ops, si, sr)
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "red_rabbit",
		Operations: ops,
	})
}

func (a *API) reportAndonResponse(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	// Delegate to the shared collect function used by both API and export.
	ops := a.collectAndonReport(screenID, date, shiftFilter, shapeFilter)
	writeJSON(w, http.StatusOK, andonReportResponse{
		ReportType: "andon_response",
		Operations: ops,
	})
}

func (a *API) reportToolChange(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	var ops []reportOperation

	a.iterateShapesShifts(screenID, date, shiftFilter, shapeFilter,
		func(si *shapeLayoutInfo, shift Shift, sess *reportSession, start, end time.Time, rows []stateRow, seed int32, seedStyle int32) {

			dr := walkDuration(rows, func(s int32) bool { return s&(1<<BitToolChangeActive) != 0 }, sess, start, end, seed)

			sr := reportShiftResult{
				Name:         shift.Name,
				TimeRange:    shift.Start + " - " + shift.End,
				TotalSeconds: dr.TotalSeconds,
				TotalCount:   dr.RisingEdges,
			}

			addShiftToOps(&ops, si, sr)
		})

	writeJSON(w, http.StatusOK, hourlyReportResponse{
		ReportType: "tool_change",
		Operations: ops,
	})
}

// computeOEEValues calculates availability, performance, quality, and OEE from raw metrics.
func computeOEEValues(totalWorkSec, downtimeSec float64, actual, planned, scrapped int) (avail, perf, qual, oee float64) {
	if totalWorkSec > 0 {
		avail = (totalWorkSec - downtimeSec) / totalWorkSec
		if avail < 0 {
			avail = 0
		}
	}
	if planned > 0 {
		perf = float64(actual) / float64(planned)
	}
	qual = 1.0
	if actual > 0 {
		qual = float64(actual-scrapped) / float64(actual)
		if qual < 0 {
			qual = 0
		}
	} else {
		perf = 0
	}
	oee = avail * perf * qual
	return
}

func (a *API) reportOEE(w http.ResponseWriter, screenID, date, shiftFilter, shapeFilter string) {
	// Delegate to the shared collect function used by both API and export.
	ops := a.collectOEEReport(screenID, date, shiftFilter, shapeFilter)
	writeJSON(w, http.StatusOK, oeeReportResponse{
		ReportType: "oee",
		Operations: ops,
	})
}

// --- Trend Reports ---

func (a *API) reportProductionTrend(w http.ResponseWriter, screenID, shapeFilter, daysStr, styleFilter string) {
	// Delegate to the shared collect function used by both API and export.
	resp := a.collectProductionTrend(screenID, shapeFilter, daysStr, styleFilter)
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) reportToolChangeTrend(w http.ResponseWriter, screenID, shapeFilter, daysStr string) {
	a.reportDurationTrend(w, "tool_change_trend", screenID, shapeFilter, daysStr,
		func(s int32) bool { return s&(1<<BitToolChangeActive) != 0 })
}

func (a *API) reportFaultTrend(w http.ResponseWriter, screenID, shapeFilter, daysStr string) {
	a.reportDurationTrend(w, "fault_trend", screenID, shapeFilter, daysStr,
		func(s int32) bool { return (s&(1<<BitFaulted) != 0) || (s&(1<<BitEStop) != 0) })
}

// reportDurationTrend is the shared implementation for duration-based trend reports.
func (a *API) reportDurationTrend(w http.ResponseWriter, reportType, screenID, shapeFilter, daysStr string, cond func(int32) bool) {
	days := parseDays(daysStr)
	settings := a.Store.GetSettings()
	today := time.Now()

	shapeInfos := a.buildShapeInfos(screenID)
	var shapes []*shapeLayoutInfo
	for _, si := range shapeInfos {
		if shapeFilter != "" && si.ID != shapeFilter {
			continue
		}
		shapes = append(shapes, si)
	}

	var labels []string
	dataMap := make(map[string][]float64) // shiftName -> data

	for d := days - 1; d >= 0; d-- {
		date := today.AddDate(0, 0, -d).Format("2006-01-02")
		labels = append(labels, date)
		idx := len(labels) - 1

		for _, shift := range settings.Shifts {
			key := shift.Name
			if _, ok := dataMap[key]; !ok {
				dataMap[key] = make([]float64, len(labels))
			}
			if len(dataMap[key]) < len(labels) {
				extended := make([]float64, len(labels))
				copy(extended, dataMap[key])
				dataMap[key] = extended
			}

			for _, si := range shapes {
				start, end, err := shiftTimeRange(date, shift)
				if err != nil {
					continue
				}
				sess := buildReportSession(shift)
				rows, seed, _, err := a.EventLog.queryStateRows(si.ID, start, end)
				if err != nil {
					continue
				}
				dr := walkDuration(rows, cond, sess, start, end, seed)
				dataMap[key][idx] += dr.TotalSeconds
			}
		}
	}

	var datasets []trendDataset
	for shiftName, data := range dataMap {
		datasets = append(datasets, trendDataset{Shift: shiftName, Data: data})
	}

	writeJSON(w, http.StatusOK, trendReportResponse{
		ReportType: reportType,
		Labels:     labels,
		Datasets:   datasets,
	})
}

// --- Report utility functions ---

// parseDays converts a days string to an int, defaulting to 30.
func parseDays(s string) int {
	if s == "" || s == "all" {
		return 365
	}
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	if n <= 0 {
		return 30
	}
	return n
}

// splitFirst splits s on the first occurrence of sep.
func splitFirst(s, sep string) [2]string {
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return [2]string{s[:i], s[i+len(sep):]}
		}
	}
	return [2]string{s, ""}
}
