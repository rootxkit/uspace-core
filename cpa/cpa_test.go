package cpa

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// origin is the point the cpa.json cases were built from.
var origin = core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}

// at returns a hovering, vertically known state northM and eastM metres
// from origin at altAMSLM, captured at 0.
func at(northM, eastM, altAMSLM float64) State {
	northPerDegM, eastPerDegM := metresPerDegree(origin.LatDeg)
	return State{
		Pos: core.LatLon{
			LatDeg: origin.LatDeg + northM/northPerDegM,
			LonDeg: core.WrapLonDeg(origin.LonDeg + eastM/eastPerDegM),
		},
		AltAMSLM:      altAMSLM,
		VerticalKnown: true,
	}
}

func (s State) moving(vn, ve, vd float64) State {
	s.VNMS, s.VEMS, s.VDMS = vn, ve, vd
	return s
}

func (s State) capturedAt(tS float64) State {
	s.CapturedAtS = tS
	return s
}

func (s State) pressure() State {
	s.VerticalKnown = false
	return s
}

func mustJudge(t *testing.T, r Result) {
	t.Helper()
	if !r.Judged || r.NotJudged != ReasonNone {
		t.Fatalf("not judged (%q), want judged", r.NotJudged)
	}
}

func mustNotJudge(t *testing.T, r Result, want Reason) {
	t.Helper()
	if r.Judged || r.Conflict || r.NotJudged != want {
		t.Fatalf("got judged=%v conflict=%v reason=%q, want not judged, %q", r.Judged, r.Conflict, r.NotJudged, want)
	}
}

func TestDefaultPolicyMatchesSpec(t *testing.T) {
	want := Policy{TCPAMaxS: 60, DHorizontalMinM: 60, DVerticalMinM: 20, NeighbourRadiusM: 800, NeighbourMaxAgeS: 10}
	if DefaultPolicy != want {
		t.Fatalf("DefaultPolicy = %+v, want %+v (spec 04 §3.3)", DefaultPolicy, want)
	}
}

// Presence and absence pairs: each test makes the conflict happen and
// then shows the twin that does not.

func TestHoveringInsideMinimaIsConflict(t *testing.T) {
	a := at(0, 0, 550)
	inside := Evaluate(a, at(30, 0, 550).moving(-0.01, 0, 0), DefaultPolicy)
	mustJudge(t, inside)
	if !inside.Conflict || inside.TCPAS < DefaultPolicy.TCPAMaxS {
		t.Fatalf("30 m apart with noise: %+v, want conflict with t_cpa beyond the window", inside)
	}
	outside := Evaluate(a, at(80, 0, 550).moving(-0.01, 0, 0), DefaultPolicy)
	mustJudge(t, outside)
	if outside.Conflict {
		t.Fatalf("80 m apart with noise: %+v, want no conflict", outside)
	}
}

func TestStationaryAndParallelDoNotDivideByZero(t *testing.T) {
	for _, tc := range []struct {
		name         string
		a, b         State
		wantConflict bool
	}{
		{"both-hovering-inside", at(0, 0, 550), at(30, 40, 550), true},
		{"both-hovering-outside", at(0, 0, 550), at(300, 400, 550), false},
		{"parallel-inside", at(0, 0, 550).moving(10, 0, 0), at(0, 45, 550).moving(10, 0, 0), true},
		{"parallel-outside", at(0, 0, 550).moving(10, 0, 0), at(0, 75, 550).moving(10, 0, 0), false},
		{"below-guard-speed", at(0, 0, 550), at(1000, 0, 550).moving(-1e-7, 0, 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Evaluate(tc.a, tc.b, DefaultPolicy)
			mustJudge(t, r)
			if r.TCPAS != 0 || r.DCPAHorizontalM != r.DHorizontalNowM {
				t.Fatalf("%+v, want t_cpa 0 and the current distance", r)
			}
			if r.Conflict != tc.wantConflict {
				t.Fatalf("conflict = %v, want %v", r.Conflict, tc.wantConflict)
			}
		})
	}
}

func TestDivergingIsNotConflictUnlessInsideNow(t *testing.T) {
	far := Evaluate(at(0, 0, 550).moving(-10, 0, 0), at(100, 0, 550).moving(10, 0, 0), DefaultPolicy)
	mustJudge(t, far)
	if far.Conflict || far.TCPAS != 0 {
		t.Fatalf("diverging 100 m apart: %+v, want no conflict, t_cpa 0", far)
	}
	tooClose := Evaluate(at(0, 0, 550).moving(-1, 0, 0), at(10, 0, 550).moving(1, 0, 0), DefaultPolicy)
	mustJudge(t, tooClose)
	if !tooClose.Conflict || tooClose.TCPAS != 0 {
		t.Fatalf("diverging 10 m apart: %+v, want conflict, t_cpa 0", tooClose)
	}
}

func TestStrictMinima(t *testing.T) {
	// A policy whose minima sit exactly on the computed distances: equal
	// is outside, a hair more is inside.
	a, b := at(0, 0, 550), at(50, 0, 560)
	r := Evaluate(a, b, DefaultPolicy)
	mustJudge(t, r)
	pol := DefaultPolicy
	pol.DHorizontalMinM, pol.DVerticalMinM = r.DHorizontalNowM, 100
	if Evaluate(a, b, pol).Conflict {
		t.Fatal("horizontal distance equal to the minimum is a conflict, want not (strict <)")
	}
	pol.DHorizontalMinM = math.Nextafter(r.DHorizontalNowM, math.Inf(1))
	if !Evaluate(a, b, pol).Conflict {
		t.Fatal("horizontal distance just below the minimum is not a conflict")
	}
	pol.DVerticalMinM = r.DAltNowM
	if Evaluate(a, b, pol).Conflict {
		t.Fatal("vertical gap equal to the minimum is a conflict, want not (strict <)")
	}
	pol.DVerticalMinM = math.Nextafter(r.DAltNowM, math.Inf(1))
	if !Evaluate(a, b, pol).Conflict {
		t.Fatal("vertical gap just below the minimum is not a conflict")
	}
}

func TestWindowIsStrict(t *testing.T) {
	a, b := at(0, 0, 550).moving(10, 0, 0), at(1000, 0, 550).moving(-10, 0, 0)
	r := Evaluate(a, b, DefaultPolicy)
	mustJudge(t, r)
	pol := DefaultPolicy
	pol.TCPAMaxS = r.TCPAS
	if Evaluate(a, b, pol).Conflict {
		t.Fatal("t_cpa equal to the window is a conflict, want not (strict <)")
	}
	pol.TCPAMaxS = math.Nextafter(r.TCPAS, math.Inf(1))
	if !Evaluate(a, b, pol).Conflict {
		t.Fatal("t_cpa just inside the window is not a conflict")
	}
}

func TestUnknownVertical(t *testing.T) {
	a, b := at(0, 0, 500).moving(10, 0, 0), at(500, 0, 600).moving(-10, 0, 0)
	known := Evaluate(a, b, DefaultPolicy)
	mustJudge(t, known)
	if known.Conflict || !known.VerticalKnown || known.DAltNowM != 100 {
		t.Fatalf("geodetic 100 m apart: %+v, want no conflict, vertical known", known)
	}
	for _, tc := range []struct {
		name string
		a, b State
	}{{"both-pressure", a.pressure(), b.pressure()}, {"a-pressure", a.pressure(), b}, {"b-pressure", a, b.pressure()}} {
		t.Run(tc.name, func(t *testing.T) {
			r := Evaluate(tc.a, tc.b, DefaultPolicy)
			mustJudge(t, r)
			if !r.Conflict || r.VerticalKnown || r.DAltNowM != 0 || r.DAltAtCPAM != 0 {
				t.Fatalf("%+v, want conflict on the horizontal alone, vertical unknown, altitude gaps zero", r)
			}
		})
	}
	// Unknown vertical is not a conflict by itself.
	apart := Evaluate(at(0, 0, 500).pressure(), at(500, 0, 500).pressure(), DefaultPolicy)
	mustJudge(t, apart)
	if apart.Conflict {
		t.Fatalf("horizontally clear pressure tracks: %+v, want no conflict", apart)
	}
}

func TestVerticalAtCPA(t *testing.T) {
	a, b := at(0, 0, 550).moving(10, 0, 0), at(1000, 0, 600).moving(-10, 0, 0)
	level := Evaluate(a, b, DefaultPolicy)
	mustJudge(t, level)
	if level.Conflict || math.Abs(level.DAltAtCPAM-50) > 1e-9 {
		t.Fatalf("level, 50 m apart: %+v, want no conflict", level)
	}
	// Climbing at 1 m/s (VD -1, down positive) closes 50 m in 50 s.
	climb := Evaluate(a.moving(10, 0, -1), b, DefaultPolicy)
	mustJudge(t, climb)
	if !climb.Conflict || climb.DAltAtCPAM > 0.01 || climb.DAltNowM != 50 {
		t.Fatalf("climbing: %+v, want conflict with the gap closed at t_cpa", climb)
	}
	// Descending at 1 m/s opens it instead.
	descend := Evaluate(a.moving(10, 0, 1), b, DefaultPolicy)
	mustJudge(t, descend)
	if descend.Conflict || math.Abs(descend.DAltAtCPAM-100) > 0.01 {
		t.Fatalf("descending: %+v, want no conflict, 100 m at t_cpa", descend)
	}
}

func TestStaleNeighbour(t *testing.T) {
	a := at(0, 0, 550).moving(10, 0, 0)
	b := at(600, 0, 550).moving(-10, 0, 0)
	atBound := Evaluate(a.capturedAt(10), b.capturedAt(0), DefaultPolicy)
	mustJudge(t, atBound)
	if !atBound.Conflict {
		t.Fatalf("exactly at the max age: %+v, want judged conflict", atBound)
	}
	beyond := Evaluate(a.capturedAt(math.Nextafter(10, 11)), b.capturedAt(0), DefaultPolicy)
	mustNotJudge(t, beyond, ReasonStaleNeighbour)
	mustNotJudge(t, Evaluate(b.capturedAt(0), a.capturedAt(11), DefaultPolicy), ReasonStaleNeighbour)
}

func TestOlderSampleIsAdvanced(t *testing.T) {
	// A is 5 s older and 50 m back down its track: advanced, the answer
	// is the same as with fresh samples.
	fresh := Evaluate(at(0, 0, 550).moving(10, 0, 0), at(1000, 0, 550).moving(-10, 0, 0), DefaultPolicy)
	older := Evaluate(at(-50, 0, 550).moving(10, 0, 0), at(1000, 0, 550).moving(-10, 0, 0).capturedAt(5), DefaultPolicy)
	mustJudge(t, fresh)
	mustJudge(t, older)
	if math.Abs(older.TCPAS-fresh.TCPAS) > 0.01 || math.Abs(older.DHorizontalNowM-fresh.DHorizontalNowM) > 0.01 {
		t.Fatalf("advanced %+v, fresh %+v: want the same t_cpa and distance", older, fresh)
	}
	// Climbing older sample: its altitude is advanced too (down positive).
	climbing := Evaluate(at(0, 0, 500).moving(0, 0, -2), at(0, 100, 520).capturedAt(5), DefaultPolicy)
	mustJudge(t, climbing)
	if math.Abs(climbing.DAltNowM-10) > 1e-9 {
		t.Fatalf("d_alt_now = %v, want 10 (500 m + 2 m/s x 5 s vs 520 m)", climbing.DAltNowM)
	}
}

func TestSymmetry(t *testing.T) {
	a := at(-300, 0, 550).moving(10, 0, 0.5).capturedAt(2)
	b := at(0, 500, 540).moving(0, -10, -0.3).pressure()
	if ab, ba := Evaluate(a, b, DefaultPolicy), Evaluate(b, a, DefaultPolicy); ab != ba {
		t.Fatalf("Evaluate(a, b) = %+v, Evaluate(b, a) = %+v", ab, ba)
	}
	// Two samples that differ only in VerticalKnown still have one order.
	c := at(0, 0, 550)
	if ab, ba := Evaluate(c, c.pressure(), DefaultPolicy), Evaluate(c.pressure(), c, DefaultPolicy); ab != ba || ab.VerticalKnown {
		t.Fatalf("Evaluate(c, c') = %+v, Evaluate(c', c) = %+v", ab, ba)
	}
}

func TestAntimeridianPair(t *testing.T) {
	// 0.001 degrees either side of the antimeridian at the equator: about
	// 223 m apart (D-10), closing head-on east-west.
	a := State{Pos: core.LatLon{LatDeg: 0, LonDeg: 179.999}, AltAMSLM: 100, VerticalKnown: true, VEMS: 5}
	b := State{Pos: core.LatLon{LatDeg: 0, LonDeg: -179.999}, AltAMSLM: 100, VerticalKnown: true, VEMS: -5}
	r := Evaluate(a, b, DefaultPolicy)
	mustJudge(t, r)
	if r.DHorizontalNowM > 250 || r.DHorizontalNowM < 200 || !r.Conflict {
		t.Fatalf("%+v, want ~223 m now and a conflict", r)
	}
}

func TestAdvance(t *testing.T) {
	s := at(0, 0, 550).moving(10, -5, 2).capturedAt(3)
	if got := Advance(s, 3); got != s {
		t.Fatalf("Advance by 0 s = %+v, want identity", got)
	}
	got := Advance(s, 13)
	northM, eastM := geodesy.LocalOffsetM(s.Pos, got.Pos)
	if math.Abs(northM-100) > 1e-6 || math.Abs(eastM+50) > 1e-6 {
		t.Fatalf("moved %v m north, %v m east, want 100, -50", northM, eastM)
	}
	if got.AltAMSLM != 530 || got.CapturedAtS != 13 {
		t.Fatalf("alt %v at %v, want 530 at 13 (down positive)", got.AltAMSLM, got.CapturedAtS)
	}
	// Back again: the radii are taken at each state's own latitude, so the
	// round trip is not exact, but it is well under a millimetre.
	back := Advance(got, 3)
	if n, e := geodesy.LocalOffsetM(s.Pos, back.Pos); math.Hypot(n, e) > 1e-3 || back.AltAMSLM != s.AltAMSLM {
		t.Fatalf("advance and back: %+v, want %+v", back, s)
	}
}

func TestAdvanceAcrossAntimeridian(t *testing.T) {
	east := State{Pos: core.LatLon{LatDeg: 10, LonDeg: 179.9995}, VEMS: 20}
	got := Advance(east, 10)
	if !got.Pos.Valid() || got.Pos.LonDeg > -179 {
		t.Fatalf("eastbound across: lon %v, want just east of -180", got.Pos.LonDeg)
	}
	_, eastM := geodesy.LocalOffsetM(east.Pos, got.Pos)
	if math.Abs(eastM-200) > 1e-6 {
		t.Fatalf("moved %v m east, want 200", eastM)
	}
	west := State{Pos: core.LatLon{LatDeg: -10, LonDeg: -179.9995}, VEMS: -20}
	got = Advance(west, 10)
	if !got.Pos.Valid() || got.Pos.LonDeg < 179 {
		t.Fatalf("westbound across: lon %v, want just west of 180", got.Pos.LonDeg)
	}
}

// TestNonFiniteInputsAreNotJudged: every number that takes part in the
// judgement refuses it when non-finite; the altitude and vertical speed
// of a track whose vertical is unknown take no part, and those rows are
// the presence twins: judged, and a conflict.
func TestNonFiniteInputsAreNotJudged(t *testing.T) {
	good := at(0, 0, 550)
	near := at(10, 0, 550) // a conflict when judged
	if r := Evaluate(good, near, DefaultPolicy); !r.Judged || !r.Conflict {
		t.Fatalf("the base pair must be a judged conflict, got %+v", r)
	}
	nan, inf := math.NaN(), math.Inf(1)
	for _, tc := range []struct {
		name   string
		mut    func(*State)
		judged bool
	}{
		{"lat-nan", func(s *State) { s.Pos.LatDeg = nan }, false},
		{"lat-inf", func(s *State) { s.Pos.LatDeg = inf }, false},
		{"lat-out-of-range", func(s *State) { s.Pos.LatDeg = 91 }, false},
		{"lon-nan", func(s *State) { s.Pos.LonDeg = nan }, false},
		{"lon-out-of-range", func(s *State) { s.Pos.LonDeg = -181 }, false},
		{"alt-nan", func(s *State) { s.AltAMSLM = nan }, false},
		{"alt-inf", func(s *State) { s.AltAMSLM = -inf }, false},
		{"vn-nan", func(s *State) { s.VNMS = nan }, false},
		{"ve-inf", func(s *State) { s.VEMS = inf }, false},
		{"vd-nan", func(s *State) { s.VDMS = nan }, false},
		{"captured-nan", func(s *State) { s.CapturedAtS = nan }, false},
		{"captured-inf", func(s *State) { s.CapturedAtS = inf }, false},
		{"alt-nan-pressure", func(s *State) { s.AltAMSLM, s.VerticalKnown = nan, false }, true},
		{"alt-inf-pressure", func(s *State) { s.AltAMSLM, s.VerticalKnown = inf, false }, true},
		{"vd-nan-pressure", func(s *State) { s.VDMS, s.VerticalKnown = nan, false }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := near
			tc.mut(&bad)
			for _, r := range []Result{Evaluate(good, bad, DefaultPolicy), Evaluate(bad, good, DefaultPolicy)} {
				if !tc.judged {
					mustNotJudge(t, r, ReasonInvalidInput)
					continue
				}
				mustJudge(t, r)
				if !r.Conflict || r.VerticalKnown {
					t.Fatalf("%+v, want a horizontal conflict with the vertical unknown", r)
				}
			}
		})
	}
}

// TestNaNAltitudeOnPressureTrack: the altitude and vertical speed of a
// track whose vertical is unknown take no part in the judgement, so a NaN
// there is judged on the horizontal (a conflict here); the same NaN on a
// track whose vertical is known is invalid input.
func TestNaNAltitudeOnPressureTrack(t *testing.T) {
	a := at(0, 0, 500).moving(10, 0, 0)
	b := at(500, 0, 600).moving(-10, 0, 0)
	nan := math.NaN()
	for _, tc := range []struct {
		name string
		mut  func(*State)
	}{
		{"alt", func(s *State) { s.AltAMSLM = nan }},
		{"alt-inf", func(s *State) { s.AltAMSLM = math.Inf(-1) }},
		{"vd", func(s *State) { s.VDMS = nan }},
		{"alt-and-vd", func(s *State) { s.AltAMSLM, s.VDMS = nan, nan }},
	} {
		t.Run(tc.name+"-pressure-judged", func(t *testing.T) {
			bad := b.pressure()
			tc.mut(&bad)
			for _, r := range []Result{Evaluate(a, bad, DefaultPolicy), Evaluate(bad, a, DefaultPolicy)} {
				mustJudge(t, r)
				if !r.Conflict || r.VerticalKnown || r.DAltNowM != 0 || r.DAltAtCPAM != 0 {
					t.Fatalf("%+v, want a horizontal conflict with the vertical unknown", r)
				}
			}
			if ab, ba := Evaluate(a, bad, DefaultPolicy), Evaluate(bad, a, DefaultPolicy); ab != ba {
				t.Fatalf("asymmetric: %+v vs %+v", ab, ba)
			}
			// Advanced from an older sample, too.
			mustJudge(t, Evaluate(a.capturedAt(5), bad, DefaultPolicy))
			mustJudge(t, Evaluate(a, bad.capturedAt(5), DefaultPolicy))
		})
		t.Run(tc.name+"-known-not-judged", func(t *testing.T) {
			bad := b
			tc.mut(&bad)
			mustNotJudge(t, Evaluate(a, bad, DefaultPolicy), ReasonInvalidInput)
			mustNotJudge(t, Evaluate(bad, a, DefaultPolicy), ReasonInvalidInput)
		})
	}
}

func TestInvalidPolicyIsNotJudged(t *testing.T) {
	a, b := at(0, 0, 550), at(10, 0, 550)
	for _, tc := range []struct {
		name string
		mut  func(*Policy)
	}{
		{"t-max-nan", func(p *Policy) { p.TCPAMaxS = math.NaN() }},
		{"h-min-nan", func(p *Policy) { p.DHorizontalMinM = math.NaN() }},
		{"v-min-inf", func(p *Policy) { p.DVerticalMinM = math.Inf(1) }},
		{"max-age-negative", func(p *Policy) { p.NeighbourMaxAgeS = -1 }},
		{"max-age-nan", func(p *Policy) { p.NeighbourMaxAgeS = math.NaN() }},
		{"h-min-zero", func(p *Policy) { p.DHorizontalMinM = 0 }},
		{"v-min-zero", func(p *Policy) { p.DVerticalMinM = 0 }},
		{"h-min-negative", func(p *Policy) { p.DHorizontalMinM = -60 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pol := DefaultPolicy
			tc.mut(&pol)
			mustNotJudge(t, Evaluate(a, b, pol), ReasonInvalidPolicy)
		})
	}
	// The radius is the caller's: Evaluate does not read it.
	pol := DefaultPolicy
	pol.NeighbourRadiusM = math.NaN()
	mustJudge(t, Evaluate(a, b, pol))
}

// TestZeroWindowMeansNowOnly: TCPAMaxS 0 is a valid policy that judges
// "inside the minima now" only. A head-on pair 1 km apart is clear under
// it and a pair inside the minima now is still a conflict; the smallest
// positive minima are valid too.
func TestZeroWindowMeansNowOnly(t *testing.T) {
	pol := DefaultPolicy
	pol.TCPAMaxS = 0
	headOn := Evaluate(at(0, 0, 550).moving(10, 0, 0), at(1000, 0, 550).moving(-10, 0, 0), pol)
	mustJudge(t, headOn)
	if headOn.Conflict {
		t.Fatalf("head-on in 50 s with a zero window: %+v, want clear", headOn)
	}
	if !Evaluate(at(0, 0, 550).moving(10, 0, 0), at(1000, 0, 550).moving(-10, 0, 0), DefaultPolicy).Conflict {
		t.Fatal("the same pair under the 60 s window is not a conflict")
	}
	now := Evaluate(at(0, 0, 550), at(30, 0, 550), pol)
	mustJudge(t, now)
	if !now.Conflict {
		t.Fatalf("inside the minima now with a zero window: %+v, want conflict", now)
	}
	tiny := DefaultPolicy
	tiny.DHorizontalMinM, tiny.DVerticalMinM = math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64
	mustJudge(t, Evaluate(at(0, 0, 550), at(30, 0, 550), tiny))
}

func TestOutOfRangeArithmeticIsNotJudged(t *testing.T) {
	// Finite inputs whose advance carries a position past the pole.
	polar := State{Pos: core.LatLon{LatDeg: 89.9999}, VNMS: 1000, VerticalKnown: true}
	other := State{Pos: core.LatLon{LatDeg: 89.9999}, VerticalKnown: true, CapturedAtS: 10}
	mustNotJudge(t, Evaluate(polar, other, DefaultPolicy), ReasonOutOfRange)
	// Finite vertical speeds whose gap at t_cpa overflows.
	up := at(0, 0, 550).moving(10, 0, -1e307)
	down := at(1000, 0, 550).moving(-10, 0, 1e307)
	mustNotJudge(t, Evaluate(up, down, DefaultPolicy), ReasonOutOfRange)
	// Finite altitudes whose difference overflows.
	high := at(0, 0, math.MaxFloat64)
	low := at(10, 0, -math.MaxFloat64)
	mustNotJudge(t, Evaluate(high, low, DefaultPolicy), ReasonOutOfRange)
	// The presence twin: the same polar pair at rest is judged.
	polar.VNMS = 0
	mustJudge(t, Evaluate(polar, other, DefaultPolicy))
}
