// eventlog_behind.go — Real-time behind-bit and overcycle computation with SSE broadcasts.
// Part of the EventLogger subsystem.
//
// Lock ordering: el.mu must NEVER be held when calling into hub (hub.mu).
// The tickBehind function uses a three-phase pattern:
//   Phase 1 (no lock): read hub overlay state
//   Phase 2 (el.mu held): compute behind/overcycle, collect pending broadcasts
//   Phase 3 (no lock): send broadcasts via hub

package server

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// pendingBroadcast is a computedState change queued for broadcast outside the lock.
type pendingBroadcast struct {
	slug          string
	shapeID       string
	shapeType     string
	computedState int32
}

// behindTicker runs every second, computing whether each shape is behind expected production.
func (el *EventLogger) behindTicker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			el.tickBehind()
		}
	}
}

// tickBehind performs one cycle of behind-bit computation.
// Three-phase to avoid lock ordering issues (el.mu → hub.mu).
func (el *EventLogger) tickBehind() {
	// Phase 1 (no lock): Read overlay state for all active screens
	overlays := el.hub.GetAllOverlays()

	// Phase 2 (el.mu held): Compute expected/actual, collect broadcasts
	var broadcasts []pendingBroadcast

	now := time.Now()

	el.mu.Lock()
	for shapeID, bs := range el.behindStates {
		if bs.currentTakt <= 0 {
			bs.lastTickTime = now
			continue
		}
		cs := el.countStates[shapeID]
		if cs == nil || cs.session == nil {
			bs.lastTickTime = now
			continue
		}
		meta := el.metadata[shapeID]
		if meta == nil {
			bs.lastTickTime = now
			continue
		}

		nowMin := now.Hour()*60 + now.Minute()
		isEarly, isOvertime := classifyHour(cs.session, now)
		if isEarly || isOvertime {
			bs.lastTickTime = now
			continue
		}

		shiftHour := computeShiftHour(cs.session, nowMin)

		// firstHourMet (bit 2) + firstHourComplete (bit 3): check once when transitioning past hour 1
		if shiftHour >= 2 && !bs.firstHourChecked {
			bs.firstHourChecked = true
			snap := el.cache[shapeID]
			if snap != nil {
				snap.ComputedState |= 1 << CsBitFirstHourComplete // set bit 3 — firstHourComplete
				if !el.checkFirstHourMet(shapeID, cs.session) {
					snap.ComputedState &^= 1 << CsBitFirstHourMet // clear bit 2 — missed first hour
				}
				broadcasts = append(broadcasts, pendingBroadcast{
					slug:          meta.ScreenSlug,
					shapeID:       shapeID,
					shapeType:     meta.ShapeType,
					computedState: snap.ComputedState,
				})
			}
		}

		workMin := el.workMinutesForHourCached(cs.session, shiftHour)
		allowanceSec := (60 - workMin) * 60

		elapsed := now.Sub(bs.lastTickTime).Seconds()
		bs.lastTickTime = now
		if elapsed <= 0 || elapsed > 5 {
			// Skip anomalous ticks (clock jump, first tick, etc.)
			continue
		}

		isOnBreak := overlays[meta.ScreenID] == "BREAK"
		if isOnBreak && bs.breakUsedSec[shiftHour] < allowanceSec {
			remaining := float64(allowanceSec - bs.breakUsedSec[shiftHour])
			if elapsed <= remaining {
				bs.breakUsedSec[shiftHour] += int(elapsed)
				// expected does NOT accumulate (free break)
			} else {
				bs.breakUsedSec[shiftHour] = allowanceSec
				bs.expectedParts += (elapsed - remaining) / bs.currentTakt
			}
		} else {
			// Not on break, or break exceeded allowance
			bs.expectedParts += elapsed / bs.currentTakt
		}

		behind := bs.actualParts < int(math.Floor(bs.expectedParts))
		if behind != bs.lastBehind {
			bs.lastBehind = behind
			snap := el.cache[shapeID]
			if snap == nil {
				continue
			}
			if behind {
				snap.ComputedState |= 1 << CsBitBehind // set bit 0
			} else {
				snap.ComputedState &^= 1 << CsBitBehind // clear bit 0
			}
			broadcasts = append(broadcasts, pendingBroadcast{
				slug:          meta.ScreenSlug,
				shapeID:       shapeID,
				shapeType:     meta.ShapeType,
				computedState: snap.ComputedState,
			})
		}
	}

	el.collectOvercycleBroadcasts(now, &broadcasts)

	el.mu.Unlock()

	// Phase 3 (no lock): Send any changed computedState broadcasts
	for _, b := range broadcasts {
		computed := b.computedState
		payload := cellDataPayload{ComputedState: &computed}
		msg := cellDataMsg{
			ShapeID: b.shapeID,
			Type:    b.shapeType,
			Data:    payload,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		el.hub.Broadcast(BroadcastMsg{
			Slug:  b.slug,
			Event: "cell_data",
			Data:  string(data),
		})
	}
}

// collectOvercycleBroadcasts checks overcycle state for all tracked shapes
// and appends broadcasts for any transitions. Called with el.mu held.
func (el *EventLogger) collectOvercycleBroadcasts(now time.Time, broadcasts *[]pendingBroadcast) {
	for shapeID, ot := range el.overcycleStates {
		if ot.phase == 0 { // idle — nothing to check
			continue
		}
		meta := el.metadata[shapeID]
		if meta == nil {
			continue
		}
		snap := el.cache[shapeID]
		if snap == nil {
			continue
		}

		var target float64
		if ot.phase == 1 {
			target = ot.currentManTarget
		} else {
			target = ot.currentMachineTarget
		}
		if target <= 0 {
			continue // no target configured — skip
		}

		elapsed := now.Sub(ot.phaseStart).Seconds()
		overcycle := elapsed > target

		if overcycle != ot.lastOvercycle {
			ot.lastOvercycle = overcycle
			if overcycle {
				snap.ComputedState |= 1 << CsBitOvercycle
			} else {
				snap.ComputedState &^= 1 << CsBitOvercycle
			}
			*broadcasts = append(*broadcasts, pendingBroadcast{
				slug:          meta.ScreenSlug,
				shapeID:       shapeID,
				shapeType:     meta.ShapeType,
				computedState: snap.ComputedState,
			})
		}
	}
}

// GetComputedState returns the current computed state for a shape.
func (el *EventLogger) GetComputedState(shapeID string) int32 {
	el.mu.Lock()
	defer el.mu.Unlock()
	if snap, ok := el.cache[shapeID]; ok {
		return snap.ComputedState
	}
	return 0
}
