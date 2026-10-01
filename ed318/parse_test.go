package ed318

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// baseDocument is testdata/authority_collection.json, the accepted
// collection the ED-318 vector is built from, as a plain JSON tree to
// mutate.
func baseDocument(t testing.TB) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "authority_collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func baseBytes(t testing.TB) []byte {
	t.Helper()
	b, err := json.Marshal(baseDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// at walks a path of member names and list indexes.
func at(t *testing.T, doc any, path ...any) any {
	t.Helper()
	cur := doc
	for _, p := range path {
		switch k := p.(type) {
		case string:
			cur = cur.(map[string]any)[k]
		case int:
			cur = cur.([]any)[k]
		}
	}
	return cur
}

func props(t *testing.T, doc map[string]any, i int) map[string]any {
	t.Helper()
	return at(t, doc, "features", i, "properties").(map[string]any)
}

func geom(t *testing.T, doc map[string]any, i int) map[string]any {
	t.Helper()
	return at(t, doc, "features", i, "geometry").(map[string]any)
}

func daily(t *testing.T, doc map[string]any, i int) map[string]any {
	t.Helper()
	return at(t, doc, "features", i, "properties", "limitedApplicability", 0, "schedule", 0).(map[string]any)
}

// Every refusal is the accepted base with one change: the base itself is
// accepted (E-01), and each change is refused naming its field.
func TestParseRefusals(t *testing.T) {
	if _, probs := Parse(baseBytes(t), Limits{}); probs != nil {
		t.Fatalf("the base is refused: %v", probs)
	}
	cases := []struct {
		name   string
		mut    func(t *testing.T, d map[string]any)
		field  string
		reason string
	}{
		{"collection type", func(_ *testing.T, d map[string]any) { d["type"] = "Feature" }, "type", "must be 'FeatureCollection'"},
		{"no collection type", func(_ *testing.T, d map[string]any) { delete(d, "type") }, "type", "missing"},
		{"no features", func(_ *testing.T, d map[string]any) { delete(d, "features") }, "features", "missing"},
		{"long name", func(_ *testing.T, d map[string]any) { d["name"] = strings.Repeat("n", 201) }, "name", "at most 200"},
		{"short bbox", func(_ *testing.T, d map[string]any) { d["bbox"] = []any{1, 2} }, "bbox", "at least four"},
		{"bbox member", func(_ *testing.T, d map[string]any) { d["bbox"] = []any{1, 2, 3, "x"} }, "bbox[3]", "finite number"},
		{"metadata not object", func(_ *testing.T, d map[string]any) { d["metadata"] = "x" }, "metadata", "must be an object"},
		{"feature not object", func(_ *testing.T, d map[string]any) { d["features"].([]any)[0] = 1 }, "features[0]", "must be an object"},
		{"feature type", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["type"] = "Zone" }, "features[0].type", "must be 'Feature'"},
		{"feature id", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["id"] = true }, "features[0].id", "string or a number"},
		{"null properties", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["properties"] = nil }, "features[0].properties", "needs its UASZone"},
		{"properties not object", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["properties"] = 1 }, "features[0].properties", "must be an object"},
		{"null geometry", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["geometry"] = nil }, "features[0].geometry", "needs a geometry"},
		{"duplicate identifier", func(t *testing.T, d map[string]any) { props(t, d, 4)["identifier"] = "TSU001" }, "features[4].properties.identifier", "also the identifier of features[0]"},
		{"unknown property", func(t *testing.T, d map[string]any) { props(t, d, 0)["restriction"] = "PROHIBITED" }, "features[0].properties.restriction", "unknown property"},
		{"no identifier", func(t *testing.T, d map[string]any) { delete(props(t, d, 0), "identifier") }, "properties.identifier", "missing"},
		{"blank identifier", func(t *testing.T, d map[string]any) { props(t, d, 0)["identifier"] = " " }, "properties.identifier", "must not be empty"},
		{"long identifier", func(t *testing.T, d map[string]any) { props(t, d, 0)["identifier"] = "TSU0001" + "X" }, "properties.identifier", "at most 7"},
		{"padded identifier", func(t *testing.T, d map[string]any) { props(t, d, 0)["identifier"] = "TSU001 " }, "properties.identifier", "leading or trailing"},
		{"identifier number", func(t *testing.T, d map[string]any) { props(t, d, 0)["identifier"] = 1 }, "properties.identifier", "must be a string"},
		{"country", func(t *testing.T, d map[string]any) { props(t, d, 0)["country"] = "geo" }, "properties.country", "alpha-3"},
		{"type", func(t *testing.T, d map[string]any) { props(t, d, 0)["type"] = "FORBIDDEN" }, "properties.type", "is not one of"},
		{"type number", func(t *testing.T, d map[string]any) { props(t, d, 0)["type"] = 1 }, "properties.type", "must be one of"},
		{"no variant", func(t *testing.T, d map[string]any) { delete(props(t, d, 0), "variant") }, "properties.variant", "missing"},
		{"region float", func(t *testing.T, d map[string]any) { props(t, d, 1)["region"] = 1.5 }, "properties.region", "integer"},
		{"region huge", func(t *testing.T, d map[string]any) { props(t, d, 1)["region"] = json.Number("99999999999999999999") }, "properties.region", "range of an int"},
		{"reason not list", func(t *testing.T, d map[string]any) { props(t, d, 0)["reason"] = "AIR_TRAFFIC" }, "properties.reason", "must be a list"},
		{"reason twice", func(t *testing.T, d map[string]any) { props(t, d, 0)["reason"] = []any{"NOISE", "NOISE"} }, "properties.reason[1]", "listed twice"},
		{"too many reasons", func(t *testing.T, d map[string]any) {
			props(t, d, 0)["reason"] = []any{"AIR_TRAFFIC", "SENSITIVE", "PRIVACY", "POPULATION", "NATURE", "NOISE", "EMERGENCY", "DAR", "OTHER", "OTHER"}
		}, "properties.reason", "at most 9"},
		{"exemption", func(t *testing.T, d map[string]any) { props(t, d, 1)["regulationExemption"] = "MAYBE" }, "properties.regulationExemption", "is not one of"},
		{"name not list", func(t *testing.T, d map[string]any) { props(t, d, 0)["name"] = "x" }, "properties.name", "must be a list"},
		{"name entry", func(t *testing.T, d map[string]any) { props(t, d, 0)["name"] = []any{"x"} }, "properties.name[0]", "must be an object"},
		{"name no lang", func(t *testing.T, d map[string]any) { props(t, d, 0)["name"] = []any{map[string]any{"text": "x"}} }, "properties.name[0].lang", "missing"},
		{"long lang", func(t *testing.T, d map[string]any) {
			props(t, d, 0)["name"] = []any{map[string]any{"text": "x", "lang": "en-GB-x"}}
		}, "properties.name[0].lang", "at most 5"},
		{"long text", func(t *testing.T, d map[string]any) {
			props(t, d, 0)["message"] = []any{map[string]any{"text": strings.Repeat("m", 201), "lang": "en"}}
		}, "properties.message[0].text", "at most 200"},
		{"extended not object", func(t *testing.T, d map[string]any) { props(t, d, 0)["extendedProperties"] = []any{} }, "properties.extendedProperties", "must be an object"},
		{"authority not list", func(t *testing.T, d map[string]any) { props(t, d, 0)["zoneAuthority"] = map[string]any{} }, "properties.zoneAuthority", "at least one"},
		{"no authority", func(t *testing.T, d map[string]any) { delete(props(t, d, 0), "zoneAuthority") }, "properties.zoneAuthority", "missing"},
		{"authority entry", func(t *testing.T, d map[string]any) { props(t, d, 0)["zoneAuthority"] = []any{1} }, "properties.zoneAuthority[0]", "must be an object"},
		{"authority purpose", func(t *testing.T, d map[string]any) { props(t, d, 4)["zoneAuthority"] = []any{map[string]any{}} }, "zoneAuthority[0].purpose", "missing"},
		{"long phone", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 0, "properties", "zoneAuthority", 0).(map[string]any)["phone"] = strings.Repeat("1", 201)
		}, "zoneAuthority[0].phone", "at most 200"},
		{"data source", func(t *testing.T, d map[string]any) { props(t, d, 0)["dataSource"] = 1 }, "properties.dataSource", "must be an object"},
		{"originator string", func(t *testing.T, d map[string]any) {
			props(t, d, 0)["dataSource"] = map[string]any{"originator": "Test authority"}
		}, "dataSource.originator", "must be an object"},
		{"date number", func(t *testing.T, d map[string]any) { at(t, d, "metadata").(map[string]any)["issued"] = 1 }, "metadata.issued", "must be an RFC 3339"},
		{"date garbage", func(t *testing.T, d map[string]any) { at(t, d, "metadata").(map[string]any)["issued"] = "yesterday" }, "metadata.issued", "is not an RFC 3339"},
		{"periods not list", func(t *testing.T, d map[string]any) { props(t, d, 1)["limitedApplicability"] = map[string]any{} }, "properties.limitedApplicability", "must be a list"},
		{"period not object", func(t *testing.T, d map[string]any) { props(t, d, 1)["limitedApplicability"] = []any{1} }, "limitedApplicability[0]", "must be an object"},
		{"period permanent", func(t *testing.T, d map[string]any) {
			props(t, d, 1)["limitedApplicability"] = []any{map[string]any{"permanent": "YES"}}
		}, "limitedApplicability[0].permanent", "unknown member"},
		{"end before start", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 1, "properties", "limitedApplicability", 0).(map[string]any)["endDateTime"] = "2026-09-01T00:00:00Z"
		}, "limitedApplicability[0].endDateTime", "not after startDateTime"},
		{"empty schedule", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 1, "properties", "limitedApplicability", 0).(map[string]any)["schedule"] = []any{}
		}, "limitedApplicability[0].schedule", "at least one"},
		{"daily not object", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 1, "properties", "limitedApplicability", 0).(map[string]any)["schedule"] = []any{"x"}
		}, "schedule[0]", "must be an object"},
		{"daily unknown", func(t *testing.T, d map[string]any) { daily(t, d, 1)["offset"] = "+04:00" }, "schedule[0].offset", "unknown member"},
		{"no start", func(t *testing.T, d map[string]any) { delete(daily(t, d, 1), "startEvent") }, "schedule[0].startTime", "give startTime or startEvent"},
		{"bad time", func(t *testing.T, d map[string]any) { daily(t, d, 2)["startTime"] = "08:00+04:00" }, "schedule[0].startTime", "is not an RFC 3339 time"},
		{"time number", func(t *testing.T, d map[string]any) { daily(t, d, 2)["endTime"] = 8 }, "schedule[0].endTime", "is not an RFC 3339 time"},
		{"two offsets", func(t *testing.T, d map[string]any) { daily(t, d, 2)["endTime"] = "18:00:00Z" }, "schedule[0].endTime", "different offset"},
		{"same times", func(t *testing.T, d map[string]any) { daily(t, d, 2)["endTime"] = "08:00:00+04:00" }, "schedule[0].endTime", "same as startTime"},
		{"no days", func(t *testing.T, d map[string]any) { daily(t, d, 2)["day"] = []any{} }, "schedule[0].day", "1 to 7"},
		{"bad day", func(t *testing.T, d map[string]any) { daily(t, d, 2)["day"] = []any{"MONDAY"} }, "schedule[0].day[0]", "is not one of"},
		{"day twice", func(t *testing.T, d map[string]any) { daily(t, d, 2)["day"] = []any{"MON", "MON"} }, "schedule[0].day[1]", "listed twice"},
		{"any with day", func(t *testing.T, d map[string]any) { daily(t, d, 2)["day"] = []any{"MON", "ANY"} }, "schedule[0].day[1]", "stands alone"},
		{"geometry not object", func(t *testing.T, d map[string]any) { at(t, d, "features", 0).(map[string]any)["geometry"] = 1 }, "features[0].geometry", "must be an object"},
		{"geometry no type", func(t *testing.T, d map[string]any) { delete(geom(t, d, 0), "type") }, "features[0].geometry.type", "missing"},
		{"geometry bad type", func(t *testing.T, d map[string]any) { geom(t, d, 0)["type"] = "Square" }, "features[0].geometry.type", "not a GeoJSON geometry type"},
		{"multipolygon", func(t *testing.T, d map[string]any) { geom(t, d, 0)["type"] = "MultiPolygon" }, "features[0].geometry.type", "not supported"},
		{"no rings", func(t *testing.T, d map[string]any) { geom(t, d, 0)["coordinates"] = []any{} }, "geometry.coordinates", "list of rings"},
		{"ring not list", func(t *testing.T, d map[string]any) { geom(t, d, 0)["coordinates"] = []any{1} }, "geometry.coordinates[0]", "list of positions"},
		{"open ring", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["coordinates"] = []any{[]any{[]any{44.7, 41.65}, []any{44.95, 41.65}, []any{44.95, 41.8}, []any{44.7, 41.8}}}
		}, "geometry.coordinates[0]", "not closed"},
		{"short ring", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["coordinates"] = []any{[]any{[]any{44.7, 41.65}, []any{44.95, 41.65}, []any{44.7, 41.65}}}
		}, "geometry.coordinates[0]", "at least four"},
		{"flat ring", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["coordinates"] = []any{[]any{[]any{44.7, 41.65}, []any{44.95, 41.65}, []any{44.7, 41.65}, []any{44.7, 41.65}}}
		}, "geometry.coordinates[0]", "three distinct"},
		{"position with altitude", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 0, "geometry", "coordinates", 0).([]any)[1] = []any{44.95, 41.65, 100}
		}, "coordinates[0][1]", "[longitude, latitude]"},
		{"position text", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 0, "geometry", "coordinates", 0).([]any)[1] = []any{"44.95", 41.65}
		}, "coordinates[0][1]", "two numbers"},
		{"position range", func(t *testing.T, d map[string]any) {
			at(t, d, "features", 0, "geometry", "coordinates", 0).([]any)[1] = []any{44.95, 91}
		}, "coordinates[0][1]", "outside longitude and latitude"},
		{"wide polygon", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["coordinates"] = []any{[]any{[]any{-170, 0}, []any{170, 0}, []any{170, 1}, []any{-170, 0}}}
		}, "features[0].geometry", "antimeridian"},
		{"no extent", func(t *testing.T, d map[string]any) { delete(geom(t, d, 1), "extent") }, "geometry.extent", "needs an extent"},
		{"extent subtype", func(t *testing.T, d map[string]any) { geom(t, d, 1)["extent"].(map[string]any)["subType"] = "Ellipse" }, "extent.subType", "must be 'Circle'"},
		{"zero radius", func(t *testing.T, d map[string]any) { geom(t, d, 1)["extent"].(map[string]any)["radius"] = 0 }, "extent.radius", "above 0"},
		{"circle past 180", func(t *testing.T, d map[string]any) { geom(t, d, 1)["coordinates"] = []any{179.99, 0} }, "features[1].geometry", "antimeridian"},
		{"circle at pole", func(t *testing.T, d map[string]any) { geom(t, d, 1)["coordinates"] = []any{0, 90} }, "features[1].geometry", "reaches past"},
		{"no geometries", func(t *testing.T, d map[string]any) { geom(t, d, 3)["geometries"] = []any{} }, "geometry.geometries", "at least one"},
		{"nested collection", func(t *testing.T, d map[string]any) {
			geom(t, d, 3)["geometries"] = []any{map[string]any{"type": "GeometryCollection", "geometries": []any{}}}
		}, "geometries[0].type", "inside a GeometryCollection"},
		{"layer twice", func(t *testing.T, d map[string]any) {
			geom(t, d, 3)["layer"] = map[string]any{"upper": 10, "upperReference": "AGL"}
		}, "geometries[0].layer", "give the layer once"},
		{"layer not object", func(t *testing.T, d map[string]any) { geom(t, d, 0)["layer"] = 1 }, "geometry.layer", "must be an object"},
		{"layer uom", func(t *testing.T, d map[string]any) { geom(t, d, 0)["layer"].(map[string]any)["uom"] = "M" }, "layer.uom", "is not one of m, ft"},
		{"layer reference", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["layer"].(map[string]any)["lowerReference"] = "SFC"
		}, "layer.lowerReference", "is not one of"},
		{"layer value text", func(t *testing.T, d map[string]any) { geom(t, d, 0)["layer"].(map[string]any)["upper"] = "300" }, "layer.upper", "finite number"},
		{"lower above upper", func(t *testing.T, d map[string]any) {
			l := geom(t, d, 3)["geometries"].([]any)[0].(map[string]any)["layer"].(map[string]any)
			l["lower"] = 60
		}, "layer.upper", "not above lower"},
		{"surface to zero", func(t *testing.T, d map[string]any) {
			geom(t, d, 0)["layer"] = map[string]any{"upper": 0, "upperReference": "AGL"}
		}, "layer.upper", "is 0 with no lower"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := baseDocument(t)
			c.mut(t, d)
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			fc, probs := Parse(raw, Limits{})
			if fc != nil {
				t.Fatal("accepted")
			}
			for _, p := range probs.List {
				if strings.HasSuffix(p.Field, c.field) && strings.Contains(p.Reason, c.reason) {
					return
				}
			}
			t.Errorf("no problem ending %q containing %q in %v", c.field, c.reason, probs)
		})
	}
}

// What the base holds, read field by field (the presence side of the
// refusals above).
func TestParseReadsEveryProperty(t *testing.T) {
	fc, probs := Parse(baseBytes(t), Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	if len(fc.Features) != 5 || *fc.Name != "Test zones around Tbilisi (ED-318)" {
		t.Fatalf("%d features, name %v", len(fc.Features), fc.Name)
	}
	m := fc.Metadata
	if m.Issued.Text != "2026-09-30T12:00:00.5Z" || m.Issued.Time.Nanosecond() != 500000000 || len(m.Provider) != 2 || *m.OtherGeoid == "" {
		t.Errorf("metadata %+v", m)
	}
	u := fc.Features[0].Properties
	if u.Type != core.ZoneUSpace || len(u.Name) != 2 || u.Name[1].Lang != "ka-GE" || u.DataSource.Originator == nil {
		t.Errorf("TSU001 %+v", u)
	}
	if string(u.ExtendedProperties["service_performance"]) != `{"cis_latency_s":5,"nid_update_hz":1,"ti_update_hz":1}` {
		t.Errorf("Art. 3(4) block kept as %s", u.ExtendedProperties["service_performance"])
	}
	d := fc.Features[1]
	if string(d.ID) != "2" || *d.Geometry.RadiusM != 1500 || *d.Geometry.Layer.Uom != UomFeet || *d.Properties.Region != 1 {
		t.Errorf("TSD001 %+v", d)
	}
	if up := d.Geometry.Layer.UpperM(); up == nil || *up != 2500*core.FeetToMetres {
		t.Errorf("2500 ft in metres: %v", up)
	}
	if d.Properties.DataSource.CreationDate == nil || d.Properties.DataSource.CreationDateTime != nil {
		t.Error("creationDate (the schema's spelling) not read into its own field")
	}
	if s := d.Properties.LimitedApplicability[0].Schedule[0]; *s.StartEvent != EventBMCT || *s.EndEvent != EventEECT || s.StartTime != nil {
		t.Errorf("daylight schedule %+v", s)
	}
	if r := fc.Features[2].Geometry; len(r.Rings) != 2 || r.Layer.UpperReference != core.RefWGS84 {
		t.Errorf("TSR001 geometry %+v", r)
	}
	if c := fc.Features[3].Geometry; len(c.Geometries) != 2 || len(c.parts()) != 2 {
		t.Errorf("TSC001 layers %+v", c)
	}
	n := fc.Features[4].Geometry.Layer
	if n.Upper != nil || n.Lower != nil || n.UpperReference != core.RefAMSL || n.LowerM() != nil || n.UpperM() != nil {
		t.Errorf("TSN001 references without limits %+v", n)
	}
}

// A null optional member is absent, read and written (as ed269 does).
func TestNullOptionalIsAbsent(t *testing.T) {
	d := baseDocument(t)
	props(t, d, 1)["restrictionConditions"] = nil
	props(t, d, 1)["message"] = nil
	raw, _ := json.Marshal(d)
	fc, probs := Parse(raw, Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	if fc.Features[1].Properties.RestrictionConditions != nil || fc.Features[1].Properties.Message != nil {
		t.Error("null read as a value")
	}
	out, err := Export(fc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(`"restrictionConditions":null`)) || bytes.Contains(out, []byte(`Emergency services only`)) {
		t.Errorf("null written back: %s", out)
	}
}

// Members the schema allows but this package does not interpret are kept
// at every level they may appear, and a byte order mark is accepted.
func TestExtrasKept(t *testing.T) {
	d := baseDocument(t)
	d["x_collection"] = map[string]any{"a": 1}
	at(t, d, "features", 0).(map[string]any)["x_feature"] = "f"
	at(t, d, "features", 0).(map[string]any)["bbox"] = []any{44.7, 41.65, 44.95, 41.8}
	geom(t, d, 0)["x_geom"] = []any{1}
	geom(t, d, 0)["bbox"] = []any{44.7, 41.65, 44.95, 41.8}
	geom(t, d, 0)["layer"].(map[string]any)["x_layer"] = true
	geom(t, d, 1)["extent"].(map[string]any)["x_extent"] = 2
	at(t, d, "metadata").(map[string]any)["technicalLimitation"] = "singular, as the Swiss sample spells it"
	at(t, d, "features", 0, "properties", "zoneAuthority", 0).(map[string]any)["x_auth"] = "a"
	at(t, d, "features", 0, "properties", "name", 0).(map[string]any)["x_text"] = "t"
	at(t, d, "features", 0, "properties", "dataSource").(map[string]any)["x_src"] = "s"
	raw, _ := json.Marshal(d)
	raw = append([]byte{0xEF, 0xBB, 0xBF}, raw...)
	fc, probs := Parse(raw, Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	out, err := Export(fc)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, out, raw[3:]) {
		t.Errorf("extras lost:\n%s", out)
	}
}

// Bounds are exceeded by a test each (E-10), with the input just inside
// each bound accepted.
func TestParseBounds(t *testing.T) {
	raw := baseBytes(t)
	if _, probs := Parse(raw, Limits{MaxBytes: len(raw)}); probs != nil {
		t.Errorf("at MaxBytes: %v", probs)
	}
	if _, probs := Parse(raw, Limits{MaxBytes: len(raw) - 1}); !hasProblem(probs, "$", "at most") {
		t.Errorf("over MaxBytes: %v", probs)
	}
	deep := strings.Repeat("[", 40) + strings.Repeat("]", 40)
	if _, probs := Parse([]byte(deep), Limits{}); !hasProblem(probs, "$", "nested too deeply") {
		t.Errorf("deep input: %v", probs)
	}
	if _, probs := Parse([]byte(strings.Repeat("[", 100000)), Limits{}); !hasProblem(probs, "$", "nested too deeply") {
		t.Errorf("a hundred thousand brackets: %v", probs)
	}
	// MaxProblems: 150 bad features report 100 and count the rest.
	d := baseDocument(t)
	var many []any
	for range 150 {
		many = append(many, 1)
	}
	d["features"] = many
	b, _ := json.Marshal(d)
	_, probs := Parse(b, Limits{})
	if probs == nil || len(probs.List) != 100 || probs.Truncated != 50 || !strings.Contains(probs.Error(), "and 50 more") {
		t.Errorf("150 problems: %d listed, %d truncated", len(probs.List), probs.Truncated)
	}
	// MaxRingVertices: a ring of n positions is accepted at the bound.
	ring := func(n int) []any {
		pts := make([]any, 0, n)
		for i := range n - 1 {
			pts = append(pts, []any{44.7 + float64(i)*1e-6, 41.65 + float64(i%2)*1e-6})
		}
		return append(pts, pts[0])
	}
	for _, c := range []struct {
		n  int
		ok bool
	}{{50, true}, {51, false}} {
		d := baseDocument(t)
		geom(t, d, 0)["coordinates"] = []any{ring(c.n)}
		b, _ := json.Marshal(d)
		_, probs := Parse(b, Limits{MaxRingVertices: 50})
		if (probs == nil) != c.ok {
			t.Errorf("ring of %d with a bound of 50: %v", c.n, probs)
		}
	}
}

func TestParseNotJSON(t *testing.T) {
	for name, c := range map[string]struct {
		in     string
		reason string
	}{
		"empty":     {"", "unexpected end"},
		"truncated": {`{"type":`, "unexpected end"},
		"garbage":   {`{"type" "x"}`, "not JSON"},
		"trailing":  {`{} {}`, "data after"},
		"array":     {`[]`, "not a JSON object"},
		"not utf-8": {"{\"name\": \"\xff\"}", "not UTF-8"},
		"repeated":  {`{"type":"FeatureCollection","type":"FeatureCollection","features":[]}`, "repeated member"},
	} {
		_, probs := Parse([]byte(c.in), Limits{})
		if probs == nil || !strings.Contains(probs.Error(), c.reason) {
			t.Errorf("%s: %v", name, probs)
		}
	}
	// Repeated names in a large object are found too (the map path).
	var b strings.Builder
	b.WriteString(`{"type":"FeatureCollection","features":[]`)
	for i := range 10 {
		fmt.Fprintf(&b, `,"x%d":1`, i)
	}
	b.WriteString(`,"x3":2}`)
	if _, probs := Parse([]byte(b.String()), Limits{}); !hasProblem(probs, "x3", "repeated member") {
		t.Errorf("repeated name in a large object: %v", probs)
	}
	// An empty collection is a collection.
	fc, probs := Parse([]byte(`{"type":"FeatureCollection","features":[]}`), Limits{})
	if probs != nil || len(fc.Features) != 0 {
		t.Errorf("empty collection: %v", probs)
	}
}

// A reason quoting a hostile value stays short.
func TestReasonsAreClipped(t *testing.T) {
	d := baseDocument(t)
	props(t, d, 0)["type"] = strings.Repeat("Z", 10000)
	geom(t, d, 0)["layer"].(map[string]any)["upper"] = map[string]any{"k": strings.Repeat("v", 10000)}
	b, _ := json.Marshal(d)
	_, probs := Parse(b, Limits{})
	if probs == nil {
		t.Fatal("accepted")
	}
	for _, p := range probs.List {
		if len(p.Reason) > 400 {
			t.Errorf("%s: a reason of %d bytes", p.Field, len(p.Reason))
		}
	}
}

func TestExportRefusesWhatParseCannotProduce(t *testing.T) {
	if _, err := Export(nil); err == nil {
		t.Error("nil exported")
	}
	fc, probs := Parse(baseBytes(t), Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	for name, mut := range map[string]func(*FeatureCollection){
		"geometry type": func(fc *FeatureCollection) { fc.Features[0].Geometry.Type = "LineString" },
		"no centre":     func(fc *FeatureCollection) { fc.Features[1].Geometry.Center = nil },
		"member type":   func(fc *FeatureCollection) { fc.Features[3].Geometry.Geometries[1].Type = "Line" },
		"bad extra":     func(fc *FeatureCollection) { fc.Extra = map[string]json.RawMessage{"x": json.RawMessage("{")} },
		"nan":           func(fc *FeatureCollection) { fc.BBox = []float64{0, 0, 0, math.NaN()} },
	} {
		c, _ := Parse(baseBytes(t), Limits{})
		mut(c)
		var fe *core.FieldError
		if _, err := Export(c); err == nil || !errors.As(err, &fe) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A DateTime built in code is written in RFC 3339.
	fc.Metadata.Issued = &DateTime{Time: fc.Metadata.Issued.Time}
	out, err := Export(fc)
	if err != nil || !bytes.Contains(out, []byte(`"issued":"2026-09-30T12:00:00.5Z"`)) {
		t.Errorf("built DateTime: %v %s", err, out)
	}
}
