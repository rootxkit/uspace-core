package cpa

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// minRelSpeedMS is the relative horizontal speed below which a pair is
// treated as not closing at all (C-02): t_cpa is 0 and the distance is
// the constant current one, instead of a division by a vanishing number.
// It is the numerical guard named in cpa.json's description, far below
// what a GPS velocity resolves, not a separation threshold.
const minRelSpeedMS = 1e-6

// State is one aircraft's latest sample as the CPA judges it.
type State struct {
	// Pos is the WGS84 position.
	Pos core.LatLon
	// AltAMSLM is the altitude above mean sea level. Separation is judged
	// in AMSL only, never AGL (D-01).
	AltAMSLM float64
	// VerticalKnown is false when AltAMSLM is a pressure altitude, or
	// otherwise not a vertical position comparable with another aircraft's
	// (R-09, S-33). One unknown makes the pair's vertical unknown.
	VerticalKnown bool
	// VNMS, VEMS and VDMS are the velocity north, east and DOWN in m/s
	// (MAVLink GLOBAL_POSITION_INT convention: positive down).
	VNMS, VEMS, VDMS float64
	// CapturedAtS is when the sample was taken, in seconds on one clock
	// shared by every aircraft compared (S-11).
	CapturedAtS float64
}

// valid reports whether every number is finite and the position is in
// range (C-09).
func (s State) valid() bool {
	return s.Pos.Valid() && core.IsFinite(s.AltAMSLM) &&
		core.IsFinite(s.VNMS) && core.IsFinite(s.VEMS) && core.IsFinite(s.VDMS) &&
		core.IsFinite(s.CapturedAtS)
}

// Policy holds the separation minima and the pair-selection limits
// (spec 04 §3.3, ARCHITECTURE §6.2). They are airspace policy: the
// caller loads them from configuration; DefaultPolicy documents the
// values the vectors pin.
type Policy struct {
	// TCPAMaxS is the look-ahead window: a closest approach this far
	// ahead or further is not yet a conflict (strictly less is).
	TCPAMaxS float64
	// DHorizontalMinM is the horizontal minimum; strictly less is inside.
	DHorizontalMinM float64
	// DVerticalMinM is the vertical minimum; strictly less is inside.
	DVerticalMinM float64
	// NeighbourRadiusM is the search radius for candidate pairs. Evaluate
	// does not use it; the caller sizes its Grid and filters with it.
	NeighbourRadiusM float64
	// NeighbourMaxAgeS is the largest difference between the two samples'
	// CapturedAtS at which the pair is still judged (C-04); exactly at
	// the bound is judged.
	NeighbourMaxAgeS float64
}

// DefaultPolicy is the policy of spec 04 §3.3 and cpa.json: 60 s, 60 m,
// 20 m, 800 m, 10 s.
var DefaultPolicy = Policy{
	TCPAMaxS:         60,
	DHorizontalMinM:  60,
	DVerticalMinM:    20,
	NeighbourRadiusM: 800,
	NeighbourMaxAgeS: 10,
}

// valid reports whether every field Evaluate uses is finite and not
// negative. A NaN minimum would make every comparison false, which reads
// as "no conflict"; such a policy is refused instead.
func (p Policy) valid() bool {
	for _, v := range [...]float64{p.TCPAMaxS, p.DHorizontalMinM, p.DVerticalMinM, p.NeighbourMaxAgeS} {
		if !core.IsFinite(v) || v < 0 {
			return false
		}
	}
	return true
}

// Reason says why a pair was not judged. The values are stable snake_case
// names a caller can use as counter names.
type Reason string

// Reasons for Result.NotJudged.
const (
	// ReasonNone: the pair was judged.
	ReasonNone Reason = ""
	// ReasonStaleNeighbour: the samples are more than NeighbourMaxAgeS
	// apart (C-04).
	ReasonStaleNeighbour Reason = "stale_neighbour"
	// ReasonInvalidInput: a coordinate, altitude, velocity or time is NaN
	// or infinite, or a position is out of range (C-09).
	ReasonInvalidInput Reason = "invalid_input"
	// ReasonInvalidPolicy: a policy value Evaluate uses is NaN, infinite
	// or negative.
	ReasonInvalidPolicy Reason = "invalid_policy"
	// ReasonOutOfRange: the inputs were finite, but advancing the older
	// sample or the CPA arithmetic left the valid domain (a position
	// carried past a pole, an overflow to infinity).
	ReasonOutOfRange Reason = "out_of_range"
)

// Result is the closest approach of a pair from "now", the later of the
// two capture times, and the conflict verdict.
//
// When Judged is false nothing else is meaningful: the pair is neither in
// conflict nor clear, and the caller must neither raise, refresh nor
// clear an alert on it (C-04, C-09). Conflict is then false, so a caller
// must test Judged first; NotJudged names the reason.
type Result struct {
	Judged bool
	// NotJudged is ReasonNone when Judged is true.
	NotJudged Reason
	// TCPAS is the time to the closest approach in seconds, never
	// negative: a diverging pair, and a pair with no relative motion,
	// have 0 (C-02).
	TCPAS float64
	// DCPAHorizontalM is the horizontal distance at TCPAS.
	DCPAHorizontalM float64
	// DAltAtCPAM is |altitude difference| at TCPAS. Zero, and not
	// evidence of anything, when VerticalKnown is false: publish null
	// (R-09).
	DAltAtCPAM float64
	// DHorizontalNowM is the horizontal distance now.
	DHorizontalNowM float64
	// DAltNowM is |altitude difference| now. Zero, and not evidence of
	// anything, when VerticalKnown is false: publish null (R-09).
	DAltNowM float64
	// VerticalKnown is true only when both states' verticals are known.
	VerticalKnown bool
	// Conflict is the verdict (C-03).
	Conflict bool
}

// metresPerDegree returns the metres per degree of latitude and of
// longitude at s's latitude, from geodesy's tangent plane (WGS84
// meridional and prime-vertical radii at that latitude).
func metresPerDegree(latDeg float64) (northPerDegM, eastPerDegM float64) {
	origin := core.LatLon{LatDeg: latDeg}
	northPerDegM, _ = geodesy.LocalOffsetM(origin, core.LatLon{LatDeg: latDeg + 1})
	_, eastPerDegM = geodesy.LocalOffsetM(origin, core.LatLon{LatDeg: latDeg, LonDeg: 1})
	return northPerDegM, eastPerDegM
}

// Advance returns s carried along its velocity, as a straight line, to
// the time toS (forward, or back for an earlier toS): north by VNMS*dt
// and east by VEMS*dt in the tangent plane at s's own latitude, altitude
// by -VDMS*dt (down positive), CapturedAtS set to toS. The longitude is
// wrapped into (-180, 180] (D-10). toS equal to s.CapturedAtS returns s
// unchanged.
//
// The latitude is not clamped: a state carried past a pole is out of
// range (Pos.Valid is false) and Evaluate refuses it. Non-finite inputs
// give non-finite outputs; Advance never panics.
func Advance(s State, toS float64) State {
	dtS := toS - s.CapturedAtS
	if dtS == 0 {
		return s
	}
	northPerDegM, eastPerDegM := metresPerDegree(s.Pos.LatDeg)
	out := s
	out.Pos.LatDeg = s.Pos.LatDeg + s.VNMS*dtS/northPerDegM
	out.Pos.LonDeg = core.WrapLonDeg(s.Pos.LonDeg + s.VEMS*dtS/eastPerDegM)
	out.AltAMSLM = s.AltAMSLM - s.VDMS*dtS
	out.CapturedAtS = toS
	return out
}

// before is a total order on valid states, used to put a pair in one
// canonical order so that Evaluate(a, b) and Evaluate(b, a) run the same
// arithmetic and return bit-identical results.
func before(a, b State) bool {
	fa := [...]float64{a.Pos.LatDeg, a.Pos.LonDeg, a.AltAMSLM, a.VNMS, a.VEMS, a.VDMS, a.CapturedAtS}
	fb := [...]float64{b.Pos.LatDeg, b.Pos.LonDeg, b.AltAMSLM, b.VNMS, b.VEMS, b.VDMS, b.CapturedAtS}
	for i := range fa {
		if fa[i] != fb[i] {
			return fa[i] < fb[i]
		}
	}
	return !a.VerticalKnown && b.VerticalKnown
}

// Evaluate judges the pair a, b under pol (cpa.json description, C-01 to
// C-04):
//
//  1. Not judged when the samples are more than NeighbourMaxAgeS apart.
//  2. The older sample is advanced to the newer one's time (Advance).
//  3. Both are projected onto the tangent plane about the pair's
//     mid-latitude (geodesy.LocalOffsetAboutMidLatM, D-10).
//  4. t_cpa = -(rel_pos . rel_vel) / |rel_vel|^2 from the horizontal
//     motion only, 0 when |rel_vel| < 1e-6 m/s, clamped to >= 0.
//  5. Distances now and at t_cpa; the vertical gap at t_cpa uses the
//     vertical velocities (down positive).
//  6. conflict = (d_h_now < d_h_min AND (vertical unknown OR d_alt_now <
//     d_v_min)) OR (t_cpa < t_max AND d_cpa_h < d_h_min AND (vertical
//     unknown OR d_alt_at_cpa < d_v_min)).
//
// Fail-safe on bad numbers: a NaN or infinite input, an out-of-range
// position, an invalid policy, or arithmetic that leaves the finite
// domain gives Judged false with a Reason, never a judged "no conflict".
// The result does not depend on the order of a and b.
func Evaluate(a, b State, pol Policy) Result {
	if !pol.valid() {
		return Result{NotJudged: ReasonInvalidPolicy}
	}
	if !a.valid() || !b.valid() {
		return Result{NotJudged: ReasonInvalidInput}
	}
	if math.Abs(a.CapturedAtS-b.CapturedAtS) > pol.NeighbourMaxAgeS {
		return Result{NotJudged: ReasonStaleNeighbour}
	}
	if before(b, a) {
		a, b = b, a
	}
	switch {
	case a.CapturedAtS < b.CapturedAtS:
		a = Advance(a, b.CapturedAtS)
	case b.CapturedAtS < a.CapturedAtS:
		b = Advance(b, a.CapturedAtS)
	}
	if !a.valid() || !b.valid() {
		return Result{NotJudged: ReasonOutOfRange}
	}

	relNorthM, relEastM := geodesy.LocalOffsetAboutMidLatM(a.Pos, b.Pos)
	relVNMS := b.VNMS - a.VNMS
	relVEMS := b.VEMS - a.VEMS
	relSpeedSq := relVNMS*relVNMS + relVEMS*relVEMS

	tCPAS := 0.0
	if relSpeedSq >= minRelSpeedMS*minRelSpeedMS {
		tCPAS = -(relNorthM*relVNMS + relEastM*relVEMS) / relSpeedSq
		if tCPAS < 0 {
			tCPAS = 0
		}
	}
	cpaNorthM := relNorthM + relVNMS*tCPAS
	cpaEastM := relEastM + relVEMS*tCPAS
	dAltM := b.AltAMSLM - a.AltAMSLM

	r := Result{
		Judged:          true,
		TCPAS:           tCPAS,
		DCPAHorizontalM: math.Sqrt(cpaNorthM*cpaNorthM + cpaEastM*cpaEastM),
		DHorizontalNowM: math.Sqrt(relNorthM*relNorthM + relEastM*relEastM),
		VerticalKnown:   a.VerticalKnown && b.VerticalKnown,
	}
	if r.VerticalKnown {
		r.DAltNowM = math.Abs(dAltM)
		// Altitude is up and VD is down: alt(t) = alt - vd*t, so the gap
		// b - a at t is dAlt + (vd_a - vd_b)*t.
		r.DAltAtCPAM = math.Abs(dAltM + (a.VDMS-b.VDMS)*tCPAS)
	}
	for _, v := range [...]float64{r.TCPAS, r.DCPAHorizontalM, r.DHorizontalNowM, r.DAltNowM, r.DAltAtCPAM} {
		if !core.IsFinite(v) {
			return Result{NotJudged: ReasonOutOfRange}
		}
	}

	insideNow := r.DHorizontalNowM < pol.DHorizontalMinM &&
		(!r.VerticalKnown || r.DAltNowM < pol.DVerticalMinM)
	insideAtCPA := r.TCPAS < pol.TCPAMaxS && r.DCPAHorizontalM < pol.DHorizontalMinM &&
		(!r.VerticalKnown || r.DAltAtCPAM < pol.DVerticalMinM)
	r.Conflict = insideNow || insideAtCPA
	return r
}
