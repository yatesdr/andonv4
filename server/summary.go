// summary.go — Shift summary computation and database persistence.
package server

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ShiftSummaryRow represents one row of the shift_summary table.
type ShiftSummaryRow struct {
	ID                       int64   `json:"-"`
	ScreenID                 string  `json:"screen_id"`
	ShapeID                  string  `json:"shape_id"`
	ShapeName                string  `json:"shape_name"`
	ShapeType                string  `json:"shape_type"`
	ShiftName                string  `json:"shift_name"`
	JobStyle                 string  `json:"job_style"`
	StyleName                string  `json:"style_name"`
	CountDate                string  `json:"count_date"`
	Actual                   int     `json:"actual"`
	Planned                  int     `json:"planned"`
	Early                    int     `json:"early"`
	Overtime                 int     `json:"overtime"`
	Availability             float64 `json:"availability"`
	Performance              float64 `json:"performance"`
	Quality                  float64 `json:"quality"`
	OEE                      float64 `json:"oee"`
	DowntimeSeconds          float64 `json:"downtime_seconds"`
	DowntimeCount            int     `json:"downtime_count"`
	OperatorEntrySeconds     float64 `json:"operator_entry_seconds"`
	OperatorOvercycleSeconds float64 `json:"operator_overcycle_seconds"`
	MachineOvercycleSeconds  float64 `json:"machine_overcycle_seconds"`
	StarvedBlockedSeconds    float64 `json:"starved_blocked_seconds"`
	ToolChangeSeconds        float64 `json:"tool_change_seconds"`
	ToolChangeCount          int     `json:"tool_change_count"`
	RedRabbitSeconds         float64 `json:"red_rabbit_seconds"`
	ScrapCount               int     `json:"scrap_count"`
	AndonProdSeconds         float64 `json:"andon_prod_seconds"`
	AndonMaintSeconds        float64 `json:"andon_maint_seconds"`
	AndonLogisticsSeconds    float64 `json:"andon_logistics_seconds"`
	AndonQualitySeconds      float64 `json:"andon_quality_seconds"`
	AndonHRSeconds           float64 `json:"andon_hr_seconds"`
	AndonEmergencySeconds    float64 `json:"andon_emergency_seconds"`
	AndonToolingSeconds      float64 `json:"andon_tooling_seconds"`
	AndonEngineeringSeconds  float64 `json:"andon_engineering_seconds"`
	AndonControlsSeconds     float64 `json:"andon_controls_seconds"`
	AndonITSeconds           float64 `json:"andon_it_seconds"`
	TotalWorkSeconds         float64 `json:"total_work_seconds"`
	StyleMinutes             int     `json:"style_minutes"`
	ComputedAt               string  `json:"computed_at"`
}

// computeShiftSummary runs all walkers once per shape, returns summary rows (one per style).
func (el *EventLogger) computeShiftSummary(screenID, date, shiftName string,
	shift Shift, shapeInfos map[string]*shapeLayoutInfo) ([]ShiftSummaryRow, error) {

	var rows []ShiftSummaryRow

	shiftStart, shiftEnd, err := shiftTimeRange(date, shift)
	if err != nil {
		return nil, fmt.Errorf("shiftTimeRange: %w", err)
	}
	sess := buildReportSession(shift)

	now := time.Now().UTC().Format(time.RFC3339)

	for _, si := range shapeInfos {
		// Query state rows once per shape — feed to all walkers
		stateRows, seed, seedStyle, err := el.queryStateRows(si.ID, shiftStart, shiftEnd)
		if err != nil {
			log.Printf("summary: queryStateRows %s: %v", si.ID, err)
			continue
		}

		// Total work seconds for OEE — pro-rated for partial/in-progress shifts
		totalWorkSec := elapsedWorkSeconds(sess, shiftStart, shiftEnd, lastEventTimestamp(stateRows))
		if totalWorkSec == 0 {
			totalWorkSec = 8 * 3600
		}

		// Get cell config for targets with style-aware resolver
		sc, ok := el.store.GetScreen(screenID)
		manTime := 0.0
		machineTime := 0.0
		var resolver targetResolver
		if ok {
			cfg := sc.CellConfig[si.ID]
			_, manTime, machineTime = effectiveTimes(cfg)
			resolver = buildTargetResolver(cfg, si.LayoutStyles)
		}

		// --- Run all walkers ---
		downtimeDR := walkDowntime(stateRows, sess, shiftStart, shiftEnd, seed)
		operatorEntryDR := walkPairedIntervals(stateRows, BitClearToEnter, BitLCBroken, 0, nil, sess, shiftStart, shiftEnd, seed, seedStyle)
		operatorOvercycleDR := walkPairedIntervals(stateRows, BitClearToEnter, BitInCycle, manTime, resolver, sess, shiftStart, shiftEnd, seed, seedStyle)
		machineOvercycleDR := walkMachineOvercycle(stateRows, machineTime, resolver, sess, shiftStart, shiftEnd, seed, seedStyle)
		starvedBlockedDR := walkStarvedBlocked(stateRows, machineTime, resolver, sess, shiftStart, shiftEnd, seed, seedStyle)
		toolChangeDR := walkDuration(stateRows, func(s int32) bool { return s&(1<<BitToolChangeActive) != 0 }, sess, shiftStart, shiftEnd, seed)
		redRabbitDR := walkDuration(stateRows, func(s int32) bool { return s&(1<<BitRedRabbit) != 0 }, sess, shiftStart, shiftEnd, seed)
		scrapDR := walkDuration(stateRows, func(s int32) bool { return s&(1<<BitPartKicked) != 0 }, sess, shiftStart, shiftEnd, seed)

		// Andon walkers — iterate the canonical AndonDefs from bits.go
		andonBits := make([]int, len(AndonDefs))
		for i, ad := range AndonDefs {
			andonBits[i] = ad.Bit
		}
		andonResults := walkMultiDuration(stateRows, andonBits, sess, shiftStart, shiftEnd, seed)

		// --- Query hourly counts (grouped by style) ---
		countRows, _ := el.QueryHourlyCounts(screenID, date, si.ID, "")

		// Group counts by style
		type styleAgg struct {
			actual       int
			planned      int
			early        int
			overtime     int
			styleMinutes int
			styleName    string
		}
		styleMap := make(map[string]*styleAgg)
		for _, cr := range countRows {
			if cr.ShiftName != shiftName {
				continue
			}
			key := cr.JobStyle
			agg, ok := styleMap[key]
			if !ok {
				sn := si.StyleNames[key]
				if sn == "" && key != "" {
					sn = "Style" + key
				}
				agg = &styleAgg{styleName: sn}
				styleMap[key] = agg
			}
			agg.actual += cr.Delta
			agg.planned += cr.Planned
			agg.styleMinutes += cr.StyleMinutes
			if cr.IsEarly {
				agg.early += cr.Delta
			}
			if cr.IsOvertime {
				agg.overtime += cr.Delta
			}
		}

		// If no count rows for this shift, still produce one row with empty style
		if len(styleMap) == 0 {
			styleMap[""] = &styleAgg{}
		}

		for jobStyle, agg := range styleMap {
			avail, perf, qual, oee := computeOEEValues(totalWorkSec, downtimeDR.TotalSeconds, agg.actual, agg.planned, scrapDR.RisingEdges)

			row := ShiftSummaryRow{
				ScreenID:                 screenID,
				ShapeID:                  si.ID,
				ShapeName:                si.Name,
				ShapeType:                si.Type,
				ShiftName:                shiftName,
				JobStyle:                 jobStyle,
				StyleName:                agg.styleName,
				CountDate:                date,
				Actual:                   agg.actual,
				Planned:                  agg.planned,
				Early:                    agg.early,
				Overtime:                 agg.overtime,
				Availability:             avail,
				Performance:              perf,
				Quality:                  qual,
				OEE:                      oee,
				DowntimeSeconds:          downtimeDR.TotalSeconds,
				DowntimeCount:            downtimeDR.RisingEdges,
				OperatorEntrySeconds:     operatorEntryDR.TotalSeconds,
				OperatorOvercycleSeconds: operatorOvercycleDR.TotalSeconds,
				MachineOvercycleSeconds:  machineOvercycleDR.TotalSeconds,
				StarvedBlockedSeconds:    starvedBlockedDR.TotalSeconds,
				ToolChangeSeconds:        toolChangeDR.TotalSeconds,
				ToolChangeCount:          toolChangeDR.RisingEdges,
				RedRabbitSeconds:         redRabbitDR.TotalSeconds,
				ScrapCount:               scrapDR.RisingEdges,
				AndonProdSeconds:         andonResults[0].TotalSeconds,
				AndonMaintSeconds:        andonResults[1].TotalSeconds,
				AndonLogisticsSeconds:    andonResults[2].TotalSeconds,
				AndonQualitySeconds:      andonResults[3].TotalSeconds,
				AndonHRSeconds:           andonResults[4].TotalSeconds,
				AndonEmergencySeconds:    andonResults[5].TotalSeconds,
				AndonToolingSeconds:      andonResults[6].TotalSeconds,
				AndonEngineeringSeconds:  andonResults[7].TotalSeconds,
				AndonControlsSeconds:     andonResults[8].TotalSeconds,
				AndonITSeconds:           andonResults[9].TotalSeconds,
				TotalWorkSeconds:         totalWorkSec,
				StyleMinutes:             agg.styleMinutes,
				ComputedAt:               now,
			}
			rows = append(rows, row)
		}
	}

	return rows, nil
}

// PopulateShiftSummary computes and UPSERTs summary for one screen+date+shift.
func (el *EventLogger) PopulateShiftSummary(screenID, date, shiftName string) error {
	settings := el.store.GetSettings()
	var shift *Shift
	for i := range settings.Shifts {
		if settings.Shifts[i].Name == shiftName {
			shift = &settings.Shifts[i]
			break
		}
	}
	if shift == nil {
		return fmt.Errorf("shift %q not found", shiftName)
	}

	shapeInfos := buildShapeInfosFromStore(el.store, screenID)
	rows, err := el.computeShiftSummary(screenID, date, shiftName, *shift, shapeInfos)
	if err != nil {
		return err
	}

	return el.upsertSummaryRows(rows)
}

// upsertSummaryRows inserts or replaces summary rows in a single transaction.
func (el *EventLogger) upsertSummaryRows(rows []ShiftSummaryRow) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := el.db.Begin()
	if err != nil {
		return fmt.Errorf("summary: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO shift_summary (
		screen_id, shape_id, shape_name, shape_type, shift_name, job_style, style_name, count_date,
		actual, planned, early, overtime,
		availability, performance, quality, oee,
		downtime_seconds, downtime_count,
		operator_entry_seconds, operator_overcycle_seconds,
		machine_overcycle_seconds, starved_blocked_seconds,
		tool_change_seconds, tool_change_count,
		red_rabbit_seconds, scrap_count,
		andon_prod_seconds, andon_maint_seconds, andon_logistics_seconds,
		andon_quality_seconds, andon_hr_seconds, andon_emergency_seconds,
		andon_tooling_seconds, andon_engineering_seconds,
		andon_controls_seconds, andon_it_seconds,
		total_work_seconds, style_minutes, computed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(screen_id, shape_id, shift_name, job_style, count_date) DO UPDATE SET
		shape_name=excluded.shape_name, shape_type=excluded.shape_type, style_name=excluded.style_name,
		actual=excluded.actual, planned=excluded.planned, early=excluded.early, overtime=excluded.overtime,
		availability=excluded.availability, performance=excluded.performance,
		quality=excluded.quality, oee=excluded.oee,
		downtime_seconds=excluded.downtime_seconds, downtime_count=excluded.downtime_count,
		operator_entry_seconds=excluded.operator_entry_seconds,
		operator_overcycle_seconds=excluded.operator_overcycle_seconds,
		machine_overcycle_seconds=excluded.machine_overcycle_seconds,
		starved_blocked_seconds=excluded.starved_blocked_seconds,
		tool_change_seconds=excluded.tool_change_seconds, tool_change_count=excluded.tool_change_count,
		red_rabbit_seconds=excluded.red_rabbit_seconds, scrap_count=excluded.scrap_count,
		andon_prod_seconds=excluded.andon_prod_seconds, andon_maint_seconds=excluded.andon_maint_seconds,
		andon_logistics_seconds=excluded.andon_logistics_seconds,
		andon_quality_seconds=excluded.andon_quality_seconds,
		andon_hr_seconds=excluded.andon_hr_seconds, andon_emergency_seconds=excluded.andon_emergency_seconds,
		andon_tooling_seconds=excluded.andon_tooling_seconds,
		andon_engineering_seconds=excluded.andon_engineering_seconds,
		andon_controls_seconds=excluded.andon_controls_seconds,
		andon_it_seconds=excluded.andon_it_seconds,
		total_work_seconds=excluded.total_work_seconds, style_minutes=excluded.style_minutes,
		computed_at=excluded.computed_at`)
	if err != nil {
		return fmt.Errorf("summary: prepare: %w", err)
	}
	defer stmt.Close()

	for _, r := range rows {
		_, err := stmt.Exec(
			r.ScreenID, r.ShapeID, r.ShapeName, r.ShapeType, r.ShiftName, r.JobStyle, r.StyleName, r.CountDate,
			r.Actual, r.Planned, r.Early, r.Overtime,
			r.Availability, r.Performance, r.Quality, r.OEE,
			r.DowntimeSeconds, r.DowntimeCount,
			r.OperatorEntrySeconds, r.OperatorOvercycleSeconds,
			r.MachineOvercycleSeconds, r.StarvedBlockedSeconds,
			r.ToolChangeSeconds, r.ToolChangeCount,
			r.RedRabbitSeconds, r.ScrapCount,
			r.AndonProdSeconds, r.AndonMaintSeconds, r.AndonLogisticsSeconds,
			r.AndonQualitySeconds, r.AndonHRSeconds, r.AndonEmergencySeconds,
			r.AndonToolingSeconds, r.AndonEngineeringSeconds,
			r.AndonControlsSeconds, r.AndonITSeconds,
			r.TotalWorkSeconds, r.StyleMinutes, r.ComputedAt,
		)
		if err != nil {
			return fmt.Errorf("summary: exec: %w", err)
		}
	}

	return tx.Commit()
}

// PopulateAllSummaries iterates a date range and all shifts for a screen.
func (el *EventLogger) PopulateAllSummaries(screenID, from, to string) (int, error) {
	settings := el.store.GetSettings()
	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil {
		return 0, fmt.Errorf("parse from: %w", err)
	}
	toDate, err := time.Parse("2006-01-02", to)
	if err != nil {
		return 0, fmt.Errorf("parse to: %w", err)
	}

	total := 0
	for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for _, shift := range settings.Shifts {
			if err := el.PopulateShiftSummary(screenID, date, shift.Name); err != nil {
				log.Printf("summary: populate %s %s %s: %v", screenID, date, shift.Name, err)
				continue
			}
			total++
		}
	}
	return total, nil
}

// QueryShiftSummary reads from shift_summary with optional filters.
func (el *EventLogger) QueryShiftSummary(screenID, from, to, shiftFilter, shapeFilter string) ([]ShiftSummaryRow, error) {
	q := `SELECT id, screen_id, shape_id, shape_name, shape_type, shift_name, job_style, style_name, count_date,
		actual, planned, early, overtime,
		availability, performance, quality, oee,
		downtime_seconds, downtime_count,
		operator_entry_seconds, operator_overcycle_seconds,
		machine_overcycle_seconds, starved_blocked_seconds,
		tool_change_seconds, tool_change_count,
		red_rabbit_seconds, scrap_count,
		andon_prod_seconds, andon_maint_seconds, andon_logistics_seconds,
		andon_quality_seconds, andon_hr_seconds, andon_emergency_seconds,
		andon_tooling_seconds, andon_engineering_seconds,
		andon_controls_seconds, andon_it_seconds,
		total_work_seconds, style_minutes, computed_at
		FROM shift_summary WHERE screen_id = ? AND count_date >= ? AND count_date <= ?`
	args := []any{screenID, from, to}

	if shiftFilter != "" {
		q += " AND shift_name = ?"
		args = append(args, shiftFilter)
	}
	if shapeFilter != "" {
		q += " AND shape_id = ?"
		args = append(args, shapeFilter)
	}
	q += " ORDER BY count_date, shape_name, shift_name, job_style"

	dbRows, err := el.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("QueryShiftSummary: %w", err)
	}
	defer dbRows.Close()

	var result []ShiftSummaryRow
	for dbRows.Next() {
		var r ShiftSummaryRow
		if err := dbRows.Scan(
			&r.ID, &r.ScreenID, &r.ShapeID, &r.ShapeName, &r.ShapeType, &r.ShiftName, &r.JobStyle, &r.StyleName, &r.CountDate,
			&r.Actual, &r.Planned, &r.Early, &r.Overtime,
			&r.Availability, &r.Performance, &r.Quality, &r.OEE,
			&r.DowntimeSeconds, &r.DowntimeCount,
			&r.OperatorEntrySeconds, &r.OperatorOvercycleSeconds,
			&r.MachineOvercycleSeconds, &r.StarvedBlockedSeconds,
			&r.ToolChangeSeconds, &r.ToolChangeCount,
			&r.RedRabbitSeconds, &r.ScrapCount,
			&r.AndonProdSeconds, &r.AndonMaintSeconds, &r.AndonLogisticsSeconds,
			&r.AndonQualitySeconds, &r.AndonHRSeconds, &r.AndonEmergencySeconds,
			&r.AndonToolingSeconds, &r.AndonEngineeringSeconds,
			&r.AndonControlsSeconds, &r.AndonITSeconds,
			&r.TotalWorkSeconds, &r.StyleMinutes, &r.ComputedAt,
		); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// summarySweep runs every 30 minutes and backfills missing shift_summary rows
// for completed shifts in the last 48 hours.
func (el *EventLogger) summarySweep(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			el.backfillMissingSummaries()
		}
	}
}

// backfillMissingSummaries checks last 48h of completed shifts for missing summaries.
func (el *EventLogger) backfillMissingSummaries() {
	settings := el.store.GetSettings()
	if len(settings.Shifts) == 0 {
		return
	}

	screens := el.store.ListScreens()
	now := time.Now()

	// Check shifts ending in the last 48 hours
	for h := 0; h < 48; h++ {
		checkTime := now.Add(-time.Duration(h) * time.Hour)
		for _, shift := range settings.Shifts {
			// Determine if this shift would have ended by now
			_, shiftEnd, err := shiftTimeRange(checkTime.Format("2006-01-02"), shift)
			if err != nil {
				continue
			}
			if shiftEnd.After(now) {
				continue // shift hasn't ended yet
			}
			date := checkTime.Format("2006-01-02")

			for _, sc := range screens {
				// Check if summary already exists
				var count int
				el.db.QueryRow(`SELECT COUNT(*) FROM shift_summary WHERE screen_id = ? AND count_date = ? AND shift_name = ?`,
					sc.ID, date, shift.Name).Scan(&count)
				if count > 0 {
					continue
				}

				if err := el.PopulateShiftSummary(sc.ID, date, shift.Name); err != nil {
					log.Printf("summary sweep: %s %s %s: %v", sc.ID, date, shift.Name, err)
				} else {
					log.Printf("summary sweep: backfilled %s %s %s", sc.Name, date, shift.Name)
				}
			}
		}
	}
}
