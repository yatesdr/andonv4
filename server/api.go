// api.go — REST API handlers for screen CRUD, settings, and utility endpoints.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type API struct {
	Store     *Store
	Hub       *Hub
	Auth      *Auth
	Warlink   *WarlinkClient
	EventLog  *EventLogger
	Backup    *BackupManager
	Simulator *Simulator
	DataGen   *DataGenerator
}

// --- Screen CRUD ---

func (a *API) ListScreens(w http.ResponseWriter, r *http.Request) {
	screens := a.Store.ListScreens()
	writeJSON(w, http.StatusOK, screens)
}

func (a *API) CreateScreen(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	sc, err := a.Store.CreateScreen(body.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sc)
}

func (a *API) GetScreen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, ok := a.Store.GetScreen(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (a *API) UpdateScreen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sc, err := a.Store.UpdateScreen(id, body.Name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func (a *API) DeleteScreen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.DeleteScreen(id); err != nil {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	layout, err := a.Store.GetLayout(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(layout)
}

func (a *API) SaveLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20) // 5 MB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	// Validate it's valid JSON array
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err != nil {
		writeError(w, http.StatusBadRequest, "layout must be a JSON array")
		return
	}

	if err := a.Store.SaveLayout(id, json.RawMessage(body)); err != nil {
		http.NotFound(w, r)
		return
	}

	// Broadcast update to display clients
	sc, ok := a.Store.GetScreen(id)
	if ok {
		a.Hub.Broadcast(BroadcastMsg{
			Slug:  sc.Slug,
			Event: "update",
			Data:  string(body),
		})
	}

	// Rebuild Warlink tag index when layouts change
	if a.Warlink != nil {
		go a.Warlink.RebuildIndex()
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetCellConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, ok := a.Store.GetScreen(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, sc.CellConfig)
}

func (a *API) SaveCellConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var cfg map[string]CellParams
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SaveCellConfig(id, cfg); err != nil {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Global State ---

func (a *API) GetGlobal(w http.ResponseWriter, r *http.Request) {
	g := a.Store.GetGlobal()
	writeJSON(w, http.StatusOK, g)
}

func (a *API) SetGlobal(w http.ResponseWriter, r *http.Request) {
	var g GlobalState
	if err := readJSON(r, &g); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetGlobal(g); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Broadcast state to ALL display clients
	data, _ := json.Marshal(g)
	a.Hub.Broadcast(BroadcastMsg{
		Slug:  "", // all screens
		Event: "state",
		Data:  string(data),
	})

	w.WriteHeader(http.StatusNoContent)
}

// --- Screen Toggles ---

func (a *API) ToggleScreenOverlay(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, ok := a.Store.GetScreen(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil || body.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	current := a.Hub.GetScreenOverlay(id)
	active := current != body.Text // toggle: if same text, turn off
	if active {
		a.Hub.SetScreenOverlay(id, body.Text)
	} else {
		a.Hub.SetScreenOverlay(id, "")
	}

	overlayMsg := struct {
		Active bool   `json:"active"`
		Text   string `json:"text"`
	}{active, body.Text}
	data, _ := json.Marshal(overlayMsg)
	a.Hub.Broadcast(BroadcastMsg{
		Slug:  sc.Slug,
		Event: "screen_overlay",
		Data:  string(data),
	})

	writeJSON(w, http.StatusOK, struct {
		Active bool   `json:"active"`
		Text   string `json:"text"`
	}{active, body.Text})
}

func (a *API) ToggleScreenActive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, ok := a.Store.GetScreen(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.Hub.SetScreenActive(id, body.Active)

	activeMsg := struct {
		Active bool `json:"active"`
	}{body.Active}
	data, _ := json.Marshal(activeMsg)
	a.Hub.Broadcast(BroadcastMsg{
		Slug:  sc.Slug,
		Event: "screen_active",
		Data:  string(data),
	})

	// Broadcast to dashboard
	changeMsg := struct {
		ScreenID string `json:"screen_id"`
		Active   bool   `json:"active"`
	}{id, body.Active}
	changeData, _ := json.Marshal(changeMsg)
	a.Hub.Broadcast(BroadcastMsg{
		Slug:  "",
		Event: "screen_active_change",
		Data:  string(changeData),
	})

	writeJSON(w, http.StatusOK, struct {
		Active bool `json:"active"`
	}{body.Active})
}

func (a *API) ToggleAutoStart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := a.Store.GetScreen(id); !ok {
		http.NotFound(w, r)
		return
	}
	var body struct {
		AutoStart bool `json:"auto_start"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetScreenAutoStart(id, body.AutoStart); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		AutoStart bool `json:"auto_start"`
	}{body.AutoStart})
}

// --- Settings ---

func (a *API) GetSettings(w http.ResponseWriter, r *http.Request) {
	s := a.Store.GetSettings()
	// Strip sensitive fields from API response
	s.AdminPassHash = ""
	s.Backup.S3SecretKey = ""
	s.EMaint.APIKey = ""
	writeJSON(w, http.StatusOK, s)
}

func (a *API) SetSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := readJSON(r, &s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Preserve immutable fields
	cur := a.Store.GetSettings()
	s.StationID = cur.StationID
	s.AdminUser = cur.AdminUser
	s.AdminPassHash = cur.AdminPassHash
	// Preserve stored secret key if incoming value is empty (was stripped from GET)
	if s.Backup.S3SecretKey == "" {
		s.Backup.S3SecretKey = cur.Backup.S3SecretKey
	}
	// Preserve stored E-Maintenance API key if incoming value is empty
	if s.EMaint.APIKey == "" {
		s.EMaint.APIKey = cur.EMaint.APIKey
	}
	if err := a.Store.SetSettings(s); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Warlink ---

func (a *API) TestWarlink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(body.URL)
	if err != nil {
		writeJSON(w, http.StatusOK, struct {
			Online bool   `json:"online"`
			Error  string `json:"error"`
		}{false, err.Error()})
		return
	}
	resp.Body.Close()
	writeJSON(w, http.StatusOK, struct {
		Online bool `json:"online"`
	}{true})
}

// WarlinkPlcs proxies GET /api/ from Warlink to avoid CORS in the browser.
func (a *API) WarlinkPlcs(w http.ResponseWriter, r *http.Request) {
	baseURL := a.Store.GetSettings().WarlinkURL
	if baseURL == "" {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	url := strings.TrimRight(baseURL, "/") + "/api/"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// WarlinkTags proxies GET /api/{plc}/all-tags from Warlink.
// Returns all known tags (including disabled) as [{name, type}].
func (a *API) WarlinkTags(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	baseURL := a.Store.GetSettings().WarlinkURL
	if baseURL == "" {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	url := strings.TrimRight(baseURL, "/") + "/api/" + plcName + "/all-tags"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	var tags []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

// --- Auth ---

func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	settings := a.Store.GetSettings()
	if !CheckPassword(body.CurrentPassword, settings.AdminPassHash) {
		writeError(w, http.StatusForbidden, "current password is incorrect")
		return
	}

	if body.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "new password is required")
		return
	}
	settings.AdminPassHash = HashPassword(body.NewPassword)
	if err := a.Store.SetSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Reporting Units & Visual Mappings ---

func (a *API) GetReportingUnits(w http.ResponseWriter, r *http.Request) {
	units := a.Store.GetReportingUnits()
	if units == nil {
		units = []string{}
	}
	writeJSON(w, http.StatusOK, units)
}

func (a *API) SetReportingUnits(w http.ResponseWriter, r *http.Request) {
	var units []string
	if err := readJSON(r, &units); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetReportingUnits(units); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetVisualMappings(w http.ResponseWriter, r *http.Request) {
	data := a.Store.GetVisualMappings()
	w.Header().Set("Content-Type", "application/json")
	if data == nil {
		w.Write([]byte("null"))
	} else {
		w.Write(data)
	}
}

func (a *API) SetVisualMappings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20) // 5 MB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := a.Store.SetVisualMappings(json.RawMessage(body)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Event Log ---

func (a *API) GetEventLogStatus(w http.ResponseWriter, r *http.Request) {
	if a.EventLog == nil {
		writeError(w, http.StatusServiceUnavailable, "event log not configured")
		return
	}
	writeJSON(w, http.StatusOK, a.EventLog.DBStatus())
}

func (a *API) PruneEventLog(w http.ResponseWriter, r *http.Request) {
	if a.EventLog == nil {
		writeError(w, http.StatusServiceUnavailable, "event log not configured")
		return
	}
	var body struct {
		Before string `json:"before"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := time.Parse(time.RFC3339, body.Before)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'before' timestamp (RFC3339 required)")
		return
	}
	deleted, err := a.EventLog.PruneBefore(t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Deleted int64 `json:"deleted"`
	}{deleted})
}

// --- Shift Summary ---

func (a *API) GetShiftSummary(w http.ResponseWriter, r *http.Request) {
	screenID := r.URL.Query().Get("screen")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	shift := r.URL.Query().Get("shift")
	shape := r.URL.Query().Get("shape")

	if screenID == "" {
		writeError(w, http.StatusBadRequest, "screen required")
		return
	}
	today := time.Now().Format("2006-01-02")
	if from == "" {
		from = today
	}
	if to == "" {
		to = today
	}

	rows, err := a.EventLog.QueryShiftSummary(screenID, from, to, shift, shape)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *API) RecomputeShiftSummary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Screen string `json:"screen"`
		From   string `json:"from"`
		To     string `json:"to"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Screen == "" || body.From == "" || body.To == "" {
		writeError(w, http.StatusBadRequest, "screen, from, to required")
		return
	}

	count, err := a.EventLog.PopulateAllSummaries(body.Screen, body.From, body.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"shifts_computed": count})
}

// --- Backup ---

func (a *API) GetBackupStatus(w http.ResponseWriter, r *http.Request) {
	if a.Backup == nil {
		writeJSON(w, http.StatusOK, struct {
			Enabled bool `json:"enabled"`
		}{false})
		return
	}
	writeJSON(w, http.StatusOK, a.Backup.Status())
}

func (a *API) TriggerBackup(w http.ResponseWriter, r *http.Request) {
	if a.Backup == nil {
		writeError(w, http.StatusServiceUnavailable, "backup not configured")
		return
	}
	a.Backup.TriggerFull()
	w.WriteHeader(http.StatusAccepted)
}

// --- Shared types ---

type shapeLayoutInfo struct {
	ID           string
	Name         string
	Type         string
	Order        int
	StyleNames   map[string]string  // plcValue -> styleName
	LayoutStyles []layoutStyleDef   // raw style defs for target resolution
}

// orderedShapes returns shape infos sorted by layout order, optionally filtered to a single shape.
// Pass shapeFilter="" to include all shapes.
func orderedShapes(infos map[string]*shapeLayoutInfo, shapeFilter string) []*shapeLayoutInfo {
	var out []*shapeLayoutInfo
	for _, si := range infos {
		if shapeFilter != "" && si.ID != shapeFilter {
			continue
		}
		out = append(out, si)
	}
	slices.SortFunc(out, func(a, b *shapeLayoutInfo) int { return a.Order - b.Order })
	return out
}

// buildShapeInfos extracts shape names, order, and style name mappings from layout.
func (a *API) buildShapeInfos(screenID string) map[string]*shapeLayoutInfo {
	return buildShapeInfosFromStore(a.Store, screenID)
}

// buildShapeInfosFromStore extracts shape layout info from a store. Usable outside API handlers.
func buildShapeInfosFromStore(store *Store, screenID string) map[string]*shapeLayoutInfo {
	sc, ok := store.GetScreen(screenID)
	if !ok {
		return map[string]*shapeLayoutInfo{}
	}

	shapes := ParseLayoutShapes(sc.Layout)
	result := make(map[string]*shapeLayoutInfo, len(shapes))
	for i, s := range shapes {
		styleNames := make(map[string]string, len(s.Styles))
		for _, sd := range s.Styles {
			styleNames[fmt.Sprintf("%d", sd.Value)] = sd.Name
		}
		result[s.ID] = &shapeLayoutInfo{
			ID:           s.ID,
			Name:         s.Label,
			Type:         s.Type,
			Order:        i,
			StyleNames:   styleNames,
			LayoutStyles: s.Styles,
		}
	}
	return result
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response: {"error": "message"}.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{msg})
}

func readJSON(r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 2<<20) // 2 MB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// --- Work Orders (stub) ---

func (a *API) SubmitWorkOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Equipment   string `json:"equipment"`
		Priority    string `json:"priority"`
		WorkType    string `json:"work_type"`
		Description string `json:"description"`
		RequestedBy string `json:"requested_by"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Equipment == "" {
		writeError(w, http.StatusBadRequest, "equipment is required")
		return
	}
	if body.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	if body.RequestedBy == "" {
		writeError(w, http.StatusBadRequest, "requested_by is required")
		return
	}

	woNumber := "WO-" + generateID()
	writeJSON(w, http.StatusCreated, struct {
		WONumber    string `json:"wo_number"`
		Status      string `json:"status"`
		Message     string `json:"message"`
		Equipment   string `json:"equipment"`
		Priority    string `json:"priority"`
		WorkType    string `json:"work_type"`
		Description string `json:"description"`
		RequestedBy string `json:"requested_by"`
		CreatedAt   string `json:"created_at"`
	}{
		WONumber:    woNumber,
		Status:      "submitted",
		Message:     "Work order created (offline mode — E-Maintenance integration pending)",
		Equipment:   body.Equipment,
		Priority:    body.Priority,
		WorkType:    body.WorkType,
		Description: body.Description,
		RequestedBy: body.RequestedBy,
		CreatedAt:   time.Now().Format(time.RFC3339),
	})
}

func (a *API) TestEMaint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Online  bool   `json:"online"`
		Message string `json:"message"`
	}{false, "E-Maintenance API integration not yet implemented. Configure settings now and connect when API documentation is available."})
}
