package zones

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func heightPolicy(maxM float64) Policy {
	p := DefaultPolicy()
	p.MaxHeightAGLM = f64(maxM)
	return p
}

func TestHeightLimitNotInForce(t *testing.T) {
	r := JudgeHeightLimit(geodetic(5000), ground500, DefaultPolicy())
	isClear(t, "no height limit in the policy", r)
	var c core.Counters
	r.Count(&c)
	if len(c.Names()) != 0 {
		t.Errorf("counted %v", c.Snapshot())
	}
	// The presence pair: with the limit in force the same aircraft raises.
	raised(t, "limit in force", JudgeHeightLimit(geodetic(5000), ground500, heightPolicy(120)), core.SeverityWarning)
}

func TestHeightLimitStrictlyGreater(t *testing.T) {
	pol := heightPolicy(120)
	isClear(t, "exactly at the limit", JudgeHeightLimit(geodetic(620), ground500, pol))
	r := raised(t, "just over", JudgeHeightLimit(geodetic(620.001), ground500, pol), core.SeverityWarning)
	if r.Kind != KindHeight || r.Detail.Identifier != "" || r.Detail.VerticalKnown != nil ||
		r.Detail.MaxHeightAGLM == nil || *r.Detail.MaxHeightAGLM != 120 ||
		r.Detail.HeightAGLM == nil || math.Abs(*r.Detail.HeightAGLM-120.001) > 1e-9 {
		t.Errorf("raise %+v", r)
	}
}

func TestHeightLimitNotEvaluated(t *testing.T) {
	pol := heightPolicy(120)
	cases := []struct {
		name   string
		ac     Aircraft
		env    Env
		pol    Policy
		reason Reason
	}{
		{"no terrain", geodetic(5000), noTerrain, pol, ReasonNoTerrain},
		{"ground unknown", geodetic(5000), Env{Ground: GroundUnknown}, pol, ReasonGroundUnknown},
		{"ground NaN", geodetic(5000), Env{Ground: GroundKnown, GroundM: math.NaN()}, pol, ReasonGroundUnknown},
		{"ground -Inf", geodetic(5000), Env{Ground: GroundKnown, GroundM: math.Inf(-1)}, pol, ReasonGroundUnknown},
		{"no altitude", Aircraft{AltSource: core.AltNone}, ground500, pol, ReasonNoAltitude},
		{"NaN altitude", Aircraft{AltAMSLM: f64(math.NaN()), AltSource: core.AltGeodetic}, ground500, pol, ReasonNoAltitude},
		{"NaN limit", geodetic(5000), ground500, heightPolicy(math.NaN()), ReasonInvalidPolicy},
		{"negative limit", geodetic(5000), ground500, heightPolicy(-1), ReasonInvalidPolicy},
		{"+Inf limit", geodetic(5000), ground500, heightPolicy(math.Inf(1)), ReasonInvalidPolicy},
	}
	for _, tc := range cases {
		r := JudgeHeightLimit(tc.ac, tc.env, tc.pol)
		notEvaluated(t, tc.name, r, tc.reason)
		var c core.Counters
		r.Count(&c)
		if c.Get(CounterHeightNotEvaluated) != 1 || c.Get(CounterZoneNotEvaluated) != 0 || c.Get(CounterZoneLimitNotJudged) != 0 {
			t.Errorf("%s: counters %v", tc.name, c.Snapshot())
		}
	}
	// The presence pair: known ground and a finite altitude are judged.
	raised(t, "known ground", JudgeHeightLimit(geodetic(5000), ground500, pol), core.SeverityWarning)
	// Unknown ground is never 0: 5000 m over unknown ground would raise
	// against 0, and does not.
	if r := JudgeHeightLimit(geodetic(5000), Env{Ground: GroundUnknown, GroundM: 0}, pol); r.Raise != nil {
		t.Errorf("unknown ground judged as 0: %+v", r.Raise)
	}
}

func TestHeightLimitFlagsUncertainAltitude(t *testing.T) {
	pol := heightPolicy(120)
	for _, src := range []core.AltSource{core.AltPressure, "baro"} {
		r := raised(t, string(src), JudgeHeightLimit(Aircraft{AltAMSLM: f64(650), AltSource: src}, ground500, pol), core.SeverityWarning)
		if r.Detail.VerticalKnown == nil || *r.Detail.VerticalKnown {
			t.Errorf("%s: not flagged: %+v", src, r.Detail)
		}
		// Judged on the indicated height: no margin.
		isClear(t, string(src)+" at the limit", JudgeHeightLimit(Aircraft{AltAMSLM: f64(620), AltSource: src}, ground500, pol))
	}
}
