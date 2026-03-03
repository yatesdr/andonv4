// api_devtools.go — API endpoints for the PLC simulator and historical data generator.
package server

import (
	"net/http"
)

// --- Live Simulator ---

func (a *API) SimStart(w http.ResponseWriter, r *http.Request) {
	if a.Simulator == nil {
		writeError(w, http.StatusServiceUnavailable, "simulator not available")
		return
	}
	var cfg SimConfig
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if cfg.ScreenSlug == "" || cfg.ScreenID == "" {
		writeError(w, http.StatusBadRequest, "screen_slug and screen_id required")
		return
	}
	if err := a.Simulator.Start(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) SimStop(w http.ResponseWriter, r *http.Request) {
	if a.Simulator == nil {
		writeError(w, http.StatusServiceUnavailable, "simulator not available")
		return
	}
	var body struct {
		ScreenSlug string `json:"screen_slug"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ScreenSlug == "" {
		writeError(w, http.StatusBadRequest, "screen_slug required")
		return
	}
	a.Simulator.Stop(body.ScreenSlug)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) SimStatus(w http.ResponseWriter, r *http.Request) {
	if a.Simulator == nil {
		writeJSON(w, http.StatusOK, []SimStatus{})
		return
	}
	status := a.Simulator.Status()
	if status == nil {
		status = []SimStatus{}
	}
	writeJSON(w, http.StatusOK, status)
}

// --- Historical Data Generator ---

func (a *API) DataGenGenerate(w http.ResponseWriter, r *http.Request) {
	if a.DataGen == nil {
		writeError(w, http.StatusServiceUnavailable, "data generator not available")
		return
	}
	var cfg DataGenConfig
	if err := readJSON(r, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if cfg.ScreenID == "" || cfg.DateFrom == "" || cfg.DateTo == "" {
		writeError(w, http.StatusBadRequest, "screen_id, date_from, date_to required")
		return
	}

	// Run async
	go func() {
		if _, err := a.DataGen.Generate(cfg); err != nil {
			// Error logged inside Generate
			_ = err
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}

func (a *API) DataGenProgress(w http.ResponseWriter, r *http.Request) {
	if a.DataGen == nil {
		writeJSON(w, http.StatusOK, struct {
			Running  bool    `json:"running"`
			Progress float64 `json:"progress"`
		}{false, 0})
		return
	}
	running, progress := a.DataGen.Progress()
	result := a.DataGen.LastResult()

	writeJSON(w, http.StatusOK, struct {
		Running  bool           `json:"running"`
		Progress float64        `json:"progress"`
		Result   *DataGenResult `json:"result,omitempty"`
	}{running, progress, result})
}

func (a *API) DataGenVerify(w http.ResponseWriter, r *http.Request) {
	if a.DataGen == nil {
		writeError(w, http.StatusServiceUnavailable, "data generator not available")
		return
	}
	var body struct {
		ScreenID string `json:"screen_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ScreenID == "" {
		writeError(w, http.StatusBadRequest, "screen_id required")
		return
	}

	rows, err := a.DataGen.Verify(body.ScreenID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Compute summary
	totalRows := len(rows)
	actualMatches := 0
	plannedMatches := 0
	for _, r := range rows {
		if r.ActualMatch {
			actualMatches++
		}
		if r.PlannedMatch {
			plannedMatches++
		}
	}

	writeJSON(w, http.StatusOK, struct {
		Rows           []VerifyRow `json:"rows"`
		Total          int         `json:"total"`
		ActualMatches  int         `json:"actual_matches"`
		PlannedMatches int         `json:"planned_matches"`
	}{rows, totalRows, actualMatches, plannedMatches})
}

func (a *API) DataGenClear(w http.ResponseWriter, r *http.Request) {
	if a.DataGen == nil {
		writeError(w, http.StatusServiceUnavailable, "data generator not available")
		return
	}
	var body struct {
		ScreenID string `json:"screen_id"`
		DateFrom string `json:"date_from"`
		DateTo   string `json:"date_to"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ScreenID == "" || body.DateFrom == "" || body.DateTo == "" {
		writeError(w, http.StatusBadRequest, "screen_id, date_from, date_to required")
		return
	}

	deleted, err := a.DataGen.Clear(body.ScreenID, body.DateFrom, body.DateTo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Deleted int64 `json:"deleted"`
	}{deleted})
}
