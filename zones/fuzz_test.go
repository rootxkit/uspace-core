package zones

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/vectors"
)

var (
	fuzzRefs    = []core.VerticalRef{core.RefAMSL, core.RefAGL, core.RefWGS84, "QNH"}
	fuzzSources = []core.AltSource{core.AltGeodetic, core.AltPressure, core.AltNetwork, core.AltNone, ""}
	fuzzTypes   = []core.ZoneType{core.ZoneProhibited, core.ZoneReqAuthorization, core.ZoneConditional, core.ZoneNoRestriction, core.ZoneUSpace, "X"}
)

func pick[T any](s []T, i uint8) T { return s[int(i)%len(s)] }

// oracleLimits evaluates every limit that needs a height, independently
// of JudgeVertical: outside is true when some limit is judged with a
// known, finite height and excludes the aircraft beyond the margin;
// allInside is true when every such limit is judged and includes it as
// indicated (beyond <= 0). needed counts those limits.
func oracleLimits(z *Zone, alt float64, widened bool, env Env, pol Policy) (outside, allInside bool, needed int) {
	margin := 0.0
	if widened {
		margin = pol.PressureUncertaintyM
		if math.IsNaN(margin) || margin < 0 {
			margin = math.Inf(1)
		}
	}
	allInside = true
	for _, b := range []struct {
		l     *Limit
		lower bool
	}{{z.Lower, true}, {z.Upper, false}} {
		if !needsHeight(b.l, b.lower) {
			continue
		}
		needed++
		var h float64
		known := true
		switch b.l.Ref {
		case core.RefAMSL:
			h = alt
		case core.RefAGL:
			known = env.Ground == GroundKnown && core.IsFinite(env.GroundM)
			h = alt - env.GroundM
		case core.RefWGS84:
			known = env.UndulationM != nil && core.IsFinite(*env.UndulationM)
			if known {
				h = alt + *env.UndulationM
			}
		default:
			known = false
		}
		if !known {
			allInside = false
			continue
		}
		beyond := h - b.l.ValueM
		if b.lower {
			beyond = b.l.ValueM - h
		}
		if beyond > margin {
			outside = true
		}
		if beyond > 0 {
			allInside = false
		}
	}
	return outside, allInside, needed
}

// validLimits reports whether every limit is finite with a known reference.
func validLimits(z *Zone) bool {
	for _, l := range []*Limit{z.Lower, z.Upper} {
		if l != nil && (!core.IsFinite(l.ValueM) || !l.Ref.Valid()) {
			return false
		}
	}
	return true
}

// FuzzJudgeVertical: no panic on any float, and the invariants in both
// directions against an independent oracle: exactly one outcome; a raise
// names the zone and carries a core severity; a limit not judged is a
// warning; inside every limit raises exactly the zone's severity (so a
// downgrade is caught); a judged excluding limit is clear; anything
// unknown is never clear.
func FuzzJudgeVertical(f *testing.F) {
	f.Add(uint8(0), 500.0, uint8(0), 700.0, uint8(0), 600.0, uint8(0), uint8(2), 500.0, 15.0, true, 250.0)
	f.Add(uint8(0), 0.0, uint8(1), 120.0, uint8(1), 550.0, uint8(1), uint8(0), 0.0, 0.0, false, 250.0)
	f.Add(uint8(2), math.NaN(), uint8(2), math.Inf(1), uint8(1), math.NaN(), uint8(2), uint8(2), math.Inf(-1), math.NaN(), true, -1.0)
	f.Add(uint8(1), 50.0, uint8(2), 600.0, uint8(4), 1e308, uint8(1), uint8(7), 1e308, 1e308, true, math.Inf(1))
	f.Fuzz(func(t *testing.T, ty uint8, lowerM float64, lowerRef uint8, upperM float64, upperRef uint8,
		altM float64, src uint8, ground uint8, groundM, undulationM float64, hasGeoid bool, marginM float64,
	) {
		z := &Zone{Identifier: "F", Type: pick(fuzzTypes, ty), Restriction: "R"}
		if lowerRef < 200 {
			z.Lower = &Limit{ValueM: lowerM, Ref: pick(fuzzRefs, lowerRef)}
		}
		if upperRef < 200 {
			z.Upper = &Limit{ValueM: upperM, Ref: pick(fuzzRefs, upperRef)}
		}
		ac := Aircraft{AltAMSLM: &altM, AltSource: pick(fuzzSources, src)}
		env := Env{Ground: GroundKind(ground % 4), GroundM: groundM}
		if hasGeoid {
			env.UndulationM = &undulationM
		}
		pol := DefaultPolicy()
		pol.PressureUncertaintyM = marginM

		r := JudgeVertical(z, ac, env, pol)

		if r.Raise != nil && r.NotEvaluated {
			t.Fatalf("raised and not evaluated: %+v", r)
		}
		if r.LimitNotJudged && (r.Raise == nil || r.Raise.Severity != core.SeverityWarning || r.Reasons == 0) {
			t.Fatalf("limit not judged without a warning and a reason: %+v", r)
		}
		if r.NotEvaluated && r.Reasons == 0 {
			t.Fatalf("not evaluated without a reason: %+v", r)
		}
		// Z-09 with S-37: a PROHIBITED or REQ_AUTHORIZATION zone is never
		// silent because a height reference is missing; it warns.
		missingOnly := r.Reasons&^ReasonsOf(ReasonNoTerrain, ReasonGroundUnknown, ReasonNoGeoid) == 0
		if r.NotEvaluated && warnsUnjudged(z.Type) && missingOnly {
			t.Fatalf("%s zone silent for a missing reference: %+v", z.Type, r)
		}
		if r.Raise != nil {
			switch r.Raise.Severity {
			case core.SeverityInfo, core.SeverityWarning, core.SeverityCritical:
			default:
				t.Fatalf("severity %q", r.Raise.Severity)
			}
			if r.Raise.Kind != KindZone || r.Raise.Detail.Identifier != "F" {
				t.Fatalf("raise %+v", r.Raise)
			}
			for _, p := range []*float64{r.Raise.Detail.HeightAGLM, r.Raise.Detail.AltHAEM} {
				if p != nil && math.IsNaN(*p) {
					t.Fatalf("NaN in detail %+v", r.Raise.Detail)
				}
			}
		}
		sev, raises := Severity(z.Type, pol)
		if !raises {
			if r.Raise != nil || r.NotEvaluated {
				t.Fatalf("a zone that raises nothing gave %+v", r)
			}
			return
		}
		alt, widened, altOK := altitude(ac)
		valid := validLimits(z)
		outside, allInside, needed := oracleLimits(z, alt, widened, env, pol)

		// Presence: every needed height known and inside as indicated (or
		// no limit needs one) raises exactly the zone's severity; flagged
		// only for a widened altitude that was judged against a limit.
		if valid && (needed == 0 || (altOK && allInside)) {
			if r.Raise == nil || r.Raise.Severity != sev {
				t.Fatalf("inside every limit: got %+v (raise %+v), want %s", r, r.Raise, sev)
			}
			d := r.Raise.Detail
			if r.LimitNotJudged || d.LimitNotJudged != nil || d.NotJudged != nil {
				t.Fatalf("inside every limit, flagged not judged: %+v", d)
			}
			if widened && needed > 0 {
				if d.VerticalKnown == nil || *d.VerticalKnown || d.WithinBand == nil || !*d.WithinBand {
					t.Fatalf("widened, inside as indicated: %+v", d)
				}
			} else if d.VerticalKnown != nil || d.WithinBand != nil {
				t.Fatalf("exact altitude, flagged: %+v", d)
			}
		}
		// Absence: a judged limit that excludes beyond the margin decides.
		if valid && altOK && outside && (r.Raise != nil || r.NotEvaluated) {
			t.Fatalf("a judged limit excludes, got %+v (raise %+v)", r, r.Raise)
		}

		if r.Raise != nil || r.NotEvaluated {
			return
		}
		// Clear: everything it rested on was known and finite, and a
		// judged limit excludes the aircraft.
		if !altOK {
			t.Fatalf("clear without a usable altitude (%v, %q)", altM, ac.AltSource)
		}
		if !valid {
			t.Fatalf("clear with an invalid limit %+v %+v", z.Lower, z.Upper)
		}
		if !outside {
			t.Fatalf("clear but no judged limit excludes: zone %+v %+v alt %v env %+v margin %v",
				z.Lower, z.Upper, alt, env, marginM)
		}
	})
}

// FuzzJudgeHeightLimit: no panic; unknown ground, altitude or limit is
// never clear; a raise is strictly over a finite limit.
func FuzzJudgeHeightLimit(f *testing.F) {
	f.Add(620.5, uint8(0), uint8(2), 500.0, 120.0)
	f.Add(math.NaN(), uint8(1), uint8(2), math.Inf(1), math.NaN())
	f.Add(5000.0, uint8(0), uint8(1), 0.0, 120.0)
	f.Fuzz(func(t *testing.T, altM float64, src, ground uint8, groundM, maxM float64) {
		ac := Aircraft{AltAMSLM: &altM, AltSource: pick(fuzzSources, src)}
		env := Env{Ground: GroundKind(ground % 4), GroundM: groundM}
		pol := DefaultPolicy()
		pol.MaxHeightAGLM = &maxM
		r := JudgeHeightLimit(ac, env, pol)
		if r.Raise != nil && r.NotEvaluated {
			t.Fatalf("raised and not evaluated: %+v", r)
		}
		known := core.IsFinite(maxM) && maxM >= 0 && env.Ground == GroundKnown && core.IsFinite(groundM)
		alt, _, altOK := altitude(ac)
		if !known || !altOK {
			if !r.NotEvaluated {
				t.Fatalf("unknown input judged: %+v (alt %v %q, env %+v, max %v)", r, altM, ac.AltSource, env, maxM)
			}
			return
		}
		if r.NotEvaluated {
			t.Fatalf("known input not evaluated: %+v", r)
		}
		over := alt-groundM > maxM
		if over != (r.Raise != nil) {
			t.Fatalf("height %v over %v: raise %+v", alt-groundM, maxM, r.Raise)
		}
	})
}

// FuzzFromED269 parses a zone document and builds a Zone from it,
// seeded with the vector zones: no panic, and a built zone has a shape,
// a box that holds what it contains, and finite limits.
func FuzzFromED269(f *testing.F) {
	file, err := vectors.Read(vectors.Dir() + "/zones_vertical.json")
	if err != nil {
		f.Fatal(err)
	}
	for _, c := range file.Cases {
		var in map[string]json.RawMessage
		if err := json.Unmarshal(c.Input, &in); err != nil {
			f.Fatal(err)
		}
		if !isJSONNull(in["zone"]) {
			f.Add([]byte(in["zone"]), 41.7151, 44.8271)
		}
	}
	f.Add([]byte(`{"identifier":"C","country":"GEO","type":"COMMON","restriction":"PROHIBITED","applicability":[{"permanent":"YES"}],"zoneAuthority":[],"geometry":[{"uomDimensions":"FT","lowerVerticalReference":"AGL","upperVerticalReference":"AMSL","upperLimit":400,"horizontalProjection":{"type":"Circle","center":[180,-16.5],"radius":60000}}]}`), -16.5, -180.0)
	f.Fuzz(func(t *testing.T, raw []byte, latDeg, lonDeg float64) {
		gz, problems := ed269.ParseZone(raw, ed269.Limits{})
		if problems != nil {
			return
		}
		z, err := FromED269(gz)
		if err != nil {
			return
		}
		if (z.Polygon == nil) == (z.Circle == nil) {
			t.Fatalf("zone has %v polygon, %v circle", z.Polygon != nil, z.Circle != nil)
		}
		for _, l := range []*Limit{z.Lower, z.Upper} {
			if l != nil && (!core.IsFinite(l.ValueM) || !l.Ref.Valid()) {
				t.Fatalf("limit %+v", *l)
			}
		}
		p := core.LatLon{LatDeg: latDeg, LonDeg: lonDeg}
		in, err := z.ContainsHorizontally(p)
		if !p.Valid() {
			if in || err == nil {
				t.Fatalf("invalid point %+v judged: %v %v", p, in, err)
			}
			return
		}
		if in && !bboxContains(z, p) {
			t.Fatalf("inside %+v but not in its box %+v", p, z.BBox)
		}
		_ = z.NeedsTerrain()
		_ = z.NeedsGeoid()
		r := JudgeVertical(z, Aircraft{AltAMSLM: &latDeg, AltSource: core.AltPressure}, Env{}, DefaultPolicy())
		if r.Raise != nil && r.NotEvaluated {
			t.Fatalf("raised and not evaluated")
		}
	})
}

// FuzzIndex: no panic on any point, and every zone whose shape contains a
// point (the shape itself, not ContainsHorizontally, which prefilters by
// the same box) is among the index's candidates for it.
func FuzzIndex(f *testing.F) {
	f.Add(41.7151, 44.8271)
	f.Add(-16.5, 180.0)
	f.Add(-16.5, -180.0)
	f.Add(89.95, 100.0)
	f.Add(math.NaN(), 0.0)
	f.Add(10.1, 20.1)
	zs := testZones()
	ix := NewIndex(zs)
	f.Fuzz(func(t *testing.T, latDeg, lonDeg float64) {
		p := core.LatLon{LatDeg: latDeg, LonDeg: lonDeg}
		cands := ix.Candidates(p)
		if !p.Valid() && cands != nil {
			t.Fatalf("invalid point %+v has candidates", p)
		}
		for _, z := range zs {
			if !p.Valid() || !shapeContains(t, z, p) {
				continue
			}
			found := false
			for _, c := range cands {
				found = found || c == z
			}
			if !found {
				t.Fatalf("%s contains %+v but is not a candidate", z.Identifier, p)
			}
		}
	})
}
