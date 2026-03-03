// store.go — Configuration store backed by config.json.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type TargetParams struct {
	ManTime         float64 `json:"man_time,omitempty"`
	MachineTime     float64 `json:"machine_time,omitempty"`
	TaktTime        float64 `json:"takt_time,omitempty"`
	JPH             int     `json:"jph,omitempty"`
	TargetDieChange float64 `json:"target_die_change,omitempty"`
}

type CellParams struct {
	ManTime         float64                `json:"man_time,omitempty"`
	MachineTime     float64                `json:"machine_time,omitempty"`
	TaktTime        float64                `json:"takt_time,omitempty"`
	JPH             int                    `json:"jph,omitempty"`
	TargetDieChange float64                `json:"target_die_change,omitempty"`
	StyleTag        string                 `json:"style_tag,omitempty"`
	Styles          map[string]TargetParams `json:"styles,omitempty"`
}

// layoutStyleDef maps a PLC style tag value to a human-readable style name.
// Used across count tracking, reports, and API responses to resolve style identities.
type layoutStyleDef struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// LayoutShape holds parsed metadata for a process or press shape from a layout.
// Used by warlink indexing, event logging, reports, and the configure page.
type LayoutShape struct {
	ID             string
	Type           string           // "process" or "press"
	Label          string           // human-readable name from the first "label" child
	Styles         []layoutStyleDef // style definitions from the shape config
	ReportingUnits []string
	// PLC tag assignments
	PLC         string
	StateTag    string
	CountTag    string
	BufferTag   string
	CoilTag     string
	StyleTag    string
	JobCountTag string // press only
	SPMTag      string // press only
	CatTags     [5]string
}

// ParseLayoutShapes unmarshals a layout JSON array and returns only the process
// and press shapes with all metadata extracted. Other shape types are skipped.
func ParseLayoutShapes(layout json.RawMessage) []LayoutShape {
	var raw []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Config struct {
			PLC            string           `json:"plc"`
			StateTag       string           `json:"state_tag"`
			CountTag       string           `json:"count_tag"`
			BufferTag      string           `json:"buffer_tag"`
			CoilTag        string           `json:"coil_tag"`
			StyleTag       string           `json:"style_tag"`
			JobCountTag    string           `json:"job_count_tag"`
			SPMTag         string           `json:"spm_tag"`
			CatID1         string           `json:"cat_id_1"`
			CatID2         string           `json:"cat_id_2"`
			CatID3         string           `json:"cat_id_3"`
			CatID4         string           `json:"cat_id_4"`
			CatID5         string           `json:"cat_id_5"`
			Styles         []layoutStyleDef `json:"styles"`
			ReportingUnits []string         `json:"reporting_units"`
			Children       []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"children"`
		} `json:"config"`
	}
	if json.Unmarshal(layout, &raw) != nil {
		return nil
	}

	var out []LayoutShape
	for _, item := range raw {
		if item.Type != "process" && item.Type != "press" {
			continue
		}
		// Derive label from the first "label" child, falling back to truncated ID.
		label := item.ID
		if len(label) > 8 {
			label = label[:8]
		}
		for _, child := range item.Config.Children {
			if child.Type == "label" && child.Text != "" {
				label = child.Text
				break
			}
		}
		ru := item.Config.ReportingUnits
		if ru == nil {
			ru = []string{}
		}
		out = append(out, LayoutShape{
			ID:             item.ID,
			Type:           item.Type,
			Label:          label,
			Styles:         item.Config.Styles,
			ReportingUnits: ru,
			PLC:            item.Config.PLC,
			StateTag:       item.Config.StateTag,
			CountTag:       item.Config.CountTag,
			BufferTag:      item.Config.BufferTag,
			CoilTag:        item.Config.CoilTag,
			StyleTag:       item.Config.StyleTag,
			JobCountTag:    item.Config.JobCountTag,
			SPMTag:         item.Config.SPMTag,
			CatTags:        [5]string{item.Config.CatID1, item.Config.CatID2, item.Config.CatID3, item.Config.CatID4, item.Config.CatID5},
		})
	}
	return out
}

type Screen struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Slug       string                `json:"slug"`
	Layout     json.RawMessage       `json:"layout"`
	CellConfig map[string]CellParams `json:"cell_config"`
	AutoStart  bool                  `json:"auto_start,omitempty"`
	Inactive   bool                  `json:"inactive,omitempty"`
}

type GlobalState struct {
	OverlayActive bool   `json:"overlay_active"`
	OverlayText   string `json:"overlay_text"`
	MuteActive    bool   `json:"mute_active"`
	AndonActive   bool   `json:"andon_active"`
}

type Shift struct {
	Name    string `json:"name"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes []int  `json:"minutes,omitempty"`
}

type BackupSettings struct {
	S3Enabled       bool   `json:"s3_enabled"`
	S3Endpoint      string `json:"s3_endpoint,omitempty"`
	S3Bucket        string `json:"s3_bucket,omitempty"`
	S3Region        string `json:"s3_region,omitempty"`
	S3AccessKey     string `json:"s3_access_key,omitempty"`
	S3SecretKey     string `json:"s3_secret_key,omitempty"`
	S3UseSSL        bool   `json:"s3_use_ssl"`
	CentralEnabled  bool   `json:"central_enabled"`
	CentralURL      string `json:"central_url,omitempty"`
	PeriodicMinutes int    `json:"periodic_minutes,omitempty"`
}

type EMaintSettings struct {
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Username string `json:"username,omitempty"`
}

type Settings struct {
	StationID     string         `json:"station_id"`
	StationName   string         `json:"station_name"`
	WarlinkURL    string         `json:"warlink_url"`
	Shifts        []Shift        `json:"shifts"`
	AdminUser     string         `json:"admin_user"`
	AdminPassHash string         `json:"admin_pass_hash,omitempty"`
	Backup        BackupSettings `json:"backup,omitempty"`
	EMaint        EMaintSettings `json:"emaint,omitempty"`
}

// parseHHMM parses "HH:MM" to minutes since midnight.
func parseHHMM(t string) (int, bool) {
	if len(t) < 5 || t[2] != ':' {
		return 0, false
	}
	h := int(t[0]-'0')*10 + int(t[1]-'0')
	m := int(t[3]-'0')*10 + int(t[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// CurrentShift returns the shift active at the given time, or nil if none.
func CurrentShift(shifts []Shift, now time.Time) *Shift {
	nowMin := now.Hour()*60 + now.Minute()
	for i := range shifts {
		s, ok1 := parseHHMM(shifts[i].Start)
		e, ok2 := parseHHMM(shifts[i].End)
		if !ok1 || !ok2 {
			continue
		}
		if e > s {
			// Normal shift (e.g. 07:00–15:00)
			if nowMin >= s && nowMin < e {
				return &shifts[i]
			}
		} else if e < s {
			// Overnight shift (e.g. 23:00–07:00)
			if nowMin >= s || nowMin < e {
				return &shifts[i]
			}
		}
	}
	return nil
}

// ResolveShift returns the shift that is about to start (within tolerance minutes)
// or the currently active shift. Used for count session initialization.
func ResolveShift(shifts []Shift, now time.Time) *Shift {
	nowMin := now.Hour()*60 + now.Minute()
	const tolerance = 30 // minutes

	// Nearest upcoming shift start within tolerance
	var nearest *Shift
	nearestDist := tolerance + 1
	for i := range shifts {
		startMin, ok := parseHHMM(shifts[i].Start)
		if !ok {
			continue
		}
		dist := (startMin - nowMin + 24*60) % (24 * 60)
		if dist <= tolerance && dist < nearestDist {
			nearestDist = dist
			nearest = &shifts[i]
		}
	}
	if nearest != nil {
		return nearest
	}

	// Fall back to currently-within-shift
	return CurrentShift(shifts, now)
}

// countDateForShift returns the YYYY-MM-DD that a shift's production counts
// should be attributed to. For overnight shifts, this is the date the shift ends.
func countDateForShift(shift *Shift, now time.Time) string {
	startMin, _ := parseHHMM(shift.Start)
	endMin, _ := parseHHMM(shift.End)
	if endMin >= startMin {
		return now.Format("2006-01-02") // normal shift ends same day
	}
	// Overnight: if we're before midnight (>= startMin), date is tomorrow
	if now.Hour()*60+now.Minute() >= startMin {
		return now.AddDate(0, 0, 1).Format("2006-01-02")
	}
	// After midnight (< endMin), already on the end date
	return now.Format("2006-01-02")
}

// shiftHour returns the 1-based shift hour index for a given minute-of-day.
func shiftHour(shiftStartMin, nowMinutes int) int {
	offset := nowMinutes - shiftStartMin
	if offset < 0 {
		offset += 1440
	}
	return offset/60 + 1
}

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(plain string) string {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		// Fallback to SHA-256 if bcrypt fails (should not happen in practice)
		h := sha256.Sum256([]byte(plain))
		return fmt.Sprintf("%x", h)
	}
	return string(b)
}

// CheckPassword verifies a plaintext password against a stored hash.
// Supports both bcrypt and legacy SHA-256 hashes (64-char hex strings).
func CheckPassword(plain, hash string) bool {
	if isLegacyHash(hash) {
		h := sha256.Sum256([]byte(plain))
		return fmt.Sprintf("%x", h) == hash
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// isLegacyHash returns true if the hash is a 64-character hex string (old SHA-256 format).
func isLegacyHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

type Store struct {
	mu     sync.RWMutex `json:"-"`
	path   string       `json:"-"`
	onSave func()       `json:"-"`

	Screens        []Screen        `json:"screens"`
	Global         GlobalState     `json:"global"`
	Settings       Settings        `json:"settings"`
	ReportingUnits []string        `json:"reporting_units,omitempty"`
	VisualMappings json.RawMessage `json:"visual_mappings,omitempty"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a name to a URL-safe lowercase slug.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "screen"
	}
	return s
}

// NewStore initializes the config store from a JSON file at the given path.
func NewStore(path string) (*Store, error) {
	st := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			st.Settings.StationID = generateUUID()
			st.Settings.AdminUser = "admin"
			st.Settings.AdminPassHash = HashPassword("admin")
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}

	if st.Settings.AdminUser == "" {
		st.Settings.AdminUser = "admin"
		st.Settings.AdminPassHash = HashPassword("admin")
		st.save()
	}

	if st.Settings.StationID == "" {
		st.Settings.StationID = generateUUID()
		st.save()
	}

	return st, nil
}

func (s *Store) Path() string       { return s.path }
func (s *Store) SetOnSave(fn func()) { s.onSave = fn }

func (s *Store) save() error {
	if s.Screens == nil {
		s.Screens = []Screen{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	if s.onSave != nil {
		s.onSave()
	}
	return nil
}

func (s *Store) ListScreens() []Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Screen, len(s.Screens))
	copy(out, s.Screens)
	return out
}

func (s *Store) GetScreen(id string) (Screen, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := s.findScreen(id); i >= 0 {
		return s.Screens[i], true
	}
	return Screen{}, false
}

func (s *Store) GetScreenBySlug(slug string) (Screen, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.Screens {
		if sc.Slug == slug {
			return sc, true
		}
	}
	return Screen{}, false
}

func (s *Store) CreateScreen(name string) (Screen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug := Slugify(name)
	// Ensure unique slug
	base := slug
	for i := 2; s.slugExists(slug); i++ {
		slug = fmt.Sprintf("%s-%d", base, i)
	}

	sc := Screen{
		ID:         generateID(),
		Name:       name,
		Slug:       slug,
		Layout:     json.RawMessage("[]"),
		CellConfig: map[string]CellParams{},
	}
	s.Screens = append(s.Screens, sc)
	return sc, s.save()
}

func (s *Store) UpdateScreen(id string, name string) (Screen, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return Screen{}, fmt.Errorf("screen not found")
	}
	if name != "" && name != s.Screens[i].Name {
		s.Screens[i].Name = name
		newSlug := Slugify(name)
		base := newSlug
		for j := 2; s.slugExistsExcept(newSlug, id); j++ {
			newSlug = fmt.Sprintf("%s-%d", base, j)
		}
		s.Screens[i].Slug = newSlug
	}
	return s.Screens[i], s.save()
}

func (s *Store) DeleteScreen(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return fmt.Errorf("screen not found")
	}
	s.Screens = append(s.Screens[:i], s.Screens[i+1:]...)
	return s.save()
}

func (s *Store) SaveLayout(id string, layout json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return fmt.Errorf("screen not found")
	}
	s.Screens[i].Layout = layout
	return s.save()
}

func (s *Store) GetLayout(id string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	i := s.findScreen(id)
	if i < 0 {
		return nil, fmt.Errorf("screen not found")
	}
	cp := make(json.RawMessage, len(s.Screens[i].Layout))
	copy(cp, s.Screens[i].Layout)
	return cp, nil
}

func (s *Store) SetScreenAutoStart(id string, auto bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return fmt.Errorf("screen not found")
	}
	s.Screens[i].AutoStart = auto
	return s.save()
}

func (s *Store) SetScreenInactive(id string, inactive bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return fmt.Errorf("screen not found")
	}
	s.Screens[i].Inactive = inactive
	return s.save()
}

func (s *Store) SetScreensInactive(ids []string, inactive bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for i := range s.Screens {
		if idSet[s.Screens[i].ID] {
			s.Screens[i].Inactive = inactive
		}
	}
	return s.save()
}

func (s *Store) SaveCellConfig(id string, cfg map[string]CellParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.findScreen(id)
	if i < 0 {
		return fmt.Errorf("screen not found")
	}
	s.Screens[i].CellConfig = cfg
	return s.save()
}

func (s *Store) GetGlobal() GlobalState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Global
}

func (s *Store) SetGlobal(g GlobalState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Global = g
	return s.save()
}

func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Settings
}

func (s *Store) SetSettings(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Settings = st
	return s.save()
}

func (s *Store) GetReportingUnits() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.ReportingUnits))
	copy(out, s.ReportingUnits)
	return out
}

func (s *Store) SetReportingUnits(units []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReportingUnits = units
	return s.save()
}

func (s *Store) GetVisualMappings() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.VisualMappings == nil {
		return nil
	}
	cp := make(json.RawMessage, len(s.VisualMappings))
	copy(cp, s.VisualMappings)
	return cp
}

func (s *Store) SetVisualMappings(data json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.VisualMappings = data
	return s.save()
}

func (s *Store) findScreen(id string) int {
	for i, sc := range s.Screens {
		if sc.ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) slugExists(slug string) bool {
	for _, sc := range s.Screens {
		if sc.Slug == slug {
			return true
		}
	}
	return false
}

func (s *Store) slugExistsExcept(slug, excludeID string) bool {
	for _, sc := range s.Screens {
		if sc.Slug == slug && sc.ID != excludeID {
			return true
		}
	}
	return false
}

func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}

func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
