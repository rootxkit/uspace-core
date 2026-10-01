package zones

import (
	"math"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
)

func limit(v float64, ref core.VerticalRef) *Limit { return &Limit{ValueM: v, Ref: ref} }

func zoneOf(t core.ZoneType, lower, upper *Limit) *Zone {
	return &Zone{Identifier: "Z", Type: t, Restriction: ed269.Restriction(t.ED269()), Lower: lower, Upper: upper}
}

func geodetic(altAMSLM float64) Aircraft {
	return Aircraft{AltAMSLM: f64(altAMSLM), AltSource: core.AltGeodetic}
}

func pressure(altAMSLM float64) Aircraft {
	return Aircraft{AltAMSLM: f64(altAMSLM), AltSource: core.AltPressure}
}

var (
	noTerrain = Env{}
	ground500 = Env{Ground: GroundKnown, GroundM: 500}
)

// isClear asserts r is "judged and clear".
func isClear(t *testing.T, name string, r Result) {
	t.Helper()
	if r.Raise != nil || r.NotEvaluated || r.LimitNotJudged {
		t.Errorf("%s: got %+v (raise %+v), want judged and clear", name, r, r.Raise)
	}
}

// raised asserts r raised sev and returns the raise.
func raised(t *testing.T, name string, r Result, sev core.Severity) *Raise {
	t.Helper()
	if r.Raise == nil {
		t.Fatalf("%s: raised nothing (not evaluated %v, reasons %q), want %s", name, r.NotEvaluated, r.Reasons, sev)
	}
	if r.NotEvaluated {
		t.Errorf("%s: a raise is not also not evaluated", name)
	}
	if r.Raise.Severity != sev {
		t.Errorf("%s: severity %s, want %s", name, r.Raise.Severity, sev)
	}
	return r.Raise
}

// notEvaluated asserts r is not evaluated for exactly reasons.
func notEvaluated(t *testing.T, name string, r Result, reasons ...Reason) {
	t.Helper()
	if r.Raise != nil || !r.NotEvaluated || r.Reasons != ReasonsOf(reasons...) {
		t.Errorf("%s: got %+v, want not evaluated for %v", name, r, ReasonsOf(reasons...))
	}
}

func TestSeverity(t *testing.T) {
	pol := DefaultPolicy()
	cases := []struct {
		t      core.ZoneType
		cond   core.Severity
		want   core.Severity
		raises bool
	}{
		{core.ZoneProhibited, core.SeverityWarning, core.SeverityCritical, true},
		{core.ZoneReqAuthorization, core.SeverityWarning, core.SeverityWarning, true},
		{core.ZoneConditional, core.SeverityWarning, core.SeverityWarning, true},
		{core.ZoneConditional, core.SeverityInfo, core.SeverityInfo, true},
		{core.ZoneConditional, core.SeverityCritical, core.SeverityCritical, true},
		{core.ZoneConditional, "loud", core.SeverityWarning, true},
		{core.ZoneConditional, "", core.SeverityWarning, true},
		{core.ZoneNoRestriction, core.SeverityWarning, "", false},
		{core.ZoneUSpace, core.SeverityWarning, core.SeverityInfo, true},
		{core.ZoneUSpace, core.SeverityCritical, core.SeverityInfo, true},
		{"", core.SeverityWarning, core.SeverityCritical, true},
		{"FORBIDDEN", core.SeverityWarning, core.SeverityCritical, true},
	}
	for _, tc := range cases {
		pol.ConditionalSeverity = tc.cond
		got, ok := Severity(tc.t, pol)
		if got != tc.want || ok != tc.raises {
			t.Errorf("Severity(%q, conditional %q): got %q %v, want %q %v", tc.t, tc.cond, got, ok, tc.want, tc.raises)
		}
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.PressureUncertaintyM != 250 || p.ConditionalSeverity != core.SeverityWarning || p.MaxHeightAGLM != nil {
		t.Fatalf("DefaultPolicy: %+v", p)
	}
}

func TestConditionalSeverityFromPolicy(t *testing.T) {
	z := zoneOf(core.ZoneConditional, nil, nil)
	pol := DefaultPolicy()
	pol.ConditionalSeverity = core.SeverityInfo
	raised(t, "info policy", JudgeVertical(z, geodetic(100), noTerrain, pol), core.SeverityInfo)
	raised(t, "default policy", JudgeVertical(z, geodetic(100), noTerrain, DefaultPolicy()), core.SeverityWarning)
	// NO_RESTRICTION raises nothing and is not counted.
	r := JudgeVertical(zoneOf(core.ZoneNoRestriction, nil, nil), geodetic(100), noTerrain, pol)
	isClear(t, "no restriction", r)
}

func TestNoAltitude(t *testing.T) {
	banded := zoneOf(core.ZoneProhibited, limit(500, core.RefAMSL), limit(700, core.RefAMSL))
	unbounded := zoneOf(core.ZoneProhibited, nil, nil)
	floorAtGround := zoneOf(core.ZoneProhibited, limit(0, core.RefAGL), nil)
	pol := DefaultPolicy()
	for name, ac := range map[string]Aircraft{
		"nil altitude":            {AltSource: core.AltNone},
		"source none with value":  {AltAMSLM: f64(600), AltSource: core.AltNone},
		"NaN altitude":            {AltAMSLM: f64(math.NaN()), AltSource: core.AltGeodetic},
		"+Inf altitude":           {AltAMSLM: f64(math.Inf(1)), AltSource: core.AltGeodetic},
		"-Inf pressure altitude":  {AltAMSLM: f64(math.Inf(-1)), AltSource: core.AltPressure},
		"nil altitude, geodetic":  {AltSource: core.AltGeodetic},
		"nil altitude, no source": {},
	} {
		r := JudgeVertical(banded, ac, ground500, pol)
		notEvaluated(t, name, r, ReasonNoAltitude)
		var c core.Counters
		r.Count(&c)
		if c.Get(CounterZoneNotEvaluated) != 1 {
			t.Errorf("%s: not counted", name)
		}
		// The presence pair: a zone without a limit that needs a height is
		// judged by horizontal containment alone.
		raised(t, name+", no limits", JudgeVertical(unbounded, ac, ground500, pol), core.SeverityCritical)
		raised(t, name+", AGL floor at ground", JudgeVertical(floorAtGround, ac, noTerrain, pol), core.SeverityCritical)
	}
	// And with an altitude the banded zone is judged.
	raised(t, "600 m", JudgeVertical(banded, geodetic(600), ground500, pol), core.SeverityCritical)
}

func TestInclusiveBounds(t *testing.T) {
	z := zoneOf(core.ZoneProhibited, limit(500, core.RefAMSL), limit(700, core.RefAMSL))
	pol := DefaultPolicy()
	raised(t, "at the floor", JudgeVertical(z, geodetic(500), noTerrain, pol), core.SeverityCritical)
	raised(t, "at the ceiling", JudgeVertical(z, geodetic(700), noTerrain, pol), core.SeverityCritical)
	isClear(t, "just below the floor", JudgeVertical(z, geodetic(499.99), noTerrain, pol))
	isClear(t, "just above the ceiling", JudgeVertical(z, geodetic(700.01), noTerrain, pol))

	agl := zoneOf(core.ZoneProhibited, limit(50, core.RefAGL), limit(120, core.RefAGL))
	d := raised(t, "AGL at the ceiling", JudgeVertical(agl, geodetic(620), ground500, pol), core.SeverityCritical).Detail
	if d.HeightAGLM == nil || *d.HeightAGLM != 120 || d.VerticalKnown != nil || d.WithinBand != nil {
		t.Errorf("AGL detail %+v", d)
	}
	raised(t, "AGL at the floor", JudgeVertical(agl, geodetic(550), ground500, pol), core.SeverityCritical)
	isClear(t, "AGL above the ceiling", JudgeVertical(agl, geodetic(620.01), ground500, pol))
}

func TestEachLimitInItsOwnReference(t *testing.T) {
	pol := DefaultPolicy()
	n := f64(15)
	// A WGS84 floor at 600: 590 AMSL + 15 = 605 HAE is above it.
	z := zoneOf(core.ZoneProhibited, limit(600, core.RefWGS84), nil)
	d := raised(t, "WGS84 floor", JudgeVertical(z, geodetic(590), Env{UndulationM: n}, pol), core.SeverityCritical).Detail
	if d.AltHAEM == nil || *d.AltHAEM != 605 || d.HeightAGLM != nil {
		t.Errorf("WGS84 detail %+v", d)
	}
	isClear(t, "WGS84 floor below", JudgeVertical(z, geodetic(580), Env{UndulationM: n}, pol))
	// A negative undulation lowers HAE.
	isClear(t, "negative undulation", JudgeVertical(z, geodetic(590), Env{UndulationM: f64(-15)}, pol))

	// The AGL ceiling reads the ground; the same number as AMSL would not.
	agl := zoneOf(core.ZoneProhibited, nil, limit(120, core.RefAGL))
	amsl := zoneOf(core.ZoneProhibited, nil, limit(120, core.RefAMSL))
	raised(t, "AGL ceiling over 500 m ground", JudgeVertical(agl, geodetic(600), ground500, pol), core.SeverityCritical)
	isClear(t, "AMSL ceiling at 120", JudgeVertical(amsl, geodetic(600), ground500, pol))
}

func TestNonFiniteEnvironmentIsUnknown(t *testing.T) {
	pol := DefaultPolicy()
	agl := zoneOf(core.ZoneProhibited, nil, limit(120, core.RefAGL))
	cond := zoneOf(core.ZoneConditional, nil, limit(120, core.RefAGL))
	for _, g := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		env := Env{Ground: GroundKnown, GroundM: g}
		r := JudgeVertical(agl, geodetic(5000), env, pol)
		raised(t, "PROHIBITED, non-finite ground", r, core.SeverityWarning)
		if !r.LimitNotJudged || r.Reasons != ReasonsOf(ReasonGroundUnknown) {
			t.Errorf("non-finite ground %v: %+v", g, r)
		}
		notEvaluated(t, "CONDITIONAL, non-finite ground", JudgeVertical(cond, geodetic(5000), env, pol), ReasonGroundUnknown)
	}
	// An out-of-range GroundKind is unknown ground too.
	r := JudgeVertical(agl, geodetic(5000), Env{Ground: GroundKind(7), GroundM: 500}, pol)
	if !r.LimitNotJudged || r.Reasons != ReasonsOf(ReasonGroundUnknown) {
		t.Errorf("unknown ground kind: %+v", r)
	}

	wgs := zoneOf(core.ZoneProhibited, nil, limit(600, core.RefWGS84))
	wgsCond := zoneOf(core.ZoneConditional, nil, limit(600, core.RefWGS84))
	for _, u := range []float64{math.NaN(), math.Inf(1)} {
		limitNotJudged(t, "non-finite undulation", JudgeVertical(wgs, geodetic(550), Env{UndulationM: f64(u)}, pol), []string{"WGS84"}, ReasonNoGeoid)
		notEvaluated(t, "CONDITIONAL, non-finite undulation", JudgeVertical(wgsCond, geodetic(550), Env{UndulationM: f64(u)}, pol), ReasonNoGeoid)
	}
	raised(t, "finite undulation", JudgeVertical(wgs, geodetic(550), Env{UndulationM: f64(15)}, pol), core.SeverityCritical)
}

func TestJudgedLimitDecidesOverUnjudged(t *testing.T) {
	pol := DefaultPolicy()
	// AMSL floor 1000 excludes; the AGL ceiling cannot be judged.
	z := zoneOf(core.ZoneProhibited, limit(1000, core.RefAMSL), limit(120, core.RefAGL))
	isClear(t, "below the AMSL floor", JudgeVertical(z, geodetic(550), noTerrain, pol))
	// Above the floor, the unjudged AGL ceiling warns.
	r := JudgeVertical(z, geodetic(1100), noTerrain, pol)
	d := raised(t, "above the AMSL floor", r, core.SeverityWarning).Detail
	if !r.LimitNotJudged || d.LimitNotJudged == nil || !*d.LimitNotJudged || d.VerticalKnown == nil || *d.VerticalKnown || d.WithinBand != nil {
		t.Errorf("detail %+v", d)
	}
	// An AGL limit judged as excluding decides over an unjudged WGS84 one.
	z2 := zoneOf(core.ZoneProhibited, limit(50, core.RefAGL), limit(600, core.RefWGS84))
	isClear(t, "below the AGL floor, no geoid", JudgeVertical(z2, geodetic(520), ground500, pol))
	limitNotJudged(t, "above the AGL floor, no geoid", JudgeVertical(z2, geodetic(560), ground500, pol), []string{"WGS84"}, ReasonNoGeoid)
}

// limitNotJudged checks the Z-09 warning: severity warning, the flags,
// not_judged naming refs, exactly the reasons, and the counter.
func limitNotJudged(t *testing.T, name string, r Result, refs []string, reasons ...Reason) {
	t.Helper()
	if r.Raise == nil || r.NotEvaluated || !r.LimitNotJudged || r.Reasons != ReasonsOf(reasons...) {
		t.Errorf("%s: got %+v, want a limit_not_judged warning for %v", name, r, ReasonsOf(reasons...))
		return
	}
	d := r.Raise.Detail
	if r.Raise.Severity != core.SeverityWarning || d.LimitNotJudged == nil || !*d.LimitNotJudged ||
		d.VerticalKnown == nil || *d.VerticalKnown || !slices.Equal(d.NotJudged, refs) {
		t.Errorf("%s: raise %+v, want warning not judged %v", name, *r.Raise, refs)
	}
	var c core.Counters
	r.Count(&c)
	if c.Get(CounterZoneNotEvaluated) != 0 || c.Get(CounterZoneLimitNotJudged) != 1 {
		t.Errorf("%s: counters %v", name, c.Snapshot())
	}
}

// Z-09 with S-37: no terrain and no geoid, a PROHIBITED zone warns and
// names both references; a CONDITIONAL one is not evaluated.
func TestUnjudgedAGLAndWGS84Warns(t *testing.T) {
	pol := DefaultPolicy()
	z := zoneOf(core.ZoneProhibited, limit(50, core.RefAGL), limit(600, core.RefWGS84))
	r := JudgeVertical(z, geodetic(560), noTerrain, pol)
	limitNotJudged(t, "AGL and WGS84 both unknown", r, []string{"AGL", "WGS84"}, ReasonNoTerrain, ReasonNoGeoid)
	cond := zoneOf(core.ZoneConditional, limit(50, core.RefAGL), limit(600, core.RefWGS84))
	notEvaluated(t, "CONDITIONAL, both unknown", JudgeVertical(cond, geodetic(560), noTerrain, pol), ReasonNoTerrain, ReasonNoGeoid)
	// The presence pair: with both known it is judged.
	raised(t, "both known", JudgeVertical(z, geodetic(560), Env{Ground: GroundKnown, GroundM: 500, UndulationM: f64(15)}, pol), core.SeverityCritical)
}

func TestLimitNotJudgedReasonAndCounter(t *testing.T) {
	pol := DefaultPolicy()
	z := zoneOf(core.ZoneReqAuthorization, nil, limit(120, core.RefAGL))
	for _, tc := range []struct {
		env    Env
		reason Reason
	}{{noTerrain, ReasonNoTerrain}, {Env{Ground: GroundUnknown}, ReasonGroundUnknown}} {
		r := JudgeVertical(z, geodetic(550), tc.env, pol)
		d := raised(t, string(tc.reason), r, core.SeverityWarning).Detail
		if !r.LimitNotJudged || r.Reasons != ReasonsOf(tc.reason) || len(d.NotJudged) != 1 || d.NotJudged[0] != "AGL" || d.HeightAGLM != nil {
			t.Errorf("%s: %+v %+v", tc.reason, r, d)
		}
		var c core.Counters
		r.Count(&c)
		if c.Get(CounterZoneLimitNotJudged) != 1 || c.Get(CounterZoneNotEvaluated) != 0 {
			t.Errorf("%s: counters %v", tc.reason, c.Snapshot())
		}
	}
	// The presence pair: with the ground, no flag and no counter.
	r := JudgeVertical(z, geodetic(550), ground500, pol)
	d := raised(t, "with ground", r, core.SeverityWarning).Detail
	if r.LimitNotJudged || d.LimitNotJudged != nil || d.VerticalKnown != nil || d.NotJudged != nil {
		t.Errorf("with ground: %+v %+v", r, d)
	}
	var c core.Counters
	r.Count(&c)
	if len(c.Names()) != 0 {
		t.Errorf("counted %v", c.Snapshot())
	}
}

func TestPressureMarginEdges(t *testing.T) {
	pol := DefaultPolicy()
	z := zoneOf(core.ZoneProhibited, limit(500, core.RefAMSL), limit(700, core.RefAMSL))
	// Exactly at the widened ceiling (700 + 250) is inside the widened band.
	d := raised(t, "at the widened ceiling", JudgeVertical(z, pressure(950), noTerrain, pol), core.SeverityWarning).Detail
	if d.WithinBand == nil || *d.WithinBand || d.VerticalKnown == nil || *d.VerticalKnown {
		t.Errorf("widened detail %+v", d)
	}
	isClear(t, "past the widened ceiling", JudgeVertical(z, pressure(950.01), noTerrain, pol))
	// The floor widens downwards the same way.
	raised(t, "below the floor, within the margin", JudgeVertical(z, pressure(260), noTerrain, pol), core.SeverityWarning)
	isClear(t, "below the widened floor", JudgeVertical(z, pressure(249.99), noTerrain, pol))
	// Inside as indicated keeps the zone's own severity, flagged.
	d = raised(t, "inside as indicated", JudgeVertical(z, pressure(600), noTerrain, pol), core.SeverityCritical).Detail
	if d.WithinBand == nil || !*d.WithinBand || d.VerticalKnown == nil || *d.VerticalKnown {
		t.Errorf("within-band detail %+v", d)
	}
	// The margin is policy: a 0 m margin judges as indicated.
	pol.PressureUncertaintyM = 0
	isClear(t, "0 m margin", JudgeVertical(z, pressure(701), noTerrain, pol))
}

func TestInvalidPressureMarginIsUnbounded(t *testing.T) {
	z := zoneOf(core.ZoneProhibited, limit(500, core.RefAMSL), limit(700, core.RefAMSL))
	for _, m := range []float64{math.NaN(), -1, math.Inf(1)} {
		pol := DefaultPolicy()
		pol.PressureUncertaintyM = m
		d := raised(t, "invalid margin", JudgeVertical(z, pressure(5000), noTerrain, pol), core.SeverityWarning).Detail
		if d.WithinBand == nil || *d.WithinBand {
			t.Errorf("margin %v: %+v", m, d)
		}
	}
	// The presence pair: a valid margin clears the same aircraft.
	isClear(t, "valid margin", JudgeVertical(z, pressure(5000), noTerrain, DefaultPolicy()))
}

func TestAltitudeSources(t *testing.T) {
	pol := DefaultPolicy()
	z := zoneOf(core.ZoneProhibited, nil, limit(700, core.RefAMSL))
	// Network altitudes are judged as indicated, like geodetic ones.
	r := JudgeVertical(z, Aircraft{AltAMSLM: f64(600), AltSource: core.AltNetwork}, noTerrain, pol)
	if d := raised(t, "network", r, core.SeverityCritical).Detail; d.VerticalKnown != nil {
		t.Errorf("network flagged: %+v", d)
	}
	isClear(t, "network above", JudgeVertical(z, Aircraft{AltAMSLM: f64(800), AltSource: core.AltNetwork}, noTerrain, pol))
	// A source this package does not know is widened like a pressure one.
	r = JudgeVertical(z, Aircraft{AltAMSLM: f64(800), AltSource: "baro"}, noTerrain, pol)
	if d := raised(t, "unknown source", r, core.SeverityWarning).Detail; d.VerticalKnown == nil || *d.VerticalKnown {
		t.Errorf("unknown source not flagged: %+v", d)
	}
}

// Being possibly inside (the widened band) never raises more than being
// definitely inside: an info zone stays info, and anything above warning
// is capped at warning. The old monitor raised warning for the info
// zone; no vector covers it (owner decision on PR #12).
func TestWidenedBandIsCappedAtTheZoneSeverity(t *testing.T) {
	pol := DefaultPolicy()
	pol.ConditionalSeverity = core.SeverityInfo
	info := zoneOf(core.ZoneConditional, nil, limit(700, core.RefAMSL))
	raised(t, "info zone, inside", JudgeVertical(info, pressure(600), noTerrain, pol), core.SeverityInfo)
	d := raised(t, "info zone, widened", JudgeVertical(info, pressure(800), noTerrain, pol), core.SeverityInfo).Detail
	if d.WithinBand == nil || *d.WithinBand {
		t.Errorf("info zone, widened: within_band %+v", d.WithinBand)
	}
	// The pair: a critical zone in the widened band is still a warning.
	crit := zoneOf(core.ZoneProhibited, nil, limit(700, core.RefAMSL))
	raised(t, "critical zone, inside", JudgeVertical(crit, pressure(600), noTerrain, pol), core.SeverityCritical)
	raised(t, "critical zone, widened", JudgeVertical(crit, pressure(800), noTerrain, pol), core.SeverityWarning)
	if got := atMostWarning(core.SeverityCritical); got != core.SeverityWarning {
		t.Errorf("atMostWarning(critical) = %s", got)
	}
	if got := atMostWarning(core.SeverityWarning); got != core.SeverityWarning {
		t.Errorf("atMostWarning(warning) = %s", got)
	}
}

func TestInvalidZone(t *testing.T) {
	pol := DefaultPolicy()
	notEvaluated(t, "nil zone", JudgeVertical(nil, geodetic(1), noTerrain, pol), ReasonInvalidZone)
	for name, l := range map[string]*Limit{
		"NaN limit":      limit(math.NaN(), core.RefAMSL),
		"Inf limit":      limit(math.Inf(-1), core.RefAMSL),
		"unknown ref":    limit(100, "QNH"),
		"empty ref":      limit(100, ""),
		"NaN AGL at 0th": limit(math.NaN(), core.RefAGL),
	} {
		notEvaluated(t, name+" as upper", JudgeVertical(zoneOf(core.ZoneProhibited, nil, l), geodetic(1), noTerrain, pol), ReasonInvalidZone)
		notEvaluated(t, name+" as lower", JudgeVertical(zoneOf(core.ZoneProhibited, l, nil), geodetic(1), noTerrain, pol), ReasonInvalidZone)
	}
}

// Finite inputs whose sum overflows to infinity still compare: far above
// a ceiling is outside, far above a floor is inside.
func TestOverflowCompares(t *testing.T) {
	pol := DefaultPolicy()
	env := Env{UndulationM: f64(math.MaxFloat64)}
	isClear(t, "HAE overflows above a ceiling", JudgeVertical(zoneOf(core.ZoneProhibited, nil, limit(600, core.RefWGS84)), geodetic(math.MaxFloat64), env, pol))
	raised(t, "HAE overflows above a floor", JudgeVertical(zoneOf(core.ZoneProhibited, limit(600, core.RefWGS84), nil), geodetic(math.MaxFloat64), env, pol), core.SeverityCritical)
}

func TestResultCount(t *testing.T) {
	var c core.Counters
	Result{}.Count(&c)
	Result{Raise: &Raise{}}.Count(&c)
	if len(c.Names()) != 0 {
		t.Fatalf("a clear or plain raise counted %v", c.Snapshot())
	}
	Result{NotEvaluated: true}.Count(&c)
	Result{Raise: &Raise{}, LimitNotJudged: true}.Count(&c)
	Result{NotEvaluated: true, height: true}.Count(&c)
	want := map[string]uint64{CounterZoneNotEvaluated: 1, CounterZoneLimitNotJudged: 1, CounterHeightNotEvaluated: 1}
	for k, v := range want {
		if c.Get(k) != v {
			t.Errorf("%s: %d, want %d", k, c.Get(k), v)
		}
	}
}

// S-37 (owner decision): a WGS84 limit without the geoid is treated as an
// AGL limit without the DEM. PROHIBITED and REQ_AUTHORIZATION warn with
// limit_not_judged and reason no_geoid, counted; CONDITIONAL stays not
// evaluated, since a warning would exceed an info zone's severity. The
// pair: with the geoid the same aircraft is judged, and nothing counted.
func TestWGS84WithoutGeoidWarns(t *testing.T) {
	pol := DefaultPolicy()
	for _, typ := range []core.ZoneType{core.ZoneProhibited, core.ZoneReqAuthorization} {
		z := zoneOf(typ, nil, limit(600, core.RefWGS84))
		for name, env := range map[string]Env{
			"no geoid":       noTerrain,
			"NaN undulation": {UndulationM: f64(math.NaN())},
		} {
			limitNotJudged(t, string(typ)+", "+name, JudgeVertical(z, geodetic(550), env, pol), []string{"WGS84"}, ReasonNoGeoid)
		}
	}
	cond := zoneOf(core.ZoneConditional, nil, limit(600, core.RefWGS84))
	r := JudgeVertical(cond, geodetic(550), noTerrain, pol)
	notEvaluated(t, "CONDITIONAL, no geoid", r, ReasonNoGeoid)
	var cc core.Counters
	r.Count(&cc)
	if cc.Get(CounterZoneNotEvaluated) != 1 || cc.Get(CounterZoneLimitNotJudged) != 0 {
		t.Errorf("CONDITIONAL: counters %v", cc.Snapshot())
	}

	z := zoneOf(core.ZoneProhibited, nil, limit(600, core.RefWGS84))
	geoid := Env{UndulationM: f64(15)}
	r = JudgeVertical(z, geodetic(550), geoid, pol)
	d := raised(t, "with geoid, inside", r, core.SeverityCritical).Detail
	if r.LimitNotJudged || r.Reasons != 0 || d.LimitNotJudged != nil || d.NotJudged != nil || d.AltHAEM == nil {
		t.Errorf("with geoid: %+v, detail %+v", r, d)
	}
	isClear(t, "with geoid, above", JudgeVertical(z, geodetic(590), geoid, pol))
	var c core.Counters
	r.Count(&c)
	if len(c.Names()) != 0 {
		t.Errorf("with geoid: counted %v", c.Snapshot())
	}
}

// USPACE raises info so that presence in U-space airspace is visible
// (owner decision, PR #12); the pair is NO_RESTRICTION, which raises
// nothing for the same aircraft.
func TestUSpaceRaisesInfo(t *testing.T) {
	pol := DefaultPolicy()
	u := &Zone{Identifier: "U", Type: core.ZoneUSpace, Upper: limit(120, core.RefAMSL)}
	d := raised(t, "inside U-space", JudgeVertical(u, geodetic(100), noTerrain, pol), core.SeverityInfo).Detail
	if d.Identifier != "U" || d.VerticalKnown != nil {
		t.Errorf("detail %+v", d)
	}
	isClear(t, "above U-space", JudgeVertical(u, geodetic(130), noTerrain, pol))
	// Widened: possibly inside stays at info, never above it.
	raised(t, "U-space, widened", JudgeVertical(u, pressure(300), noTerrain, pol), core.SeverityInfo)

	n := &Zone{Identifier: "N", Type: core.ZoneNoRestriction, Upper: limit(120, core.RefAMSL)}
	isClear(t, "inside NO_RESTRICTION", JudgeVertical(n, geodetic(100), noTerrain, pol))
}

// Every missing reference is reported, not only the first.
func TestReasonsCarryEveryMissingReference(t *testing.T) {
	pol := DefaultPolicy()
	z := zoneOf(core.ZoneProhibited, limit(50, core.RefAGL), limit(600, core.RefWGS84))
	r := JudgeVertical(z, geodetic(560), noTerrain, pol)
	want := ReasonsOf(ReasonNoTerrain, ReasonNoGeoid)
	if !r.LimitNotJudged || r.Reasons != want {
		t.Fatalf("got %+v, want limit not judged for %v", r, want)
	}
	if !r.Reasons.Has(ReasonNoTerrain) || !r.Reasons.Has(ReasonNoGeoid) || r.Reasons.Has(ReasonGroundUnknown) || r.Reasons.Has("") {
		t.Errorf("Has: %v", r.Reasons)
	}
	if got := r.Reasons.String(); got != "no_terrain,no_geoid" {
		t.Errorf("String: %q", got)
	}
	// Unknown ground and no geoid.
	r = JudgeVertical(z, geodetic(560), Env{Ground: GroundUnknown}, pol)
	if r.Reasons != ReasonsOf(ReasonGroundUnknown, ReasonNoGeoid) {
		t.Errorf("unknown ground: %v", r.Reasons)
	}
	// The pair: one missing reference gives one reason.
	r = JudgeVertical(z, geodetic(560), Env{UndulationM: f64(15)}, pol)
	if !r.LimitNotJudged || r.Reasons != ReasonsOf(ReasonNoTerrain) || len(r.Reasons.List()) != 1 {
		t.Errorf("AGL only: %+v", r)
	}
	if Reasons(0).String() != "" || Reasons(0).List() != nil || ReasonsOf("bogus") != 0 {
		t.Errorf("empty set")
	}
}
