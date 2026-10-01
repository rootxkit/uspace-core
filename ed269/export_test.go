package ed269

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// canonical decodes JSON with numbers as float64 values, so that two
// documents compare by value (5.0 == 5) and not by key order.
func canonical(t testing.TB, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return byValue(v)
}

func byValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = byValue(e)
		}
	case []any:
		for i, e := range t {
			t[i] = byValue(e)
		}
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return string(t)
		}
		return f
	}
	return v
}

// The accepted twins of refusals whose acceptance is not shown by the
// vectors.
func TestAcceptedVariants(t *testing.T) {
	cases := map[string]map[string]any{
		"type CUSTOMIZED": {"type": "CUSTOMIZED"},
		"upper limit above 0 from the surface": {"geometry": []any{map[string]any{
			"uomDimensions": "M", "lowerVerticalReference": "AGL", "upperLimit": 0.5,
			"upperVerticalReference": "AGL", "horizontalProjection": map[string]any{"type": "Circle", "center": []any{44.8, 41.7}, "radius": 100},
		}}},
		"lower limit 0": {"geometry": []any{map[string]any{
			"uomDimensions": "M", "lowerLimit": raw("-0"), "lowerVerticalReference": "AGL",
			"upperVerticalReference": "AGL", "horizontalProjection": map[string]any{"type": "Circle", "center": []any{44.8, 41.7}, "radius": 100},
		}}},
		"reason absent":                     {"reason": nil},
		"reason empty":                      {"reason": []any{}},
		"conditions as a string":            {"restrictionConditions": "Notify first"},
		"conditions empty list":             {"restrictionConditions": []any{}},
		"title on the zone":                 {"title": "A title"},
		"extended properties a list":        {"extendedProperties": []any{1.5, "x", nil, true, map[string]any{"a": []any{}}}},
		"offset without colon":              {"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"ANY"}, "startTime": "08:00+0400", "endTime": "09:00:30.5+0400"}}}}},
		"date with an offset without colon": {"applicability": []any{map[string]any{"permanent": "NO", "startDateTime": "2026-01-01T00:00:00.5+0400", "endDateTime": "2026-01-02T00:00-0130"}}},
		"date without seconds":              {"applicability": []any{map[string]any{"permanent": "NO", "startDateTime": "2026-01-01T00:00Z"}}},
		"negative offset night":             {"applicability": []any{map[string]any{"permanent": "NO", "schedule": []any{map[string]any{"day": []any{"SUN"}, "startTime": "22:00-03:00", "endTime": "02:00-03:00"}}}}},
		"upper below lower in another reference": {"geometry": []any{map[string]any{
			"uomDimensions": "M", "lowerLimit": 100, "lowerVerticalReference": "AMSL", "upperLimit": 50,
			"upperVerticalReference": "AGL", "horizontalProjection": map[string]any{"type": "Circle", "center": []any{44.8, 41.7}, "radius": 100},
		}}},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			data := docOf(t, feature(t, set, nil))
			doc := mustParse(t, data)
			out, err := Export(doc)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(canonical(t, out), canonical(t, data)) {
				t.Errorf("round trip\n got %s\nwant %s", out, data)
			}
		})
	}
}

func TestListWrapperRoundTrips(t *testing.T) {
	var f any
	if err := json.Unmarshal([]byte(baseFeature), &f); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"formatVersion": "1.0", "createdAt": "2026-10-01T00:00:00Z", "UASZoneList": []any{f}})
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, data)
	if doc.Wrapper != WrapperUASZoneList || doc.FormatVersion == nil || *doc.FormatVersion != "1.0" {
		t.Fatalf("got %+v", doc)
	}
	out, err := Export(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical(t, out), canonical(t, data)) {
		t.Errorf("round trip\n got %s\nwant %s", out, data)
	}
}

// Periods built in code, without published text, evaluate and export.
func TestProgrammaticPeriods(t *testing.T) {
	start := mustTime(t, "2026-10-01T00:00:00Z")
	loc := time.FixedZone("+04:00", 4*3600)
	d := DailyPeriod{
		Days:   []time.Weekday{time.Monday, time.Wednesday},
		Start:  time.Date(0, 1, 1, 9, 0, 0, 0, loc),
		End:    time.Date(0, 1, 1, 17, 0, 0, 500000000, loc),
		Offset: loc,
	}
	p := Period{Start: &start, Schedule: []DailyPeriod{d}}
	if !p.Contains(mustTime(t, "2026-10-05T06:00:00Z")) || p.Contains(mustTime(t, "2026-10-06T06:00:00Z")) {
		t.Error("Contains")
	}
	noOffset := DailyPeriod{Days: []time.Weekday{time.Monday}, Start: time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC), End: time.Date(0, 1, 1, 10, 0, 0, 0, time.UTC)}
	if !noOffset.Contains(mustTime(t, "2026-10-05T09:30:00Z")) {
		t.Error("nil Offset is UTC")
	}
	if !(Period{End: &start}).Contains(start) {
		t.Error("bounded only by an end")
	}
	every := DailyPeriod{Days: []time.Weekday{0, 1, 2, 3, 4, 5, 6}, Start: d.Start, End: d.End, Offset: loc}
	z := &GeoZone{
		Identifier: "P1", Country: "GEO", Type: "COMMON", Restriction: RestrictionConditional,
		Applicability: []Period{p, {Schedule: []DailyPeriod{every}}},
		ZoneAuthority: []Authority{},
		Geometry: []Volume{{Uom: UomMetres, LowerRef: core.RefAGL, UpperRef: core.RefAGL,
			Projection: HorizontalProjection{Type: ShapeCircle, Center: &Position{LatDeg: 41.7, LonDeg: 44.8}, Radius: ptr(100.0)}}},
	}
	m, err := Feature(z)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m["applicability"])
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"permanent":"NO","schedule":[{"day":["MON","WED"],"endTime":"17:00:00.5+04:00","startTime":"09:00:00+04:00"}],"startDateTime":"2026-10-01T00:00:00Z"},{"permanent":"NO","schedule":[{"day":["ANY"],"endTime":"17:00:00.5+04:00","startTime":"09:00:00+04:00"}]}]`
	if string(b) != want {
		t.Errorf("applicability\n got %s\nwant %s", b, want)
	}
	// What Feature wrote reads back.
	zb, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	back, probs := ParseZone(zb, DefaultLimits)
	if probs != nil {
		t.Fatalf("re-read: %v", probs)
	}
	if !Applies(back.Applicability, mustTime(t, "2026-10-05T06:00:00Z")) {
		t.Error("re-read period does not apply")
	}
}

func TestExportErrors(t *testing.T) {
	good := func() *Document {
		return mustParse(t, docOf(t, feature(t, nil, nil)))
	}
	var fe *core.FieldError
	if _, err := Export(nil); err == nil {
		t.Error("nil document")
	}
	d := good()
	d.Wrapper = "zones"
	if _, err := Export(d); err == nil {
		t.Error("unknown wrapper")
	}
	d = good()
	d.Wrapper = ""
	if _, err := Export(d); err != nil {
		t.Errorf("empty wrapper writes features: %v", err)
	}
	d = good()
	d.Zones[0].Geometry[0].UpperLimit = ptr(math.Inf(1))
	_, err := Export(d)
	if !asFieldError(err, &fe) || fe.Field != "features[0].geometry[0].upperLimit" {
		t.Errorf("infinite limit: %v", err)
	}
	if _, err := Feature(&d.Zones[0]); err == nil {
		t.Error("Feature with an infinite limit")
	}
	d = good()
	d.Zones[0].Geometry[0].Projection.Type = "Ellipse"
	if _, err := Export(d); err == nil {
		t.Error("unknown shape")
	}
	if _, err := Feature(&d.Zones[0]); err == nil {
		t.Error("Feature with an unknown shape")
	}
	d = good()
	d.Zones[0].Geometry[0].Projection = HorizontalProjection{Type: ShapeCircle}
	if _, err := Export(d); err == nil {
		t.Error("circle without center")
	}
	d = good()
	d.Zones[0].ExtendedProperties = json.RawMessage(`{`)
	if _, err := Export(d); err == nil {
		t.Error("invalid extendedProperties")
	}
	d = good()
	d.Zones[0].RestrictionConditions = []string{"a", "b"}
	d.Zones[0].RestrictionConditionsIsText = true
	out, err := Export(d)
	if err != nil || !strings.Contains(string(out), `"restrictionConditions":["a","b"]`) {
		t.Errorf("two conditions as text: %s %v", out, err)
	}
	d = good()
	d.Zones[0].Geometry[0].LowerLimit = ptr(1e21)
	d.Zones[0].Geometry[0].UpperLimit = ptr(2.5e-7)
	out, err = Export(d)
	if err != nil || !strings.Contains(string(out), `"lowerLimit":1e+21`) || !strings.Contains(string(out), `"upperLimit":2.5e-07`) {
		t.Errorf("number forms: %s %v", out, err)
	}
	if (*Problems)(nil).Error() != "no problems" {
		t.Error("nil Problems")
	}
}

func asFieldError(err error, fe **core.FieldError) bool {
	e, ok := err.(*core.FieldError) //nolint:errorlint // Export returns *core.FieldError unwrapped
	if ok {
		*fe = e
	}
	return ok
}

// Z-01: a null optional field is read as absent and written back absent.
func TestNullOptionalsExportAbsent(t *testing.T) {
	data := docOf(t, feature(t, map[string]any{
		"name":          nil,
		"message":       json.RawMessage("null"),
		"zoneAuthority": []any{map[string]any{"name": json.RawMessage("null"), "purpose": "INFORMATION"}},
	}, map[string]any{"upperLimit": json.RawMessage("null")}))
	out, err := Export(mustParse(t, data))
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{`"message"`, `"name"`, `"upperLimit"`, "null"} {
		if bytes.Contains(out, []byte(absent)) {
			t.Errorf("export contains %s: %s", absent, out)
		}
	}
	if !bytes.Contains(out, []byte(`"zoneAuthority":[{"purpose":"INFORMATION"}]`)) {
		t.Errorf("authority: %s", out)
	}
}
