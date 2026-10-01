package cpa

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// FuzzEvaluate feeds arbitrary states (NaN, Inf, out of range and
// overflowing values included) to Evaluate and Advance. None may panic,
// and the verdict is fail-safe:
//   - a non-finite or out-of-range input the judgement uses is never
//     judged, and NaN in the altitude or vertical speed of a track whose
//     vertical is unknown changes nothing;
//   - a pair not judged is never a conflict and always names a reason;
//   - a judged result has finite, non-negative numbers, and a pair inside
//     both minima now is a conflict whatever t_cpa says (C-03);
//   - the result does not depend on the order of the pair.
func FuzzEvaluate(f *testing.F) {
	// Seeds from cpa.json (head-on, hovering with noise, stale, pressure)
	// and the degenerate inputs.
	f.Add(41.7151, 44.8271, 550.0, 10.0, 0.0, 0.0, 0.0, true, 41.72410351324941, 44.8271, 550.0, -10.0, 0.0, 0.0, 0.0, true)
	f.Add(41.7151, 44.8271, 550.0, 0.0, 0.0, 0.0, 0.0, true, 41.71537000, 44.8271, 550.0, -0.01, 0.0, 0.0, 0.0, true)
	f.Add(41.7151, 44.8271, 550.0, 10.0, 0.0, 0.0, 11.0, true, 41.7196, 44.8271, 550.0, -10.0, 0.0, 0.0, 0.0, true)
	f.Add(41.7151, 44.8271, 500.0, 10.0, 0.0, 0.0, 0.0, false, 41.7196, 44.8271, 600.0, -10.0, 0.0, 0.0, 0.0, true)
	f.Add(0.0, 179.999, 100.0, 0.0, 5.0, 0.0, 0.0, true, 0.0, -179.999, 100.0, 0.0, -5.0, 0.0, 0.0, true)
	f.Add(89.9999, 0.0, 0.0, 1000.0, 0.0, 0.0, 0.0, true, 89.9999, 0.0, 0.0, 0.0, 0.0, 0.0, 10.0, true)
	f.Add(math.NaN(), math.Inf(1), math.Inf(-1), math.NaN(), 1e308, -1e308, math.NaN(), true, 91.0, 181.0, 1e308, 1e308, 1e308, 1e308, 1e308, false)
	f.Fuzz(func(t *testing.T,
		latA, lonA, altA, vnA, veA, vdA, tA float64, vkA bool,
		latB, lonB, altB, vnB, veB, vdB, tB float64, vkB bool,
	) {
		a := State{Pos: core.LatLon{LatDeg: latA, LonDeg: lonA}, AltAMSLM: altA, VerticalKnown: vkA, VNMS: vnA, VEMS: veA, VDMS: vdA, CapturedAtS: tA}
		b := State{Pos: core.LatLon{LatDeg: latB, LonDeg: lonB}, AltAMSLM: altB, VerticalKnown: vkB, VNMS: vnB, VEMS: veB, VDMS: vdB, CapturedAtS: tB}
		_ = Advance(a, tB)
		pol := DefaultPolicy
		r := Evaluate(a, b, pol)
		if rev := Evaluate(b, a, pol); rev != r {
			t.Fatalf("asymmetric: Evaluate(a, b) = %+v, Evaluate(b, a) = %+v", r, rev)
		}
		if !a.valid() || !b.valid() {
			if r.Judged || r.NotJudged != ReasonInvalidInput {
				t.Fatalf("invalid input judged: %+v", r)
			}
		}
		// The vertical numbers of a track whose vertical is unknown take
		// no part: replacing them with NaN changes nothing.
		an, bn := a, b
		if !vkA {
			an.AltAMSLM, an.VDMS = math.NaN(), math.NaN()
		}
		if !vkB {
			bn.AltAMSLM, bn.VDMS = math.NaN(), math.NaN()
		}
		if rn := Evaluate(an, bn, pol); rn != r {
			t.Fatalf("unknown-vertical NaN changed the result: %+v, was %+v", rn, r)
		}
		if !r.Judged {
			if r.Conflict || r.NotJudged == ReasonNone || r.NotJudged == ReasonUnset {
				t.Fatalf("not judged but %+v", r)
			}
			return
		}
		if r.NotJudged != ReasonNone {
			t.Fatalf("judged with a reason: %+v", r)
		}
		for _, v := range []float64{r.TCPAS, r.DCPAHorizontalM, r.DAltAtCPAM, r.DHorizontalNowM, r.DAltNowM} {
			if !core.IsFinite(v) || v < 0 {
				t.Fatalf("judged with a bad number: %+v", r)
			}
		}
		if r.VerticalKnown != (vkA && vkB) {
			t.Fatalf("vertical known %v, inputs %v %v", r.VerticalKnown, vkA, vkB)
		}
		if !r.VerticalKnown && (r.DAltNowM != 0 || r.DAltAtCPAM != 0) {
			t.Fatalf("unknown vertical reported as a number: %+v", r)
		}
		insideNow := r.DHorizontalNowM < pol.DHorizontalMinM && (!r.VerticalKnown || r.DAltNowM < pol.DVerticalMinM)
		if insideNow && (!r.Conflict || r.LoSStartS != 0) {
			t.Fatalf("inside the minima now but %+v", r)
		}
		if (r.Conflict && !(r.LoSStartS >= 0 && r.LoSStartS <= pol.TCPAMaxS)) || (!r.Conflict && r.LoSStartS != 0) {
			t.Fatalf("LoSStartS %v outside the window for conflict %v", r.LoSStartS, r.Conflict)
		}
	})
}
