package cpa

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/vectors"
)

// Tolerances recorded in the cpa.json header; TestVectorsCPA asserts the
// file still says so.
const (
	tolTCPAS     = 0.01
	tolLoSStartS = 0.01
	tolDistanceM = 0.01
)

// cpaCases is the number of cases in cpa.json.
const cpaCases = 37

type cpaDescribedAs struct {
	NorthM float64 `json:"north_m"`
	EastM  float64 `json:"east_m"`
}

type cpaState struct {
	LatDeg        float64 `json:"lat_deg"`
	LonDeg        float64 `json:"lon_deg"`
	AltAMSLM      float64 `json:"alt_amsl_m"`
	VNMS          float64 `json:"vn_ms"`
	VEMS          float64 `json:"ve_ms"`
	VDMS          float64 `json:"vd_ms"`
	CapturedAtS   float64 `json:"captured_at_s"`
	VerticalKnown bool    `json:"vertical_known"`
	// DescribedAs is how the case was built; decoded so the strict decoder
	// passes, never used as input.
	DescribedAs cpaDescribedAs `json:"described_as"`
}

func (s cpaState) state() State {
	return State{
		Pos:           core.LatLon{LatDeg: s.LatDeg, LonDeg: s.LonDeg},
		AltAMSLM:      s.AltAMSLM,
		VerticalKnown: s.VerticalKnown,
		VNMS:          s.VNMS,
		VEMS:          s.VEMS,
		VDMS:          s.VDMS,
		CapturedAtS:   s.CapturedAtS,
	}
}

type cpaInput struct {
	A                cpaState `json:"a"`
	B                cpaState `json:"b"`
	NeighbourMaxAgeS float64  `json:"neighbour_max_age_s"`
	// Policy, when present, replaces the header policy for this case.
	Policy *cpaPolicy `json:"policy"`
}

type cpaExpected struct {
	Judged          bool     `json:"judged"`
	TCPAS           *float64 `json:"t_cpa_s"`
	DCPAHorizontalM *float64 `json:"d_cpa_horizontal_m"`
	DAltAtCPAM      *float64 `json:"d_alt_at_cpa_m"`
	DHorizontalNowM *float64 `json:"d_horizontal_now_m"`
	DAltNowM        *float64 `json:"d_alt_now_m"`
	VerticalKnown   *bool    `json:"vertical_known"`
	Conflict        *bool    `json:"conflict"`
	LoSStartS       *float64 `json:"los_start_s"`
	NotJudged       *string  `json:"not_judged"`
}

// vecFloat is a policy number of the vector file: a JSON number, or the
// string "NaN", since JSON has no NaN (the cpa.json header says so).
type vecFloat float64

func (f *vecFloat) UnmarshalJSON(b []byte) error {
	if string(b) == `"NaN"` {
		*f = vecFloat(math.NaN())
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = vecFloat(v)
	return nil
}

type cpaPolicy struct {
	TCPAMaxS         vecFloat `json:"t_cpa_max_s"`
	DHorizontalMinM  vecFloat `json:"d_horizontal_min_m"`
	DVerticalMinM    vecFloat `json:"d_vertical_min_m"`
	NeighbourRadiusM vecFloat `json:"neighbour_radius_m"`
}

func wantF(t *testing.T, field string, v *float64) float64 {
	t.Helper()
	if v == nil {
		t.Fatalf("expected.%s missing", field)
		return 0
	}
	return *v
}

func wantB(t *testing.T, field string, v *bool) bool {
	t.Helper()
	if v == nil {
		t.Fatalf("expected.%s missing", field)
		return false
	}
	return *v
}

// TestVectorsCPA runs the cases of cpa.json, each in both orders: the
// result must match the vector and Evaluate(b, a) must equal Evaluate(a, b)
// exactly.
func TestVectorsCPA(t *testing.T) {
	f := vectors.Load(t, "cpa.json")
	if tol, ok := f.FloatTolerance("t_cpa_s"); !ok || tol != tolTCPAS {
		t.Fatalf("header tolerance t_cpa_s = %v (%v), want %v", tol, ok, tolTCPAS)
	}
	if tol, ok := f.FloatTolerance("los_start_s"); !ok || tol != tolLoSStartS {
		t.Fatalf("header tolerance los_start_s = %v (%v), want %v", tol, ok, tolLoSStartS)
	}
	if tol, ok := f.FloatTolerance("distances_m"); !ok || tol != tolDistanceM {
		t.Fatalf("header tolerance distances_m = %v (%v), want %v", tol, ok, tolDistanceM)
	}
	if v, ok := f.Tolerance["booleans"]; !ok || v != "exact" {
		t.Fatalf("header tolerance booleans = %v, want exact", v)
	}
	var hp cpaPolicy
	f.Header(t, "policy", &hp)
	if float64(hp.TCPAMaxS) != DefaultPolicy.TCPAMaxS || float64(hp.DHorizontalMinM) != DefaultPolicy.DHorizontalMinM ||
		float64(hp.DVerticalMinM) != DefaultPolicy.DVerticalMinM || float64(hp.NeighbourRadiusM) != DefaultPolicy.NeighbourRadiusM {
		t.Fatalf("header policy %+v differs from DefaultPolicy %+v", hp, DefaultPolicy)
	}

	ran, conflicts, notJudged := 0, 0, 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in cpaInput
		var exp cpaExpected
		c.Decode(t, &in, &exp)
		ran++
		cp := hp
		if in.Policy != nil {
			cp = *in.Policy
		}
		pol := Policy{
			TCPAMaxS:         float64(cp.TCPAMaxS),
			DHorizontalMinM:  float64(cp.DHorizontalMinM),
			DVerticalMinM:    float64(cp.DVerticalMinM),
			NeighbourRadiusM: float64(cp.NeighbourRadiusM),
			NeighbourMaxAgeS: in.NeighbourMaxAgeS,
		}
		a, b := in.A.state(), in.B.state()
		got := Evaluate(a, b, pol)
		if rev := Evaluate(b, a, pol); rev != got {
			t.Errorf("asymmetric: Evaluate(a, b) = %+v, Evaluate(b, a) = %+v", got, rev)
		}
		if got.Judged != exp.Judged {
			t.Fatalf("judged = %v (%q), want %v", got.Judged, got.NotJudged, exp.Judged)
		}
		if !exp.Judged {
			notJudged++
			if exp.NotJudged == nil {
				t.Fatal("expected.not_judged missing")
			}
			if got.Conflict || string(got.NotJudged) != *exp.NotJudged {
				t.Errorf("not judged: %+v, want no verdict, reason %q", got, *exp.NotJudged)
			}
			return
		}
		if exp.NotJudged != nil {
			t.Errorf("judged, but the vector gives not_judged %q", *exp.NotJudged)
		}
		vectors.Near(t, "t_cpa_s", got.TCPAS, wantF(t, "t_cpa_s", exp.TCPAS), tolTCPAS)
		vectors.Near(t, "d_cpa_horizontal_m", got.DCPAHorizontalM, wantF(t, "d_cpa_horizontal_m", exp.DCPAHorizontalM), tolDistanceM)
		vectors.Near(t, "d_horizontal_now_m", got.DHorizontalNowM, wantF(t, "d_horizontal_now_m", exp.DHorizontalNowM), tolDistanceM)
		if vk := wantB(t, "vertical_known", exp.VerticalKnown); got.VerticalKnown != vk {
			t.Errorf("vertical_known = %v, want %v", got.VerticalKnown, vk)
		}
		// A null altitude difference is the unknown vertical: Result
		// carries zero there, and VerticalKnown false says it is no number.
		if exp.DAltNowM == nil || exp.DAltAtCPAM == nil {
			if exp.DAltNowM != nil || exp.DAltAtCPAM != nil || got.VerticalKnown {
				t.Errorf("d_alt null on one side only, or vertical known: %+v", got)
			}
			if got.DAltNowM != 0 || got.DAltAtCPAM != 0 {
				t.Errorf("unknown vertical reported as %v, %v; want 0 with VerticalKnown false", got.DAltNowM, got.DAltAtCPAM)
			}
		} else {
			vectors.Near(t, "d_alt_now_m", got.DAltNowM, *exp.DAltNowM, tolDistanceM)
			vectors.Near(t, "d_alt_at_cpa_m", got.DAltAtCPAM, *exp.DAltAtCPAM, tolDistanceM)
		}
		if want := wantB(t, "conflict", exp.Conflict); got.Conflict != want {
			t.Errorf("conflict = %v, want %v (%+v)", got.Conflict, want, got)
		}
		// los_start_s is null exactly when there is no conflict; Result
		// carries 0 then.
		switch {
		case exp.LoSStartS == nil && got.Conflict:
			t.Errorf("los_start_s null, but a conflict starting at %v", got.LoSStartS)
		case exp.LoSStartS == nil && got.LoSStartS != 0:
			t.Errorf("no conflict, but LoSStartS %v", got.LoSStartS)
		case exp.LoSStartS != nil:
			vectors.Near(t, "los_start_s", got.LoSStartS, *exp.LoSStartS, tolLoSStartS)
		}
		if got.Conflict {
			conflicts++
		}
	})
	if ran != cpaCases {
		t.Fatalf("ran %d cases, want %d", ran, cpaCases)
	}
	t.Logf("%d/%d cases, each in both orders: %d conflicts, %d not judged", ran, cpaCases, conflicts, notJudged)
}
