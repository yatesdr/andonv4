// simulator.go — Real-time PLC simulator for dev/test environments.
// Generates synthetic PLC events through the full pipeline via InjectEvent.
package server

import (
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// SimConfig configures a live simulation for a single screen.
type SimConfig struct {
	ScreenSlug      string    `json:"screen_slug"`
	ScreenID        string    `json:"screen_id"`
	TaktMultiplier  float64   `json:"takt_multiplier"`  // multiplier for base takt (1.0 = use configured takt)
	TimeCompression float64   `json:"time_compression"` // speed factor (10 = 10x faster)
	FaultProb       float64   `json:"fault_prob"`       // probability of fault per cycle (0-1)
	AndonProb       float64   `json:"andon_prob"`       // probability of andon per cycle (0-1)
	StyleValues     []int     `json:"style_values"`     // PLC style values to rotate through
	StyleInterval   int       `json:"style_interval"`   // seconds between style changes (real-time)
}

// SimStatus reports the current state of a simulation.
type SimStatus struct {
	ScreenSlug  string `json:"screen_slug"`
	ScreenID    string `json:"screen_id"`
	Running     bool   `json:"running"`
	ShapesActive int   `json:"shapes_active"`
	TotalEvents int64  `json:"total_events"`
	StartedAt   string `json:"started_at,omitempty"`
}

// simShape tracks per-shape state within a simulation.
type simShape struct {
	id        string
	shapeType string
	label     string
	counter   int16
	partID    int16
	state     int32
	styleIdx  int
}

// simInstance manages a single screen's simulation goroutines.
type simInstance struct {
	cfg       SimConfig
	cancel    func()
	events    atomic.Int64
	startedAt time.Time
	shapes    []simShape
}

// Simulator manages live PLC simulations across multiple screens.
type Simulator struct {
	warlink  *WarlinkClient
	store    *Store
	hub      *Hub
	eventLog *EventLogger

	mu        sync.Mutex
	instances map[string]*simInstance // screenSlug -> instance
}

// NewSimulator creates a new Simulator.
func NewSimulator(warlink *WarlinkClient, store *Store, hub *Hub, eventLog *EventLogger) *Simulator {
	return &Simulator{
		warlink:   warlink,
		store:     store,
		hub:       hub,
		eventLog:  eventLog,
		instances: make(map[string]*simInstance),
	}
}

// Start begins a simulation for the given config. Stops any existing sim for that screen.
func (s *Simulator) Start(cfg SimConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing instance if any
	if inst, ok := s.instances[cfg.ScreenSlug]; ok {
		inst.cancel()
		delete(s.instances, cfg.ScreenSlug)
	}

	// Validate and set defaults
	if cfg.TimeCompression <= 0 {
		cfg.TimeCompression = 1
	}
	if cfg.TaktMultiplier <= 0 {
		cfg.TaktMultiplier = 1
	}

	// Ensure screen is active
	if !s.hub.IsScreenActive(cfg.ScreenID) {
		s.hub.SetScreenActive(cfg.ScreenID, true)
	} else {
		// Start count session even if already active (re-init)
		s.eventLog.StartCountSession(cfg.ScreenID)
	}

	// Get shapes from layout
	sc, ok := s.store.GetScreen(cfg.ScreenID)
	if !ok {
		return nil
	}
	layoutShapes := ParseLayoutShapes(sc.Layout)
	if len(layoutShapes) == 0 {
		return nil
	}

	var shapes []simShape
	for _, ls := range layoutShapes {
		shapes = append(shapes, simShape{
			id:        ls.ID,
			shapeType: ls.Type,
			label:     ls.Label,
			partID:    1,
		})
	}

	done := make(chan struct{})
	inst := &simInstance{
		cfg:       cfg,
		cancel:    func() { close(done) },
		startedAt: time.Now(),
		shapes:    shapes,
	}
	s.instances[cfg.ScreenSlug] = inst

	// Launch per-shape goroutines
	for i := range shapes {
		go s.runShape(inst, &inst.shapes[i], done)
	}

	// Launch style rotation if configured
	if len(cfg.StyleValues) > 0 && cfg.StyleInterval > 0 {
		go s.runStyleRotation(inst, done)
	}

	log.Printf("simulator: started for %s (%d shapes, %.0fx compression)",
		cfg.ScreenSlug, len(shapes), cfg.TimeCompression)
	return nil
}

// Stop stops simulation for the given screen slug.
func (s *Simulator) Stop(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if inst, ok := s.instances[slug]; ok {
		inst.cancel()
		delete(s.instances, slug)
		log.Printf("simulator: stopped for %s (%d events injected)", slug, inst.events.Load())
	}
}

// Status returns the status of all simulations.
func (s *Simulator) Status() []SimStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []SimStatus
	for slug, inst := range s.instances {
		out = append(out, SimStatus{
			ScreenSlug:   slug,
			ScreenID:     inst.cfg.ScreenID,
			Running:      true,
			ShapesActive: len(inst.shapes),
			TotalEvents:  inst.events.Load(),
			StartedAt:    inst.startedAt.Format(time.RFC3339),
		})
	}
	return out
}

// compressedSleep sleeps for the given duration divided by the time compression factor.
func compressedSleep(d time.Duration, compression float64, done <-chan struct{}) bool {
	actual := time.Duration(float64(d) / compression)
	if actual < 10*time.Millisecond {
		actual = 10 * time.Millisecond
	}
	select {
	case <-done:
		return false
	case <-time.After(actual):
		return true
	}
}

// runShape runs the state machine for a single shape.
func (s *Simulator) runShape(inst *simInstance, shape *simShape, done <-chan struct{}) {
	cfg := inst.cfg
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(len(shape.id))))

	// Get takt time for this shape
	sc, ok := s.store.GetScreen(cfg.ScreenID)
	if !ok {
		return
	}
	cellCfg := sc.CellConfig[shape.id]
	baseTakt := cellCfg.TaktTime
	if baseTakt <= 0 {
		baseTakt = 30 // default 30s
	}
	taktSeconds := baseTakt * cfg.TaktMultiplier

	manTime := cellCfg.ManTime
	if manTime <= 0 {
		manTime = taktSeconds * 0.4 // 40% of takt
	} else {
		manTime *= cfg.TaktMultiplier
	}
	machineTime := cellCfg.MachineTime
	if machineTime <= 0 {
		machineTime = taktSeconds * 0.5 // 50% of takt
	} else {
		machineTime *= cfg.TaktMultiplier
	}

	inject := func(role string, val int32) {
		s.warlink.InjectEvent(cfg.ScreenSlug, cfg.ScreenID, shape.id, shape.shapeType, role, val)
		inst.events.Add(1)
	}

	setState := func(state int32) {
		shape.state = state
		inject("state", state)
	}

	// Initial state: inAuto
	setState(1 << 1) // bit 1 = inAuto

	for {
		// IDLE: in auto
		setState(1 << 1)
		if !compressedSleep(500*time.Millisecond, cfg.TimeCompression, done) {
			return
		}

		// clearToEnter rising — man phase
		setState(1<<1 | 1<<BitClearToEnter)
		if !compressedSleep(time.Duration(manTime*float64(time.Second)), cfg.TimeCompression, done) {
			return
		}

		// Fault injection check
		if cfg.FaultProb > 0 && rng.Float64() < cfg.FaultProb {
			faultDuration := 5 + rng.Float64()*25 // 5-30 seconds
			setState(1<<1 | 1<<BitFaulted)
			if !compressedSleep(time.Duration(faultDuration*float64(time.Second)), cfg.TimeCompression, done) {
				return
			}
			setState(1 << 1) // clear fault
			continue         // restart cycle
		}

		// Andon injection check
		if cfg.AndonProb > 0 && rng.Float64() < cfg.AndonProb {
			andonBits := []int{BitProdAndon, BitMaintAndon, BitLogisticsAndon, BitQualityAndon,
				BitHRAndon, BitEmergencyAndon, BitToolingAndon, BitEngineeringAndon, BitControlsAndon, BitITAndon}
			andonBit := andonBits[rng.Intn(len(andonBits))]
			andonDuration := 10 + rng.Float64()*50 // 10-60 seconds
			setState(1<<1 | 1<<BitClearToEnter | int32(1<<andonBit))
			if !compressedSleep(time.Duration(andonDuration*float64(time.Second)), cfg.TimeCompression, done) {
				return
			}
			// Clear andon, continue with cycle
		}

		// cycleStart rising — machine phase start
		setState(1<<1 | 1<<BitCycleStart)
		if !compressedSleep(500*time.Millisecond, cfg.TimeCompression, done) {
			return
		}

		// inCycle rising, clear clearToEnter and cycleStart
		setState(1<<1 | 1<<BitInCycle)
		if !compressedSleep(time.Duration(machineTime*float64(time.Second)), cfg.TimeCompression, done) {
			return
		}

		// Count increment
		shape.counter++
		if shape.shapeType == "process" {
			countVal := int32(shape.partID) | (int32(shape.counter) << 16)
			inject("count", countVal)
		} else {
			inject("count", int32(shape.counter))
		}

		// Clear inCycle
		setState(1 << 1)
	}
}

// runStyleRotation periodically rotates the style value for all shapes.
func (s *Simulator) runStyleRotation(inst *simInstance, done <-chan struct{}) {
	cfg := inst.cfg
	interval := time.Duration(cfg.StyleInterval) * time.Second
	styleIdx := 0

	for {
		if !compressedSleep(interval, 1, done) { // style rotation is in real-time
			return
		}

		styleIdx = (styleIdx + 1) % len(cfg.StyleValues)
		styleVal := int32(cfg.StyleValues[styleIdx])

		for i := range inst.shapes {
			shape := &inst.shapes[i]
			if shape.shapeType == "process" {
				shape.partID = int16(styleVal)
				shape.counter = 0 // reset counter on style change
			}
			s.warlink.InjectEvent(cfg.ScreenSlug, cfg.ScreenID, shape.id, shape.shapeType, "style", styleVal)
			inst.events.Add(1)
		}

		log.Printf("simulator: style rotated to %d for %s", styleVal, cfg.ScreenSlug)
	}
}
