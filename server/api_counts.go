// api_counts.go — GET /api/hourly-counts endpoint and response builders.
package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// GetHourlyCounts returns aggregated hourly production counts for a screen.
func (a *API) GetHourlyCounts(w http.ResponseWriter, r *http.Request) {
	screenID := r.URL.Query().Get("screen")
	if screenID == "" {
		writeError(w, http.StatusBadRequest, "screen parameter required")
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	shapeFilter := r.URL.Query().Get("shape")

	if a.EventLog == nil {
		writeError(w, http.StatusServiceUnavailable, "event log not configured")
		return
	}

	rows, err := a.EventLog.QueryHourlyCounts(screenID, date, shapeFilter, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	shapeInfos := a.buildShapeInfos(screenID)
	settings := a.Store.GetSettings()

	var shiftInfoList []countsShiftInfo
	for _, s := range settings.Shifts {
		shiftInfoList = append(shiftInfoList, countsShiftInfo{
			Name:      s.Name,
			TimeRange: s.Start + " - " + s.End,
		})
	}

	// Group rows by shapeID -> shiftName -> jobStyle
	grouped := make(map[string]*shapeAccum)
	for _, row := range rows {
		sa, ok := grouped[row.ShapeID]
		if !ok {
			sa = &shapeAccum{shifts: make(map[string]*shiftAccum)}
			grouped[row.ShapeID] = sa
		}
		sha, ok := sa.shifts[row.ShiftName]
		if !ok {
			sha = &shiftAccum{styles: make(map[string]*styleAccum)}
			sa.shifts[row.ShiftName] = sha
		}
		sta, ok := sha.styles[row.JobStyle]
		if !ok {
			sta = &styleAccum{hours: make(map[int]*apiHourBucket)}
			sha.styles[row.JobStyle] = sta
		}
		if row.IsEarly {
			sta.early += row.Delta
		} else if row.IsOvertime {
			sta.overtime += row.Delta
		} else {
			hb, ok := sta.hours[row.ShiftHour]
			if !ok {
				hb = &apiHourBucket{ShiftHour: row.ShiftHour}
				sta.hours[row.ShiftHour] = hb
			}
			hb.Actual += row.Delta
			hb.Planned += row.Planned
		}
	}

	// Build operations — layout order first, then remaining shapes
	var operations []apiOperation
	shapesEmitted := make(map[string]bool)

	orderedInfos := orderedShapes(shapeInfos, "")

	for _, si := range orderedInfos {
		sa, ok := grouped[si.ID]
		if !ok {
			continue
		}
		shapesEmitted[si.ID] = true
		op := apiOperation{ShapeID: si.ID, Name: si.Name}
		shiftEmitted := make(map[string]bool)
		for _, sinfo := range shiftInfoList {
			if sha, ok := sa.shifts[sinfo.Name]; ok {
				op.Shifts = append(op.Shifts, buildAPIShiftFromAccum(shapeInfos, shiftInfoList, si.ID, sinfo.Name, sha))
				shiftEmitted[sinfo.Name] = true
			}
		}
		for shiftName, sha := range sa.shifts {
			if !shiftEmitted[shiftName] {
				op.Shifts = append(op.Shifts, buildAPIShiftFromAccum(shapeInfos, shiftInfoList, si.ID, shiftName, sha))
			}
		}
		for _, sh := range op.Shifts {
			op.GrandActual += sh.TotalActual
			op.GrandPlanned += sh.TotalPlanned
		}
		operations = append(operations, op)
	}

	for shapeID, sa := range grouped {
		if shapesEmitted[shapeID] {
			continue
		}
		name := shapeID[:8]
		if len(shapeID) < 8 {
			name = shapeID
		}
		op := apiOperation{ShapeID: shapeID, Name: name}
		for shiftName, sha := range sa.shifts {
			op.Shifts = append(op.Shifts, buildAPIShiftFromAccum(shapeInfos, shiftInfoList, shapeID, shiftName, sha))
		}
		for _, sh := range op.Shifts {
			op.GrandActual += sh.TotalActual
			op.GrandPlanned += sh.TotalPlanned
		}
		operations = append(operations, op)
	}

	var grandActual, grandPlanned int
	for _, op := range operations {
		grandActual += op.GrandActual
		grandPlanned += op.GrandPlanned
	}

	// Build hour columns and apply labels
	hourCols := buildCountsHourColumns(operations, settings)
	labelMap := make(map[int]string)
	for _, col := range hourCols {
		labelMap[col.ShiftHour] = col.Label
	}
	for oi := range operations {
		for si := range operations[oi].Shifts {
			for hi := range operations[oi].Shifts[si].Hours {
				operations[oi].Shifts[si].Hours[hi].Label = labelMap[operations[oi].Shifts[si].Hours[hi].ShiftHour]
			}
			for sti := range operations[oi].Shifts[si].Styles {
				for hi := range operations[oi].Shifts[si].Styles[sti].Hours {
					operations[oi].Shifts[si].Styles[sti].Hours[hi].Label = labelMap[operations[oi].Shifts[si].Styles[sti].Hours[hi].ShiftHour]
				}
			}
		}
	}

	resp := struct {
		Operations        []apiOperation     `json:"operations"`
		GrandTotalActual  int                `json:"grand_total_actual"`
		GrandTotalPlanned int                `json:"grand_total_planned"`
		Hours             []reportHourColumn `json:"hours"`
	}{
		Operations:        operations,
		GrandTotalActual:  grandActual,
		GrandTotalPlanned: grandPlanned,
		Hours:             hourCols,
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- Hourly counts types ---

type apiOperation struct {
	ShapeID      string     `json:"shape_id"`
	Name         string     `json:"name"`
	Shifts       []apiShift `json:"shifts"`
	GrandActual  int        `json:"grand_actual"`
	GrandPlanned int        `json:"grand_planned"`
}

type apiShift struct {
	Name         string          `json:"name"`
	TimeRange    string          `json:"time_range"`
	Styles       []apiStyle      `json:"styles"`
	Early        int             `json:"early"`
	Hours        []apiHourBucket `json:"hours"`
	Overtime     int             `json:"overtime"`
	TotalActual  int             `json:"total_actual"`
	TotalPlanned int             `json:"total_planned"`
}

type apiStyle struct {
	JobStyle     string          `json:"job_style"`
	StyleName    string          `json:"style_name"`
	Early        int             `json:"early"`
	Hours        []apiHourBucket `json:"hours"`
	Overtime     int             `json:"overtime"`
	TotalActual  int             `json:"total_actual"`
	TotalPlanned int             `json:"total_planned"`
}

type apiHourBucket struct {
	ShiftHour int    `json:"shift_hour"`
	Label     string `json:"label"`
	Actual    int    `json:"actual"`
	Planned   int    `json:"planned"`
}

// --- Accumulator types ---

type styleAccum struct {
	early    int
	overtime int
	hours    map[int]*apiHourBucket // shiftHour -> bucket
}

type shiftAccum struct {
	styles map[string]*styleAccum
}

type shapeAccum struct {
	shifts map[string]*shiftAccum
}

type countsShiftInfo struct {
	Name      string
	TimeRange string
}

// --- Builder helpers ---

func sortHourBuckets(hours []apiHourBucket) {
	slices.SortFunc(hours, func(a, b apiHourBucket) int { return a.ShiftHour - b.ShiftHour })
}

// resolveStyleName maps a (shapeID, jobStyle) to a human-readable name.
func resolveStyleName(shapeInfos map[string]*shapeLayoutInfo, shapeID, jobStyle string) string {
	if si, ok := shapeInfos[shapeID]; ok {
		if name, ok := si.StyleNames[jobStyle]; ok {
			return name
		}
	}
	if jobStyle == "" || jobStyle == "0" {
		return "Default"
	}
	return "Style" + jobStyle
}

// buildAPIStyleFromAccum converts a styleAccum to an apiStyle.
func buildAPIStyleFromAccum(shapeInfos map[string]*shapeLayoutInfo, shapeID, jobStyle string, sta *styleAccum) apiStyle {
	s := apiStyle{
		JobStyle:  jobStyle,
		StyleName: resolveStyleName(shapeInfos, shapeID, jobStyle),
		Early:     sta.early,
		Overtime:  sta.overtime,
	}
	for _, hb := range sta.hours {
		s.Hours = append(s.Hours, *hb)
		s.TotalActual += hb.Actual
		s.TotalPlanned += hb.Planned
	}
	s.TotalActual += sta.early + sta.overtime
	sortHourBuckets(s.Hours)
	return s
}

// buildAPIShiftFromAccum converts a shiftAccum to an apiShift.
func buildAPIShiftFromAccum(shapeInfos map[string]*shapeLayoutInfo, shiftInfoList []countsShiftInfo, shapeID, shiftName string, sha *shiftAccum) apiShift {
	sh := apiShift{
		Name: shiftName,
	}
	for _, si := range shiftInfoList {
		if si.Name == shiftName {
			sh.TimeRange = si.TimeRange
			break
		}
	}
	for jobStyle, sta := range sha.styles {
		sh.Styles = append(sh.Styles, buildAPIStyleFromAccum(shapeInfos, shapeID, jobStyle, sta))
	}
	slices.SortFunc(sh.Styles, func(a, b apiStyle) int { return strings.Compare(a.JobStyle, b.JobStyle) })
	shiftHours := make(map[int]*apiHourBucket)
	for _, st := range sh.Styles {
		sh.Early += st.Early
		sh.Overtime += st.Overtime
		sh.TotalActual += st.TotalActual
		sh.TotalPlanned += st.TotalPlanned
		for _, hb := range st.Hours {
			shb, ok := shiftHours[hb.ShiftHour]
			if !ok {
				shb = &apiHourBucket{ShiftHour: hb.ShiftHour, Label: hb.Label}
				shiftHours[hb.ShiftHour] = shb
			}
			shb.Actual += hb.Actual
			shb.Planned += hb.Planned
		}
	}
	for _, hb := range shiftHours {
		sh.Hours = append(sh.Hours, *hb)
	}
	sortHourBuckets(sh.Hours)
	return sh
}

// buildCountsHourColumns builds hour columns for the hourly counts response.
func buildCountsHourColumns(operations []apiOperation, settings Settings) []reportHourColumn {
	dataShifts := make(map[string]bool)
	maxShiftHour := 0
	for _, op := range operations {
		for _, sh := range op.Shifts {
			dataShifts[sh.Name] = true
			for _, hb := range sh.Hours {
				if hb.ShiftHour > maxShiftHour {
					maxShiftHour = hb.ShiftHour
				}
			}
		}
	}

	shiftStartMin := 0
	totalShiftHours := 8
	for _, s := range settings.Shifts {
		if !dataShifts[s.Name] {
			continue
		}
		sm, _ := parseHHMM(s.Start)
		em, _ := parseHHMM(s.End)
		shiftStartMin = sm
		duration := em - sm
		if duration <= 0 {
			duration += 1440
		}
		n := duration / 60
		if duration%60 > 0 {
			n++
		}
		if n > totalShiftHours {
			totalShiftHours = n
		}
	}
	if maxShiftHour > totalShiftHours {
		totalShiftHours = maxShiftHour
	}
	if totalShiftHours < 8 {
		totalShiftHours = 8
	}

	cols := make([]reportHourColumn, totalShiftHours)
	for i := 0; i < totalShiftHours; i++ {
		sm := (shiftStartMin + i*60) % 1440
		cols[i] = reportHourColumn{
			ShiftHour: i + 1,
			Index:     i + 1,
			Label:     fmt.Sprintf("%d:%02d", sm/60, sm%60),
			StartMin:  sm,
		}
	}
	return cols
}
