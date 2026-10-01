package ed269

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// baseFeature is the base zone of ed269_parse.json (TST001), accepted.
const baseFeature = `{"identifier":"TST001","country":"GEO","name":"Test prohibited square with a hole","type":"COMMON","restriction":"PROHIBITED","reason":["SENSITIVE","AIR_TRAFFIC"],"message":"Test zone, not a real restriction","applicability":[{"permanent":"YES"}],"zoneAuthority":[{"name":"Test authority","service":"Airspace","contactName":"Duty officer","siteURL":"https://example.invalid/zones","email":"zones@example.invalid","phone":"+995 32 000 0000","purpose":"AUTHORIZATION","intervalBefore":"P2D"}],"geometry":[{"uomDimensions":"M","lowerLimit":0,"lowerVerticalReference":"AGL","upperLimit":120,"upperVerticalReference":"AGL","horizontalProjection":{"type":"Polygon","coordinates":[[[44.8,41.7],[44.82,41.7],[44.82,41.72],[44.8,41.72],[44.8,41.7]],[[44.805,41.705],[44.815,41.705],[44.815,41.715],[44.805,41.715],[44.805,41.705]]]}}]}`

// feature returns the base feature with the given top-level members set
// (a nil value deletes the member) and the given volume members set.
func feature(t testing.TB, set map[string]any, vol map[string]any) map[string]any {
	t.Helper()
	var f map[string]any
	if err := json.Unmarshal([]byte(baseFeature), &f); err != nil {
		t.Fatal(err)
	}
	for k, v := range set {
		if v == nil {
			delete(f, k)
		} else {
			f[k] = v
		}
	}
	if vol != nil {
		g, _ := f["geometry"].([]any)
		v0, _ := g[0].(map[string]any)
		for k, v := range vol {
			if v == nil {
				delete(v0, k)
			} else {
				v0[k] = v
			}
		}
	}
	return f
}

func docOf(t testing.TB, features ...any) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"features": features})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustParse(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, probs := Parse(data, DefaultLimits)
	if probs != nil {
		t.Fatalf("refused: %v", probs)
	}
	return doc
}

// E-01: the base feature is accepted, so every refusal below is caused by
// its one mutation.
func TestBaseFeatureAccepted(t *testing.T) {
	doc := mustParse(t, docOf(t, feature(t, nil, nil)))
	if len(doc.Zones) != 1 || doc.Zones[0].Identifier != "TST001" || doc.Wrapper != WrapperFeatures {
		t.Fatalf("got %+v", doc)
	}
	if doc.Zones[0].Restriction.ZoneType() != core.ZoneProhibited {
		t.Errorf("zone type %q", doc.Zones[0].Restriction.ZoneType())
	}
}

func TestRefusals(t *testing.T) {
	const f0 = "features[0]"
	const v0 = f0 + ".geometry[0]"
	const hp = v0 + ".horizontalProjection"
	ring := func(n int) []any {
		out := make([]any, 0, n)
		for i := range n - 1 {
			a := 2 * math.Pi * float64(i) / float64(n-1)
			out = append(out, []any{44.8 + 0.01*math.Cos(a), 41.7 + 0.01*math.Sin(a)})
		}
		return append(out, out[0])
	}
	poly := func(rings ...any) map[string]any {
		return map[string]any{"type": "Polygon", "coordinates": rings}
	}
	tests := []struct {
		name   string
		set    map[string]any
		vol    map[string]any
		field  string
		reason string
	}{
		{"identifier spaces", map[string]any{"identifier": " T1"}, nil, f0 + ".identifier", "leading or trailing spaces"},
		{"identifier blank", map[string]any{"identifier": "  "}, nil, f0 + ".identifier", "must not be empty"},
		{"identifier number", map[string]any{"identifier": 7}, nil, f0 + ".identifier", "must be a string, not a number"},
		{"country four letters", map[string]any{"country": "GEOR"}, nil, f0 + ".country", "at most 3"},
		{"name long", map[string]any{"name": strings.Repeat("n", 201)}, nil, f0 + ".name", "at most 200"},
		{"name counts characters", map[string]any{"name": strings.Repeat("ö", 201)}, nil, f0 + ".name", "is 201 characters"},
		{"type outside the enumeration", map[string]any{"type": "CUSTOM"}, nil, f0 + ".type", "'CUSTOM' is not one of COMMON, CUSTOMIZED"},
		{"type number", map[string]any{"type": 1}, nil, f0 + ".type", "not a number"},
		{"restriction missing", map[string]any{"restriction": nil}, nil, f0 + ".restriction", "missing"},
		{"restriction number", map[string]any{"restriction": 1}, nil, f0 + ".restriction", "not a number"},
		{"reason not a list", map[string]any{"reason": "NOISE"}, nil, f0 + ".reason", "must be a list"},
		{"ten reasons", map[string]any{"reason": []any{"AIR_TRAFFIC", "SENSITIVE", "PRIVACY", "POPULATION", "NATURE", "NOISE", "FOREIGN_TERRITORY", "EMERGENCY", "OTHER", "OTHER"}}, nil, f0 + ".reason", "at most 9"},
		{"message number", map[string]any{"message": 1}, nil, f0 + ".message", "must be a string"},
		{"authority not a list", map[string]any{"zoneAuthority": map[string]any{}}, nil, f0 + ".zoneAuthority", "must be a list, not an object"},
		{"authority not an object", map[string]any{"zoneAuthority": []any{"x"}}, nil, f0 + ".zoneAuthority[0]", "must be an object"},
		{"authority purpose number", map[string]any{"zoneAuthority": []any{map[string]any{"purpose": 1}}}, nil, f0 + ".zoneAuthority[0].purpose", "not a number"},
		{"authority name long", map[string]any{"zoneAuthority": []any{map[string]any{"name": strings.Repeat("a", 201)}}}, nil, f0 + ".zoneAuthority[0].name", "at most 200"},
		{"authority email number", map[string]any{"zoneAuthority": []any{map[string]any{"email": 5}}}, nil, f0 + ".zoneAuthority[0].email", "must be a string"},
		{"period not an object", map[string]any{"applicability": []any{"YES"}}, nil, f0 + ".applicability[0]", "must be an object"},
		{"period unknown field", map[string]any{"applicability": []any{map[string]any{"permanent": "YES", "until": "x"}}}, nil, f0 + ".applicability[0].until", "unknown field"},
		{"permanent missing", map[string]any{"applicability": []any{map[string]any{"startDateTime": "2026-01-01T00:00:00Z"}}}, nil, f0 + ".applicability[0].permanent", "not null"},
		{"permanent with schedule", map[string]any{"applicability": []any{map[string]any{"permanent": "YES", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule", "permanent YES"},
		{"date number", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "startDateTime": 5}}}, nil, f0 + ".applicability[0].startDateTime", "not a number"},
		{"date garbage", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "startDateTime": "soon"}}}, nil, f0 + ".applicability[0].startDateTime", "not an ISO 8601 date-time"},
		{"date only", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "endDateTime": "2026-01-01"}}}, nil, f0 + ".applicability[0].endDateTime", "no offset"},
		{"equal dates", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "startDateTime": "2026-01-01T04:00:00+04:00", "endDateTime": "2026-01-01T00:00:00Z"}}}, nil, f0 + ".applicability[0].endDateTime", "not after"},
		{"schedule empty", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{}}}}, nil, f0 + ".applicability[0].schedule", "at least one daily period"},
		{"daily not an object", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{1}}}}, nil, f0 + ".applicability[0].schedule[0]", "must be an object"},
		{"daily unknown field", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00Z", "endTime": "09:00Z", "week": 1}}}}}, nil, f0 + ".applicability[0].schedule[0].week", "unknown field"},
		{"days empty", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{}, "startTime": "08:00Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].day", "1 to 7"},
		{"day twice", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON", "MON"}, "startTime": "08:00Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].day[1]", "MON is listed twice"},
		{"day number", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{1}, "startTime": "08:00Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].day[0]", "1 is not one of"},
		{"clock missing", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "null is not a time of day"},
		{"clock hour 24", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "24:00Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"clock seven fraction digits", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00:00.1234567Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"clock bad seconds", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00:61Z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"clock offset hour", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00+25:00", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"clock offset minutes", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00+04:6", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"clock lower-case z", map[string]any{"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"MON"}, "startTime": "08:00z", "endTime": "09:00Z"}}}}}, nil, f0 + ".applicability[0].schedule[0].startTime", "with an offset"},
		{"geometry not a list", map[string]any{"geometry": map[string]any{}}, nil, f0 + ".geometry", "one airspace volume"},
		{"geometry empty", map[string]any{"geometry": []any{}}, nil, f0 + ".geometry", "one airspace volume"},
		{"volume not an object", map[string]any{"geometry": []any{1}}, nil, v0, "must be an object"},
		{"volume unknown field", nil, map[string]any{"altitude": 1}, v0 + ".altitude", "unknown field"},
		{"uom missing", nil, map[string]any{"uomDimensions": nil}, v0 + ".uomDimensions", "not null"},
		{"limit beyond a double", nil, map[string]any{"upperLimit": raw("1e400")}, v0 + ".upperLimit", "range of a double"},
		{"limit boolean", nil, map[string]any{"upperLimit": true}, v0 + ".upperLimit", "not a boolean"},
		{"projection missing", nil, map[string]any{"horizontalProjection": nil}, v0 + ".horizontalProjection", "missing"},
		{"projection type missing", nil, map[string]any{"horizontalProjection": map[string]any{"coordinates": []any{}}}, hp + ".type", "null is not Polygon or Circle"},
		{"polygon with radius", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Polygon", "coordinates": []any{ring(5)}, "radius": 5}}, hp + ".radius", "not a field of a Polygon"},
		{"polygon without coordinates", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Polygon"}}, hp + ".coordinates", "list of rings"},
		{"polygon no rings", nil, map[string]any{"horizontalProjection": poly()}, hp + ".coordinates", "list of rings"},
		{"ring not a list", nil, map[string]any{"horizontalProjection": poly(5)}, hp + ".coordinates[0]", "a ring must be a list"},
		{"position of three", nil, map[string]any{"horizontalProjection": poly([]any{[]any{1, 2, 3}})}, hp + ".coordinates[0][0]", "[longitude, latitude]"},
		{"position of strings", nil, map[string]any{"horizontalProjection": poly([]any{[]any{"1", "2"}})}, hp + ".coordinates[0][0]", "two numbers"},
		{"longitude out of range", nil, map[string]any{"horizontalProjection": poly([]any{[]any{180.5, 2}})}, hp + ".coordinates[0][0]", "[180.5, 2] is outside"},
		{"two distinct positions", nil, map[string]any{"horizontalProjection": poly([]any{[]any{1, 2}, []any{3, 4}, []any{1, 2}, []any{3, 4}, []any{1, 2}})}, hp + ".coordinates[0]", "three distinct"},
		{"minus zero is zero", nil, map[string]any{"horizontalProjection": poly([]any{[]any{0, 0}, []any{raw("-0"), 0}, []any{1, 1}, []any{0, 0}})}, hp + ".coordinates[0]", "three distinct"},
		{"circle without center", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "radius": 5}}, hp + ".center", "[longitude, latitude]"},
		{"circle radius zero", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "radius": 0, "center": []any{1, 2}}}, hp + ".radius", "above 0"},
		{"polygon spans more than 180 degrees", nil, map[string]any{"horizontalProjection": poly([]any{[]any{-90.5, 0}, []any{90.5, 0}, []any{90.5, 1}, []any{-90.5, 0}})}, hp, "longitude span exceeds 180°"},
		{"polygon across the antimeridian", nil, map[string]any{"horizontalProjection": poly([]any{[]any{179.9, 0}, []any{-179.9, 0}, []any{-179.9, 1}, []any{179.9, 0}})}, hp, "crosses the antimeridian"},
		{"a hole decides the span too", nil, map[string]any{"horizontalProjection": poly(ring(5), []any{[]any{-170, 0}, []any{-169, 0}, []any{-169, 1}, []any{-170, 0}})}, hp, "longitude span exceeds 180°"},
		{"circle past 180 east", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "center": []any{179.999, 41.7}, "radius": 1000}}, hp, "reaches past ±180°"},
		{"circle past 180 west", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "center": []any{-179.999, 41.7}, "radius": 1000}}, hp, "crosses the antimeridian"},
		{"circle radius in metres crosses", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "center": []any{179.985, 41.7}, "radius": 3000}}, hp, "crosses the antimeridian"},
		{"circle wider than half the parallel", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "center": []any{0, 89.999}, "radius": 1000}}, hp, "longitude span exceeds 180°"},
		{"circle on a pole", nil, map[string]any{"horizontalProjection": map[string]any{"type": "Circle", "center": []any{0, 90}, "radius": 1}}, hp, "longitude span exceeds 180°"},
		{"conditions number", map[string]any{"restrictionConditions": 5}, nil, f0 + ".restrictionConditions", "string or a list of strings"},
		{"conditions mixed", map[string]any{"restrictionConditions": []any{"a", 5}}, nil, f0 + ".restrictionConditions", "string or a list of strings"},
		{"region fraction", map[string]any{"region": 7.5}, nil, f0 + ".region", "must be an integer, not 7.5"},
		{"region exponent", map[string]any{"region": raw("1e2")}, nil, f0 + ".region", "must be an integer, not 1e2"},
		{"region huge", map[string]any{"region": raw("123456789012345678901234567890")}, nil, f0 + ".region", "range of an int"},
		{"exemption number", map[string]any{"regulationExemption": 1}, nil, f0 + ".regulationExemption", "not a number"},
		{"other reason long", map[string]any{"otherReasonInfo": strings.Repeat("o", 31)}, nil, f0 + ".otherReasonInfo", "at most 30"},
		{"uSpaceClass long", map[string]any{"uSpaceClass": strings.Repeat("u", 101)}, nil, f0 + ".uSpaceClass", "at most 100"},
		{"title number", map[string]any{"title": 1}, nil, f0 + ".title", "must be a string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, probs := Parse(docOf(t, feature(t, tc.set, tc.vol)), DefaultLimits)
			if doc != nil || !hasProblem(probs, tc.field, tc.reason) {
				t.Fatalf("want %s: %q; got %v", tc.field, tc.reason, probs)
			}
		})
	}
	// The accepted twins of the longitude span refusals.
	for name, vol := range map[string]map[string]any{
		"polygon spanning exactly 180 degrees": {"horizontalProjection": poly([]any{[]any{-90, 0}, []any{90, 0}, []any{90, 1}, []any{-90, 0}})},
		"polygon next to the antimeridian":     {"horizontalProjection": poly([]any{[]any{179.9, 0}, []any{180, 0}, []any{180, 1}, []any{179.9, 0}})},
		"circle next to the antimeridian":      {"horizontalProjection": map[string]any{"type": "Circle", "center": []any{179.9, 41.7}, "radius": 1000}},
		"circle radius in feet stays inside": {"uomDimensions": "FT", "lowerLimit": 0, "upperLimit": 400,
			"horizontalProjection": map[string]any{"type": "Circle", "center": []any{179.985, 41.7}, "radius": 3000}},
	} {
		t.Run(name, func(t *testing.T) {
			mustParse(t, docOf(t, feature(t, nil, vol)))
		})
	}
	t.Run("ring of 5000 accepted", func(t *testing.T) {
		mustParse(t, docOf(t, feature(t, nil, map[string]any{"horizontalProjection": poly(ring(5000))})))
	})
	t.Run("ring of 5001 refused", func(t *testing.T) {
		_, probs := Parse(docOf(t, feature(t, nil, map[string]any{"horizontalProjection": poly(ring(5001))})), DefaultLimits)
		if !hasProblem(probs, hp+".coordinates[0]", "has 5001 positions; at most 5000 per ring") {
			t.Fatalf("got %v", probs)
		}
	})
	t.Run("ring cap is a limit", func(t *testing.T) {
		lim := DefaultLimits
		lim.MaxRingVertices = 4
		_, probs := Parse(docOf(t, feature(t, nil, nil)), lim)
		if !hasProblem(probs, hp+".coordinates[0]", "at most 4 per ring") {
			t.Fatalf("got %v", probs)
		}
	})
}

func TestDocumentRefusals(t *testing.T) {
	tests := []struct {
		name, data, field, reason string
	}{
		{"empty", "", "$", "not JSON"},
		{"truncated", `{"features": [`, "$", "not JSON"},
		{"trailing data", `{"features": []} {}`, "$", "not JSON"},
		{"scalar", `5`, "$", "not a JSON object"},
		{"title number", `{"features": [], "title": 1}`, "title", "must be a string"},
		{"list wrapper refuses title", `{"UASZoneList": [], "title": "x"}`, "title", "unknown field"},
		{"list wrapper not a list", `{"UASZoneList": 1}`, "UASZoneList", "list of zones"},
		{"both wrappers", `{"features": [], "UASZoneList": []}`, "UASZoneList", "unknown field"},
		{"zone not an object", `{"features": [1]}`, "features[0]", "a zone must be an object"},
		{"repeated member", `{"features": [], "features": []}`, "features", "repeated member name"},
		{"repeated member deep", `{"features": [{"a": {"b": 1, "b": 2}}]}`, "features[0].a.b", "repeated member name"},
		{"repeated member in a large object", `{"features": [], "extra": {"a1":1,"a2":2,"a3":3,"a4":4,"a5":5,"a6":6,"a7":7,"a8":8,"a9":9,"a1":10}}`, "extra.a1", "repeated member name"},
		{"repeated member in a list", `{"features": [], "x": [{"k": 1, "k": 2}]}`, "x[0].k", "repeated member name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, probs := Parse([]byte(tc.data), DefaultLimits)
			if doc != nil || !hasProblem(probs, tc.field, tc.reason) {
				t.Fatalf("want %s: %q; got %v", tc.field, tc.reason, probs)
			}
		})
	}
	t.Run("large object without repeats accepted", func(t *testing.T) {
		ext := map[string]any{}
		for i := range 20 {
			ext[fmt.Sprintf("k%d", i)] = i
		}
		mustParse(t, docOf(t, feature(t, map[string]any{"extendedProperties": ext}, nil)))
	})
}

func TestRepeatedIdentifierNamesBothPlaces(t *testing.T) {
	_, probs := Parse(docOf(t, feature(t, nil, nil), feature(t, map[string]any{"identifier": "TST002"}, nil), feature(t, nil, nil)), DefaultLimits)
	if !hasProblem(probs, "features[2].identifier", "'TST001' is also the identifier of features[0]") {
		t.Fatalf("got %v", probs)
	}
	if len(probs.List) != 1 {
		t.Errorf("want one problem, got %v", probs)
	}
	mustParse(t, docOf(t, feature(t, nil, nil), feature(t, map[string]any{"identifier": "TST002"}, nil)))
}

// E-10: the problem list is capped and counts the rest.
func TestProblemCap(t *testing.T) {
	withUnknown := func(n int) []byte {
		set := map[string]any{}
		for i := range n {
			set[fmt.Sprintf("x%03d", i)] = 1
		}
		return docOf(t, feature(t, set, nil))
	}
	_, probs := Parse(withUnknown(100), DefaultLimits)
	if probs == nil || len(probs.List) != 100 || probs.Truncated != 0 {
		t.Fatalf("100 problems: got %d listed, %d truncated", len(probs.List), probs.Truncated)
	}
	_, probs = Parse(withUnknown(101), DefaultLimits)
	if probs == nil || len(probs.List) != 100 || probs.Truncated != 1 {
		t.Fatalf("101 problems: got %d listed, %d truncated", len(probs.List), probs.Truncated)
	}
	if !strings.HasSuffix(probs.Error(), "; and 1 more") {
		t.Errorf("Error() = %q", probs.Error())
	}
}

// E-10: depth 32 is accepted, 33 refused, counted while tokenising.
func TestDepthBound(t *testing.T) {
	nested := func(k int) []byte {
		// The document, features and the zone are three levels.
		ext := strings.Repeat("[", k) + strings.Repeat("]", k)
		return []byte(`{"features":[` + strings.TrimSuffix(baseFeature, "}") + `,"extendedProperties":` + ext + `}]}`)
	}
	doc := mustParse(t, nested(29))
	if got := string(doc.Zones[0].ExtendedProperties); got != strings.Repeat("[", 28)+strings.Repeat("]", 28) && len(got) != 58 {
		t.Errorf("extendedProperties %s", got)
	}
	_, probs := Parse(nested(30), DefaultLimits)
	if !hasProblem(probs, "$", "nested too deeply") {
		t.Fatalf("depth 33: got %v", probs)
	}
	lim := Limits{MaxDepth: 2}
	if _, probs := Parse([]byte(`{"features":[]}`), lim); probs != nil {
		t.Errorf("depth 2 at MaxDepth 2: %v", probs)
	}
	if _, probs := Parse([]byte(`{"features":[{}]}`), lim); !hasProblem(probs, "$", "nested too deeply") {
		t.Errorf("depth 3 at MaxDepth 2: %v", probs)
	}
}

// E-10: an input over MaxBytes is refused before it is parsed.
func TestMaxBytes(t *testing.T) {
	data := docOf(t, feature(t, nil, nil))
	lim := Limits{MaxBytes: len(data)}
	if _, probs := Parse(data, lim); probs != nil {
		t.Fatalf("at the bound: %v", probs)
	}
	lim.MaxBytes = len(data) - 1
	_, probs := Parse(data, lim)
	if !hasProblem(probs, "$", fmt.Sprintf("is %d bytes; at most %d", len(data), len(data)-1)) {
		t.Fatalf("past the bound: %v", probs)
	}
	// Not JSON either: the size check must come first.
	big := bytes.Repeat([]byte("x"), 11)
	_, probs = Parse(big, Limits{MaxBytes: 10})
	if len(probs.List) != 1 || !strings.Contains(probs.List[0].Reason, "bytes; at most 10") {
		t.Fatalf("got %v", probs)
	}
}

func TestParseZoneRefusals(t *testing.T) {
	if _, probs := ParseZone(raw(`{`), DefaultLimits); !hasProblem(probs, "zone", "not JSON") {
		t.Errorf("not JSON: %v", probs)
	}
	if _, probs := ParseZone([]byte{0xff}, DefaultLimits); !hasProblem(probs, "zone", "not UTF-8") {
		t.Errorf("not UTF-8: %v", probs)
	}
	if _, probs := ParseZone(raw(`[]`), DefaultLimits); !hasProblem(probs, "zone", "a zone must be an object") {
		t.Errorf("not an object: %v", probs)
	}
	if _, probs := ParseZone(raw(baseFeature), Limits{MaxBytes: 10}); !hasProblem(probs, "zone", "at most 10") {
		t.Errorf("too big: %v", probs)
	}
	if _, probs := ParseZone(raw(`{"identifier":"A","identifier":"B"}`), DefaultLimits); !hasProblem(probs, "zone.identifier", "repeated member name") {
		t.Errorf("repeated: %v", probs)
	}
	z, probs := ParseZone(raw(baseFeature), Limits{})
	if probs != nil || z.Identifier != "TST001" {
		t.Fatalf("accepted twin: %v", probs)
	}
}

func TestFirstInvalid(t *testing.T) {
	if got := firstInvalid([]byte("ab\xc3")); got != "invalid byte 0xc3 at offset 2" {
		t.Errorf("got %q", got)
	}
	if got := firstInvalid([]byte("ok")); got != "invalid encoding" {
		t.Errorf("got %q", got)
	}
}
