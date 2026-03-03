// warlink.go — Warlink PLC bridge client with SSE tag subscriptions.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// tagTarget maps a Warlink tag to a shape on a specific screen.
type tagTarget struct {
	ScreenSlug string
	ScreenID   string
	ShapeID    string
	ShapeType  string // "process" or "press"
	TagRole    string // "state", "count", "buffer", "coil", "style", "job_count", "spm", "cat1"-"cat5"
}

// WarlinkClient connects to a Warlink SSE stream, decodes PLC bitfields,
// and broadcasts cell_data events to display clients via the Hub.
type WarlinkClient struct {
	store    *Store
	hub      *Hub
	eventLog *EventLogger

	mu          sync.RWMutex
	index       map[string][]tagTarget // "PLC.tag" -> targets
	enabledTags map[string]bool        // "PLC.tag" keys we auto-enabled in Warlink
}

// NewWarlinkClient creates a new WarlinkClient.
func NewWarlinkClient(store *Store, hub *Hub, eventLog *EventLogger) *WarlinkClient {
	wc := &WarlinkClient{
		store:       store,
		hub:         hub,
		eventLog:    eventLog,
		index:       make(map[string][]tagTarget),
		enabledTags: make(map[string]bool),
	}
	wc.RebuildIndex()
	return wc
}

// RebuildIndex scans all screen layouts and rebuilds the tag → target index.
func (wc *WarlinkClient) RebuildIndex() {
	newIndex := make(map[string][]tagTarget)

	screens := wc.store.ListScreens()
	for _, sc := range screens {
		shapes := ParseLayoutShapes(sc.Layout)
		for _, shape := range shapes {
			if shape.PLC == "" {
				continue
			}

			addTag := func(tag, role string) {
				if tag == "" {
					return
				}
				key := shape.PLC + "." + tag
				newIndex[key] = append(newIndex[key], tagTarget{
					ScreenSlug: sc.Slug,
					ScreenID:   sc.ID,
					ShapeID:    shape.ID,
					ShapeType:  shape.Type,
					TagRole:    role,
				})
			}
			addTag(shape.StateTag, "state")
			addTag(shape.CountTag, "count")
			addTag(shape.BufferTag, "buffer")
			addTag(shape.CoilTag, "coil")
			addTag(shape.StyleTag, "style")
			if shape.Type == "press" {
				addTag(shape.JobCountTag, "job_count")
				addTag(shape.SPMTag, "spm")
				for i, tag := range shape.CatTags {
					addTag(tag, fmt.Sprintf("cat%d", i+1))
				}
			}
		}
	}

	wc.mu.Lock()
	wc.index = newIndex
	wc.mu.Unlock()

	if wc.eventLog != nil {
		wc.eventLog.RebuildMetadata()
	}

	log.Printf("warlink: index rebuilt — %d tag mappings", len(newIndex))

	// Ensure all indexed tags are enabled in Warlink
	go wc.ensureTagsEnabled()
}

// lookupTargets returns the targets for a given PLC.tag key.
func (wc *WarlinkClient) lookupTargets(key string) []tagTarget {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.index[key]
}

// ensureTagsEnabled enables tags needed by the current index and disables
// tags we previously auto-enabled that are no longer needed.
func (wc *WarlinkClient) ensureTagsEnabled() {
	baseURL := wc.store.GetSettings().WarlinkURL
	if baseURL == "" {
		return
	}
	base := strings.TrimRight(baseURL, "/")

	// Build the set of PLC.tag keys currently needed
	subs := wc.sseSubscription() // map[plc][]tag
	nowNeeded := make(map[string]bool)
	for plc, tags := range subs {
		for _, tag := range tags {
			nowNeeded[plc+"."+tag] = true
		}
	}

	// Determine which previously-enabled tags are stale
	wc.mu.RLock()
	prevEnabled := make(map[string]bool, len(wc.enabledTags))
	for k := range wc.enabledTags {
		prevEnabled[k] = true
	}
	wc.mu.RUnlock()

	stale := make(map[string]bool)
	for key := range prevEnabled {
		if !nowNeeded[key] {
			stale[key] = true
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	newEnabled := make(map[string]bool)

	// Enable needed tags that aren't already enabled in Warlink
	for plc, neededTags := range subs {
		// GET /api/{plc}/all-tags to check enabled status
		resp, err := client.Get(base + "/api/" + plc + "/all-tags")
		if err != nil {
			log.Printf("warlink: ensure-tags: fetch all-tags for %s: %v", plc, err)
			continue
		}

		var allTags []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		err = json.NewDecoder(resp.Body).Decode(&allTags)
		resp.Body.Close()
		if err != nil {
			log.Printf("warlink: ensure-tags: decode all-tags for %s: %v", plc, err)
			continue
		}

		tagStatus := make(map[string]bool, len(allTags))
		for _, t := range allTags {
			tagStatus[t.Name] = t.Enabled
		}

		for _, tag := range neededTags {
			key := plc + "." + tag
			enabled, exists := tagStatus[tag]
			if !exists {
				log.Printf("warlink: ensure-tags: tag %s not found in Warlink", key)
				continue
			}
			if enabled {
				// Already enabled — track it if we were the ones who enabled it
				if prevEnabled[key] {
					newEnabled[key] = true
				}
				continue
			}
			// Need to enable it
			if wc.patchTagEnabled(client, base, plc, tag, true) {
				log.Printf("warlink: auto-enabled tag %s", key)
				newEnabled[key] = true
			}
		}
	}

	// Disable stale tags we previously auto-enabled
	for key := range stale {
		dot := strings.IndexByte(key, '.')
		if dot < 0 {
			continue
		}
		plc, tag := key[:dot], key[dot+1:]
		if wc.patchTagEnabled(client, base, plc, tag, false) {
			log.Printf("warlink: auto-disabled tag %s (no longer needed)", key)
		}
	}

	// Update tracked set
	wc.mu.Lock()
	wc.enabledTags = newEnabled
	wc.mu.Unlock()
}

// patchTagEnabled sends PATCH /api/{plc}/tags/{tag} to enable or disable a tag.
func (wc *WarlinkClient) patchTagEnabled(client *http.Client, base, plc, tag string, enabled bool) bool {
	payload := `{"enabled":false}`
	if enabled {
		payload = `{"enabled":true}`
	}
	req, err := http.NewRequest("PATCH", base+"/api/"+plc+"/tags/"+tag, strings.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("warlink: ensure-tags: PATCH %s.%s failed: %v", plc, tag, err)
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("warlink: ensure-tags: PATCH %s.%s returned %d", plc, tag, resp.StatusCode)
		return false
	}
	return true
}

// tagsForSlug returns the set of PLC.tag keys needed for a screen slug.
func (wc *WarlinkClient) tagsForSlug(slug string) map[string][]tagTarget {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	result := make(map[string][]tagTarget)
	for key, targets := range wc.index {
		for _, t := range targets {
			if t.ScreenSlug == slug {
				result[key] = append(result[key], t)
			}
		}
	}
	return result
}

// warlinkTagResponse is the response from Warlink GET /{plc}/tags/{tag}.
type warlinkTagResponse struct {
	PLC   string          `json:"plc"`
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
	Error string          `json:"error"`
}

// FetchCurrentState reads current tag values from Warlink for a screen
// and returns cell_data JSON messages ready to send to the display client.
func (wc *WarlinkClient) FetchCurrentState(slug string) []string {
	baseURL := wc.store.GetSettings().WarlinkURL
	if baseURL == "" {
		return nil
	}
	base := strings.TrimRight(baseURL, "/")

	screenTags := wc.tagsForSlug(slug)
	if len(screenTags) == 0 {
		return nil
	}

	// Group tags by PLC so we can batch: GET /{plc}/tags returns all values
	plcTags := make(map[string]map[string][]tagTarget) // plc -> tag -> targets
	for key, targets := range screenTags {
		dot := strings.IndexByte(key, '.')
		if dot < 0 {
			continue
		}
		plc := key[:dot]
		tag := key[dot+1:]
		if plcTags[plc] == nil {
			plcTags[plc] = make(map[string][]tagTarget)
		}
		plcTags[plc][tag] = targets
	}

	// Accumulate per-shape payloads
	type shapeAcc struct {
		shapeType string
		payload   cellDataPayload
	}
	shapes := make(map[string]*shapeAcc) // shapeID -> accumulated payload

	client := &http.Client{Timeout: 5 * time.Second}

	for plc, tagMap := range plcTags {
		// GET /{plc}/tags — returns map of "plc.tag" -> {value, ...}
		url := base + "/api/" + plc + "/tags"
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("warlink: fetch tags for %s: %v", plc, err)
			continue
		}
		var allTags map[string]warlinkTagResponse
		err = json.NewDecoder(resp.Body).Decode(&allTags)
		resp.Body.Close()
		if err != nil {
			log.Printf("warlink: decode tags for %s: %v", plc, err)
			continue
		}

		for tag, targets := range tagMap {
			// Look up by "plc.tag" key
			tr, ok := allTags[plc+"."+tag]
			if !ok {
				continue
			}
			if tr.Error != "" {
				continue
			}

			// Parse value as int32
			var val int32
			if err := json.Unmarshal(tr.Value, &val); err != nil {
				var fval float64
				if err := json.Unmarshal(tr.Value, &fval); err != nil {
					continue
				}
				val = int32(fval)
			}

			for _, t := range targets {
				acc := shapes[t.ShapeID]
				if acc == nil {
					acc = &shapeAcc{shapeType: t.ShapeType}
					shapes[t.ShapeID] = acc
				}
				applyTagRole(&acc.payload, t.TagRole, val, wc.eventLog, t.ShapeID)
			}
		}
	}

	// Serialize cell_data messages
	msgs := make([]string, 0, len(shapes))
	for shapeID, acc := range shapes {
		msg := cellDataMsg{
			ShapeID: shapeID,
			Type:    acc.shapeType,
			Data:    acc.payload,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		msgs = append(msgs, string(data))
	}
	return msgs
}

// sseSubscription returns the filtered SSE URL query params from the current index.
// Groups tags by PLC for filtered subscriptions.
func (wc *WarlinkClient) sseSubscription() map[string][]string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	plcTags := make(map[string]map[string]bool)
	for key := range wc.index {
		dot := strings.IndexByte(key, '.')
		if dot < 0 {
			continue
		}
		plc := key[:dot]
		tag := key[dot+1:]
		if plcTags[plc] == nil {
			plcTags[plc] = make(map[string]bool)
		}
		plcTags[plc][tag] = true
	}

	result := make(map[string][]string, len(plcTags))
	for plc, tagSet := range plcTags {
		tags := make([]string, 0, len(tagSet))
		for tag := range tagSet {
			tags = append(tags, tag)
		}
		result[plc] = tags
	}
	return result
}

// Run is the main loop: connects to Warlink SSE, reconnects on failure.
func (wc *WarlinkClient) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		settings := wc.store.GetSettings()
		baseURL := settings.WarlinkURL
		if baseURL == "" {
			// No Warlink configured — wait and retry
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}

		err := wc.connectAndStream(ctx, baseURL)
		if err != nil {
			log.Printf("warlink: connection lost: %v", err)
		}

		// Backoff before reconnect
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// connectAndStream opens a single filtered SSE connection for all PLCs/tags.
func (wc *WarlinkClient) connectAndStream(ctx context.Context, baseURL string) error {
	base := strings.TrimRight(baseURL, "/")
	subs := wc.sseSubscription()
	if len(subs) == 0 {
		// No tags to subscribe to — wait for index to be populated
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return fmt.Errorf("no tags in index")
		}
	}

	// Collect all unique PLCs and tags
	plcs := make([]string, 0, len(subs))
	tagSet := make(map[string]bool)
	for plc, tags := range subs {
		plcs = append(plcs, plc)
		for _, tag := range tags {
			tagSet[tag] = true
		}
	}
	allTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		allTags = append(allTags, tag)
	}

	url := base + "/api/events?types=value-change&plcs=" + strings.Join(plcs, ",") + "&tags=" + strings.Join(allTags, ",")
	return wc.streamSSE(ctx, url)
}

// streamSSE connects to a Warlink SSE endpoint and processes events.
func (wc *WarlinkClient) streamSSE(ctx context.Context, url string) error {
	log.Printf("warlink: connecting to %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0} // no timeout for SSE
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	log.Printf("warlink: connected")

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Keepalive comment
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Empty line = dispatch event
		if line == "" {
			if eventType != "" && len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				wc.handleEvent(eventType, data)
			}
			eventType = ""
			dataLines = nil
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return fmt.Errorf("stream ended")
}

// warlinkEvent is the JSON payload for a value-change event.
type warlinkEvent struct {
	PLC   string          `json:"plc"`
	Tag   string          `json:"tag"`
	Value json.RawMessage `json:"value"`
	Type  string          `json:"type"`
}

func (wc *WarlinkClient) handleEvent(eventType, data string) {
	switch eventType {
	case "value-change":
		var ev warlinkEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			log.Printf("warlink: failed to parse value-change: %v", err)
			return
		}

		key := ev.PLC + "." + ev.Tag
		targets := wc.lookupTargets(key)
		if len(targets) == 0 {
			return
		}

		// Parse the value as a number (DINT)
		var val int32
		if err := json.Unmarshal(ev.Value, &val); err != nil {
			// Try as float and truncate
			var fval float64
			if err := json.Unmarshal(ev.Value, &fval); err != nil {
				log.Printf("warlink: cannot parse value for %s: %v", key, err)
				return
			}
			val = int32(fval)
		}

		for _, t := range targets {
			wc.decodeAndBroadcast(t, val)
		}

	case "status-change":
		// quiet — only log on connect/disconnect, not every heartbeat
	}
}

// cellDataMsg is the wire format for cell_data SSE events.
type cellDataMsg struct {
	ShapeID string      `json:"shapeId"`
	Type    string      `json:"type"`
	Data    interface{} `json:"data"`
}

// cellDataPayload sends raw DINT values to the browser.
// Decoding happens client-side to minimize wire bandwidth.
// Only the non-nil field is included (partial updates).
type cellDataPayload struct {
	State         *int32 `json:"state,omitempty"`
	Count         *int32 `json:"count,omitempty"`
	Buffer        *int32 `json:"buffer,omitempty"`
	CoilPct       *int32 `json:"coilPct,omitempty"`
	ComputedState *int32 `json:"computedState,omitempty"`
	JobCount      *int32 `json:"jobCount,omitempty"`
	SPM           *int32 `json:"spm,omitempty"`
	Style         *int32 `json:"style,omitempty"`
	CatID1        *int32 `json:"catId1,omitempty"`
	CatID2        *int32 `json:"catId2,omitempty"`
	CatID3        *int32 `json:"catId3,omitempty"`
	CatID4        *int32 `json:"catId4,omitempty"`
	CatID5        *int32 `json:"catId5,omitempty"`
}

// applyTagRole sets the appropriate field on payload for the given tag role.
// Creates a local copy of val so pointers remain valid.
func applyTagRole(payload *cellDataPayload, role string, val int32, eventLog *EventLogger, shapeID string) {
	v := val
	switch role {
	case "state":
		payload.State = &v
		var computed int32
		if eventLog != nil {
			computed = eventLog.GetComputedState(shapeID)
		}
		payload.ComputedState = &computed
	case "count":
		payload.Count = &v
	case "buffer":
		payload.Buffer = &v
	case "coil":
		payload.CoilPct = &v
	case "job_count":
		payload.JobCount = &v
	case "spm":
		payload.SPM = &v
	case "cat1":
		payload.CatID1 = &v
	case "cat2":
		payload.CatID2 = &v
	case "cat3":
		payload.CatID3 = &v
	case "cat4":
		payload.CatID4 = &v
	case "cat5":
		payload.CatID5 = &v
	case "style":
		payload.Style = &v
	}
}

// InjectEvent constructs a tagTarget directly and calls decodeAndBroadcast,
// bypassing SSE parsing. Used by the simulator for testing the full pipeline.
func (wc *WarlinkClient) InjectEvent(screenSlug, screenID, shapeID, shapeType, tagRole string, val int32) {
	t := tagTarget{
		ScreenSlug: screenSlug,
		ScreenID:   screenID,
		ShapeID:    shapeID,
		ShapeType:  shapeType,
		TagRole:    tagRole,
	}
	wc.decodeAndBroadcast(t, val)
}

func (wc *WarlinkClient) decodeAndBroadcast(t tagTarget, val int32) {
	// Record to event log before broadcasting
	if wc.eventLog != nil {
		wc.eventLog.Record(t.ShapeID, t.TagRole, val)
	}

	var payload cellDataPayload
	applyTagRole(&payload, t.TagRole, val, wc.eventLog, t.ShapeID)

	msg := cellDataMsg{
		ShapeID: t.ShapeID,
		Type:    t.ShapeType,
		Data:    payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("warlink: failed to marshal cell_data: %v", err)
		return
	}

	wc.hub.Broadcast(BroadcastMsg{
		Slug:  t.ScreenSlug,
		Event: "cell_data",
		Data:  string(data),
	})
}
