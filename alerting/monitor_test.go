package alerting

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/sources"
	"github.com/rootxkit/uspace-core/vectors"
	"github.com/rootxkit/uspace-core/zones"
)

// The test field is the vectors' origin; positions are offsets from it.
const (
	originLatDeg = 41.7151
	originLonDeg = 44.8271
	// metresPerDegLat is close enough at 41.7 N for offsets that stay far
	// from every threshold.
	metresPerDegLat = 111_050.0
)

func ptr[T any](v T) *T { return &v }

// at returns a flying geodetic track northM metres north of the origin,
// captured, received and stamped at tS, moving north at vnMS.
func at(id string, northM, vnMS, tS float64) Track {
	return Track{
		ID:          id,
		Pos:         core.LatLon{LatDeg: originLatDeg + northM/metresPerDegLat, LonDeg: originLonDeg},
		AltAMSLM:    ptr(550.0),
		AltSource:   core.AltGeodetic,
		VNMS:        vnMS,
		Flying:      ptr(true),
		CapturedAtS: tS,
		RxAtS:       tS,
		SourceTS:    ptr(tS),
		Source:      "relay",
	}
}

func kinds(ev Events) (raised, cleared []string) {
	for _, a := range ev.Raised {
		raised = append(raised, a.Key)
	}
	for i := range ev.Cleared {
		cleared = append(cleared, ev.Cleared[i].Key+"="+string(ev.Cleared[i].Reason))
	}
	return raised, cleared
}

func wantEvents(t *testing.T, what string, ev Events, raised, cleared []string) {
	t.Helper()
	r, c := kinds(ev)
	if !slices.Equal(r, raised) || !slices.Equal(c, cleared) {
		t.Fatalf("%s: raised %v cleared %v; want raised %v cleared %v", what, r, c, raised, cleared)
	}
}

// headOn returns a monitor with A and B head-on 500 m apart at t=0 and the
// conflict raised.
func headOn(t *testing.T, cfg Config) *Monitor {
	t.Helper()
	m := NewMonitor(cfg)
	m.Observe(at("A", 0, 10, 0), 0)
	ev := m.Observe(at("B", 500, -10, 0), 0)
	wantEvents(t, "head-on", ev, []string{"conflict:A:B"}, nil)
	return m
}

func polygonZone(t *testing.T, id, restriction, extra string) *zones.Zone {
	t.Helper()
	if extra == "" {
		extra = `"lowerVerticalReference":"AMSL","upperVerticalReference":"AMSL",`
	}
	raw := fmt.Sprintf(`{"identifier":%q,"country":"GEO","type":"COMMON","restriction":%q,
		"applicability":[{"permanent":"YES"}],"zoneAuthority":[],
		"geometry":[{"uomDimensions":"M",%s
		"horizontalProjection":{"type":"Polygon","coordinates":[[[44.8171,41.7051],[44.8371,41.7051],[44.8371,41.7251],[44.8171,41.7251],[44.8171,41.7051]]]}}]}`,
		id, restriction, extra)
	gz, problems := ed269.ParseZone([]byte(raw), ed269.Limits{})
	if problems != nil {
		t.Fatalf("ParseZone: %v", problems)
	}
	z, err := zones.FromED269(gz)
	if err != nil {
		t.Fatalf("FromED269: %v", err)
	}
	return z
}

// --- admission: every refusal and its accepted twin (E-01) ---

func TestAdmissionPairs(t *testing.T) {
	type tc struct {
		name    string
		mutate  func(*Track)
		wallS   float64
		counter string
	}
	cases := []tc{
		{"backlog", func(tr *Track) { tr.Backlog = true }, 0, CounterRejectedBacklog},
		{"late", func(tr *Track) { tr.RxAtS = -10.5 }, 0, CounterRejectedLate},
		{"empty id", func(tr *Track) { tr.ID = "" }, 0, CounterRejectedInvalid},
		{"NaN captured", func(tr *Track) { tr.CapturedAtS = math.NaN() }, 0, CounterRejectedInvalid},
		{"Inf rx", func(tr *Track) { tr.RxAtS = math.Inf(1) }, 0, CounterRejectedInvalid},
		{"NaN source ts", func(tr *Track) { tr.SourceTS = ptr(math.NaN()) }, 0, CounterRejectedInvalid},
		{"NaN wall", func(*Track) {}, math.NaN(), CounterRejectedInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewMonitor(DefaultConfig())
			m.Observe(at("A", 0, 10, 0), 0)
			b := at("B", 500, -10, 0)
			c.mutate(&b)
			ev := m.Observe(b, c.wallS)
			wantEvents(t, "refused", ev, nil, nil)
			if got := m.Counters().Get(c.counter); got != 1 {
				t.Fatalf("%s = %d, want 1", c.counter, got)
			}
			// The twin: the same sample, admitted, raises.
			ev = m.Observe(at("B", 500, -10, 0), 0)
			wantEvents(t, "admitted", ev, []string{"conflict:A:B"}, nil)
		})
	}
}

func TestLateBoundIsOnTheReceiveLegOnly(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	a := at("A", 0, 10, 110)
	a.RxAtS = 110 // exactly at the bound: live
	m.Observe(a, 120)
	b := at("B", 500, -10, 110)
	b.RxAtS = 110
	b.SourceTS = ptr(-1e9) // a station clock decades behind costs nothing
	ev := m.Observe(b, 120)
	wantEvents(t, "at the bound", ev, []string{"conflict:A:B"}, nil)
	if got := m.Counters().Get(CounterRejectedLate); got != 0 {
		t.Fatalf("rejected_late = %d", got)
	}
}

func TestOutOfOrderPair(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 10, 1), 1)
	older := at("A", -10, 10, 0.5)
	wantEvents(t, "older", m.Observe(older, 2), nil, nil)
	if got := m.Counters().Get(CounterRejectedOutOfOrder); got != 1 {
		t.Fatalf("rejected_out_of_order = %d", got)
	}
	// Older on its source clock but received later: admitted.
	later := at("A", -10, 10, 1.5)
	later.SourceTS = ptr(0.5)
	m.Observe(later, 2)
	// Another station's clock is never compared with this one's.
	other := at("A", -10, 10, 1.5)
	other.Station = "gs-2"
	other.SourceTS = ptr(-100.0)
	m.Observe(other, 2)
	if got := m.Counters().Get(CounterRejectedOutOfOrder); got != 1 {
		t.Fatalf("rejected_out_of_order = %d after admitted samples", got)
	}
}

func TestSourceOrderBound(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSourcesPerAircraft = 2
	m := NewMonitor(cfg)
	for i, st := range []string{"s1", "s2", "s3"} {
		tr := at("A", 0, 0, float64(i))
		tr.Station = st
		m.Observe(tr, float64(i))
	}
	if got := m.Counters().Get(CounterSourceOrderEvicted); got != 1 {
		t.Fatalf("source_order_evicted = %d, want 1", got)
	}
	if n := len(m.aircraft["A"].order); n != 2 {
		t.Fatalf("%d entries, want 2", n)
	}
	// s1 was evicted: its old sample is no longer ordered against it.
	tr := at("A", 0, 0, 2)
	tr.Station = "s1"
	tr.SourceTS = ptr(-5.0)
	m.Observe(tr, 2)
	if got := m.Counters().Get(CounterRejectedOutOfOrder); got != 0 {
		t.Fatalf("rejected_out_of_order = %d", got)
	}
}

// --- the clear reasons, each with its absent twin (E-01) ---

func TestResolvedNeedsMoreThanTheHysteresis(t *testing.T) {
	m := headOn(t, DefaultConfig())
	// B turns away: shown false from t=1.
	wantEvents(t, "t=1", m.Observe(at("B", 500, 10, 1), 1), nil, nil)
	wantEvents(t, "t=3", m.Observe(at("B", 510, 10, 3), 3), nil, nil)
	ev := m.Observe(at("B", 520, 10, 3.5), 3.5)
	wantEvents(t, "t=3.5", ev, nil, []string{"conflict:A:B=resolved"})
	if !ev.Cleared[0].ShownFalse || ev.Cleared[0].LastFalseS != 3.5 || ev.Cleared[0].LastTrueS != 0 {
		t.Fatalf("clear times %+v", ev.Cleared[0].Alert)
	}
}

func TestStalePair(t *testing.T) {
	m := headOn(t, DefaultConfig())
	wantEvents(t, "t=15", m.Tick(15), nil, nil)
	wantEvents(t, "t=15.5", m.Tick(15.5), nil, []string{"conflict:A:B=stale"})
	if m.Tracked() != 0 {
		t.Fatalf("%d aircraft still held", m.Tracked())
	}
}

func TestLandedPair(t *testing.T) {
	m := headOn(t, DefaultConfig())
	b := at("B", 500, 0, 1)
	b.Flying = ptr(false)
	wantEvents(t, "disarm", m.Observe(b, 1), nil, []string{"conflict:A:B=landed"})
	// Twin: still flying holds it.
	m = headOn(t, DefaultConfig())
	wantEvents(t, "flying", m.Observe(at("B", 490, -10, 1), 1), nil, nil)
}

func TestFlyingUnknownHoldsThenGoesStale(t *testing.T) {
	m := headOn(t, DefaultConfig())
	for s := 1.0; s <= 15; s++ {
		b := at("B", 500, 0, s)
		b.Flying = nil
		// A keeps flying and reporting; B's track is held at t=0.
		wantEvents(t, fmt.Sprintf("t=%v", s), m.Observe(b, s), nil, nil)
		m.Observe(at("A", 10*s, 10, s), s)
	}
	if got := m.Counters().Get(CounterFlyingUnknown); got != 15 {
		t.Fatalf("flying_unknown = %d", got)
	}
	b := at("B", 500, 0, 15.5)
	b.Flying = nil
	wantEvents(t, "15.5", m.Observe(b, 15.5), nil, []string{"conflict:A:B=stale"})
}

func TestSourceDisabledPair(t *testing.T) {
	m := headOn(t, DefaultConfig())
	st := sources.State{Epoch: "e", Version: 1, Controls: []sources.Control{{SourceType: "relay", Enabled: false}}}
	ev := m.SwitchSource(st, 1)
	wantEvents(t, "off", ev, nil, []string{"conflict:A:B=source_disabled"})
	// Twin: switching another type off touches nothing.
	m = headOn(t, DefaultConfig())
	st.Controls[0].SourceType = "remote_id"
	wantEvents(t, "other type", m.SwitchSource(st, 1), nil, nil)
}

func TestSourceStateOnlyMovesForward(t *testing.T) {
	m := headOn(t, DefaultConfig())
	on := sources.State{Epoch: "e", Version: 5}
	off := sources.State{Epoch: "e", Version: 4, Controls: []sources.Control{{SourceType: "relay", Enabled: false}}}
	m.SwitchSource(on, 1)
	wantEvents(t, "older version", m.SwitchSource(off, 1), nil, nil)
	if got := m.Counters().Get(CounterSourceStateIgnored); got != 1 {
		t.Fatalf("source_state_ignored = %d", got)
	}
	// A new epoch (a restored database) is taken whatever its version.
	off.Epoch = "e2"
	wantEvents(t, "new epoch", m.SwitchSource(off, 1), nil, []string{"conflict:A:B=source_disabled"})
	if got := m.Counters().Get(CounterRejectedSourceDisabled); got != 0 {
		t.Fatalf("rejected_source_disabled = %d before any sample", got)
	}
	wantEvents(t, "disabled sample", m.Observe(at("A", 10, 10, 2), 2), nil, nil)
	if got := m.Counters().Get(CounterRejectedSourceDisabled); got != 1 {
		t.Fatalf("rejected_source_disabled = %d", got)
	}
}

func TestDisabledSampleDropsAircraftTrackedFromIt(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 10, 0), 0)
	b := at("B", 500, -10, 0)
	b.Station = "gs-2"
	m.Observe(b, 0)
	// The switch arrives while B is tracked from gs-2 and is applied.
	st := sources.State{Epoch: "e", Version: 1, Controls: []sources.Control{{SourceType: "relay", InstanceID: ptr("gs-2"), Enabled: false}}}
	wantEvents(t, "switch", m.SwitchSource(st, 0.5), nil, []string{"conflict:A:B=source_disabled"})
	// B heard again from gs-2 is refused, then from gs-1 is tracked.
	b = at("B", 490, -10, 1)
	b.Station = "gs-2"
	wantEvents(t, "gs-2", m.Observe(b, 1), nil, nil)
	b.Station = "gs-1"
	wantEvents(t, "gs-1", m.Observe(b, 1), []string{"conflict:A:B"}, nil)
	// With default deny, a station with no row is refused from its first
	// sample and never held.
	m2 := NewMonitor(DefaultConfig())
	m2.SwitchSource(sources.State{Epoch: "e", Version: 1, DefaultDeny: true}, 0)
	c := at("C", 0, 0, 0)
	c.Station = "gs-9"
	wantEvents(t, "default deny", m2.Observe(c, 0), nil, nil)
	if m2.Tracked() != 0 {
		t.Fatalf("%d tracked", m2.Tracked())
	}
}

func TestDropWithReason(t *testing.T) {
	m := headOn(t, DefaultConfig())
	wantEvents(t, "no reason", m.Drop("B", "", 1), nil, nil)
	if got := m.Counters().Get(CounterDropWithoutReason); got != 1 {
		t.Fatalf("drop_without_reason = %d", got)
	}
	wantEvents(t, "unknown", m.Drop("Z", ClearFlightEnded, 1), nil, nil)
	wantEvents(t, "flight ended", m.Drop("B", ClearFlightEnded, 1), nil, []string{"conflict:A:B=flight_ended"})
	if _, ok := m.aircraft["B"]; ok {
		t.Fatal("B still held")
	}
}

// --- fail-safe: what cannot be judged never clears ---

func TestUnjudgedPairNeverClears(t *testing.T) {
	m := headOn(t, DefaultConfig())
	// B reports a NaN velocity: the pair cannot be judged.
	for s := 1.0; s <= 10; s++ {
		b := at("B", 500, math.NaN(), s)
		wantEvents(t, "NaN", m.Observe(b, s), nil, nil)
	}
	if got := m.Counters().Get(CounterPairsNotJudged + "_" + "invalid_input"); got != 10 {
		t.Fatalf("not judged invalid_input = %d", got)
	}
	if len(m.Active()) != 1 {
		t.Fatal("the alert did not survive unjudgeable samples")
	}
	// Twin: a judged non-conflict clears it.
	ev := Events{}
	for s := 11.0; s <= 15; s++ {
		ev = m.Observe(at("B", 5000, 10, s), s)
		m.Observe(at("A", 0, 0, s), s)
	}
	_, cleared := kinds(ev)
	if len(m.Active()) != 0 {
		t.Fatalf("still active after judged clear: %v", cleared)
	}
}

func TestInvalidPositionNeitherPairsNorClears(t *testing.T) {
	m := headOn(t, DefaultConfig())
	b := at("B", 500, -10, 1)
	b.Pos.LatDeg = math.Inf(1)
	wantEvents(t, "inf", m.Observe(b, 1), nil, nil)
	if got := m.Counters().Get(CounterInvalidPosition); got != 1 {
		t.Fatalf("invalid_position = %d", got)
	}
	if len(m.Active()) != 1 {
		t.Fatal("alert lost")
	}
}

func TestActivePartnerOutsideRadiusIsStillJudged(t *testing.T) {
	m := headOn(t, DefaultConfig())
	// B jumps 2 km away, still closing fast: still a conflict, refreshed.
	wantEvents(t, "far but closing", m.Observe(at("B", 2000, -40, 1), 1), nil, nil)
	got := m.Active()[0]
	if got.LastTrueS != 1 {
		t.Fatalf("not refreshed: %+v", got)
	}
}

func TestPressureAltitudeConflictIsHorizontal(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 10, 0), 0)
	b := at("B", 500, -10, 0)
	b.AltAMSLM = ptr(900.0)
	b.AltSource = core.AltPressure
	ev := m.Observe(b, 0)
	if len(ev.Raised) != 1 {
		t.Fatalf("raised %v", ev.Raised)
	}
	d := ev.Raised[0].Detail
	if d["vertical_separation_known"] != false || d["d_alt_at_cpa_m"] != nil {
		t.Fatalf("detail %v", d)
	}
	// Twin: 350 m apart vertically on geodetic altitudes is no conflict.
	m = NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 10, 0), 0)
	b.AltSource = core.AltGeodetic
	wantEvents(t, "geodetic", m.Observe(b, 0), nil, nil)
}

func TestActiveRanksByLossOfSeparation(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	// C and D are inside the minima now; A and B lose separation later.
	m.Observe(at("A", 0, 10, 0), 0)
	m.Observe(at("B", 500, -10, 0), 0)
	m.Observe(at("C", 3000, 0, 0), 0)
	m.Observe(at("D", 3030, 0, 0), 0)
	act := m.Active()
	if len(act) != 2 || act[0].Key != "conflict:C:D" || act[1].Key != "conflict:A:B" {
		t.Fatalf("order %v", act)
	}
	if act[0].Detail["los_start_s"].(float64) != 0 || act[1].Detail["los_start_s"].(float64) <= 0 {
		t.Fatalf("los_start_s %v %v", act[0].Detail, act[1].Detail)
	}
	// The copy is the caller's: changing it changes nothing here.
	act[0].Detail["t_cpa_s"] = -1.0
	act[0].Aircraft[0] = "Z"
	if again := m.Active(); again[0].Detail["t_cpa_s"] == -1.0 || again[0].Aircraft[0] != "C" {
		t.Fatal("Active shares state with the caller")
	}
}

func TestKeysNeverCollide(t *testing.T) {
	if conflictKey("a:b", "c") == conflictKey("a", "b:c") {
		t.Fatal("collision")
	}
	if got := conflictKey("b%", "a:"); got != "conflict:a%3A:b%25" {
		t.Fatalf("key %q", got)
	}
}

// --- zones, identification, mismatch, height ---

func zoneMonitor(t *testing.T, zs ...*zones.Zone) *Monitor {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Zones = zs
	return NewMonitor(cfg)
}

func ident(status core.IdentStatus, reason core.IdentReason, mismatch bool) *core.Identification {
	return &core.Identification{Status: status, Reason: reason, Mismatch: mismatch}
}

func TestIdentificationOnlyInIncidentZones(t *testing.T) {
	for _, c := range []struct {
		restriction string
		want        []string
	}{
		{"PROHIBITED", []string{"zone:GEO:Z:A", "identification:GEO:Z:A"}},
		{"REQ_AUTHORISATION", []string{"zone:GEO:Z:A", "identification:GEO:Z:A"}},
		{"CONDITIONAL", []string{"zone:GEO:Z:A"}},
		{"NO_RESTRICTION", nil},
	} {
		t.Run(c.restriction, func(t *testing.T) {
			m := zoneMonitor(t, polygonZone(t, "Z", c.restriction, ""))
			a := at("A", 0, 0, 0)
			a.Identification = ident(core.IdentUnidentified, core.ReasonNoSerial, false)
			wantEvents(t, "unidentified", m.Observe(a, 0), c.want, nil)
		})
	}
	// A registered aircraft raises the zone alert only.
	m := zoneMonitor(t, polygonZone(t, "Z", "PROHIBITED", ""))
	a := at("A", 0, 0, 0)
	a.Identification = ident(core.IdentRegistered, core.ReasonMatched, false)
	wantEvents(t, "registered", m.Observe(a, 0), []string{"zone:GEO:Z:A"}, nil)
}

func TestIdentificationHeldWithoutIdentification(t *testing.T) {
	m := zoneMonitor(t, polygonZone(t, "Z", "PROHIBITED", ""))
	a := at("A", 0, 0, 0)
	a.Identification = ident(core.IdentUnidentified, core.ReasonNoSerial, false)
	m.Observe(a, 0)
	for s := 1.0; s <= 5; s++ {
		// No identification block: the identification alert is held.
		wantEvents(t, "no ident", m.Observe(at("A", 0, 0, s), s), nil, nil)
	}
	// Positive evidence: registered now; cleared after the hysteresis.
	var cleared []string
	for s := 6.0; s <= 10; s++ {
		a := at("A", 0, 0, s)
		a.Identification = ident(core.IdentRegistered, core.ReasonMatched, false)
		_, c := kinds(m.Observe(a, s))
		cleared = append(cleared, c...)
	}
	if !slices.Equal(cleared, []string{"identification:GEO:Z:A=resolved"}) {
		t.Fatalf("cleared %v", cleared)
	}
}

func TestZoneNotEvaluatedHolds(t *testing.T) {
	// A CONDITIONAL zone with an AGL ceiling and no terrain is not
	// evaluated (Z-09); its alert is held, never cleared by it.
	cond := polygonZone(t, "C", "CONDITIONAL", `"lowerVerticalReference":"AGL","upperVerticalReference":"AGL","lowerLimit":0,"upperLimit":120,`)
	m := zoneMonitor(t, cond)
	a := at("A", 0, 0, 0)
	a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: 500}
	wantEvents(t, "known ground", m.Observe(a, 0), []string{"zone:GEO:C:A"}, nil)
	for s := 1.0; s <= 10; s++ {
		a := at("A", 0, 0, s) // ground unknown now
		wantEvents(t, "unknown ground", m.Observe(a, s), nil, nil)
	}
	if got := m.Counters().Get(CounterZoneNotEvaluated); got != 10 {
		t.Fatalf("zone_checks_not_evaluated = %d", got)
	}
	// Twin: judged outside vertically (700 m over 500 m ground), clears.
	var cleared []string
	for s := 11.0; s <= 15; s++ {
		a := at("A", 0, 0, s)
		a.AltAMSLM = ptr(700.0)
		a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: 500}
		_, c := kinds(m.Observe(a, s))
		cleared = append(cleared, c...)
	}
	if !slices.Equal(cleared, []string{"zone:GEO:C:A=resolved"}) {
		t.Fatalf("cleared %v", cleared)
	}
}

func TestZoneLimitNotJudgedWarns(t *testing.T) {
	z := polygonZone(t, "P", "PROHIBITED", `"lowerVerticalReference":"AGL","upperVerticalReference":"AGL","lowerLimit":0,"upperLimit":120,`)
	m := zoneMonitor(t, z)
	ev := m.Observe(at("A", 0, 0, 0), 0)
	if len(ev.Raised) != 1 || ev.Raised[0].Severity != core.SeverityWarning || ev.Raised[0].Detail["limit_not_judged"] != true {
		t.Fatalf("raised %+v", ev.Raised)
	}
	if nj, ok := ev.Raised[0].Detail["not_judged"].([]string); !ok || !slices.Equal(nj, []string{"AGL"}) {
		t.Fatalf("not_judged %v", ev.Raised[0].Detail["not_judged"])
	}
	if got := m.Counters().Get(CounterZoneLimitNotJudged); got != 1 {
		t.Fatalf("zone_limits_not_judged = %d", got)
	}
}

func TestZoneContainmentErrorHolds(t *testing.T) {
	good := polygonZone(t, "Z", "PROHIBITED", "")
	m := zoneMonitor(t, good)
	m.Observe(at("A", 0, 0, 0), 0)
	// The same zone loses its shape (a zone built in code): containment
	// errors, and the alert is held.
	good.Polygon = nil
	for s := 1.0; s <= 6; s++ {
		wantEvents(t, "error", m.Observe(at("A", 0, 0, s), s), nil, nil)
	}
	if got := m.Counters().Get(CounterZoneNotEvaluated); got != 6 {
		t.Fatalf("zone_checks_not_evaluated = %d", got)
	}
}

func TestZoneKeyIsCountryAndIdentifier(t *testing.T) {
	// One identifier in two countries: two zones, two keys, no fallback.
	other := polygonZone(t, "Z", "REQ_AUTHORISATION", "")
	other.Country = "ARM"
	m := zoneMonitor(t, polygonZone(t, "Z", "PROHIBITED", ""), other)
	wantEvents(t, "two countries", m.Observe(at("A", 0, 0, 0), 0), []string{"zone:GEO:Z:A", "zone:ARM:Z:A"}, nil)
	if n := m.Counters().Get(CounterZoneKeyDuplicate); n != 0 {
		t.Fatalf("zone_key_duplicate = %d", n)
	}
	// A true duplicate within one country: the last-resort fallback,
	// counted. A genuine identifier that looks like the fallback stays
	// distinct from it.
	m = zoneMonitor(t,
		polygonZone(t, "Z", "PROHIBITED", ""),
		polygonZone(t, "Z#2", "PROHIBITED", ""),
		polygonZone(t, "Z", "REQ_AUTHORISATION", ""))
	wantEvents(t, "duplicate", m.Observe(at("A", 0, 0, 0), 0), []string{"zone:GEO:Z:A", "zone:GEO:Z#2:A", "zone:GEO:Z#2#2:A"}, nil)
	if n := m.Counters().Get(CounterZoneKeyDuplicate); n != 1 {
		t.Fatalf("zone_key_duplicate = %d, want 1", n)
	}
}

func TestUSpaceZoneRaisesInfo(t *testing.T) {
	z := polygonZone(t, "U", "PROHIBITED", "")
	z.Type = core.ZoneUSpace
	m := zoneMonitor(t, z)
	ev := m.Observe(at("A", 0, 0, 0), 0)
	if len(ev.Raised) != 1 || ev.Raised[0].Severity != core.SeverityInfo {
		t.Fatalf("raised %+v", ev.Raised)
	}
}

func TestHeightLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ZonePolicy.MaxHeightAGLM = ptr(120.0)
	m := NewMonitor(cfg)
	a := at("A", 0, 0, 0)
	a.AltAMSLM = ptr(650.0)
	a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: 500}
	ev := m.Observe(a, 0)
	wantEvents(t, "150 m", ev, []string{"height:A"}, nil)
	if ev.Raised[0].Detail["height_agl_m"] != 150.0 || ev.Raised[0].Detail["max_height_agl_m"] != 120.0 {
		t.Fatalf("detail %v", ev.Raised[0].Detail)
	}
	// Unknown ground: not evaluated, held.
	for s := 1.0; s <= 5; s++ {
		a := at("A", 0, 0, s)
		a.Env = zones.Env{Ground: zones.GroundUnknown}
		wantEvents(t, "unknown", m.Observe(a, s), nil, nil)
	}
	if got := m.Counters().Get(CounterHeightNotEvaluated); got != 5 {
		t.Fatalf("height_checks_not_evaluated = %d", got)
	}
	// Exactly at the limit is allowed: shown false, cleared after 3 s.
	var cleared []string
	for s := 6.0; s <= 10; s++ {
		a := at("A", 0, 0, s)
		a.AltAMSLM = ptr(620.0)
		a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: 500}
		_, c := kinds(m.Observe(a, s))
		cleared = append(cleared, c...)
	}
	if !slices.Equal(cleared, []string{"height:A=resolved"}) {
		t.Fatalf("cleared %v", cleared)
	}
}

func TestMismatchLifecycle(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	a := at("A", 0, 0, 0)
	a.Flying = ptr(false)
	a.Identification = ident(core.IdentUnknownOperator, core.ReasonOperatorMismatch, true)
	wantEvents(t, "raise", m.Observe(a, 0), []string{"identification_mismatch:A"}, nil)
	// A landing does not clear it: it is about who, not where.
	wantEvents(t, "on ground", m.Observe(a, 1), nil, nil)
	// Samples with no identification hold it and do not keep it fresh.
	for s := 2.0; s <= 15; s++ {
		wantEvents(t, "no ident", m.Observe(at("A", 0, 0, s), s), nil, nil)
	}
	wantEvents(t, "stale", m.Observe(at("A", 0, 0, 16.5), 16.5), nil, []string{"identification_mismatch:A=stale"})

	// Twin: a registered identification shows it false and resolves it.
	m = NewMonitor(DefaultConfig())
	a.CapturedAtS, a.RxAtS, a.SourceTS = 0, 0, ptr(0.0)
	m.Observe(a, 0)
	var cleared []string
	for s := 1.0; s <= 4; s++ {
		b := at("A", 0, 0, s)
		b.Identification = ident(core.IdentRegistered, core.ReasonMatched, false)
		_, c := kinds(m.Observe(b, s))
		cleared = append(cleared, c...)
	}
	if !slices.Equal(cleared, []string{"identification_mismatch:A=resolved"}) {
		t.Fatalf("cleared %v", cleared)
	}
}

func TestMismatchSurvivesLandingButNotDrop(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	a := at("A", 0, 0, 0)
	a.Identification = ident(core.IdentUnknownOperator, core.ReasonOperatorMismatch, true)
	m.Observe(a, 0)
	a.Flying = ptr(false)
	a.CapturedAtS, a.RxAtS, a.SourceTS = 1, 1, ptr(1.0)
	wantEvents(t, "landed", m.Observe(a, 1), nil, nil)
	wantEvents(t, "drop", m.Drop("A", ClearLanded, 1), nil, []string{"identification_mismatch:A=landed"})
}

// --- bounds, isolation, determinism ---

func TestMaxAircraftEvicts(t *testing.T) {
	const n = 50_001
	m := NewMonitor(DefaultConfig())
	for i := range n {
		tr := at(fmt.Sprintf("U%05d", i), 0, 0, 0)
		tr.Flying = ptr(false) // on the ground: no pairing, cheap
		m.Observe(tr, 0)
	}
	if m.Tracked() != 50_000 {
		t.Fatalf("%d aircraft held", m.Tracked())
	}
	if got := m.Counters().Get(CounterAircraftEvicted); got != 1 {
		t.Fatalf("aircraft_evicted = %d", got)
	}
	if _, ok := m.aircraft["U00000"]; ok {
		t.Fatal("the oldest was not the one evicted")
	}
	if m.CapacityExceeded() {
		t.Fatal("capacity exceeded with evictable aircraft held")
	}
}

func TestEvictionSparesAlertHolders(t *testing.T) {
	// Every aircraft in the cap holds an alert: the new id is refused and
	// counted, the condition is exposed, and nothing is cleared.
	cfg := DefaultConfig()
	cfg.MaxAircraft = 2
	m := headOn(t, cfg)
	wantEvents(t, "C refused", m.Observe(at("C", 9000, 0, 0), 0), nil, nil)
	if got := m.Counters().Get(CounterRejectedCapacity); got != 1 {
		t.Fatalf("rejected_capacity = %d", got)
	}
	if !m.CapacityExceeded() || m.Tracked() != 2 || len(m.Active()) != 1 {
		t.Fatalf("exceeded %v tracked %d active %d", m.CapacityExceeded(), m.Tracked(), len(m.Active()))
	}
	// Twin: an aircraft without an alert is evicted instead, silently.
	cfg.MaxAircraft = 3
	m = headOn(t, cfg)
	m.Observe(at("C", 9000, 0, 0), 0)
	wantEvents(t, "D evicts C", m.Observe(at("D", 12000, 0, 0), 0), nil, nil)
	if _, ok := m.aircraft["C"]; ok || m.Counters().Get(CounterAircraftEvicted) != 1 || m.CapacityExceeded() {
		t.Fatal("C was not the one evicted")
	}
	if len(m.Active()) != 1 {
		t.Fatal("the conflict was lost")
	}
}

func TestEvictionPrefersGroundThenUnidentified(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAircraft = 5
	m := headOn(t, cfg)
	m.Observe(at("C", 9000, 0, 0), 0) // flying, identified, oldest
	d := at("D", 12000, 0, 0)
	d.Identified = ptr(false) // flying, unidentified
	m.Observe(d, 0)
	e := at("E", 15000, 0, 0)
	e.Flying = nil // unknown flying: no track
	m.Observe(e, 0)
	evicts := func(id, want string) {
		t.Helper()
		m.Observe(at(id, 30000+float64(len(id))*100, 0, 0), 0)
		if _, ok := m.aircraft[want]; ok {
			t.Fatalf("%s arrived and %s was not evicted", id, want)
		}
	}
	evicts("F", "E")   // not flying first
	evicts("GG", "D")  // then unidentified
	evicts("HHH", "C") // then least recently heard
	if len(m.Active()) != 1 {
		t.Fatal("the conflict was lost")
	}
}

func TestSpoofedFloodNeverClearsARealConflict(t *testing.T) {
	// Review probe: 50 000 spoofed ids after a real conflict. The old
	// least-recently-used eviction cleared it as evicted.
	m := headOn(t, DefaultConfig())
	for i := range 50_000 {
		tr := at(fmt.Sprintf("S%05d", i), 20000, 0, 0.5)
		tr.Flying = ptr(false)
		if ev := m.Observe(tr, 0.5); len(ev.Cleared) > 0 {
			t.Fatalf("spoof %d cleared %v", i, ev.Cleared)
		}
	}
	if len(m.Active()) != 1 || m.Tracked() != 50_000 {
		t.Fatalf("active %d tracked %d", len(m.Active()), m.Tracked())
	}
	if got := m.Counters().Get(CounterAircraftEvicted); got != 2 {
		t.Fatalf("aircraft_evicted = %d, want 2", got)
	}
	// The pair is still judged: A and B report on and the alert refreshes.
	m.Observe(at("A", 10, 10, 1), 1)
	if a := m.Active(); len(a) != 1 || a[0].LastTrueS != 1 {
		t.Fatalf("active %+v", a)
	}
}

func TestTwoMonitorsShareNothing(t *testing.T) {
	m1 := headOn(t, DefaultConfig())
	m2 := NewMonitor(DefaultConfig())
	wantEvents(t, "m2 B", m2.Observe(at("B", 500, -10, 0), 0), nil, nil)
	if len(m2.Active()) != 0 || m2.Tracked() != 1 || m1.Tracked() != 2 {
		t.Fatalf("shared state: %d %d %d", len(m2.Active()), m2.Tracked(), m1.Tracked())
	}
	m2.Observe(at("B", 500, -10, 0), 0)
	if m1.Counters() == m2.Counters() || m1.Counters().Get(CounterRejectedOutOfOrder) != 0 {
		t.Fatal("shared counters")
	}
}

func TestSanitiseConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ClearAfterS = math.NaN()
	cfg.StaleAfterS = -1
	cfg.LiveMaxAgeS = math.Inf(1)
	cfg.MismatchSeverity = "loud"
	cfg.IdentificationSeverity = ""
	cfg.GridCellM = 10
	cfg.MaxAircraft = 0
	cfg.MaxSourcesPerAircraft = -1
	m := NewMonitor(cfg)
	got := m.Config()
	d := DefaultConfig()
	if got.ClearAfterS != d.ClearAfterS || got.StaleAfterS != d.StaleAfterS || got.LiveMaxAgeS != d.LiveMaxAgeS ||
		got.MismatchSeverity != d.MismatchSeverity || got.IdentificationSeverity != d.IdentificationSeverity ||
		got.GridCellM != d.GridCellM || got.MaxAircraft != d.MaxAircraft || got.MaxSourcesPerAircraft != d.MaxSourcesPerAircraft {
		t.Fatalf("config %+v", got)
	}
	if n := m.Counters().Get(CounterConfigInvalid); n != 5 {
		t.Fatalf("config_invalid = %d, want 5", n)
	}
	// An invalid separation policy is kept and counted, and every pair it
	// refuses is counted: no conflict is ever silently missing.
	cfg = DefaultConfig()
	cfg.Policy.DHorizontalMinM = 0
	m = NewMonitor(cfg)
	if n := m.Counters().Get(CounterConfigInvalid); n != 1 {
		t.Fatalf("config_invalid = %d for a bad policy", n)
	}
	m.Observe(at("A", 0, 10, 0), 0)
	m.Observe(at("B", 500, -10, 0), 0)
	if n := m.Counters().Get(CounterPairsNotJudged + "_invalid_policy"); n != 1 {
		t.Fatalf("pairs not judged invalid_policy = %d", n)
	}
	// The healthy configuration counts nothing.
	if n := NewMonitor(DefaultConfig()).Counters().Get(CounterConfigInvalid); n != 0 {
		t.Fatalf("config_invalid = %d for the default", n)
	}
}

func TestQuietMonitorSaysNothing(t *testing.T) {
	// The branch that says nothing is wrong (E-02): aircraft far apart,
	// outside every zone, registered, live: nothing raised, nothing
	// counted, and a tick clears nothing.
	m := zoneMonitor(t, polygonZone(t, "Z", "PROHIBITED", ""))
	for s := 0.0; s < 10; s++ {
		for i, id := range []string{"A", "B", "C"} {
			tr := at(id, 5000+float64(i)*2000, 0, s)
			tr.Identification = ident(core.IdentRegistered, core.ReasonMatched, false)
			wantEvents(t, "quiet", m.Observe(tr, s), nil, nil)
		}
		wantEvents(t, "tick", m.Tick(s+0.5), nil, nil)
	}
	if names := m.Counters().Names(); len(names) != 0 {
		t.Fatalf("counters %v", m.Counters().Snapshot())
	}
	if len(m.Active()) != 0 || m.Tracked() != 3 {
		t.Fatalf("active %d tracked %d", len(m.Active()), m.Tracked())
	}
	// Silence then drops them, quietly: nothing was active.
	wantEvents(t, "silence", m.Tick(100), nil, nil)
	if m.Tracked() != 0 {
		t.Fatalf("%d still tracked", m.Tracked())
	}
}

func TestVectorsAreDeterministic(t *testing.T) {
	f := vectors.Load(t, "alert_lifecycle.json")
	var pol vPolicy
	f.Header(t, "policy", &pol)
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vInput
		c.Decode(t, &in, nil)
		cfg := configOf(t, pol, in.Config)
		first, m1 := runSteps(t, cfg, in.Steps)
		second, m2 := runSteps(t, cfg, in.Steps)
		if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(m1.Active(), m2.Active()) ||
			!reflect.DeepEqual(m1.Counters().Snapshot(), m2.Counters().Snapshot()) {
			t.Fatal("two runs differ")
		}
	})
}

func TestPlacedTimeOutOfRangeAppliesEveryZone(t *testing.T) {
	if !placedTime(math.Inf(1)).IsZero() || !placedTime(2e11).IsZero() {
		t.Fatal("out-of-range time not unknown")
	}
	z := polygonZone(t, "Z", "PROHIBITED", "")
	z.Periods = []ed269.Period{{}}
	m := zoneMonitor(t, z)
	tr := at("A", 0, 0, 2e11)
	ev := m.Observe(tr, 2e11)
	wantEvents(t, "unknown time", ev, []string{"zone:GEO:Z:A"}, nil)
}

func TestGridCellRaisedToRadius(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Policy.NeighbourRadiusM = 3000
	cfg.GridCellM = 1000
	m := NewMonitor(cfg)
	if m.Config().GridCellM != 3000 {
		t.Fatalf("cell %v", m.Config().GridCellM)
	}
	// Brute force agrees with the grid at that radius.
	m.Observe(at("A", 0, 0, 0), 0)
	far := at("B", 2900, 0, 0)
	far.VNMS = -60
	ev := m.Observe(far, 0)
	if len(ev.Raised) != 1 {
		n, e := geodesy.LocalOffsetAboutMidLatM(m.aircraft["A"].state.Pos, far.Pos)
		t.Fatalf("pair at %v m not judged: %v", math.Hypot(n, e), ev)
	}
}

func TestSameTransmitterCounted(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	a := at("A", 0, 10, 0)
	a.Transmitter, a.Identified = ptr("TX"), ptr(false)
	b := at("B", 5, 10, 0)
	b.Transmitter, b.Identified = ptr("TX"), ptr(true)
	m.Observe(a, 0)
	wantEvents(t, "same radio", m.Observe(b, 0), nil, nil)
	if got := m.Counters().Get(CounterPairsSameTransmitter); got != 1 {
		t.Fatalf("pairs_same_transmitter = %d", got)
	}
	// Different addresses pair.
	b.Transmitter = ptr("TY")
	wantEvents(t, "two radios", m.Observe(b, 0), []string{"conflict:A:B"}, nil)
}

func TestNeighbourRadiusPair(t *testing.T) {
	// 900 m apart and closing at 40 m/s: a loss of separation in 22 s,
	// but outside the 800 m search radius, so not paired (as the old
	// index); at 790 m it is.
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 0, 0), 0)
	wantEvents(t, "900 m", m.Observe(at("B", 900, -40, 0), 0), nil, nil)
	if n := len(m.Counters().Names()); n != 0 {
		t.Fatalf("counters %v", m.Counters().Snapshot())
	}
	wantEvents(t, "790 m", m.Observe(at("B", 790, -40, 0.1), 0.1), []string{"conflict:A:B"}, nil)
}

func TestUnknownAltSourceIsHorizontal(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 10, 0), 0)
	b := at("B", 500, -10, 0)
	b.AltAMSLM = ptr(5000.0)
	b.AltSource = ""
	ev := m.Observe(b, 0)
	if len(ev.Raised) != 1 || ev.Raised[0].Detail["vertical_separation_known"] != false {
		t.Fatalf("raised %+v", ev.Raised)
	}
}

func TestNonFiniteTickChangesNothing(t *testing.T) {
	m := headOn(t, DefaultConfig())
	wantEvents(t, "NaN tick", m.Tick(math.NaN()), nil, nil)
	wantEvents(t, "Inf tick", m.Tick(math.Inf(1)), nil, nil)
	if got := m.Counters().Get(CounterRejectedInvalid); got != 2 {
		t.Fatalf("rejected_invalid = %d", got)
	}
	if len(m.Active()) != 1 || m.Tracked() != 2 {
		t.Fatal("state changed")
	}
}

func TestActiveRanksBySeverity(t *testing.T) {
	cond := polygonZone(t, "C", "CONDITIONAL", "")
	usp := polygonZone(t, "U", "PROHIBITED", "")
	usp.Type = core.ZoneUSpace
	m := zoneMonitor(t, cond, nil, usp)
	m.Observe(at("A", 0, 0, 0), 0)
	m.Observe(at("B", 30, 0, 0), 0)
	var got []string
	for _, a := range m.Active() {
		got = append(got, string(a.Severity)+" "+a.Key)
	}
	want := []string{
		"critical conflict:A:B",
		"warning zone:GEO:C:A", "warning zone:GEO:C:B",
		"info zone:GEO:U:A", "info zone:GEO:U:B",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v", got)
	}
}

func TestIdentityKeepsAircraftWithoutTrack(t *testing.T) {
	// An aircraft on the ground with a fresh identity is held, and
	// forgotten only once both its identity and its last sample are old.
	m := NewMonitor(DefaultConfig())
	a := at("A", 0, 0, 0)
	a.Flying = ptr(false)
	a.Identification = ident(core.IdentRegistered, core.ReasonMatched, false)
	m.Observe(a, 0)
	m.Observe(at("B", 5000, 0, 10), 10)
	wantEvents(t, "t=14", m.Tick(14), nil, nil)
	if m.Tracked() != 2 {
		t.Fatalf("%d held at 14 s", m.Tracked())
	}
	m.Tick(15.5)
	if _, ok := m.aircraft["A"]; ok || m.Tracked() != 1 {
		t.Fatalf("A still held at 15.5 s (%d held)", m.Tracked())
	}
}

func TestResolvedClearCarriesClearingNumbers(t *testing.T) {
	// The vectors' diverging pair: inside until t=3 (55 m), shown false
	// from t=4, cleared at t=7 at 75 m. Detail keeps the last true numbers
	// (alert_lifecycle.json); ClearingDetail the clearing judgement (C-14).
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 0, 0), 0)
	var ev Events
	for s := 0.0; s <= 7; s++ {
		ev = m.Observe(at("B", 40+5*s, 5, s), s)
	}
	if len(ev.Cleared) != 1 || ev.Cleared[0].Reason != ClearResolved {
		t.Fatalf("cleared %+v", ev.Cleared)
	}
	c := ev.Cleared[0]
	near := func(name string, got any, want float64) {
		t.Helper()
		g, ok := got.(float64)
		if !ok || math.Abs(g-want) > 0.5 {
			t.Fatalf("%s = %v, want about %v", name, got, want)
		}
	}
	near("d_horizontal_now_m", c.Detail["d_horizontal_now_m"], 55)
	cd := c.ClearingDetail
	near("clearing_d_horizontal_now_m", cd["clearing_d_horizontal_now_m"], 75)
	near("clearing_d_cpa_horizontal_m", cd["clearing_d_cpa_horizontal_m"], 75)
	near("clearing_t_cpa_s", cd["clearing_t_cpa_s"], 0)
	near("clearing_d_alt_now_m", cd["clearing_d_alt_now_m"], 0)
	near("clearing_d_alt_at_cpa_m", cd["clearing_d_alt_at_cpa_m"], 0)
	near("clearing_at_s", cd["clearing_at_s"], 7)
	if cd["clearing_vertical_separation_known"] != true || len(cd) != 7 {
		t.Fatalf("clearing detail %v", cd)
	}
	// Twin: a clear that rests on no judgement carries none.
	m = headOn(t, DefaultConfig())
	ev = m.Tick(16)
	if len(ev.Cleared) != 1 || ev.Cleared[0].Reason != ClearStale || ev.Cleared[0].ClearingDetail != nil {
		t.Fatalf("stale clear %+v", ev.Cleared)
	}
}

func TestClearingDetailVerticalUnknown(t *testing.T) {
	m := NewMonitor(DefaultConfig())
	m.Observe(at("A", 0, 0, 0), 0)
	var ev Events
	for s := 0.0; s <= 7; s++ {
		b := at("B", 40+5*s, 5, s)
		b.AltSource = core.AltPressure
		ev = m.Observe(b, s)
	}
	if len(ev.Cleared) != 1 {
		t.Fatalf("cleared %+v", ev.Cleared)
	}
	cd := ev.Cleared[0].ClearingDetail
	if cd["clearing_d_alt_now_m"] != nil || cd["clearing_d_alt_at_cpa_m"] != nil || cd["clearing_vertical_separation_known"] != false {
		t.Fatalf("clearing detail %v", cd)
	}
}

func TestOlderPlacementNeverRewindsTheTrack(t *testing.T) {
	// Review probe (T-06): A and B head-on, reporting every second up to
	// t=14. One A sample placed at t=0 arrives at 14.5, with no source
	// time (so T-03 cannot order it), or from another station.
	for _, variant := range []string{"no source time", "other station"} {
		t.Run(variant, func(t *testing.T) {
			m := NewMonitor(DefaultConfig())
			for s := 0.0; s <= 14; s++ {
				m.Observe(at("A", 10*s, 10, s), s)
				m.Observe(at("B", 500-10*s, -10, s), s)
			}
			old := at("A", 0, 10, 0)
			old.RxAtS = 14.5
			if variant == "no source time" {
				old.SourceTS = nil
			} else {
				old.Station = "gs-2"
			}
			wantEvents(t, "older sample", m.Observe(old, 14.5), nil, nil)
			if got := m.Counters().Get(CounterRejectedOlderPlacement); got != 1 {
				t.Fatalf("rejected_older_than_held = %d", got)
			}
			if a := m.aircraft["A"]; a.state.CapturedAtS != 14 || a.seenS != 14 || a.heardS != 14 {
				t.Fatalf("track rewound: captured %v seen %v heard %v", a.state.CapturedAtS, a.seenS, a.heardS)
			}
			wantEvents(t, "tick 15.5", m.Tick(15.5), nil, nil)
			m.Observe(at("B", 350, -10, 15), 15.5)
			if got := m.Counters().Get(CounterPairsNotJudged + "_stale_neighbour"); got != 0 {
				t.Fatalf("stale_neighbour pairs = %d", got)
			}
			if len(m.Active()) != 1 || m.Active()[0].LastTrueS != 15 {
				t.Fatalf("active %+v", m.Active())
			}
		})
	}
}

func TestOlderTrueNeverShortensTheHysteresis(t *testing.T) {
	// Review probe: shown true at 8 (A's sample), then true at 2 (B's
	// latest sample, placed at 2, judged against A at 8), then false once
	// at 9. Only 1 s has passed since the last true: no clear.
	m := headOn(t, DefaultConfig())
	wantEvents(t, "A true at 8", m.Observe(at("A", 80, 10, 8), 8), nil, nil)
	b := at("B", 480, -10, 2)
	b.RxAtS = 8
	wantEvents(t, "B true at 2", m.Observe(b, 8), nil, nil)
	if a := m.Active()[0]; a.LastTrueS != 8 {
		t.Fatalf("LastTrueS went back to %v", a.LastTrueS)
	}
	// A turns south, parallel to B and 320 m from it: judged false at 9.
	wantEvents(t, "A false at 9", m.Observe(at("A", 90, -10, 9), 9), nil, nil)
	a := m.Active()
	if len(a) != 1 || !a[0].ShownFalse || a[0].LastFalseS != 9 {
		t.Fatalf("active %+v", a)
	}
	// An older false never moves LastFalseS back either.
	b = at("B", 470, 10, 3)
	b.RxAtS = 9
	m.Observe(b, 9)
	if a := m.Active(); len(a) == 1 && a[0].LastFalseS != 9 {
		t.Fatalf("LastFalseS went back to %v", a[0].LastFalseS)
	}
}

func TestDropRefusesEvidenceReasons(t *testing.T) {
	// resolved and stale are the monitor's own judgements: a caller
	// cannot claim them.
	for _, r := range []ClearReason{ClearResolved, ClearStale} {
		m := headOn(t, DefaultConfig())
		wantEvents(t, string(r), m.Drop("B", r, 1), nil, nil)
		if got := m.Counters().Get(CounterDropRefusedReason); got != 1 {
			t.Fatalf("%s: drop_refused_reason = %d", r, got)
		}
		if m.Tracked() != 2 || len(m.Active()) != 1 {
			t.Fatalf("%s: state changed", r)
		}
	}
	// Twin: a caller's reason is taken.
	m := headOn(t, DefaultConfig())
	wantEvents(t, "landed", m.Drop("B", ClearLanded, 1), nil, []string{"conflict:A:B=landed"})
}

func TestSwitchDropsByLastSourceAndOtherSourceReRaises(t *testing.T) {
	// A is heard by Remote ID and by relay gs-1; its last sample before
	// the switch is the relay's. Switching the relay off drops A as
	// source_disabled (one-station-off-drops-only-its-aircraft pins the
	// drop); A's next sample through the still-enabled Remote ID is a
	// new track and raises the conflict again.
	m := NewMonitor(DefaultConfig())
	rid := at("A", 0, 10, 0)
	rid.Source, rid.Station = "remote_id", "rx-1"
	m.Observe(rid, 0)
	relay := at("A", 0, 10, 0.5)
	relay.Station = "gs-1"
	m.Observe(relay, 0.5)
	b := at("B", 495, -10, 0.5)
	b.Source, b.Station = "remote_id", "rx-1"
	wantEvents(t, "raise", m.Observe(b, 0.5), []string{"conflict:A:B"}, nil)
	off := sources.State{Epoch: "e", Version: 1, Controls: []sources.Control{{SourceType: "relay", Enabled: false}}}
	wantEvents(t, "relay off", m.SwitchSource(off, 0.6), nil, []string{"conflict:A:B=source_disabled"})
	rid = at("A", 10, 10, 1)
	rid.Source, rid.Station = "remote_id", "rx-1"
	wantEvents(t, "remote id re-raises", m.Observe(rid, 1), []string{"conflict:A:B"}, nil)
}
