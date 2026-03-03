// sse.go — Server-Sent Events hub for real-time PLC state broadcasting.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type SSEClient struct {
	ch   chan string
	slug string
}

type BroadcastMsg struct {
	Slug  string // empty = broadcast to all
	Event string // "update" or "state"
	Data  string
}

type Hub struct {
	mu             sync.RWMutex
	clients        map[*SSEClient]bool
	store          *Store
	warlink        *WarlinkClient
	eventLog       *EventLogger
	overlays     map[string]string // screen ID -> overlay text ("BREAK", "CHANGEOVER", etc.), empty = none
	screenActive map[string]bool   // screen ID -> active state; absent = active (default)
}

// NewHub creates a new SSE broadcast hub.
func NewHub(store *Store) *Hub {
	return &Hub{
		clients:      make(map[*SSEClient]bool),
		store:        store,
		overlays:     make(map[string]string),
		screenActive: make(map[string]bool),
	}
}

// LoadScreenState loads persisted screen active state from the store on boot.
func (h *Hub) LoadScreenState() {
	screens := h.store.ListScreens()
	h.mu.Lock()
	for _, sc := range screens {
		if sc.Inactive {
			h.screenActive[sc.ID] = false
		}
	}
	h.mu.Unlock()
}

// SetWarlink sets the WarlinkClient for snapshot support on SSE connect.
func (h *Hub) SetWarlink(wc *WarlinkClient) {
	h.warlink = wc
}

// SetEventLog sets the EventLogger for count session hooks.
func (h *Hub) SetEventLog(el *EventLogger) {
	h.eventLog = el
}

func (h *Hub) GetScreenOverlay(screenID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.overlays[screenID]
}

func (h *Hub) SetScreenOverlay(screenID, text string) {
	h.mu.Lock()
	if text == "" {
		delete(h.overlays, screenID)
	} else {
		h.overlays[screenID] = text
	}
	h.mu.Unlock()
}

func (h *Hub) GetAllOverlays() map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]string, len(h.overlays))
	for k, v := range h.overlays {
		out[k] = v
	}
	return out
}

func (h *Hub) IsScreenActive(screenID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	active, ok := h.screenActive[screenID]
	if !ok {
		return true // default active
	}
	return active
}

func (h *Hub) SetScreenActive(screenID string, active bool) {
	h.mu.Lock()
	if active {
		delete(h.screenActive, screenID) // return to default
	} else {
		h.screenActive[screenID] = false
	}
	h.mu.Unlock()

	h.store.SetScreenInactive(screenID, !active)

	if h.eventLog != nil {
		if active {
			h.eventLog.StartCountSession(screenID)
		} else {
			h.eventLog.StopCountSession(screenID)
		}
	}
}


func (h *Hub) GetAllScreenInactive() map[string]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]bool)
	for k, v := range h.screenActive {
		if !v {
			out[k] = true
		}
	}
	return out
}

func (h *Hub) Register(c *SSEClient) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *SSEClient) {
	h.mu.Lock()
	delete(h.clients, c)
	close(c.ch)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(msg BroadcastMsg) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	formatted := fmt.Sprintf("event: %s\ndata: %s\n\n", msg.Event, msg.Data)
	for c := range h.clients {
		if msg.Slug == "" || c.slug == msg.Slug {
			select {
			case c.ch <- formatted:
			default:
				// drop if buffer full
			}
		}
	}
}

func (h *Hub) ServeSSE(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}

	// Verify screen exists
	if _, ok := h.store.GetScreenBySlug(slug); !ok {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := &SSEClient{
		ch:   make(chan string, 32),
		slug: slug,
	}
	h.Register(client)
	defer h.Unregister(client)

	// Send full global state on every connect (includes andon_active)
	g := h.store.GetGlobal()
	stateJSON, _ := json.Marshal(g)
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", string(stateJSON))
	flusher.Flush()

	// Send current layout on connect
	sc, _ := h.store.GetScreenBySlug(slug)
	fmt.Fprintf(w, "event: update\ndata: %s\n\n", string(sc.Layout))
	flusher.Flush()

	// Fetch current tag values from Warlink and send as cell_data
	if h.warlink != nil {
		for _, cellJSON := range h.warlink.FetchCurrentState(slug) {
			fmt.Fprintf(w, "event: cell_data\ndata: %s\n\n", cellJSON)
		}
		flusher.Flush()
	}

	// Send per-screen overlay on connect
	if ov := h.GetScreenOverlay(sc.ID); ov != "" {
		ovJSON, _ := json.Marshal(struct {
			Active bool   `json:"active"`
			Text   string `json:"text"`
		}{true, ov})
		fmt.Fprintf(w, "event: screen_overlay\ndata: %s\n\n", ovJSON)
		flusher.Flush()
	}

	// Send per-screen active state on connect
	if !h.IsScreenActive(sc.ID) {
		activeJSON, _ := json.Marshal(struct {
			Active bool `json:"active"`
		}{false})
		fmt.Fprintf(w, "event: screen_active\ndata: %s\n\n", activeJSON)
		flusher.Flush()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}

// RunAutoStart watches the shift schedule and activates/deactivates screens
// that have AutoStart enabled on shift transitions. It sleeps until the next
// shift boundary rather than polling, waking up precisely when a shift starts
// or ends. Reloads settings every 5 minutes to pick up config changes.
func (h *Hub) RunAutoStart(ctx context.Context) {
	const maxSleep = 30 * time.Second // reload settings at least this often

	settings := h.store.GetSettings()
	cur := CurrentShift(settings.Shifts, time.Now())
	prevShift := ""
	if cur != nil {
		prevShift = cur.Name
	}

	for {
		settings = h.store.GetSettings()
		sleepDur := nextShiftBoundary(settings.Shifts, time.Now())
		if sleepDur <= 0 || sleepDur > maxSleep {
			sleepDur = maxSleep
		}

		timer := time.NewTimer(sleepDur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		cur = CurrentShift(settings.Shifts, time.Now())
		nowShift := ""
		if cur != nil {
			nowShift = cur.Name
		}

		if nowShift == prevShift {
			continue
		}

		if nowShift != "" && prevShift == "" {
			// Shift started — activate auto-start screens
			log.Printf("[auto-start] shift started: %s", nowShift)
			h.activateAutoScreens(true)
		} else if nowShift == "" && prevShift != "" {
			// Shift ended with no following shift — let overtime accumulate.
			// Screens stay active; operators turn off manually when done.
			log.Printf("[auto-start] shift ended: %s (screens stay active for overtime)", prevShift)
			h.populateShiftSummaries(prevShift)
		} else {
			// Shift changed (A→B) — deactivate then activate
			log.Printf("[auto-start] shift change: %s → %s", prevShift, nowShift)
			h.populateShiftSummaries(prevShift)
			h.activateAutoScreens(false)
			h.activateAutoScreens(true)
		}

		prevShift = nowShift
	}
}

// populateShiftSummaries computes shift_summary rows for the given shift
// across all screens. Runs async to avoid blocking the auto-start loop.
func (h *Hub) populateShiftSummaries(shiftName string) {
	if h.eventLog == nil {
		return
	}
	screens := h.store.ListScreens()
	date := time.Now().Format("2006-01-02")
	for _, sc := range screens {
		screenID := sc.ID
		screenName := sc.Name
		go func() {
			if err := h.eventLog.PopulateShiftSummary(screenID, date, shiftName); err != nil {
				log.Printf("shift summary: %s %s %s: %v", screenName, date, shiftName, err)
			} else {
				log.Printf("shift summary: populated %s %s %s", screenName, date, shiftName)
			}
		}()
	}
}

// nextShiftBoundary returns the duration until the next shift start or end time.
// Returns 0 if no valid shifts are configured.
func nextShiftBoundary(shifts []Shift, now time.Time) time.Duration {
	nowMin := now.Hour()*60 + now.Minute()
	nowSec := now.Second()
	best := time.Duration(0)

	for _, s := range shifts {
		startMin, ok1 := parseHHMM(s.Start)
		endMin, ok2 := parseHHMM(s.End)
		if !ok1 || !ok2 {
			continue
		}
		for _, boundary := range []int{startMin, endMin} {
			// Minutes until this boundary (mod 1440)
			diff := boundary - nowMin
			if diff <= 0 {
				diff += 1440
			}
			// Convert to duration, subtracting current seconds and adding 50ms
			// so we wake just past the boundary, not a hair before it.
			dur := time.Duration(diff)*time.Minute - time.Duration(nowSec)*time.Second + 50*time.Millisecond
			if dur <= 0 {
				dur += 24 * time.Hour
			}
			if best == 0 || dur < best {
				best = dur
			}
		}
	}
	return best
}

func (h *Hub) activateAutoScreens(active bool) {
	screens := h.store.ListScreens()

	// Collect changes under a single lock
	type change struct {
		id   string
		slug string
	}
	var changed []change

	h.mu.Lock()
	for _, sc := range screens {
		if !sc.AutoStart {
			continue
		}
		cur, ok := h.screenActive[sc.ID]
		isActive := !ok || cur // absent = active
		if active == isActive {
			continue
		}
		if active {
			delete(h.screenActive, sc.ID)
		} else {
			h.screenActive[sc.ID] = false
		}
		changed = append(changed, change{id: sc.ID, slug: sc.Slug})
	}
	h.mu.Unlock()

	// Broadcast after releasing the lock
	if len(changed) == 0 {
		return
	}

	// Persist to store
	changedIDs := make([]string, len(changed))
	for i, c := range changed {
		changedIDs[i] = c.id
	}
	h.store.SetScreensInactive(changedIDs, !active)

	activeJSON, _ := json.Marshal(struct {
		Active bool `json:"active"`
	}{active})
	for _, c := range changed {
		h.Broadcast(BroadcastMsg{
			Slug:  c.slug,
			Event: "screen_active",
			Data:  string(activeJSON),
		})
		// Broadcast to dashboard (slug "")
		changeJSON, _ := json.Marshal(struct {
			ScreenID string `json:"screen_id"`
			Active   bool   `json:"active"`
		}{c.id, active})
		h.Broadcast(BroadcastMsg{
			Slug:  "",
			Event: "screen_active_change",
			Data:  string(changeJSON),
		})
		if h.eventLog != nil {
			if active {
				h.eventLog.StartCountSession(c.id)
			} else {
				h.eventLog.StopCountSession(c.id)
			}
		}
	}

}

// ServeDashboardSSE is an SSE endpoint for the dashboard page.
// It uses slug "_dashboard" which receives all slug="" broadcasts (state, screen_active_change).
func (h *Hub) ServeDashboardSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := &SSEClient{
		ch:   make(chan string, 32),
		slug: "", // receives all slug="" broadcasts
	}
	h.Register(client)
	defer h.Unregister(client)

	// Send current global state on connect
	g := h.store.GetGlobal()
	stateJSON, _ := json.Marshal(g)
	fmt.Fprintf(w, "event: state\ndata: %s\n\n", string(stateJSON))
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.ch:
			if !ok {
				return
			}
			fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}
