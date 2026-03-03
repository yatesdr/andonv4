// reports.go — State-change event walking and duration computation for shift reports.
package server

import (
	"fmt"
	"time"
)

// --- Types ---

type stateRow struct {
	Ts    time.Time
	State int32
	Count int32
	Style int32
	Field string // "state" or "style"
}

type durationResult struct {
	TotalSeconds float64
	RisingEdges  int
	ByHour       map[int]float64 // shiftHour -> seconds
	EdgesByHour  map[int]int     // shiftHour -> edge count
}

func newDurationResult() durationResult {
	return durationResult{
		ByHour:      make(map[int]float64),
		EdgesByHour: make(map[int]int),
	}
}

type reportSession struct {
	shiftStart int   // minutes since midnight
	overnight  bool
	minutes    []int // work minutes per hour
}

// targetResolver returns manTime and machineTime targets for a given PLC style value.
// For style=0 (legacy data), it falls through to default targets.
type targetResolver func(style int32) (manTime, machineTime float64)

// buildTargetResolver creates a targetResolver from cell config and layout style definitions.
func buildTargetResolver(cfg CellParams, layoutStyles []layoutStyleDef) targetResolver {
	return func(style int32) (float64, float64) {
		return resolveStyleManMachine(fmt.Sprintf("%d", style), cfg, layoutStyles)
	}
}

// --- Query Helpers ---

// queryStateRows fetches process_events for a shape within a time range, ordered by ts.
// It also fetches the last "state" and "style" rows before the range as seeds.
// Returns: rows, seedState, seedStyle, error.
func (el *EventLogger) queryStateRows(shapeID string, from, to time.Time) ([]stateRow, int32, int32, error) {
	fromStr := from.UTC().Format(time.RFC3339Nano)
	toStr := to.UTC().Format(time.RFC3339Nano)

	// Seed: last state before range
	var seedState int32
	err := el.db.QueryRow(`SELECT state FROM process_events
		WHERE shape_id = ? AND trigger_field = 'state' AND ts < ?
		ORDER BY ts DESC LIMIT 1`, shapeID, fromStr).Scan(&seedState)
	if err != nil {
		seedState = 0 // no prior state
	}

	// Seed: last style before range
	var seedStyle int32
	el.db.QueryRow(`SELECT style FROM process_events
		WHERE shape_id = ? AND trigger_field = 'style' AND ts < ?
		ORDER BY ts DESC LIMIT 1`, shapeID, fromStr).Scan(&seedStyle)

	// Main query
	rows, err := el.db.Query(`SELECT ts, state, count, style, trigger_field FROM process_events
		WHERE shape_id = ? AND trigger_field IN ('state','style')
		AND ts >= ? AND ts < ?
		ORDER BY ts ASC`, shapeID, fromStr, toStr)
	if err != nil {
		return nil, seedState, seedStyle, fmt.Errorf("queryStateRows: %w", err)
	}
	defer rows.Close()

	var result []stateRow
	for rows.Next() {
		var r stateRow
		var tsStr string
		if err := rows.Scan(&tsStr, &r.State, &r.Count, &r.Style, &r.Field); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339Nano, tsStr)
		r.Ts = t.In(time.Local) // shift math uses local hours/minutes
		result = append(result, r)
	}
	return result, seedState, seedStyle, nil
}

// --- Shift Time Range ---

// shiftTimeRange computes absolute start/end times for a date+shift.
func shiftTimeRange(dateStr string, shift Shift) (time.Time, time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date: %w", err)
	}

	startMin, ok := parseHHMM(shift.Start)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid shift start: %s", shift.Start)
	}
	endMin, ok := parseHHMM(shift.End)
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid shift end: %s", shift.End)
	}

	start := date.Add(time.Duration(startMin) * time.Minute)
	end := date.Add(time.Duration(endMin) * time.Minute)

	// Overnight shift: end is on the next day
	if endMin <= startMin {
		end = end.Add(24 * time.Hour)
	}

	return start, end, nil
}

// --- Bucket Duration ---

// bucketDuration splits a time interval across shift hours.
func bucketDuration(result *durationResult, session *reportSession, from, to time.Time) {
	cur := from
	for cur.Before(to) {
		curMin := cur.Hour()*60 + cur.Minute()
		hour := reportShiftHour(session, curMin)

		// End of this shift hour
		hourEndMin := (session.shiftStart + hour*60) % 1440
		dayStart := time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, cur.Location())
		hourEnd := dayStart.Add(time.Duration(hourEndMin) * time.Minute)
		// Handle midnight wrap
		if !hourEnd.After(cur) {
			hourEnd = hourEnd.Add(24 * time.Hour)
		}

		end := to
		if hourEnd.Before(to) {
			end = hourEnd
		}

		secs := end.Sub(cur).Seconds()
		if secs > 0 {
			result.ByHour[hour] += secs
			result.TotalSeconds += secs
		}

		cur = end
	}
}

// reportShiftHour returns 1-based shift hour for a minute-of-day.
func reportShiftHour(session *reportSession, nowMinutes int) int {
	return shiftHour(session.shiftStart, nowMinutes)
}

// --- Generic Walker: walkDuration ---

// walkDuration walks state rows and accumulates duration where cond(state) is true.
func walkDuration(rows []stateRow, cond func(int32) bool, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32) durationResult {

	result := newDurationResult()
	active := cond(seedState)
	var activeStart time.Time
	if active {
		activeStart = shiftStart
		result.RisingEdges++
		hour := reportShiftHour(session, shiftStart.Hour()*60+shiftStart.Minute())
		result.EdgesByHour[hour]++
	}

	for _, r := range rows {
		if r.Field != "state" {
			continue
		}
		nowActive := cond(r.State)
		if !active && nowActive {
			// Rising edge
			active = true
			activeStart = r.Ts
			result.RisingEdges++
			hour := reportShiftHour(session, r.Ts.Hour()*60+r.Ts.Minute())
			result.EdgesByHour[hour]++
		} else if active && !nowActive {
			// Falling edge
			active = false
			bucketDuration(&result, session, activeStart, r.Ts)
		}
	}

	// Still active at end
	if active {
		closeAt := shiftEnd
		now := time.Now()
		if now.Before(shiftEnd) {
			closeAt = now
		}
		if closeAt.After(activeStart) {
			bucketDuration(&result, session, activeStart, closeAt)
		}
	}

	return result
}

// --- Downtime Walker ---

// walkDowntime tracks intervals where (bit3|bit5) is high, closed by bit4 rising.
// Overlapping faults merge into one stoppage.
func walkDowntime(rows []stateRow, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32) durationResult {

	result := newDurationResult()

	faultActive := (seedState&(1<<BitFaulted) != 0) || (seedState&(1<<BitEStop) != 0)
	var activeStart time.Time
	if faultActive {
		activeStart = shiftStart
		result.RisingEdges++
		result.EdgesByHour[reportShiftHour(session, shiftStart.Hour()*60+shiftStart.Minute())]++
	}

	prevState := seedState

	for _, r := range rows {
		if r.Field != "state" {
			continue
		}
		nowFault := (r.State&(1<<BitFaulted) != 0) || (r.State&(1<<BitEStop) != 0)
		nowInCycle := r.State&(1<<BitInCycle) != 0
		prevInCycle := prevState&(1<<BitInCycle) != 0

		if !faultActive && nowFault {
			// Fault/eStop rising
			faultActive = true
			activeStart = r.Ts
			result.RisingEdges++
			hour := reportShiftHour(session, r.Ts.Hour()*60+r.Ts.Minute())
			result.EdgesByHour[hour]++
		} else if faultActive && nowInCycle && !prevInCycle {
			// inCycle rising while faulted -> close downtime
			faultActive = false
			bucketDuration(&result, session, activeStart, r.Ts)
		}

		prevState = r.State
	}

	// Still active at end
	if faultActive {
		closeAt := shiftEnd
		now := time.Now()
		if now.Before(shiftEnd) {
			closeAt = now
		}
		if closeAt.After(activeStart) {
			bucketDuration(&result, session, activeStart, closeAt)
		}
	}

	return result
}

// --- Paired Interval Walker ---

// walkPairedIntervals tracks intervals from startBit rising to endBit rising.
// subtractTarget is subtracted from each interval (0 for entry, ManTime for overcycle).
// If resolver is non-nil, subtractTarget is dynamically updated from style events.
// Fault/eStop cancels open intervals.
func walkPairedIntervals(rows []stateRow, startBit, endBit int,
	subtractTarget float64, resolver targetResolver, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32, seedStyle int32) durationResult {

	result := newDurationResult()

	// Resolve initial target from seed style
	currentTarget := subtractTarget
	if resolver != nil && seedStyle != 0 {
		manTime, _ := resolver(seedStyle)
		if manTime > 0 {
			currentTarget = manTime
		}
	}

	prevState := seedState
	inInterval := false
	var intervalStart time.Time

	for _, r := range rows {
		// Track style changes for dynamic target resolution
		if r.Field == "style" && resolver != nil {
			manTime, _ := resolver(r.Style)
			if manTime > 0 {
				currentTarget = manTime
			}
			continue
		}
		if r.Field != "state" {
			continue
		}
		// Edge detection
		startRising := (r.State&(1<<startBit) != 0) && (prevState&(1<<startBit) == 0)
		endRising := (r.State&(1<<endBit) != 0) && (prevState&(1<<endBit) == 0)
		fault := (r.State&(1<<BitFaulted) != 0) || (r.State&(1<<BitEStop) != 0)

		if fault && inInterval {
			// Cancel open interval
			inInterval = false
		}

		if startRising && !fault {
			inInterval = true
			intervalStart = r.Ts
		}

		if endRising && inInterval {
			inInterval = false
			dur := r.Ts.Sub(intervalStart).Seconds() - currentTarget
			if dur > 0 {
				// Create a temp interval for bucketing
				adjustedStart := r.Ts.Add(-time.Duration(dur * float64(time.Second)))
				bucketDuration(&result, session, adjustedStart, r.Ts)
				result.RisingEdges++
				hour := reportShiftHour(session, intervalStart.Hour()*60+intervalStart.Minute())
				result.EdgesByHour[hour]++
			}
		}

		prevState = r.State
	}

	return result
}

// --- Machine Overcycle Walker ---

// walkMachineOvercycle tracks inCycle(bit4) intervals and reports excess over machineTime.
// If resolver is non-nil, machineTime is dynamically updated from style events.
func walkMachineOvercycle(rows []stateRow, machineTime float64, resolver targetResolver, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32, seedStyle int32) durationResult {

	result := newDurationResult()

	// Resolve initial target from seed style
	currentTarget := machineTime
	if resolver != nil && seedStyle != 0 {
		_, mt := resolver(seedStyle)
		if mt > 0 {
			currentTarget = mt
		}
	}
	if currentTarget <= 0 {
		return result
	}

	inCycle := seedState&(1<<BitInCycle) != 0
	var cycleStart time.Time
	if inCycle {
		cycleStart = shiftStart
	}
	prevState := seedState

	for _, r := range rows {
		// Track style changes for dynamic target resolution
		if r.Field == "style" && resolver != nil {
			_, mt := resolver(r.Style)
			if mt > 0 {
				currentTarget = mt
			}
			continue
		}
		if r.Field != "state" {
			continue
		}
		nowInCycle := r.State&(1<<BitInCycle) != 0
		prevInCycle := prevState&(1<<BitInCycle) != 0

		if nowInCycle && !prevInCycle {
			// Rising edge
			inCycle = true
			cycleStart = r.Ts
		} else if !nowInCycle && prevInCycle && inCycle {
			// Falling edge — completed cycle
			dur := r.Ts.Sub(cycleStart).Seconds()
			excess := dur - currentTarget
			if excess > 0 {
				adjustedStart := r.Ts.Add(-time.Duration(excess * float64(time.Second)))
				bucketDuration(&result, session, adjustedStart, r.Ts)
				result.RisingEdges++
				hour := reportShiftHour(session, cycleStart.Hour()*60+cycleStart.Minute())
				result.EdgesByHour[hour]++
			}
			inCycle = false
		}

		prevState = r.State
	}

	return result
}

// --- Starved/Blocked Walker ---

// walkStarvedBlocked tracks time when (bit9|bit10) AND inCycle duration > machineTime.
// If resolver is non-nil, machineTime is dynamically updated from style events.
func walkStarvedBlocked(rows []stateRow, machineTime float64, resolver targetResolver, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32, seedStyle int32) durationResult {

	result := newDurationResult()

	// Resolve initial target from seed style
	currentTarget := machineTime
	if resolver != nil && seedStyle != 0 {
		_, mt := resolver(seedStyle)
		if mt > 0 {
			currentTarget = mt
		}
	}
	if currentTarget <= 0 {
		return result
	}

	// Track inCycle start
	inCycle := seedState&(1<<BitInCycle) != 0
	var cycleStart time.Time
	if inCycle {
		cycleStart = shiftStart
	}

	// Track starved/blocked active state
	sbActive := false
	var sbStart time.Time

	prevState := seedState

	for _, r := range rows {
		// Track style changes for dynamic target resolution
		if r.Field == "style" && resolver != nil {
			_, mt := resolver(r.Style)
			if mt > 0 {
				currentTarget = mt
			}
			continue
		}
		if r.Field != "state" {
			continue
		}
		nowInCycle := r.State&(1<<BitInCycle) != 0
		prevInCycle := prevState&(1<<BitInCycle) != 0
		nowSB := (r.State&(1<<BitStarved) != 0) || (r.State&(1<<BitBlocked) != 0)

		// Track inCycle edges
		if nowInCycle && !prevInCycle {
			inCycle = true
			cycleStart = r.Ts
		} else if !nowInCycle && prevInCycle {
			inCycle = false
			if sbActive {
				sbActive = false
				bucketDuration(&result, session, sbStart, r.Ts)
			}
		}

		// Check if starved/blocked AND in overcycle territory
		if inCycle && nowSB {
			elapsed := r.Ts.Sub(cycleStart).Seconds()
			if elapsed > currentTarget && !sbActive {
				sbActive = true
				sbStart = r.Ts
			}
		} else if sbActive && (!nowSB || !nowInCycle) {
			sbActive = false
			bucketDuration(&result, session, sbStart, r.Ts)
		}

		prevState = r.State
	}

	// Close open interval
	if sbActive {
		closeAt := shiftEnd
		now := time.Now()
		if now.Before(shiftEnd) {
			closeAt = now
		}
		if closeAt.After(sbStart) {
			bucketDuration(&result, session, sbStart, closeAt)
		}
	}

	return result
}

// --- Multi-Duration Walker (Andon bits) ---

// walkMultiDuration tracks multiple bits in a single pass. Returns one durationResult per bit.
func walkMultiDuration(rows []stateRow, bits []int, session *reportSession,
	shiftStart, shiftEnd time.Time, seedState int32) []durationResult {

	results := make([]durationResult, len(bits))
	active := make([]bool, len(bits))
	starts := make([]time.Time, len(bits))

	for i, bit := range bits {
		results[i] = newDurationResult()
		if seedState&(1<<bit) != 0 {
			active[i] = true
			starts[i] = shiftStart
			results[i].RisingEdges++
			hour := reportShiftHour(session, shiftStart.Hour()*60+shiftStart.Minute())
			results[i].EdgesByHour[hour]++
		}
	}

	for _, r := range rows {
		if r.Field != "state" {
			continue
		}
		for i, bit := range bits {
			nowActive := r.State&(1<<bit) != 0
			if !active[i] && nowActive {
				active[i] = true
				starts[i] = r.Ts
				results[i].RisingEdges++
				hour := reportShiftHour(session, r.Ts.Hour()*60+r.Ts.Minute())
				results[i].EdgesByHour[hour]++
			} else if active[i] && !nowActive {
				active[i] = false
				bucketDuration(&results[i], session, starts[i], r.Ts)
			}
		}
	}

	// Close open intervals
	closeAt := shiftEnd
	now := time.Now()
	if now.Before(shiftEnd) {
		closeAt = now
	}
	for i := range bits {
		if active[i] && closeAt.After(starts[i]) {
			bucketDuration(&results[i], session, starts[i], closeAt)
		}
	}

	return results
}

// elapsedWorkSeconds computes the work seconds that have actually elapsed
// within a shift, capped at the configured work minutes per hour.
// For completed historical shifts, returns the full configured total.
// For in-progress shifts, pro-rates based on current time.
// lastEventTs (if non-zero) is used as a proxy for early-ending shifts:
// if the gap between lastEventTs and shiftEnd exceeds 30 minutes,
// the shift is considered to have ended at lastEventTs.
func elapsedWorkSeconds(session *reportSession, shiftStart, shiftEnd time.Time, lastEventTs time.Time) float64 {
	effectiveEnd := shiftEnd
	now := time.Now()

	// In-progress shift: cap at now
	if now.Before(shiftEnd) {
		effectiveEnd = now
	}

	// Early-ending shift: if last event is >30min before shift end,
	// treat the shift as having ended at the last event
	if !lastEventTs.IsZero() && effectiveEnd.Equal(shiftEnd) {
		gap := shiftEnd.Sub(lastEventTs)
		if gap > 30*time.Minute {
			effectiveEnd = lastEventTs
		}
	}

	// Sum configured work seconds per hour, pro-rating the partial last hour
	total := 0.0
	hourStart := shiftStart
	for _, workMin := range session.minutes {
		hourEnd := hourStart.Add(60 * time.Minute)
		if !hourStart.Before(effectiveEnd) {
			break
		}
		if hourEnd.After(effectiveEnd) {
			// Partial hour — pro-rate
			fraction := effectiveEnd.Sub(hourStart).Minutes() / 60.0
			total += float64(workMin) * 60 * fraction
		} else {
			total += float64(workMin) * 60
		}
		hourStart = hourEnd
	}
	return total
}

// lastEventTimestamp returns the timestamp of the last state row, or zero time if empty.
func lastEventTimestamp(rows []stateRow) time.Time {
	if len(rows) == 0 {
		return time.Time{}
	}
	return rows[len(rows)-1].Ts
}
