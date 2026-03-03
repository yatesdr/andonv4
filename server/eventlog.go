// eventlog.go — Event recording, flushing, and database lifecycle.
// Core of the EventLogger subsystem; satellite files handle counting
// (eventlog_counts.go) and behind/overcycle computation (eventlog_behind.go).
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS process_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              TEXT    NOT NULL,
    screen_id       TEXT    NOT NULL,
    shape_id        TEXT    NOT NULL,
    trigger_field   TEXT    NOT NULL,
    state           INTEGER NOT NULL DEFAULT 0,
    count           INTEGER NOT NULL DEFAULT 0,
    buffer          INTEGER NOT NULL DEFAULT 0,
    computed_state  INTEGER NOT NULL DEFAULT 0,
    reporting_units TEXT    NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS press_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              TEXT    NOT NULL,
    screen_id       TEXT    NOT NULL,
    shape_id        TEXT    NOT NULL,
    trigger_field   TEXT    NOT NULL,
    state           INTEGER NOT NULL DEFAULT 0,
    count           INTEGER NOT NULL DEFAULT 0,
    job_count       INTEGER NOT NULL DEFAULT 0,
    spm             INTEGER NOT NULL DEFAULT 0,
    coil_pct        INTEGER NOT NULL DEFAULT 0,
    computed_state  INTEGER NOT NULL DEFAULT 0,
    cat_id_1        INTEGER NOT NULL DEFAULT 0,
    cat_id_2        INTEGER NOT NULL DEFAULT 0,
    cat_id_3        INTEGER NOT NULL DEFAULT 0,
    cat_id_4        INTEGER NOT NULL DEFAULT 0,
    cat_id_5        INTEGER NOT NULL DEFAULT 0,
    reporting_units TEXT    NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_process_ts       ON process_events(ts);
CREATE INDEX IF NOT EXISTS idx_process_shape_ts ON process_events(shape_id, ts);
CREATE INDEX IF NOT EXISTS idx_press_ts         ON press_events(ts);
CREATE INDEX IF NOT EXISTS idx_press_shape_ts   ON press_events(shape_id, ts);

CREATE TABLE IF NOT EXISTS hourly_counts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    screen_id       TEXT    NOT NULL,
    shape_id        TEXT    NOT NULL,
    shape_type      TEXT    NOT NULL,
    shift_name      TEXT    NOT NULL,
    job_style       TEXT    NOT NULL DEFAULT '',
    count_date      TEXT    NOT NULL,
    hour            INTEGER NOT NULL,
    shift_hour      INTEGER NOT NULL DEFAULT 0,
    delta           INTEGER NOT NULL DEFAULT 0,
    planned         INTEGER NOT NULL DEFAULT 0,
    style_minutes   INTEGER NOT NULL DEFAULT 0,
    break_minutes   INTEGER NOT NULL DEFAULT 0,
    is_early        INTEGER NOT NULL DEFAULT 0,
    is_overtime     INTEGER NOT NULL DEFAULT 0,
    UNIQUE(screen_id, shape_id, shift_name, job_style, count_date, shift_hour)
);
CREATE INDEX IF NOT EXISTS idx_hourly_screen_date ON hourly_counts(screen_id, count_date);

CREATE TABLE IF NOT EXISTS shift_summary (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    screen_id                   TEXT NOT NULL,
    shape_id                    TEXT NOT NULL,
    shape_name                  TEXT NOT NULL DEFAULT '',
    shape_type                  TEXT NOT NULL,
    shift_name                  TEXT NOT NULL,
    job_style                   TEXT NOT NULL DEFAULT '',
    style_name                  TEXT NOT NULL DEFAULT '',
    count_date                  TEXT NOT NULL,
    actual                      INTEGER NOT NULL DEFAULT 0,
    planned                     INTEGER NOT NULL DEFAULT 0,
    early                       INTEGER NOT NULL DEFAULT 0,
    overtime                    INTEGER NOT NULL DEFAULT 0,
    availability                REAL NOT NULL DEFAULT 0,
    performance                 REAL NOT NULL DEFAULT 0,
    quality                     REAL NOT NULL DEFAULT 0,
    oee                         REAL NOT NULL DEFAULT 0,
    downtime_seconds            REAL NOT NULL DEFAULT 0,
    downtime_count              INTEGER NOT NULL DEFAULT 0,
    operator_entry_seconds      REAL NOT NULL DEFAULT 0,
    operator_overcycle_seconds  REAL NOT NULL DEFAULT 0,
    machine_overcycle_seconds   REAL NOT NULL DEFAULT 0,
    starved_blocked_seconds     REAL NOT NULL DEFAULT 0,
    tool_change_seconds         REAL NOT NULL DEFAULT 0,
    tool_change_count           INTEGER NOT NULL DEFAULT 0,
    red_rabbit_seconds          REAL NOT NULL DEFAULT 0,
    scrap_count                 INTEGER NOT NULL DEFAULT 0,
    andon_prod_seconds          REAL NOT NULL DEFAULT 0,
    andon_maint_seconds         REAL NOT NULL DEFAULT 0,
    andon_logistics_seconds     REAL NOT NULL DEFAULT 0,
    andon_quality_seconds       REAL NOT NULL DEFAULT 0,
    andon_hr_seconds            REAL NOT NULL DEFAULT 0,
    andon_emergency_seconds     REAL NOT NULL DEFAULT 0,
    andon_tooling_seconds       REAL NOT NULL DEFAULT 0,
    andon_engineering_seconds   REAL NOT NULL DEFAULT 0,
    andon_controls_seconds      REAL NOT NULL DEFAULT 0,
    andon_it_seconds            REAL NOT NULL DEFAULT 0,
    total_work_seconds          REAL NOT NULL DEFAULT 0,
    style_minutes               INTEGER NOT NULL DEFAULT 0,
    computed_at                 TEXT NOT NULL,
    UNIQUE(screen_id, shape_id, shift_name, job_style, count_date)
);
CREATE INDEX IF NOT EXISTS idx_summary_screen_date ON shift_summary(screen_id, count_date);
CREATE INDEX IF NOT EXISTS idx_summary_date ON shift_summary(count_date);
`

// shapeSnapshot holds the cached field values for a shape.
type shapeSnapshot struct {
	State         int32
	Count         int32
	Buffer        int32
	CoilPct       int32
	ComputedState int32
	JobCount      int32
	SPM           int32
	Style         int32
	CatID1        int32
	CatID2        int32
	CatID3        int32
	CatID4        int32
	CatID5        int32
}

// shapeMetadata holds layout-derived info about a shape.
type shapeMetadata struct {
	ScreenID       string
	ScreenSlug     string
	ShapeType      string // "process" or "press"
	ReportingUnits []string
}

// behindState tracks per-shape real-time behind computation.
type behindState struct {
	actualParts   int       // cumulative parts since shift start
	expectedParts float64   // cumulative expected (fractional, 1/takt per second)
	currentTakt   float64   // current style's takt time in seconds
	lastTickTime  time.Time // for elapsed-time accumulation
	breakUsedSec  map[int]int // shiftHour -> seconds of break allowance consumed
	lastBehind    bool      // to detect transitions and avoid redundant broadcasts
	firstHourChecked bool  // true once hour-1 target has been evaluated
}

// overcycleTracker tracks per-shape man/machine cycle timing for overcycle detection.
// Phase state machine: idle → man (state.6 ↑) → machine (state.8 ↑) → man (state.6 ↑) → ...
// Any fault/eStop/anomaly → idle. computedState bit 1 = overcycle.
type overcycleTracker struct {
	phase              int       // 0=idle, 1=man, 2=machine
	phaseStart         time.Time
	prevClearToEnter   bool      // previous value of state bit 6
	prevCycleStart     bool      // previous value of state bit 8
	currentManTarget   float64   // target man time in seconds (from active style)
	currentMachineTarget float64 // target machine time in seconds (from active style)
	lastOvercycle      bool      // for transition detection in ticker
}

// pendingEvent is a fully-resolved event ready for batch insert.
type pendingEvent struct {
	ts             time.Time
	screenID       string
	shapeID        string
	shapeType      string
	triggerField   string
	snapshot       shapeSnapshot
	reportingUnits string // JSON array
}

// countSession holds the resolved shift context for a screen's count tracking.
type countSession struct {
	shiftName  string // resolved shift name, or "Unknown"
	countDate  string // YYYY-MM-DD
	shiftStart int    // minutes since midnight
	shiftEnd   int    // minutes since midnight
	overnight  bool   // start > end
	minutes    []int  // per-hour work minutes from shift config
}

// shapeCountState tracks per-shape count deltas for hourly aggregation.
type shapeCountState struct {
	initialized  bool
	lastCount    int32
	lastPartID   int16  // process only
	lastCounter  int16  // process only
	currentStyle string // for style_minutes tracking
	session      *countSession
}

// hourlyCountEvent is a pending hourly count delta for batch insert.
type hourlyCountEvent struct {
	screenID     string
	shapeID      string
	shapeType    string
	shiftName    string
	jobStyle     string
	countDate    string
	hour         int // clock hour 0-23
	shiftHour    int // 1-based shift hour index
	delta        int
	styleMinutes int
	breakMinutes int
	isEarly      bool
	isOvertime   bool
}

// DBStatus holds the event log database status.
type DBStatus struct {
	SizeBytes   int64 `json:"size_bytes"`
	SizeMB      int   `json:"size_mb"`
	Warning     bool  `json:"warning"`
	ProcessRows int64 `json:"process_rows"`
	PressRows   int64 `json:"press_rows"`
}

// Lock ordering: el.mu must NEVER be held when calling hub methods (hub.mu).
// Functions that need both use a three-phase pattern:
//
//	Phase 1 (no lock): read hub state
//	Phase 2 (el.mu held): compute and collect pending broadcasts
//	Phase 3 (no lock): send broadcasts via hub

// EventLogger records PLC state changes to a SQLite database.
type EventLogger struct {
	db     *sql.DB
	dbPath string
	store  *Store
	hub    *Hub

	mu       sync.Mutex
	cache    map[string]*shapeSnapshot // shapeID -> snapshot
	metadata map[string]*shapeMetadata // shapeID -> metadata
	stopped  bool // set on shutdown to reject new events

	eventCh  chan pendingEvent
	hourlyCh chan hourlyCountEvent

	countStates     map[string]*shapeCountState  // shapeID -> count state
	behindStates    map[string]*behindState      // shapeID -> behind state
	overcycleStates map[string]*overcycleTracker // shapeID -> overcycle tracker

	sizeMu      sync.RWMutex
	dbSizeBytes int64
}

// NewEventLogger opens (or creates) the SQLite database and applies the schema.
func NewEventLogger(dbPath string, store *Store, hub *Hub) (*EventLogger, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open db: %w", err)
	}
	// Apply schema (each statement separately for PRAGMAs)
	for _, stmt := range splitStatements(schema) {
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("eventlog: schema exec %q: %w", stmt[:min(len(stmt), 60)], err)
		}
	}

	// Migrations for existing DBs (silently ignored if column already exists)
	db.Exec("ALTER TABLE hourly_counts ADD COLUMN shift_hour INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE hourly_counts ADD COLUMN break_minutes INTEGER NOT NULL DEFAULT 0")
	db.Exec("ALTER TABLE process_events ADD COLUMN style INTEGER NOT NULL DEFAULT 0")

	// Migrate from clock-hour UNIQUE to shift_hour UNIQUE: drop old table so new schema takes effect.
	// Old data is invalidated by the semantic change to shift-relative bucketing.
	var oldUniqueSQL string
	db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='hourly_counts'`).Scan(&oldUniqueSQL)
	if oldUniqueSQL != "" && !strings.Contains(oldUniqueSQL, "count_date, shift_hour)") {
		db.Exec("DROP TABLE hourly_counts")
		for _, stmt := range splitStatements(schema) {
			if strings.Contains(stmt, "hourly_counts") {
				db.Exec(stmt)
			}
		}
		log.Println("eventlog: migrated hourly_counts to shift_hour UNIQUE constraint (old data cleared)")
	}

	el := &EventLogger{
		db:          db,
		dbPath:      dbPath,
		store:       store,
		hub:         hub,
		cache:       make(map[string]*shapeSnapshot),
		metadata:    make(map[string]*shapeMetadata),
		eventCh:     make(chan pendingEvent, 4096),
		hourlyCh:    make(chan hourlyCountEvent, 4096),
		countStates:     make(map[string]*shapeCountState),
		behindStates:    make(map[string]*behindState),
		overcycleStates: make(map[string]*overcycleTracker),
	}
	el.RebuildMetadata()
	return el, nil
}

// splitStatements splits SQL text on semicolons (simple, no quoted semicolons expected).
func splitStatements(s string) []string {
	var stmts []string
	for _, p := range strings.Split(s, ";") {
		if t := strings.TrimSpace(p); t != "" {
			stmts = append(stmts, t)
		}
	}
	return stmts
}

// RebuildMetadata scans all screen layouts and builds the shape metadata map.
func (el *EventLogger) RebuildMetadata() {
	newMeta := make(map[string]*shapeMetadata)

	screens := el.store.ListScreens()
	for _, sc := range screens {
		for _, shape := range ParseLayoutShapes(sc.Layout) {
			newMeta[shape.ID] = &shapeMetadata{
				ScreenID:       sc.ID,
				ScreenSlug:     sc.Slug,
				ShapeType:      shape.Type,
				ReportingUnits: shape.ReportingUnits,
			}
		}
	}

	el.mu.Lock()
	el.metadata = newMeta
	el.mu.Unlock()

	log.Printf("eventlog: metadata rebuilt — %d shapes tracked", len(newMeta))
}

// Record updates the in-memory cache and enqueues an event if the value changed.
func (el *EventLogger) Record(shapeID, tagRole string, val int32) {
	el.mu.Lock()

	meta, ok := el.metadata[shapeID]
	if !ok {
		el.mu.Unlock()
		return
	}

	snap, ok := el.cache[shapeID]
	if !ok {
		snap = &shapeSnapshot{}
		el.cache[shapeID] = snap
	}

	// Update the relevant field and check for change
	changed := false
	switch tagRole {
	case "state":
		// Mask bit 0 (heartbeat) before comparing
		masked := val & ^int32(1<<BitHeartbeat)
		prevMasked := snap.State & ^int32(1<<BitHeartbeat)
		if masked != prevMasked {
			changed = true
		}
		snap.State = val

		// Overcycle edge detection (process shapes only)
		if meta.ShapeType == "process" {
			el.updateOvercycleEdges(snap, shapeID, meta, val)
		}
	case "count":
		if val != snap.Count {
			changed = true
			el.trackCountDelta(shapeID, meta, snap, val)
		}
		snap.Count = val
	case "buffer":
		if val != snap.Buffer {
			changed = true
		}
		snap.Buffer = val
	case "coil":
		if val != snap.CoilPct {
			changed = true
		}
		snap.CoilPct = val
	case "job_count":
		if val != snap.JobCount {
			changed = true
		}
		snap.JobCount = val
	case "spm":
		if val != snap.SPM {
			changed = true
		}
		snap.SPM = val
	case "cat1":
		if val != snap.CatID1 {
			changed = true
		}
		snap.CatID1 = val
	case "cat2":
		if val != snap.CatID2 {
			changed = true
		}
		snap.CatID2 = val
	case "cat3":
		if val != snap.CatID3 {
			changed = true
		}
		snap.CatID3 = val
	case "cat4":
		if val != snap.CatID4 {
			changed = true
		}
		snap.CatID4 = val
	case "cat5":
		if val != snap.CatID5 {
			changed = true
		}
		snap.CatID5 = val
	case "style":
		if val != snap.Style {
			changed = true
			// Update currentStyle for count tracking
			cs := el.countStates[shapeID]
			if cs != nil {
				cs.currentStyle = fmt.Sprintf("%d", val)
			}
			// Update behind takt and overcycle targets for new style
			bs := el.behindStates[shapeID]
			ot := el.overcycleStates[shapeID]
			if bs != nil || ot != nil {
				cellCfg, layoutStyles := el.getShapeConfig(meta.ScreenID, shapeID)
				styleStr := fmt.Sprintf("%d", val)
				if bs != nil {
					if newTakt := resolveStyleTakt(styleStr, cellCfg, layoutStyles); newTakt > 0 {
						bs.currentTakt = newTakt
					}
				}
				if ot != nil {
					ot.currentManTarget, ot.currentMachineTarget = resolveStyleManMachine(styleStr, cellCfg, layoutStyles)
				}
			}
		}
		snap.Style = val
	default:
		el.mu.Unlock()
		return
	}

	if !changed {
		el.mu.Unlock()
		return
	}

	// Take a copy of the snapshot
	snapCopy := *snap
	screenID := meta.ScreenID
	shapeType := meta.ShapeType
	ruJSON, _ := json.Marshal(meta.ReportingUnits)

	el.mu.Unlock()

	// Log events for any individually-active screen, regardless of global toggle
	if !el.hub.IsScreenActive(screenID) {
		return
	}

	ev := pendingEvent{
		ts:             time.Now().UTC(),
		screenID:       screenID,
		shapeID:        shapeID,
		shapeType:      shapeType,
		triggerField:   tagRole,
		snapshot:       snapCopy,
		reportingUnits: string(ruJSON),
	}

	// Hold lock across stopped check and non-blocking send to prevent
	// send-on-closed-channel race during shutdown.
	el.mu.Lock()
	if el.stopped {
		el.mu.Unlock()
		return
	}
	select {
	case el.eventCh <- ev:
	default:
		log.Printf("eventlog: WARNING — event channel full, dropping event for shape %s", shapeID)
	}
	el.mu.Unlock()
}

// updateOvercycleEdges processes state-change edges for overcycle tracking.
// Called with el.mu held.
func (el *EventLogger) updateOvercycleEdges(snap *shapeSnapshot, shapeID string, meta *shapeMetadata, val int32) {
	ot := el.overcycleStates[shapeID]
	if ot == nil {
		ot = &overcycleTracker{
			// Init prev bits from current state to avoid false edges on first update
			prevClearToEnter: val&(1<<BitClearToEnter) != 0,
			prevCycleStart:   val&(1<<BitCycleStart) != 0,
		}
		el.overcycleStates[shapeID] = ot
		// Resolve initial man/machine targets
		cellCfg, layoutStyles := el.getShapeConfig(meta.ScreenID, shapeID)
		ot.currentManTarget, ot.currentMachineTarget = resolveStyleManMachine(
			fmt.Sprintf("%d", snap.Style), cellCfg, layoutStyles)
		return
	}

	faulted := val&(1<<BitFaulted) != 0
	eStop := val&(1<<BitEStop) != 0
	clearToEnter := val&(1<<BitClearToEnter) != 0
	cycleStart := val&(1<<BitCycleStart) != 0
	now := time.Now()

	if faulted || eStop {
		// Fault or E-stop: clear overcycle, go idle
		ot.phase = 0
		ot.lastOvercycle = false
		snap.ComputedState &^= 1 << CsBitOvercycle
	} else {
		// clearToEnter dropped during man phase → safety re-engaged, go idle
		if ot.phase == 1 && !clearToEnter && ot.prevClearToEnter {
			ot.phase = 0
			ot.lastOvercycle = false
			snap.ComputedState &^= 1 << CsBitOvercycle
		}
		// Rising edge on clearToEnter → start man phase
		if clearToEnter && !ot.prevClearToEnter {
			ot.phase = 1
			ot.phaseStart = now
			ot.lastOvercycle = false
			snap.ComputedState &^= 1 << CsBitOvercycle
		}
		// Rising edge on cycleStart → start machine phase
		if cycleStart && !ot.prevCycleStart {
			ot.phase = 2
			ot.phaseStart = now
			ot.lastOvercycle = false
			snap.ComputedState &^= 1 << CsBitOvercycle
		}
	}
	ot.prevClearToEnter = val&(1<<BitClearToEnter) != 0
	ot.prevCycleStart = val&(1<<BitCycleStart) != 0
}

// Run starts the flusher, size-checker, style-minute ticker, behind ticker, and summary sweep goroutines. Blocks until ctx is done.
func (el *EventLogger) Run(ctx context.Context) {
	go el.sizeChecker(ctx)
	go el.styleTimeTicker(ctx)
	go el.behindTicker(ctx)
	go el.summarySweep(ctx)
	el.flusher(ctx)
}

// flusher reads events from the channel and batch-inserts them.
func (el *EventLogger) flusher(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var batch []pendingEvent
	var hourlyBatch []hourlyCountEvent

	flush := func() {
		if len(batch) > 0 {
			if err := el.insertBatch(batch); err != nil {
				log.Printf("eventlog: flush error: %v", err)
			}
			batch = batch[:0]
		}
		if len(hourlyBatch) > 0 {
			if err := el.insertHourlyBatch(hourlyBatch); err != nil {
				log.Printf("eventlog: hourly flush error: %v", err)
			}
			hourlyBatch = hourlyBatch[:0]
		}
	}

	for {
		select {
		case ev := <-el.eventCh:
			batch = append(batch, ev)
			if len(batch) >= 200 {
				flush()
			}
		case hev := <-el.hourlyCh:
			hourlyBatch = append(hourlyBatch, hev)
			if len(hourlyBatch) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			// Stop accepting new events, then drain and flush
			el.mu.Lock()
			el.stopped = true
			el.mu.Unlock()
			close(el.eventCh)
			close(el.hourlyCh)
			for ev := range el.eventCh {
				batch = append(batch, ev)
			}
			for hev := range el.hourlyCh {
				hourlyBatch = append(hourlyBatch, hev)
			}
			flush()
			el.db.Close()
			return
		}
	}
}

// insertBatch writes a slice of events in a single transaction.
func (el *EventLogger) insertBatch(batch []pendingEvent) error {
	tx, err := el.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	procStmt, err := tx.Prepare(`INSERT INTO process_events
		(ts, screen_id, shape_id, trigger_field, state, count, buffer, computed_state, reporting_units, style)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare process: %w", err)
	}
	defer procStmt.Close()

	pressStmt, err := tx.Prepare(`INSERT INTO press_events
		(ts, screen_id, shape_id, trigger_field, state, count, job_count, spm, coil_pct, computed_state,
		 cat_id_1, cat_id_2, cat_id_3, cat_id_4, cat_id_5, reporting_units)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare press: %w", err)
	}
	defer pressStmt.Close()

	for _, ev := range batch {
		ts := ev.ts.Format(time.RFC3339Nano)
		s := ev.snapshot
		switch ev.shapeType {
		case "process":
			_, err = procStmt.Exec(ts, ev.screenID, ev.shapeID, ev.triggerField,
				s.State, s.Count, s.Buffer, s.ComputedState, ev.reportingUnits, s.Style)
		case "press":
			_, err = pressStmt.Exec(ts, ev.screenID, ev.shapeID, ev.triggerField,
				s.State, s.Count, s.JobCount, s.SPM, s.CoilPct, s.ComputedState,
				s.CatID1, s.CatID2, s.CatID3, s.CatID4, s.CatID5, ev.reportingUnits)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert %s event: %w", ev.shapeType, err)
		}
	}

	return tx.Commit()
}

// sizeChecker periodically checks the database file size.
func (el *EventLogger) sizeChecker(ctx context.Context) {
	check := func() {
		info, err := os.Stat(el.dbPath)
		if err != nil {
			return
		}
		el.sizeMu.Lock()
		el.dbSizeBytes = info.Size()
		el.sizeMu.Unlock()
	}
	check() // initial
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// DBStatus returns current database stats.
func (el *EventLogger) DBStatus() DBStatus {
	el.sizeMu.RLock()
	sizeBytes := el.dbSizeBytes
	el.sizeMu.RUnlock()

	var processRows, pressRows int64
	if err := el.db.QueryRow("SELECT COUNT(*) FROM process_events").Scan(&processRows); err != nil {
		log.Printf("eventlog: DBStatus process_events count: %v", err)
	}
	if err := el.db.QueryRow("SELECT COUNT(*) FROM press_events").Scan(&pressRows); err != nil {
		log.Printf("eventlog: DBStatus press_events count: %v", err)
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	return DBStatus{
		SizeBytes:   sizeBytes,
		SizeMB:      sizeMB,
		Warning:     sizeBytes >= 1<<30, // >= 1 GB
		ProcessRows: processRows,
		PressRows:   pressRows,
	}
}

// PruneBefore deletes events older than the given time. Returns total rows deleted.
func (el *EventLogger) PruneBefore(before time.Time) (int64, error) {
	ts := before.UTC().Format(time.RFC3339Nano)
	var total int64

	res, err := el.db.Exec("DELETE FROM process_events WHERE ts < ?", ts)
	if err != nil {
		return 0, fmt.Errorf("prune process_events: %w", err)
	}
	n, _ := res.RowsAffected()
	total += n

	res, err = el.db.Exec("DELETE FROM press_events WHERE ts < ?", ts)
	if err != nil {
		return total, fmt.Errorf("prune press_events: %w", err)
	}
	n, _ = res.RowsAffected()
	total += n

	// Reclaim space
	el.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")

	return total, nil
}

// DB returns the underlying database handle for direct queries (e.g., data generator).
func (el *EventLogger) DB() *sql.DB {
	return el.db
}
