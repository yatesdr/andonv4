// eventlog_counts.go — Hourly count tracking, style-time accumulation, and planned recomputation.
// Part of the EventLogger subsystem. All functions that access el.mu document their locking expectations.

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// StartCountSession initializes count tracking for all shapes on a screen.
func (el *EventLogger) StartCountSession(screenID string) {
	settings := el.store.GetSettings()
	now := time.Now()
	shift := ResolveShift(settings.Shifts, now)

	var sess *countSession
	if shift != nil {
		startMin, _ := parseHHMM(shift.Start)
		endMin, _ := parseHHMM(shift.End)
		sess = &countSession{
			shiftName:  shift.Name,
			countDate:  countDateForShift(shift, now),
			shiftStart: startMin,
			shiftEnd:   endMin,
			overnight:  endMin < startMin,
			minutes:    shift.Minutes,
		}
	} else {
		// No matching shift — use activation time as shift start, open-ended
		nowMin := now.Hour()*60 + now.Minute()
		sess = &countSession{
			shiftName:  "Shift: None",
			countDate:  now.Format("2006-01-02"),
			shiftStart: nowMin,
			shiftEnd:   nowMin, // same = no early/overtime classification
		}
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	for shapeID, meta := range el.metadata {
		if meta.ScreenID != screenID {
			continue
		}
		cs := el.countStates[shapeID]
		if cs == nil {
			cs = &shapeCountState{}
			el.countStates[shapeID] = cs
		}
		cs.session = sess
		cs.initialized = false
		cs.currentStyle = ""

		// Initialize baseline from existing snapshot so the very first
		// increment after session start produces a delta (not swallowed as baseline).
		if snap, ok := el.cache[shapeID]; ok && snap.Count != 0 {
			switch meta.ShapeType {
			case "process":
				pc := DecodeProcessCount(snap.Count)
				cs.lastPartID = pc.PartID
				cs.lastCounter = pc.Counter
				cs.currentStyle = fmt.Sprintf("%d", pc.PartID)
				cs.initialized = true
			case "press":
				cs.lastCount = snap.Count
				if snap.Style != 0 {
					cs.currentStyle = fmt.Sprintf("%d", snap.Style)
				}
				cs.initialized = true
			}
		}

		// Initialize current style from style tag snapshot if available
		if snap, ok := el.cache[shapeID]; ok && snap.Style != 0 {
			cs.currentStyle = fmt.Sprintf("%d", snap.Style)
		}

		// Initialize behind state
		cellCfg, layoutStyles := el.getShapeConfig(screenID, shapeID)
		takt := cellCfg.TaktTime
		if cs.currentStyle != "" {
			if st := resolveStyleTakt(cs.currentStyle, cellCfg, layoutStyles); st > 0 {
				takt = st
			}
		}
		bs := &behindState{
			currentTakt:  takt,
			lastTickTime: now,
			breakUsedSec: make(map[int]int),
		}
		el.behindStates[shapeID] = bs
		el.recoverBehindState(bs, shapeID, sess)

		// firstHourMet (bit 2): default to 1, clear if hour 1 already passed and missed target
		snap := el.cache[shapeID]
		if snap == nil {
			snap = &shapeSnapshot{}
			el.cache[shapeID] = snap
		}
		nowMin := now.Hour()*60 + now.Minute()
		shiftHour := computeShiftHour(sess, nowMin)
		if shiftHour >= 2 {
			// Hour 1 already complete — check DB
			bs.firstHourChecked = true
			snap.ComputedState |= 1 << CsBitFirstHourComplete // set bit 3 — firstHourComplete
			if !el.checkFirstHourMet(shapeID, sess) {
				snap.ComputedState &^= 1 << CsBitFirstHourMet // clear bit 2
			} else {
				snap.ComputedState |= 1 << CsBitFirstHourMet
			}
		} else {
			snap.ComputedState &^= 1 << CsBitFirstHourComplete // clear bit 3 — still in hour 1
			snap.ComputedState |= 1 << CsBitFirstHourMet   // force bit 2 = 1 during hour 1
		}
	}

	log.Printf("eventlog: count session started for screen %s — shift=%s date=%s", screenID, sess.shiftName, sess.countDate)
}

// InitActiveSessions starts count sessions for all screens that are currently active.
// Called once at boot after the Hub and EventLogger are wired together.
func (el *EventLogger) InitActiveSessions() {
	screens := el.store.ListScreens()
	for _, sc := range screens {
		if el.hub.IsScreenActive(sc.ID) {
			el.StartCountSession(sc.ID)
		}
	}
}

// StopCountSession clears count tracking for all shapes on a screen.
func (el *EventLogger) StopCountSession(screenID string) {
	el.mu.Lock()
	defer el.mu.Unlock()

	for shapeID, meta := range el.metadata {
		if meta.ScreenID != screenID {
			continue
		}
		if cs, ok := el.countStates[shapeID]; ok {
			cs.session = nil
		}
		delete(el.behindStates, shapeID)
	}

	log.Printf("eventlog: count session stopped for screen %s", screenID)
}

// recoverBehindState populates a behindState from DB data on server restart / session start.
// Called with el.mu held.
func (el *EventLogger) recoverBehindState(bs *behindState, shapeID string, sess *countSession) {
	// Recover actual parts from DB
	var actualParts int
	err := el.db.QueryRow(`SELECT COALESCE(SUM(delta), 0) FROM hourly_counts
		WHERE shape_id=? AND shift_name=? AND count_date=?`,
		shapeID, sess.shiftName, sess.countDate).Scan(&actualParts)
	if err != nil {
		return
	}
	bs.actualParts = actualParts

	// Recover break_used_sec per hour from DB
	rows, err := el.db.Query(`SELECT shift_hour, COALESCE(SUM(break_minutes), 0)
		FROM hourly_counts WHERE shape_id=? AND shift_name=? AND count_date=?
		GROUP BY shift_hour`, shapeID, sess.shiftName, sess.countDate)
	if err == nil {
		for rows.Next() {
			var hour, breakMin int
			if rows.Scan(&hour, &breakMin) == nil && breakMin > 0 {
				bs.breakUsedSec[hour] = breakMin * 60
			}
		}
		rows.Close()
	}

	// Reconstruct expectedParts from elapsed shift time minus consumed break allowance.
	// Uses default takt — self-corrects within seconds as the ticker runs.
	if bs.currentTakt <= 0 {
		return
	}
	now := time.Now()
	nowMin := now.Hour()*60 + now.Minute()
	currentShiftHour := computeShiftHour(sess, nowMin)

	var totalWorkSec float64
	for h := 1; h <= currentShiftHour; h++ {
		isLast := h == currentShiftHour
		workMin := 60
		idx := h - 1
		if idx >= 0 && idx < len(sess.minutes) {
			workMin = sess.minutes[idx]
		}
		allowanceSec := (60 - workMin) * 60

		var hourSec float64
		if isLast {
			// Partial hour: compute elapsed seconds within this hour
			hourStartMin := sess.shiftStart + (h-1)*60
			if hourStartMin >= 1440 {
				hourStartMin -= 1440
			}
			elapsed := nowMin - hourStartMin
			if elapsed < 0 {
				elapsed += 1440
			}
			hourSec = float64(elapsed * 60)
		} else {
			hourSec = 3600
		}

		// Subtract consumed break allowance for this hour
		consumed := bs.breakUsedSec[h]
		if consumed > allowanceSec {
			consumed = allowanceSec
		}
		productiveSec := hourSec - float64(consumed)
		if productiveSec < 0 {
			productiveSec = 0
		}
		totalWorkSec += productiveSec
	}

	bs.expectedParts = totalWorkSec / bs.currentTakt
}

// checkFirstHourMet queries the DB to see if hour-1 actual >= planned.
// Only returns true when there is real production that met a real plan.
// Called with el.mu held.
func (el *EventLogger) checkFirstHourMet(shapeID string, sess *countSession) bool {
	var actual, planned int
	err := el.db.QueryRow(`SELECT COALESCE(SUM(delta), 0), COALESCE(SUM(planned), 0)
		FROM hourly_counts WHERE shape_id=? AND shift_name=? AND count_date=? AND shift_hour=1`,
		shapeID, sess.shiftName, sess.countDate).Scan(&actual, &planned)
	if err != nil || planned <= 0 || actual <= 0 {
		return false
	}
	return actual >= planned
}

// trackCountDelta computes and enqueues count deltas. Called with el.mu held.
func (el *EventLogger) trackCountDelta(shapeID string, meta *shapeMetadata, snap *shapeSnapshot, newVal int32) {
	cs := el.countStates[shapeID]
	if cs == nil || cs.session == nil {
		return
	}

	now := time.Now()

	switch meta.ShapeType {
	case "process":
		pc := DecodeProcessCount(newVal)
		if !cs.initialized {
			cs.lastPartID = pc.PartID
			cs.lastCounter = pc.Counter
			cs.currentStyle = fmt.Sprintf("%d", pc.PartID)
			cs.initialized = true
			return
		}

		var delta int

		if pc.PartID != cs.lastPartID {
			// Part ID change = style change. The Counter value represents
			// production under the NEW part ID — count it as delta.
			cs.currentStyle = fmt.Sprintf("%d", pc.PartID)
			cs.lastPartID = pc.PartID
			delta = int(pc.Counter)
			cs.lastCounter = pc.Counter
		} else {
			// Same part ID — compute delta from counter change
			if pc.Counter >= cs.lastCounter {
				delta = int(pc.Counter - cs.lastCounter)
			} else {
				// Counter rollover — conservative: count the new value
				delta = int(pc.Counter)
			}
			cs.lastCounter = pc.Counter
		}

		if delta <= 0 {
			return
		}

		el.enqueueCountDelta(shapeID, meta, cs, delta, now)

	case "press":
		if !cs.initialized {
			cs.lastCount = newVal
			if snap.Style != 0 {
				cs.currentStyle = fmt.Sprintf("%d", snap.Style)
			}
			cs.initialized = true
			return
		}

		var delta int
		if newVal > cs.lastCount {
			delta = int(newVal - cs.lastCount)
		}
		cs.lastCount = newVal

		if delta <= 0 {
			return
		}

		el.enqueueCountDelta(shapeID, meta, cs, delta, now)
	}
}

// enqueueCountDelta updates behind state and enqueues a hourly count event.
// Called with el.mu held.
func (el *EventLogger) enqueueCountDelta(shapeID string, meta *shapeMetadata, cs *shapeCountState, delta int, now time.Time) {
	if bs := el.behindStates[shapeID]; bs != nil {
		bs.actualParts += delta
	}
	isEarly, isOvertime := classifyHour(cs.session, now)
	sh := computeShiftHour(cs.session, now.Hour()*60+now.Minute())
	el.enqueueHourlyCount(hourlyCountEvent{
		screenID:   meta.ScreenID,
		shapeID:    shapeID,
		shapeType:  meta.ShapeType,
		shiftName:  cs.session.shiftName,
		jobStyle:   cs.currentStyle,
		countDate:  cs.session.countDate,
		hour:       sh,
		shiftHour:  sh,
		delta:      delta,
		isEarly:    isEarly,
		isOvertime: isOvertime,
	})
}

// computeShiftHour returns the 1-based shift hour index for a given minute-of-day.
func computeShiftHour(session *countSession, nowMinutes int) int {
	return shiftHour(session.shiftStart, nowMinutes)
}

// enqueueHourlyCount sends an event to hourlyCh (non-blocking). Called with el.mu held.
func (el *EventLogger) enqueueHourlyCount(ev hourlyCountEvent) {
	if el.stopped {
		return
	}
	select {
	case el.hourlyCh <- ev:
	default:
		log.Printf("eventlog: WARNING — hourly count channel full, dropping event for shape %s", ev.shapeID)
	}
}

// classifyHour determines if the current time is early or overtime relative to the shift.
func classifyHour(session *countSession, now time.Time) (isEarly, isOvertime bool) {
	if session.shiftStart == session.shiftEnd {
		return // No shift boundaries (unscheduled), no early/overtime classification
	}
	nowMin := now.Hour()*60 + now.Minute()
	if session.overnight {
		// e.g., 23:00-07:00: "in shift" = nowMin >= start OR nowMin < end
		inShift := nowMin >= session.shiftStart || nowMin < session.shiftEnd
		if !inShift {
			if nowMin < session.shiftStart {
				isEarly = true
			}
			if nowMin >= session.shiftEnd && nowMin < session.shiftStart {
				isOvertime = true
			}
		}
	} else {
		if nowMin < session.shiftStart {
			isEarly = true
		}
		if nowMin >= session.shiftEnd {
			isOvertime = true
		}
	}
	return
}

// styleTimeTicker increments style_minutes once per minute for active shapes.
func (el *EventLogger) styleTimeTicker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			el.flushStyleMinutes()
		}
	}
}

// flushStyleMinutes enqueues a style_minutes=1 event for each active shape.
// Break-aware: pauses style_minutes during allowed break time.
func (el *EventLogger) flushStyleMinutes() {
	// Phase 1: Read overlay state BEFORE acquiring el.mu (avoids lock ordering issues with hub.mu)
	overlays := el.hub.GetAllOverlays()

	el.mu.Lock()
	defer el.mu.Unlock()

	if el.stopped {
		return
	}

	now := time.Now()
	for shapeID, cs := range el.countStates {
		if cs.session == nil || cs.currentStyle == "" {
			continue
		}
		meta := el.metadata[shapeID]
		if meta == nil {
			continue
		}
		isEarly, isOvertime := classifyHour(cs.session, now)
		shiftHour := computeShiftHour(cs.session, now.Hour()*60+now.Minute())

		styleMin := 1
		breakMin := 0

		isOnBreak := overlays[meta.ScreenID] == "BREAK"
		if isOnBreak {
			breakMin = 1
			workMin := el.workMinutesForHourCached(cs.session, shiftHour)
			allowance := 60 - workMin // e.g. 45 workMin → 15 min allowance
			if allowance > 0 {
				bs := el.behindStates[shapeID]
				breakUsedMin := 0
				if bs != nil {
					breakUsedMin = bs.breakUsedSec[shiftHour] / 60
				}
				if breakUsedMin < allowance {
					styleMin = 0 // paused: within allowance
				}
			}
		}

		select {
		case el.hourlyCh <- hourlyCountEvent{
			screenID:     meta.ScreenID,
			shapeID:      shapeID,
			shapeType:    meta.ShapeType,
			shiftName:    cs.session.shiftName,
			jobStyle:     cs.currentStyle,
			countDate:    cs.session.countDate,
			hour:         shiftHour,
			shiftHour:    shiftHour,
			delta:        0,
			styleMinutes: styleMin,
			breakMinutes: breakMin,
			isEarly:      isEarly,
			isOvertime:   isOvertime,
		}:
		default:
		}
	}
}

// workMinutesForHourCached returns work minutes from the session's cached minutes slice.
// Falls back to 60 if not configured. Does not require el.mu (uses session data directly).
func (el *EventLogger) workMinutesForHourCached(session *countSession, shiftHour int) int {
	idx := shiftHour - 1
	if idx >= 0 && idx < len(session.minutes) {
		return session.minutes[idx]
	}
	return 60
}

// insertHourlyBatch upserts hourly count events and recomputes planned.
func (el *EventLogger) insertHourlyBatch(batch []hourlyCountEvent) error {
	tx, err := el.db.Begin()
	if err != nil {
		return fmt.Errorf("begin hourly tx: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO hourly_counts
		(screen_id, shape_id, shape_type, shift_name, job_style, count_date, hour, shift_hour, delta, style_minutes, break_minutes, is_early, is_overtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(screen_id, shape_id, shift_name, job_style, count_date, shift_hour)
		DO UPDATE SET delta = delta + excluded.delta, style_minutes = style_minutes + excluded.style_minutes, break_minutes = break_minutes + excluded.break_minutes`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare hourly: %w", err)
	}
	defer stmt.Close()

	// Track affected keys for planned recomputation
	type hourlyKey struct {
		screenID  string
		shapeID   string
		shiftName string
		countDate string
		shiftHour int
	}
	affectedSet := make(map[hourlyKey]bool)

	for _, ev := range batch {
		isEarly := 0
		if ev.isEarly {
			isEarly = 1
		}
		isOT := 0
		if ev.isOvertime {
			isOT = 1
		}
		_, err = stmt.Exec(ev.screenID, ev.shapeID, ev.shapeType, ev.shiftName,
			ev.jobStyle, ev.countDate, ev.hour, ev.shiftHour, ev.delta, ev.styleMinutes, ev.breakMinutes, isEarly, isOT)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert hourly: %w", err)
		}
		affectedSet[hourlyKey{ev.screenID, ev.shapeID, ev.shiftName, ev.countDate, ev.shiftHour}] = true
	}

	// Recompute planned for affected keys
	for key := range affectedSet {
		el.recomputePlanned(tx, key.screenID, key.shapeID, key.shiftName, key.countDate, key.shiftHour)
	}

	return tx.Commit()
}

// recomputePlanned updates the planned column for all rows matching a key.
func (el *EventLogger) recomputePlanned(tx *sql.Tx, screenID, shapeID, shiftName, countDate string, shiftHour int) {
	rows, err := tx.Query(`SELECT id, job_style, style_minutes, is_early, is_overtime
		FROM hourly_counts
		WHERE screen_id=? AND shape_id=? AND shift_name=? AND count_date=? AND shift_hour=?`,
		screenID, shapeID, shiftName, countDate, shiftHour)
	if err != nil {
		return
	}

	type row struct {
		id           int64
		jobStyle     string
		styleMinutes int
		isEarly      bool
		isOvertime   bool
	}
	var hourRows []row
	for rows.Next() {
		var r row
		var isE, isOT int
		if err := rows.Scan(&r.id, &r.jobStyle, &r.styleMinutes, &isE, &isOT); err != nil {
			continue
		}
		r.isEarly = isE != 0
		r.isOvertime = isOT != 0
		hourRows = append(hourRows, r)
	}
	rows.Close()

	if len(hourRows) == 0 {
		return
	}

	// Skip early/overtime rows (planned stays 0)
	if hourRows[0].isEarly || hourRows[0].isOvertime {
		return
	}

	// Get shift config for work minutes
	workMin := el.workMinutesForHour(shiftName, shiftHour)
	if workMin <= 0 {
		return
	}

	// Get cell config for takt lookup
	cellCfg, layoutStyles := el.getShapeConfig(screenID, shapeID)

	// Total style_minutes across all styles in this hour
	totalStyleMin := 0
	for _, r := range hourRows {
		totalStyleMin += r.styleMinutes
	}

	for _, r := range hourRows {
		var planned int
		if totalStyleMin == 0 {
			// No style time tracked yet — use default takt
			if cellCfg.TaktTime > 0 {
				planned = int(float64(workMin) / (cellCfg.TaktTime / 60.0))
			}
		} else {
			takt := resolveStyleTakt(r.jobStyle, cellCfg, layoutStyles)
			if takt > 0 {
				fraction := float64(r.styleMinutes) / float64(totalStyleMin)
				planned = int(float64(workMin) * fraction / (takt / 60.0))
			}
		}
		if _, err := tx.Exec("UPDATE hourly_counts SET planned=? WHERE id=?", planned, r.id); err != nil {
			log.Printf("eventlog: recomputePlanned update id=%d: %v", r.id, err)
		}
	}
}

// workMinutesForHour returns the configured work minutes for a given shift hour
// (1-based index). Falls back to 60 if not configured.
func (el *EventLogger) workMinutesForHour(shiftName string, shiftHour int) int {
	settings := el.store.GetSettings()
	for _, s := range settings.Shifts {
		if s.Name != shiftName {
			continue
		}
		idx := shiftHour - 1
		if idx >= 0 && idx < len(s.Minutes) {
			return s.Minutes[idx]
		}
		return 60
	}
	return 60
}

// getShapeConfig returns the CellParams and layout styles for a shape.
func (el *EventLogger) getShapeConfig(screenID, shapeID string) (CellParams, []layoutStyleDef) {
	sc, ok := el.store.GetScreen(screenID)
	if !ok {
		return CellParams{}, nil
	}

	cfg := sc.CellConfig[shapeID]

	// Parse layout for styles array
	var shapes []struct {
		ID     string `json:"id"`
		Config struct {
			Styles []layoutStyleDef `json:"styles"`
		} `json:"config"`
	}
	json.Unmarshal(sc.Layout, &shapes)

	for _, s := range shapes {
		if s.ID == shapeID {
			return cfg, s.Config.Styles
		}
	}
	return cfg, nil
}

// resolveStyleTakt maps a PLC style value to a takt time.
// Looks up the style value in layout styles to get the name, then checks CellParams.Styles.
// Falls back to the shape's default TaktTime.
func resolveStyleTakt(jobStyle string, cfg CellParams, layoutStyles []layoutStyleDef) float64 {
	if cfg.Styles != nil && len(layoutStyles) > 0 {
		// Try to find the style name for this PLC value
		for _, ls := range layoutStyles {
			if fmt.Sprintf("%d", ls.Value) == jobStyle {
				if sp, ok := cfg.Styles[ls.Name]; ok && sp.TaktTime > 0 {
					return sp.TaktTime
				}
				break
			}
		}
	}
	return cfg.TaktTime
}

// resolveStyleManMachine maps a PLC style value to man/machine time targets.
// Same lookup pattern as resolveStyleTakt: PLC value → layout style name → per-style targets.
// Falls back to the shape's default ManTime/MachineTime.
func resolveStyleManMachine(jobStyle string, cfg CellParams, layoutStyles []layoutStyleDef) (float64, float64) {
	if cfg.Styles != nil && len(layoutStyles) > 0 {
		for _, ls := range layoutStyles {
			if fmt.Sprintf("%d", ls.Value) == jobStyle {
				if sp, ok := cfg.Styles[ls.Name]; ok {
					manTime := sp.ManTime
					machineTime := sp.MachineTime
					if manTime <= 0 {
						manTime = cfg.ManTime
					}
					if machineTime <= 0 {
						machineTime = cfg.MachineTime
					}
					return manTime, machineTime
				}
				break
			}
		}
	}
	return cfg.ManTime, cfg.MachineTime
}

// QueryHourlyCounts returns hourly count rows for the API.
func (el *EventLogger) QueryHourlyCounts(screenID, countDate, shapeID, style string) ([]HourlyCountRow, error) {
	query := `SELECT screen_id, shape_id, shape_type, shift_name, job_style, count_date, hour, shift_hour,
		delta, planned, style_minutes, is_early, is_overtime
		FROM hourly_counts WHERE screen_id=? AND count_date=?`
	args := []interface{}{screenID, countDate}

	if shapeID != "" {
		query += " AND shape_id=?"
		args = append(args, shapeID)
	}
	if style != "" {
		query += " AND job_style=?"
		args = append(args, style)
	}

	query += " ORDER BY shift_name, shift_hour"

	rows, err := el.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HourlyCountRow
	for rows.Next() {
		var r HourlyCountRow
		var isE, isOT int
		if err := rows.Scan(&r.ScreenID, &r.ShapeID, &r.ShapeType, &r.ShiftName,
			&r.JobStyle, &r.CountDate, &r.Hour, &r.ShiftHour, &r.Delta, &r.Planned,
			&r.StyleMinutes, &isE, &isOT); err != nil {
			continue
		}
		r.IsEarly = isE != 0
		r.IsOvertime = isOT != 0
		result = append(result, r)
	}
	return result, nil
}

// HourlyCountRow is a single row from hourly_counts.
type HourlyCountRow struct {
	ScreenID     string
	ShapeID      string
	ShapeType    string
	ShiftName    string
	JobStyle     string
	CountDate    string
	Hour         int
	ShiftHour    int
	Delta        int
	Planned      int
	StyleMinutes int
	IsEarly      bool
	IsOvertime   bool
}
