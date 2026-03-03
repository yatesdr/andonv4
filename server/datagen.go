// datagen.go — Historical data generator for dev/test environments.
// Inserts synthetic data directly into SQLite for past dates.
// Generates realistic state-transition events that feed all report types.
package server

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"
)

// DataGenConfig configures historical data generation.
type DataGenConfig struct {
	ScreenID      string  `json:"screen_id"`
	DateFrom      string  `json:"date_from"`      // YYYY-MM-DD
	DateTo        string  `json:"date_to"`        // YYYY-MM-DD
	CountVariance float64 `json:"count_variance"` // 0-1, fraction of planned (e.g. 0.15 = +/-15%)
	FaultProb     float64 `json:"fault_prob"`     // probability per cycle of fault
	AndonProb     float64 `json:"andon_prob"`     // probability per cycle of andon call
	GenEvents     bool    `json:"gen_events"`     // generate process_events state transitions
	GenSummary    bool    `json:"gen_summary"`    // generate shift_summary rows
}

// DataGenResult reports what was generated.
type DataGenResult struct {
	HourlyRows  int                `json:"hourly_rows"`
	EventRows   int                `json:"event_rows"`
	SummaryRows int                `json:"summary_rows"`
	GroundTruth []GroundTruthEntry `json:"ground_truth"`
}

// GroundTruthEntry records the exact values inserted for verification.
type GroundTruthEntry struct {
	ShapeID   string `json:"shape_id"`
	ShapeName string `json:"shape_name"`
	ShiftName string `json:"shift_name"`
	CountDate string `json:"count_date"`
	ShiftHour int    `json:"shift_hour"`
	Actual    int    `json:"actual"`
	Planned   int    `json:"planned"`
	JobStyle  string `json:"job_style"`
}

// VerifyRow compares expected (ground truth) vs reported (from production API).
type VerifyRow struct {
	ShapeID      string `json:"shape_id"`
	ShapeName    string `json:"shape_name"`
	ShiftName    string `json:"shift_name"`
	CountDate    string `json:"count_date"`
	ShiftHour    int    `json:"shift_hour"`
	ExpActual    int    `json:"exp_actual"`
	ExpPlanned   int    `json:"exp_planned"`
	GotActual    int    `json:"got_actual"`
	GotPlanned   int    `json:"got_planned"`
	ActualMatch  bool   `json:"actual_match"`
	PlannedMatch bool   `json:"planned_match"`
}

// DataGenerator generates synthetic historical data.
type DataGenerator struct {
	eventLog *EventLogger
	store    *Store

	mu          sync.Mutex
	running     bool
	progress    float64
	lastResult  *DataGenResult
	groundTruth []GroundTruthEntry
}

// NewDataGenerator creates a new DataGenerator.
func NewDataGenerator(eventLog *EventLogger, store *Store) *DataGenerator {
	return &DataGenerator{
		eventLog: eventLog,
		store:    store,
	}
}

// Progress returns the current generation progress (0-1).
func (dg *DataGenerator) Progress() (running bool, progress float64) {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	return dg.running, dg.progress
}

// LastResult returns the result of the last generation run.
func (dg *DataGenerator) LastResult() *DataGenResult {
	dg.mu.Lock()
	defer dg.mu.Unlock()
	return dg.lastResult
}

// Generate starts historical data generation. Runs synchronously.
func (dg *DataGenerator) Generate(cfg DataGenConfig) (*DataGenResult, error) {
	dg.mu.Lock()
	if dg.running {
		dg.mu.Unlock()
		return nil, fmt.Errorf("generation already in progress")
	}
	dg.running = true
	dg.progress = 0
	dg.mu.Unlock()

	defer func() {
		dg.mu.Lock()
		dg.running = false
		dg.mu.Unlock()
	}()

	result, err := dg.generate(cfg)
	if err != nil {
		return nil, err
	}

	dg.mu.Lock()
	dg.lastResult = result
	dg.groundTruth = result.GroundTruth
	dg.progress = 1
	dg.mu.Unlock()

	return result, nil
}

func (dg *DataGenerator) generate(cfg DataGenConfig) (*DataGenResult, error) {
	dateFrom, err := time.Parse("2006-01-02", cfg.DateFrom)
	if err != nil {
		return nil, fmt.Errorf("invalid date_from: %w", err)
	}
	dateTo, err := time.Parse("2006-01-02", cfg.DateTo)
	if err != nil {
		return nil, fmt.Errorf("invalid date_to: %w", err)
	}
	if dateFrom.After(dateTo) {
		return nil, fmt.Errorf("date_from must be before date_to")
	}

	sc, ok := dg.store.GetScreen(cfg.ScreenID)
	if !ok {
		return nil, fmt.Errorf("screen not found")
	}
	layoutShapes := ParseLayoutShapes(sc.Layout)
	if len(layoutShapes) == 0 {
		return nil, fmt.Errorf("no shapes in layout")
	}
	settings := dg.store.GetSettings()
	if len(settings.Shifts) == 0 {
		return nil, fmt.Errorf("no shifts configured")
	}

	totalDays := int(dateTo.Sub(dateFrom).Hours()/24) + 1
	totalUnits := totalDays * len(settings.Shifts) * len(layoutShapes)
	doneUnits := 0

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var result DataGenResult
	db := dg.eventLog.DB()

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	hourlyStmt, err := tx.Prepare(`INSERT INTO hourly_counts
		(screen_id, shape_id, shape_type, shift_name, job_style, count_date, hour, shift_hour, delta, planned, style_minutes, break_minutes, is_early, is_overtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)
		ON CONFLICT(screen_id, shape_id, shift_name, job_style, count_date, shift_hour)
		DO UPDATE SET delta=excluded.delta, planned=excluded.planned, style_minutes=excluded.style_minutes`)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("prepare hourly: %w", err)
	}
	defer hourlyStmt.Close()

	var eventStmt *sql.Stmt
	if cfg.GenEvents {
		eventStmt, err = tx.Prepare(`INSERT INTO process_events
			(ts, screen_id, shape_id, trigger_field, state, count, buffer, computed_state, reporting_units, style)
			VALUES (?, ?, ?, ?, ?, 0, 0, 0, '[]', 0)`)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("prepare events: %w", err)
		}
		defer eventStmt.Close()
	}

	var summaryStmt *sql.Stmt
	if cfg.GenSummary {
		summaryStmt, err = tx.Prepare(`INSERT INTO shift_summary
			(screen_id, shape_id, shape_name, shape_type, shift_name, job_style, style_name,
			 count_date, actual, planned, early, overtime,
			 availability, performance, quality, oee,
			 downtime_seconds, downtime_count,
			 operator_entry_seconds, operator_overcycle_seconds, machine_overcycle_seconds,
			 starved_blocked_seconds, tool_change_seconds, tool_change_count,
			 red_rabbit_seconds, scrap_count,
			 andon_prod_seconds, andon_maint_seconds, andon_logistics_seconds,
			 andon_quality_seconds, andon_hr_seconds, andon_emergency_seconds,
			 andon_tooling_seconds, andon_engineering_seconds, andon_controls_seconds,
			 andon_it_seconds, total_work_seconds, style_minutes, computed_at)
			VALUES (?, ?, ?, ?, ?, '', '', ?, ?, ?, 0, 0,
			 ?, ?, 1.0, ?,
			 ?, ?, 0, 0, 0, 0, 0, 0, 0, 0,
			 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, ?, 0, ?)
			ON CONFLICT(screen_id, shape_id, shift_name, job_style, count_date)
			DO UPDATE SET actual=excluded.actual, planned=excluded.planned,
			 availability=excluded.availability, performance=excluded.performance,
			 oee=excluded.oee, downtime_seconds=excluded.downtime_seconds,
			 downtime_count=excluded.downtime_count, total_work_seconds=excluded.total_work_seconds,
			 computed_at=excluded.computed_at`)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("prepare summary: %w", err)
		}
		defer summaryStmt.Close()
	}

	for d := dateFrom; !d.After(dateTo); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")

		for _, shift := range settings.Shifts {
			startMin, ok1 := parseHHMM(shift.Start)
			endMin, ok2 := parseHHMM(shift.End)
			if !ok1 || !ok2 {
				continue
			}

			var shiftDurationMin int
			if endMin > startMin {
				shiftDurationMin = endMin - startMin
			} else {
				shiftDurationMin = (1440 - startMin) + endMin
			}
			maxHours := (shiftDurationMin + 59) / 60

			for _, ls := range layoutShapes {
				cellCfg := sc.CellConfig[ls.ID]
				takt := cellCfg.TaktTime
				if takt <= 0 {
					takt = 30
				}
				manTime := cellCfg.ManTime
				if manTime <= 0 {
					manTime = takt * 0.4
				}
				machineTime := cellCfg.MachineTime
				if machineTime <= 0 {
					machineTime = takt * 0.5
				}

				var shiftActualTotal, shiftPlannedTotal int
				var shiftDowntimeSec float64
				var shiftDowntimeCount int

				for h := 1; h <= maxHours; h++ {
					workMin := 60
					idx := h - 1
					if idx >= 0 && idx < len(shift.Minutes) {
						workMin = shift.Minutes[idx]
					}

					planned := int(float64(workMin) / (takt / 60.0))
					variance := 1.0 + (rng.Float64()*2-1)*cfg.CountVariance
					actual := int(math.Round(float64(planned) * variance))
					if actual < 0 {
						actual = 0
					}

					clockHour := (startMin + (h-1)*60) / 60 % 24

					// Generate realistic state-transition events for this hour
					if cfg.GenEvents && actual > 0 && ls.Type == "process" {
						hourStartMin := startMin + (h-1)*60
						// Base time for this hour
						// Use time.Local + no %24 so Go normalizes midnight rollover for overnight shifts
						baseTime := time.Date(d.Year(), d.Month(), d.Day(),
							hourStartMin/60, hourStartMin%60, 0, 0, time.Local)

						cycleSec := float64(workMin*60) / float64(actual)
						var hourDowntimeSec float64

						emitState := func(ts time.Time, state int32) error {
							_, err := eventStmt.Exec(ts.UTC().Format(time.RFC3339Nano), cfg.ScreenID, ls.ID, "state", state)
							if err == nil {
								result.EventRows++
							}
							return err
						}

						for p := 0; p < actual; p++ {
							cycleOffset := time.Duration(float64(p)*cycleSec*1e9) * time.Nanosecond
							cycleStart := baseTime.Add(cycleOffset)

							stateIdle := int32(1 << 1) // bit 1 = inAuto

							// --- Tool change injection (~every 80 cycles) ---
							if p > 0 && p%80 == 0 {
								tcDur := 120.0 + rng.Float64()*300.0 // 2-7 minutes
								if err := emitState(cycleStart, stateIdle|(1<<BitToolChangeActive)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tDone := cycleStart.Add(time.Duration(tcDur * float64(time.Second)))
								if err := emitState(tDone, stateIdle); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								continue
							}

							// --- Fault injection ---
							if cfg.FaultProb > 0 && rng.Float64() < cfg.FaultProb {
								faultDur := 5.0 + rng.Float64()*25.0
								hourDowntimeSec += faultDur
								shiftDowntimeCount++

								tFault := cycleStart.Add(time.Duration(manTime * 0.5 * float64(time.Second)))
								if err := emitState(tFault, stateIdle|(1<<BitFaulted)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tResume := tFault.Add(time.Duration(faultDur * float64(time.Second)))
								if err := emitState(tResume, stateIdle|(1<<BitInCycle)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tIdle := tResume.Add(100 * time.Millisecond)
								if err := emitState(tIdle, stateIdle); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								continue
							}

							// --- Andon injection (self-contained cycle with merged bits) ---
							if cfg.AndonProb > 0 && rng.Float64() < cfg.AndonProb {
								andonBitList := []int{BitProdAndon, BitMaintAndon, BitLogisticsAndon, BitQualityAndon,
									BitHRAndon, BitEmergencyAndon, BitToolingAndon, BitEngineeringAndon,
									BitControlsAndon, BitITAndon}
								andonBit := andonBitList[rng.Intn(len(andonBitList))]
								andonDur := 10.0 + rng.Float64()*50.0

								// CTE
								tCTE := cycleStart
								stateCTE := stateIdle | (1 << BitClearToEnter)
								if err := emitState(tCTE, stateCTE); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								// LCBroken
								tLCB := tCTE.Add(time.Duration(manTime * 0.3 * float64(time.Second)))
								stateLCB := stateCTE | (1 << BitLCBroken)
								if err := emitState(tLCB, stateLCB); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								// Andon fires (merged with LCB+CTE)
								tAndon := tLCB.Add(500 * time.Millisecond)
								if err := emitState(tAndon, stateLCB|int32(1<<andonBit)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								// Andon cleared
								tClear := tAndon.Add(time.Duration(andonDur * float64(time.Second)))
								if err := emitState(tClear, stateLCB); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								// Machine cycle after andon resolves
								tCS := tClear.Add(time.Duration(manTime * 0.7 * float64(time.Second)))
								if err := emitState(tCS, stateIdle|(1<<BitCycleStart)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tIC := tCS.Add(500 * time.Millisecond)
								if err := emitState(tIC, stateIdle|(1<<BitInCycle)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tDone := tIC.Add(time.Duration(machineTime * float64(time.Second)))
								if err := emitState(tDone, stateIdle); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								continue
							}

							// --- Normal cycle: CTE → lcBroken → cycleStart → inCycle → idle ---

							// Phase 1: clearToEnter rising (man phase begins)
							tCTE := cycleStart
							stateCTE := stateIdle | (1 << BitClearToEnter)
							if err := emitState(tCTE, stateCTE); err != nil {
								tx.Rollback()
								return nil, fmt.Errorf("insert event: %w", err)
							}

							// Phase 2: lcBroken rising (operator breaks light curtain)
							tLCB := tCTE.Add(time.Duration(manTime * 0.3 * float64(time.Second)))
							stateLCB := stateCTE | (1 << BitLCBroken)
							if err := emitState(tLCB, stateLCB); err != nil {
								tx.Rollback()
								return nil, fmt.Errorf("insert event: %w", err)
							}

							// Red rabbit injection (quality hold during man phase)
							if cfg.FaultProb > 0 && rng.Float64() < cfg.FaultProb*0.2 {
								rrDur := 5.0 + rng.Float64()*25.0
								tRR := tLCB.Add(time.Duration(manTime * 0.1 * float64(time.Second)))
								if err := emitState(tRR, stateLCB|(1<<BitRedRabbit)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tRRClear := tRR.Add(time.Duration(rrDur * float64(time.Second)))
								if err := emitState(tRRClear, stateLCB); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
							}

							// Phase 3: cycleStart rising (machine phase begins)
							tCS := tCTE.Add(time.Duration(manTime * float64(time.Second)))
							stateCS := stateIdle | (1 << BitCycleStart)
							if err := emitState(tCS, stateCS); err != nil {
								tx.Rollback()
								return nil, fmt.Errorf("insert event: %w", err)
							}

							// Phase 4: inCycle rising (clear CTE and CS)
							tIC := tCS.Add(500 * time.Millisecond)
							stateIC := stateIdle | (1 << BitInCycle)
							if err := emitState(tIC, stateIC); err != nil {
								tx.Rollback()
								return nil, fmt.Errorf("insert event: %w", err)
							}

							// Starved/blocked injection (extends inCycle beyond machineTime)
							if cfg.FaultProb > 0 && rng.Float64() < cfg.FaultProb*0.3 && machineTime > 0 {
								extraSec := 3.0 + rng.Float64()*7.0
								sbBit := BitStarved
								if rng.Float64() < 0.5 {
									sbBit = BitBlocked
								}
								tSB := tIC.Add(time.Duration((machineTime + 0.5) * float64(time.Second)))
								if err := emitState(tSB, stateIC|int32(1<<sbBit)); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tSBClear := tSB.Add(time.Duration(extraSec * float64(time.Second)))
								if err := emitState(tSBClear, stateIC); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
								tDone := tSBClear.Add(100 * time.Millisecond)
								if err := emitState(tDone, stateIdle); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
							} else {
								// Phase 5: cycle complete, back to idle
								tDone := tIC.Add(time.Duration(machineTime * float64(time.Second)))
								if err := emitState(tDone, stateIdle); err != nil {
									tx.Rollback()
									return nil, fmt.Errorf("insert event: %w", err)
								}
							}
						}

						// Reduce actual count by faulted cycles
						faultedCycles := int(hourDowntimeSec / cycleSec)
						if faultedCycles > 0 && actual > faultedCycles {
							actual -= faultedCycles
						}
						shiftDowntimeSec += hourDowntimeSec
					} else if cfg.FaultProb > 0 && rng.Float64() < cfg.FaultProb {
						// Non-event path: still apply fault reduction to counts
						reduction := 0.2 + rng.Float64()*0.2
						actual = int(float64(actual) * (1 - reduction))
						if actual < 0 {
							actual = 0
						}
					}

					_, err := hourlyStmt.Exec(
						cfg.ScreenID, ls.ID, ls.Type, shift.Name, "",
						dateStr, clockHour, h, actual, planned, workMin, 0,
					)
					if err != nil {
						tx.Rollback()
						return nil, fmt.Errorf("insert hourly: %w", err)
					}
					result.HourlyRows++

					result.GroundTruth = append(result.GroundTruth, GroundTruthEntry{
						ShapeID:   ls.ID,
						ShapeName: ls.Label,
						ShiftName: shift.Name,
						CountDate: dateStr,
						ShiftHour: h,
						Actual:    actual,
						Planned:   planned,
					})

					shiftActualTotal += actual
					shiftPlannedTotal += planned
				}

				// Generate shift summary
				if cfg.GenSummary && shiftPlannedTotal > 0 {
					totalWorkSec := float64(shiftDurationMin * 60)
					if !cfg.GenEvents {
						// Estimate downtime when not generating events
						if cfg.FaultProb > 0 {
							shiftDowntimeCount = int(cfg.FaultProb * float64(maxHours) * 2)
							shiftDowntimeSec = float64(shiftDowntimeCount) * (15 + rng.Float64()*30)
						}
					}
					avail := (totalWorkSec - shiftDowntimeSec) / totalWorkSec
					if avail < 0 {
						avail = 0
					}
					perf := 0.0
					if shiftPlannedTotal > 0 {
						perf = float64(shiftActualTotal) / float64(shiftPlannedTotal)
					}
					oee := avail * perf * 1.0

					_, err := summaryStmt.Exec(
						cfg.ScreenID, ls.ID, ls.Label, ls.Type, shift.Name,
						dateStr, shiftActualTotal, shiftPlannedTotal,
						avail, perf, oee,
						shiftDowntimeSec, shiftDowntimeCount,
						totalWorkSec,
						time.Now().UTC().Format(time.RFC3339),
					)
					if err != nil {
						tx.Rollback()
						return nil, fmt.Errorf("insert summary: %w", err)
					}
					result.SummaryRows++
				}

				doneUnits++
				dg.mu.Lock()
				dg.progress = float64(doneUnits) / float64(totalUnits)
				dg.mu.Unlock()
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	log.Printf("datagen: generated %d hourly rows, %d events, %d summaries for %s",
		result.HourlyRows, result.EventRows, result.SummaryRows, cfg.ScreenID)
	return &result, nil
}

// Verify compares ground truth against production API results.
func (dg *DataGenerator) Verify(screenID string) ([]VerifyRow, error) {
	dg.mu.Lock()
	gt := dg.groundTruth
	dg.mu.Unlock()

	if len(gt) == 0 {
		return nil, fmt.Errorf("no ground truth available — run Generate first")
	}

	dates := make(map[string]bool)
	for _, entry := range gt {
		dates[entry.CountDate] = true
	}

	type rowKey struct {
		shapeID   string
		shiftName string
		countDate string
		shiftHour int
	}
	dbRows := make(map[rowKey]struct{ actual, planned int })

	for date := range dates {
		rows, err := dg.eventLog.QueryHourlyCounts(screenID, date, "", "")
		if err != nil {
			return nil, fmt.Errorf("query hourly counts for %s: %w", date, err)
		}
		for _, r := range rows {
			key := rowKey{r.ShapeID, r.ShiftName, r.CountDate, r.ShiftHour}
			existing := dbRows[key]
			existing.actual += r.Delta
			existing.planned += r.Planned
			dbRows[key] = existing
		}
	}

	var result []VerifyRow
	for _, entry := range gt {
		key := rowKey{entry.ShapeID, entry.ShiftName, entry.CountDate, entry.ShiftHour}
		db := dbRows[key]
		result = append(result, VerifyRow{
			ShapeID:      entry.ShapeID,
			ShapeName:    entry.ShapeName,
			ShiftName:    entry.ShiftName,
			CountDate:    entry.CountDate,
			ShiftHour:    entry.ShiftHour,
			ExpActual:    entry.Actual,
			ExpPlanned:   entry.Planned,
			GotActual:    db.actual,
			GotPlanned:   db.planned,
			ActualMatch:  entry.Actual == db.actual,
			PlannedMatch: entry.Planned == db.planned,
		})
	}

	return result, nil
}

// Clear deletes generated data for a screen and date range.
func (dg *DataGenerator) Clear(screenID, dateFrom, dateTo string) (int64, error) {
	db := dg.eventLog.DB()
	var total int64

	res, err := db.Exec(`DELETE FROM hourly_counts WHERE screen_id=? AND count_date>=? AND count_date<=?`,
		screenID, dateFrom, dateTo)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	total += n

	// Convert local date range to UTC timestamps, +2 days to cover overnight shifts
	fromLocal, _ := time.ParseInLocation("2006-01-02", dateFrom, time.Local)
	toLocal, _ := time.ParseInLocation("2006-01-02", dateTo, time.Local)
	fromUTC := fromLocal.UTC().Format(time.RFC3339Nano)
	toUTC := toLocal.AddDate(0, 0, 2).UTC().Format(time.RFC3339Nano)
	res, err = db.Exec(`DELETE FROM process_events WHERE screen_id=? AND ts>=? AND ts<=?`,
		screenID, fromUTC, toUTC)
	if err != nil {
		return total, err
	}
	n, _ = res.RowsAffected()
	total += n

	res, err = db.Exec(`DELETE FROM shift_summary WHERE screen_id=? AND count_date>=? AND count_date<=?`,
		screenID, dateFrom, dateTo)
	if err != nil {
		return total, err
	}
	n, _ = res.RowsAffected()
	total += n

	dg.mu.Lock()
	dg.groundTruth = nil
	dg.lastResult = nil
	dg.mu.Unlock()

	log.Printf("datagen: cleared %d rows for %s (%s to %s)", total, screenID, dateFrom, dateTo)
	return total, nil
}
