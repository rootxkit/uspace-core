package alerting

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/cpa"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/sources"
	"github.com/rootxkit/uspace-core/vectors"
	"github.com/rootxkit/uspace-core/zones"
)

// alertLifecycleCases is the number of cases in alert_lifecycle.json.
const alertLifecycleCases = 28

// tolDetail mirrors the header's `tolerance."detail floats"`: the old
// monitor rounded every detail float to 0.1 and the monitor keeps full
// precision. The test compares the unrounded value with the vector's
// rounded one within half a step (plus float slack), which accepts
// exactly the values that round to it in either rounding mode (Python's
// round() is half-even, Go's math.Round half away from zero).
const (
	tolDetail      = 0.1
	tolDetailMatch = tolDetail/2 + 1e-9
)

// extraDetailKeys are detail keys the monitor adds that the vectors
// predate; they are removed before comparing, and their presence is
// checked instead.
var extraDetailKeys = map[string][]string{
	KindConflict: {"los_start_s"},
}

// reasonOverride is a step whose expected clear reason the owner decided
// to change without editing the vector (CLAUDE.md: a vector is law, so
// the override is named, counted and listed in the PR).
type reasonOverride struct {
	caseName string
	step     int
	kind     string
	want     ClearReason // what the vector records
	got      ClearReason // what the monitor does by decision
	why      string
}

// reasonOverrides: plan §11 gap 4 and C-14. The vector records the old
// monitor clearing a disarm as stale; the owner decided a disarm or
// landing clears as landed.
var reasonOverrides = []reasonOverride{{
	caseName: "disarming-clears-as-stale",
	step:     2,
	kind:     KindConflict,
	want:     ClearStale,
	got:      ClearLanded,
	why:      "owner decision (plan §11 gap 4, LESSONS C-14): a disarm clears as landed",
}}

type vAircraft struct {
	ID string `json:"id"`
	// NorthM and EastM are how the case was built (provenance); decoded so
	// the strict decoder passes, never used.
	NorthM              *float64             `json:"north_m"`
	EastM               *float64             `json:"east_m"`
	LatDeg              float64              `json:"lat_deg"`
	LonDeg              float64              `json:"lon_deg"`
	AltAMSLM            *float64             `json:"alt_amsl_m"`
	VN                  float64              `json:"vn"`
	VE                  float64              `json:"ve"`
	VD                  float64              `json:"vd"`
	Flying              *bool                `json:"flying"`
	CapturedAtS         *float64             `json:"captured_at_s"`
	RxAtS               *float64             `json:"rx_at_s"`
	StationClockOffsetS *float64             `json:"station_clock_offset_s"`
	Backlog             bool                 `json:"backlog"`
	Source              *string              `json:"source"`
	Station             *string              `json:"station"`
	AltSource           *string              `json:"alt_source"`
	Identification      *core.Identification `json:"identification"`
	Transmitter         *string              `json:"transmitter"`
	Identified          *bool                `json:"identified"`
}

type vStep struct {
	TS         float64    `json:"t_s"`
	Op         string     `json:"op"`
	Aircraft   *vAircraft `json:"aircraft"`
	SourceType string     `json:"source_type"`
	InstanceID *string    `json:"instance_id"`
	Enabled    *bool      `json:"enabled"`
}

type vConfig struct {
	ClearAfterS      *float64          `json:"clear_after_s"`
	StaleAfterS      *float64          `json:"stale_after_s"`
	NeighbourMaxAgeS *float64          `json:"neighbour_max_age_s"`
	LiveMaxAgeS      *float64          `json:"live_max_age_s"`
	Zones            []json.RawMessage `json:"zones"`
}

type vInput struct {
	Config vConfig `json:"config"`
	Steps  []vStep `json:"steps"`
}

type vAlert struct {
	Kind     string         `json:"kind"`
	Severity string         `json:"severity"`
	Aircraft []string       `json:"aircraft"`
	Detail   map[string]any `json:"detail"`
	Reason   string         `json:"reason"`
}

type vStepExp struct {
	Raised  []vAlert `json:"raised"`
	Cleared []vAlert `json:"cleared"`
}

type vExpected struct {
	PerStep     []vStepExp        `json:"per_step"`
	ActiveAfter []vAlert          `json:"active_after"`
	Counters    map[string]uint64 `json:"counters"`
}

type vPolicy struct {
	TCPAMaxS         float64 `json:"t_cpa_max_s"`
	DHorizontalMinM  float64 `json:"d_horizontal_min_m"`
	DVerticalMinM    float64 `json:"d_vertical_min_m"`
	NeighbourRadiusM float64 `json:"neighbour_radius_m"`
}

// track maps a vector aircraft onto a Track with the description's
// defaults: captured_at_s t_s, rx_at_s captured_at_s, source relay,
// flying true, alt_amsl_m 550, station clock offset 0 (SourceTS =
// captured_at_s + offset, so every sample has one). An absent alt_source
// is geodetic: the old monitor treated every altitude but "pressure" as a
// vertical position, and the vectors' conflict details say
// vertical_separation_known true. Env is GroundNotConfigured with no
// undulation: the vectors' zone limits are AMSL only.
func (a *vAircraft) track(tS float64) Track {
	captured := tS
	if a.CapturedAtS != nil {
		captured = *a.CapturedAtS
	}
	rx := captured
	if a.RxAtS != nil {
		rx = *a.RxAtS
	}
	sourceTS := captured
	if a.StationClockOffsetS != nil {
		sourceTS += *a.StationClockOffsetS
	}
	alt := 550.0
	if a.AltAMSLM != nil {
		alt = *a.AltAMSLM
	}
	flying := true
	if a.Flying != nil {
		flying = *a.Flying
	}
	tr := Track{
		ID:             a.ID,
		Pos:            core.LatLon{LatDeg: a.LatDeg, LonDeg: a.LonDeg},
		AltAMSLM:       &alt,
		AltSource:      core.AltGeodetic,
		VNMS:           a.VN,
		VEMS:           a.VE,
		VDMS:           a.VD,
		Flying:         &flying,
		CapturedAtS:    captured,
		RxAtS:          rx,
		SourceTS:       &sourceTS,
		Backlog:        a.Backlog,
		Source:         "relay",
		Transmitter:    a.Transmitter,
		Identified:     a.Identified,
		Identification: a.Identification,
	}
	if a.AltSource != nil {
		tr.AltSource = core.AltSource(*a.AltSource)
	}
	if a.Source != nil {
		tr.Source = *a.Source
	}
	if a.Station != nil {
		tr.Station = *a.Station
	}
	return tr
}

// configOf builds the monitor configuration of a case from the header
// policy, the description's defaults and the case's overrides.
func configOf(t *testing.T, pol vPolicy, in vConfig) Config {
	t.Helper()
	c := DefaultConfig()
	c.Policy = cpa.Policy{
		TCPAMaxS:         pol.TCPAMaxS,
		DHorizontalMinM:  pol.DHorizontalMinM,
		DVerticalMinM:    pol.DVerticalMinM,
		NeighbourRadiusM: pol.NeighbourRadiusM,
		NeighbourMaxAgeS: 10,
	}
	set := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&c.ClearAfterS, in.ClearAfterS)
	set(&c.StaleAfterS, in.StaleAfterS)
	set(&c.LiveMaxAgeS, in.LiveMaxAgeS)
	set(&c.Policy.NeighbourMaxAgeS, in.NeighbourMaxAgeS)
	for i, raw := range in.Zones {
		gz, problems := ed269.ParseZone(raw, ed269.Limits{})
		if problems != nil {
			t.Fatalf("zones[%d]: ParseZone: %v", i, problems)
		}
		z, err := zones.FromED269(gz)
		if err != nil {
			t.Fatalf("zones[%d]: FromED269: %v", i, err)
		}
		c.Zones = append(c.Zones, z)
	}
	return c
}

// switchState accumulates the switch_source steps of a case into
// published states, one version higher each time.
type switchState struct {
	st sources.State
}

func (s *switchState) apply(typ string, instance *string, enabled bool) sources.State {
	s.st.Epoch = "vectors"
	s.st.Version++
	for i, c := range s.st.Controls {
		if c.SourceType == typ && ptrEq(c.InstanceID, instance) {
			s.st.Controls[i].Enabled = enabled
			return s.clone()
		}
	}
	s.st.Controls = append(s.st.Controls, sources.Control{SourceType: typ, InstanceID: instance, Enabled: enabled})
	return s.clone()
}

func (s *switchState) clone() sources.State {
	out := s.st
	out.Controls = slices.Clone(s.st.Controls)
	return out
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// runSteps feeds a case's steps to a fresh monitor and returns the events
// of each step and the monitor.
func runSteps(t *testing.T, cfg Config, steps []vStep) ([]Events, *Monitor) {
	t.Helper()
	m := NewMonitor(cfg)
	var sw switchState
	out := make([]Events, len(steps))
	for i, s := range steps {
		switch s.Op {
		case "observe":
			if s.Aircraft == nil {
				t.Fatalf("step %d: observe without aircraft", i)
			}
			out[i] = m.Observe(s.Aircraft.track(s.TS), s.TS)
		case "tick":
			out[i] = m.Tick(s.TS)
		case "switch_source":
			if s.Enabled == nil {
				t.Fatalf("step %d: switch_source without enabled", i)
			}
			out[i] = m.SwitchSource(sw.apply(s.SourceType, s.InstanceID, *s.Enabled), s.TS)
		default:
			t.Fatalf("step %d: unknown op %q", i, s.Op)
		}
	}
	return out, m
}

// normalised is an alert in the comparable form: kind, severity, sorted
// aircraft, detail without the keys the vectors predate, reason.
func normalised(t *testing.T, a Alert, reason ClearReason) vAlert {
	t.Helper()
	d := make(map[string]any, len(a.Detail))
	for k, v := range a.Detail {
		d[k] = v
	}
	for _, k := range extraDetailKeys[a.Kind] {
		if _, ok := d[k]; !ok {
			t.Errorf("%s: detail lacks %q", a.Key, k)
		}
		delete(d, k)
	}
	ids := slices.Clone(a.Aircraft)
	slices.Sort(ids)
	return vAlert{Kind: a.Kind, Severity: string(a.Severity), Aircraft: ids, Detail: d, Reason: string(reason)}
}

func detailValueEqual(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && math.Abs(g-w) <= tolDetailMatch
	case []any:
		g, ok := got.([]string)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range g {
			if s, ok := w[i].(string); !ok || s != g[i] {
				return false
			}
		}
		return true
	case nil:
		return got == nil
	}
	return got == want
}

func alertEqual(got, want vAlert) bool {
	if got.Kind != want.Kind || got.Severity != want.Severity || got.Reason != want.Reason ||
		!slices.Equal(got.Aircraft, want.Aircraft) || len(got.Detail) != len(want.Detail) {
		return false
	}
	for k, w := range want.Detail {
		g, ok := got.Detail[k]
		if !ok || !detailValueEqual(g, w) {
			return false
		}
	}
	return true
}

// compareSets compares two alert lists as unordered sets.
func compareSets(t *testing.T, what string, got, want []vAlert) {
	t.Helper()
	used := make([]bool, len(got))
	var missing []vAlert
	for _, w := range want {
		found := false
		for i, g := range got {
			if !used[i] && alertEqual(g, w) {
				used[i], found = true, true
				break
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	var extra []vAlert
	for i, g := range got {
		if !used[i] {
			extra = append(extra, g)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("%s:\n  missing %s\n  unexpected %s", what, show(missing), show(extra))
	}
}

func show(as []vAlert) string {
	if len(as) == 0 {
		return "none"
	}
	parts := make([]string, len(as))
	for i, a := range as {
		parts[i] = fmt.Sprintf("%+v", a)
	}
	return strings.Join(parts, "; ")
}

func TestVectorsAlertLifecycle(t *testing.T) {
	f := vectors.Load(t, "alert_lifecycle.json")
	if tol, ok := f.FloatTolerance("detail floats"); !ok || tol != tolDetail {
		t.Fatalf("header tolerance for detail floats is %v (%v); this test compares with %v", tol, ok, tolDetail)
	}
	if len(f.Cases) != alertLifecycleCases {
		t.Fatalf("alert_lifecycle.json has %d cases, want %d", len(f.Cases), alertLifecycleCases)
	}
	var pol vPolicy
	f.Header(t, "policy", &pol)
	overridesUsed := 0
	ran := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		ran++
		var in vInput
		var exp vExpected
		c.Decode(t, &in, &exp)
		if len(exp.PerStep) != len(in.Steps) {
			t.Fatalf("%d steps, %d expected steps", len(in.Steps), len(exp.PerStep))
		}
		got, m := runSteps(t, configOf(t, pol, in.Config), in.Steps)
		for i, ev := range got {
			var raised, cleared []vAlert
			for _, a := range ev.Raised {
				raised = append(raised, normalised(t, a, ""))
			}
			for _, cl := range ev.Cleared {
				cleared = append(cleared, normalised(t, cl.Alert, cl.Reason))
			}
			want := exp.PerStep[i].Cleared
			for _, o := range reasonOverrides {
				if o.caseName != c.Name || o.step != i {
					continue
				}
				for j := range want {
					if want[j].Kind == o.kind && want[j].Reason == string(o.want) {
						want[j].Reason = string(o.got)
						overridesUsed++
						t.Logf("step %d: expecting %s instead of the vector's %s: %s", i, o.got, o.want, o.why)
					}
				}
			}
			compareSets(t, fmt.Sprintf("step %d raised", i), raised, exp.PerStep[i].Raised)
			compareSets(t, fmt.Sprintf("step %d cleared", i), cleared, want)
		}
		var active []vAlert
		for _, a := range m.Active() {
			active = append(active, normalised(t, a, ""))
		}
		compareSets(t, "active_after", active, exp.ActiveAfter)
		for name, want := range exp.Counters {
			if g := m.Counters().Get(name); g != want {
				t.Errorf("counter %s = %d, want %d", name, g, want)
			}
		}
	})
	// With every case run (no -run filter), every override must have
	// matched exactly once: a vector that changed under one fails here.
	if ran == alertLifecycleCases && overridesUsed != len(reasonOverrides) {
		t.Errorf("%d reason overrides applied, want %d: a vector changed under an override", overridesUsed, len(reasonOverrides))
	}
}

// TestVectorEpoch derives the clock the generator used for the zone
// applicability cases: t_s is seconds since the Unix epoch, so the window
// of zone-stops-applying-clears (10:00 to 11:00 UTC on 2026-10-01) ends
// between its first step (1790852399, inside) and its second
// (1790852401, outside).
func TestVectorEpoch(t *testing.T) {
	end := time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)
	if got := placedTime(1790852400); !got.Equal(end) {
		t.Fatalf("placedTime(1790852400) = %v, want %v", got, end)
	}
	if got := placedTime(1790852399.5); !got.Equal(end.Add(-500 * time.Millisecond)) {
		t.Fatalf("fractional seconds: %v", got)
	}
}
