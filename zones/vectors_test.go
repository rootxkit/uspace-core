package zones

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/vectors"
)

// tolDetailM mirrors the file's `tolerance."detail floats"` header: the
// old monitor rounded details to 0.1, and JudgeVertical returns full
// precision.
const tolDetailM = 0.1

// vectorCases is the number of cases in zones_vertical.json.
const vectorCases = 38

type vectorAircraft struct {
	AltAMSLM  *float64 `json:"alt_amsl_m"`
	AltSource string   `json:"alt_source"`
}

type vectorInput struct {
	Zone                 json.RawMessage `json:"zone"`
	Aircraft             vectorAircraft  `json:"aircraft"`
	Terrain              json.RawMessage `json:"terrain"`
	GeoidUndulationM     *float64        `json:"geoid_undulation_m"`
	MaxHeightAGLM        *float64        `json:"max_height_agl_m"`
	PressureUncertaintyM float64         `json:"pressure_uncertainty_m"`
}

type vectorRaise struct {
	Kind     string          `json:"kind"`
	Severity string          `json:"severity"`
	Aircraft []string        `json:"aircraft"`
	Detail   json.RawMessage `json:"detail"`
}

type vectorExpected struct {
	Raised   []vectorRaise     `json:"raised"`
	Counters map[string]uint64 `json:"counters"`
}

// envOf maps the vector's `terrain` ("none", "unknown here" or
// {"ground_m": x}) and geoid onto an Env.
func envOf(t *testing.T, in vectorInput) Env {
	t.Helper()
	env := Env{UndulationM: in.GeoidUndulationM}
	var word string
	if err := json.Unmarshal(in.Terrain, &word); err == nil {
		switch word {
		case "none":
			env.Ground = GroundNotConfigured
		case "unknown here":
			env.Ground = GroundUnknown
		default:
			t.Fatalf("terrain %q is not one the header describes", word)
		}
		return env
	}
	var known struct {
		GroundM *float64 `json:"ground_m"`
	}
	vectors.Unmarshal(t, in.Terrain, &known)
	if known.GroundM == nil {
		t.Fatalf("terrain %s has no ground_m", in.Terrain)
	}
	env.Ground = GroundKnown
	env.GroundM = *known.GroundM
	return env
}

func TestVectorsZonesVertical(t *testing.T) {
	f := vectors.Load(t, "zones_vertical.json")
	if tol, ok := f.FloatTolerance("detail floats"); !ok || tol != tolDetailM {
		t.Fatalf("header tolerance for detail floats is %v (%v); this test compares with %v", tol, ok, tolDetailM)
	}
	if len(f.Cases) != vectorCases {
		t.Fatalf("zones_vertical.json has %d cases, want %d", len(f.Cases), vectorCases)
	}
	ran := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		ran++
		var in vectorInput
		var exp vectorExpected
		c.Decode(t, &in, &exp)
		env := envOf(t, in)
		pol := DefaultPolicy()
		pol.PressureUncertaintyM = in.PressureUncertaintyM
		pol.MaxHeightAGLM = in.MaxHeightAGLM
		ac := Aircraft{AltAMSLM: in.Aircraft.AltAMSLM, AltSource: core.AltSource(in.Aircraft.AltSource)}

		var res Result
		if isJSONNull(in.Zone) {
			res = JudgeHeightLimit(ac, env, pol)
		} else {
			gz, problems := ed269.ParseZone(in.Zone, ed269.Limits{})
			if problems != nil {
				t.Fatalf("ParseZone: %v", problems)
			}
			z, err := FromED269(gz)
			if err != nil {
				t.Fatalf("FromED269: %v", err)
			}
			// The header: the aircraft is inside horizontally and the zone
			// applies. Check that the fixture agrees rather than assume it.
			inside, err := z.ContainsHorizontally(core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271})
			if err != nil || !inside {
				t.Fatalf("fixture point not inside zone %s: %v %v", z.Identifier, inside, err)
			}
			res = JudgeVertical(z, ac, env, pol)
		}
		if (res.NotEvaluated || res.LimitNotJudged) && res.Reason == "" {
			t.Errorf("not evaluated or not judged without a reason: %+v", res)
		}
		var counters core.Counters
		res.Count(&counters)
		for name, want := range exp.Counters {
			if got := counters.Get(name); got != want {
				t.Errorf("counter %s: got %d, want %d", name, got, want)
			}
		}

		switch {
		case len(exp.Raised) == 0:
			if res.Raise != nil {
				t.Fatalf("raised %+v, want nothing", *res.Raise)
			}
			return
		case len(exp.Raised) > 1:
			t.Fatalf("vector raises %d; one observation raises at most one", len(exp.Raised))
		case res.Raise == nil:
			t.Fatalf("raised nothing (not evaluated %v, reason %q), want %s %s",
				res.NotEvaluated, res.Reason, exp.Raised[0].Kind, exp.Raised[0].Severity)
		}
		want := exp.Raised[0]
		if !slices.Equal(want.Aircraft, []string{"A"}) {
			t.Fatalf("vector aircraft %v; this test judges one aircraft A", want.Aircraft)
		}
		if res.NotEvaluated {
			t.Errorf("a raise is not also not evaluated")
		}
		got := res.Raise
		if got.Kind != want.Kind {
			t.Errorf("kind: got %q, want %q", got.Kind, want.Kind)
		}
		if string(got.Severity) != want.Severity {
			t.Errorf("severity: got %q, want %q", got.Severity, want.Severity)
		}
		var wd Detail
		vectors.Unmarshal(t, want.Detail, &wd)
		compareDetail(t, got.Detail, wd)
	})
	if ran != vectorCases {
		t.Fatalf("ran %d cases, want %d", ran, vectorCases)
	}
}

// compareDetail compares every Detail member: strings and flags exactly,
// numbers within tolDetailM, and a key the vector does not have must be
// nil in the result.
func compareDetail(t *testing.T, got, want Detail) {
	t.Helper()
	if got.Identifier != want.Identifier {
		t.Errorf("identifier: got %q, want %q", got.Identifier, want.Identifier)
	}
	if got.Restriction != want.Restriction {
		t.Errorf("restriction: got %q, want %q", got.Restriction, want.Restriction)
	}
	equalBoolPtr(t, "vertical_known", got.VerticalKnown, want.VerticalKnown)
	equalBoolPtr(t, "within_band", got.WithinBand, want.WithinBand)
	equalBoolPtr(t, "limit_not_judged", got.LimitNotJudged, want.LimitNotJudged)
	if !slices.Equal(got.NotJudged, want.NotJudged) || (got.NotJudged == nil) != (want.NotJudged == nil) {
		t.Errorf("not_judged: got %v, want %v", got.NotJudged, want.NotJudged)
	}
	vectors.NearPtr(t, "height_agl_m", got.HeightAGLM, want.HeightAGLM, tolDetailM)
	vectors.NearPtr(t, "alt_hae_m", got.AltHAEM, want.AltHAEM, tolDetailM)
	vectors.NearPtr(t, "max_height_agl_m", got.MaxHeightAGLM, want.MaxHeightAGLM, tolDetailM)
}

func equalBoolPtr(t *testing.T, field string, got, want *bool) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Errorf("%s: got absent, want %v", field, *want)
	case want == nil:
		t.Errorf("%s: got %v, want absent", field, *got)
	case *got != *want:
		t.Errorf("%s: got %v, want %v", field, *got, *want)
	}
}

func isJSONNull(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) == 0 || bytes.Equal(b, []byte("null"))
}
