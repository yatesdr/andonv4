// handlers.go — HTTP page handlers for dashboard, designer, display, and settings pages.
package server

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	Store     *Store
	Templates *template.Template
	Auth      *Auth
	Hub       *Hub
}

// NavData is embedded in page data structs for the nav-bar partial.
type NavData struct {
	ActiveTab      string
	PageLabel      string
	PageLabelStyle string
	LoggedIn       bool
	LoginNext      string
}

// SimpleBarData is embedded in page data structs for the simple-bar partial.
type SimpleBarData struct {
	Title      string
	ShowLogout bool
}

func (h *Handlers) render(w http.ResponseWriter, name string, data interface{}) {
	if err := h.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

type dashboardScreen struct {
	ID        string
	Name      string
	Slug      string
	Layout    string // JSON as string, not []byte
	Overlay   string // current per-screen overlay text, empty = none
	Active    bool   // per-screen active state
	AutoStart bool   // auto-start/stop with shift schedule
}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	screens := h.Store.ListScreens()
	global := h.Store.GetGlobal()

	overlays := h.Hub.GetAllOverlays()
	inactive := h.Hub.GetAllScreenInactive()
	ds := make([]dashboardScreen, len(screens))
	for i, sc := range screens {
		ds[i] = dashboardScreen{
			ID:        sc.ID,
			Name:      sc.Name,
			Slug:      sc.Slug,
			Layout:    string(sc.Layout),
			Overlay:   overlays[sc.ID],
			Active:    !inactive[sc.ID],
			AutoStart: sc.AutoStart,
		}
	}

	loggedIn := h.Auth != nil && h.Auth.IsLoggedIn(r)

	data := struct {
		NavData
		Screens []dashboardScreen
		Global  GlobalState
	}{
		NavData: NavData{ActiveTab: "dashboard", PageLabel: "ANDON", PageLabelStyle: "font-style:italic; font-family:'Arial Black',Impact,sans-serif;", LoggedIn: loggedIn, LoginNext: "/"},
		Screens: ds,
		Global:  global,
	}

	h.render(w, "dashboard.html", data)
}

func (h *Handlers) Designer(w http.ResponseWriter, r *http.Request) {
	screenID := r.URL.Query().Get("screen")

	data := struct {
		ScreenID   string
		ScreenName string
		Layout     template.JS
		WarlinkURL string
	}{}
	data.WarlinkURL = h.Store.GetSettings().WarlinkURL

	if screenID != "" {
		sc, ok := h.Store.GetScreen(screenID)
		if ok {
			data.ScreenID = sc.ID
			data.ScreenName = sc.Name
			data.Layout = template.JS(sc.Layout)
		}
	}

	h.render(w, "designer.html", data)
}

func (h *Handlers) Display(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	sc, ok := h.Store.GetScreenBySlug(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}

	data := struct {
		Screen Screen
		Layout template.JS
	}{
		Screen: sc,
		Layout: template.JS(sc.Layout),
	}

	h.render(w, "display.html", data)
}

func (h *Handlers) Settings(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil && !h.Auth.IsLoggedIn(r) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}

	settings := h.Store.GetSettings()
	settings.AdminPassHash = ""
	settings.Backup.S3SecretKey = ""
	settingsJSON, _ := json.Marshal(settings)

	data := struct {
		NavData
		SettingsJSON template.JS
	}{
		NavData:      NavData{ActiveTab: "settings", PageLabel: "Settings", LoggedIn: true},
		SettingsJSON: template.JS(settingsJSON),
	}

	h.render(w, "settings.html", data)
}

func (h *Handlers) Counts(w http.ResponseWriter, r *http.Request) {
	loggedIn := h.Auth != nil && h.Auth.IsLoggedIn(r)
	screens := h.Store.ListScreens()
	screenID := r.URL.Query().Get("screen")
	if screenID == "" && len(screens) > 0 {
		screenID = screens[0].ID
	}

	data := struct {
		NavData
		Screens      []Screen
		ActiveScreen string
		Date         string
	}{
		NavData:      NavData{ActiveTab: "counts", PageLabel: "Production Counts", LoggedIn: loggedIn, LoginNext: "/counts"},
		Screens:      screens,
		ActiveScreen: screenID,
		Date:         r.URL.Query().Get("date"),
	}
	h.render(w, "counts.html", data)
}

func (h *Handlers) Reports(w http.ResponseWriter, r *http.Request) {
	loggedIn := h.Auth != nil && h.Auth.IsLoggedIn(r)
	screens := h.Store.ListScreens()
	screenID := r.URL.Query().Get("screen")
	if screenID == "" && len(screens) > 0 {
		screenID = screens[0].ID
	}

	data := struct {
		NavData
		Screens      []Screen
		ActiveScreen string
		Date         string
	}{
		NavData:      NavData{ActiveTab: "reports", PageLabel: "Shift Reports", LoggedIn: loggedIn, LoginNext: "/reports"},
		Screens:      screens,
		ActiveScreen: screenID,
		Date:         r.URL.Query().Get("date"),
	}
	h.render(w, "reports.html", data)
}

func (h *Handlers) WorkOrders(w http.ResponseWriter, r *http.Request) {
	loggedIn := h.Auth != nil && h.Auth.IsLoggedIn(r)
	screens := h.Store.ListScreens()

	// Build equipment list from all screens' layout shapes
	var equipment []string
	seen := map[string]bool{}
	for _, sc := range screens {
		shapes := ParseLayoutShapes(sc.Layout)
		for _, s := range shapes {
			if !seen[s.Label] {
				seen[s.Label] = true
				equipment = append(equipment, s.Label)
			}
		}
	}

	equipJSON, _ := json.Marshal(equipment)

	data := struct {
		NavData
		EquipmentJSON template.JS
	}{
		NavData:       NavData{ActiveTab: "workorders", PageLabel: "Work Orders", LoggedIn: loggedIn, LoginNext: "/workorders"},
		EquipmentJSON: template.JS(equipJSON),
	}
	h.render(w, "workorders.html", data)
}

func (h *Handlers) VisualMappings(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil && !h.Auth.IsLoggedIn(r) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}

	h.render(w, "visual-mappings.html", struct{ NavData }{
		NavData{ActiveTab: "mappings", PageLabel: "Bit Mappings", LoggedIn: true},
	})
}

func (h *Handlers) DevTools(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil && !h.Auth.IsLoggedIn(r) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}

	screens := h.Store.ListScreens()
	screensJSON, _ := json.Marshal(screens)

	data := struct {
		NavData
		ScreensJSON template.JS
	}{
		NavData:     NavData{ActiveTab: "devtools", PageLabel: "Dev Tools", LoggedIn: true},
		ScreensJSON: template.JS(screensJSON),
	}

	h.render(w, "devtools.html", data)
}

func (h *Handlers) Configure(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, ok := h.Store.GetScreen(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Build shape list with labels and styles for the template.
	type shapeInfoJSON struct {
		ID     string           `json:"id"`
		Type   string           `json:"type"`
		Label  string           `json:"label"`
		Styles []layoutStyleDef `json:"styles"`
	}
	parsed := ParseLayoutShapes(sc.Layout)
	shapes := make([]shapeInfoJSON, 0, len(parsed))
	for _, ls := range parsed {
		shapes = append(shapes, shapeInfoJSON{
			ID:     ls.ID,
			Type:   ls.Type,
			Label:  ls.Label,
			Styles: ls.Styles,
		})
	}

	shapesJSON, _ := json.Marshal(shapes)
	cellConfigJSON, _ := json.Marshal(sc.CellConfig)
	if cellConfigJSON == nil {
		cellConfigJSON = []byte("{}")
	}

	data := struct {
		SimpleBarData
		Screen         Screen
		ShapesJSON     template.JS
		CellConfigJSON template.JS
	}{
		SimpleBarData:  SimpleBarData{Title: "Configure: " + sc.Name, ShowLogout: true},
		Screen:         sc,
		ShapesJSON:     template.JS(shapesJSON),
		CellConfigJSON: template.JS(cellConfigJSON),
	}

	h.render(w, "configure.html", data)
}
