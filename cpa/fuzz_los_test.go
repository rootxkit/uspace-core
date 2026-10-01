package cpa

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/geodesy"
)

// bounded maps an arbitrary float onto [-limit, limit], so the fuzzer's
// NaN, Inf and huge values still give a meaningful encounter.
func bounded(x, limit float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return math.Mod(x, limit)
}

// FuzzLossOfSeparation checks the conflict verdict against an
// independent brute force: the relative motion is sampled every 0.05 s
// over [0, TCPAMaxS] in the same tangent plane, and if any sample is
// inside both minima by a margin (1 cm horizontally and vertically) the
// pair must be a conflict, with LoSStartS no later than that sample.
// Conversely, a conflict must have some sample inside both minima
// widened by the margin, or an interval shorter than one step.
func FuzzLossOfSeparation(f *testing.F) {
	f.Add(1000.0, 0.0, 10.0, 0.0, 0.0, -10.0, 0.0, 0.0, 0.0, true, true)   // head-on
	f.Add(1000.0, 0.0, 10.0, 0.0, 1.4, -10.0, 0.0, 0.0, -48.6, true, true) // vertical gap closes before t_cpa
	f.Add(110.0, 0.0, 0.0, 0.0, 0.0, -1.0, 0.0, 0.0, 0.0, true, true)      // t_cpa beyond the window
	f.Add(30.0, 0.0, 0.0, 0.0, 0.0, -0.01, 0.0, 0.0, 0.0, true, true)      // hovering with noise
	f.Add(500.0, 0.0, 10.0, 0.0, 0.0, -10.0, 0.0, 0.0, 100.0, false, true) // pressure track
	f.Add(-300.0, 500.0, 10.0, 0.0, 0.5, 0.0, -10.0, -0.3, -5.0, true, true)
	f.Fuzz(func(t *testing.T, northM, eastM, vnA, veA, vdA, vnB, veB, vdB, dAltM float64, vkA, vkB bool) {
		northM, eastM = bounded(northM, 3000), bounded(eastM, 3000)
		a := at(0, 0, 500).moving(bounded(vnA, 60), bounded(veA, 60), bounded(vdA, 20))
		b := at(northM, eastM, 500+bounded(dAltM, 300)).moving(bounded(vnB, 60), bounded(veB, 60), bounded(vdB, 20))
		a.VerticalKnown, b.VerticalKnown = vkA, vkB
		pol := DefaultPolicy
		r := Evaluate(a, b, pol)
		if !r.Judged {
			t.Fatalf("an ordinary encounter was not judged: %+v", r)
		}

		pn, pe := geodesy.LocalOffsetAboutMidLatM(a.Pos, b.Pos)
		rvn, rve := b.VNMS-a.VNMS, b.VEMS-a.VEMS
		gapM, rateMS := b.AltAMSLM-a.AltAMSLM, a.VDMS-b.VDMS
		vertical := vkA && vkB
		const stepS, marginM = 0.05, 0.01
		firstInsideS := math.NaN()
		nearInside := false
		for i := 0; float64(i)*stepS <= pol.TCPAMaxS; i++ {
			ts := float64(i) * stepS
			hM := math.Hypot(pn+rvn*ts, pe+rve*ts)
			vM := math.Abs(gapM + rateMS*ts)
			if hM < pol.DHorizontalMinM-marginM && (!vertical || vM < pol.DVerticalMinM-marginM) {
				firstInsideS = ts
				break
			}
			if hM < pol.DHorizontalMinM+marginM && (!vertical || vM < pol.DVerticalMinM+marginM) {
				nearInside = true
			}
		}
		if !math.IsNaN(firstInsideS) {
			if !r.Conflict {
				t.Fatalf("inside both minima at t=%v s but judged clear: %+v", firstInsideS, r)
			}
			if r.LoSStartS > firstInsideS+1e-9 {
				t.Fatalf("LoSStartS %v after the first inside sample %v", r.LoSStartS, firstInsideS)
			}
			return
		}
		if r.Conflict && !nearInside {
			// Only an overlap shorter than a step can hide between samples.
			end := math.Min(r.LoSStartS+stepS, pol.TCPAMaxS)
			for ts := r.LoSStartS; ts <= end; ts += stepS / 100 {
				hM := math.Hypot(pn+rvn*ts, pe+rve*ts)
				vM := math.Abs(gapM + rateMS*ts)
				if hM < pol.DHorizontalMinM+marginM && (!vertical || vM < pol.DVerticalMinM+marginM) {
					return
				}
			}
			t.Fatalf("conflict with no sample near both minima: %+v", r)
		}
	})
}
