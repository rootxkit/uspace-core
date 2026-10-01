package ed318

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/vectors"
)

// validED269 is the document of ed269_parse.json's
// valid-file-round-trips-unchanged: TST001 to TST004.
func validED269(t *testing.T) *ed269.Document {
	t.Helper()
	f := vectors.Load(t, "ed269_parse.json")
	for _, c := range f.Cases {
		if c.Name != "valid-file-round-trips-unchanged" {
			continue
		}
		var in struct {
			Document json.RawMessage `json:"document"`
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			t.Fatal(err)
		}
		doc, probs := ed269.Parse(in.Document, ed269.Limits{})
		if probs != nil {
			t.Fatal(probs)
		}
		return doc
	}
	t.Fatal("ed269_parse.json has no valid-file-round-trips-unchanged case")
	return nil
}

// only is doc with the zones named, in order.
func only(doc *ed269.Document, ids ...string) *ed269.Document {
	out := *doc
	out.Zones = nil
	for _, id := range ids {
		for i := range doc.Zones {
			if doc.Zones[i].Identifier == id {
				out.Zones = append(out.Zones, doc.Zones[i])
			}
		}
	}
	return &out
}

// sameED269 compares two ED-269 documents by value: every published field
// equal, extendedProperties as JSON values, periods by their instants and
// clock windows (ED-318 writes clock times in RFC 3339, so 08:00+04:00
// returns as 08:00:00+04:00: the same time).
func sameED269(t *testing.T, got, want *ed269.Document) {
	t.Helper()
	if !reflect.DeepEqual(got.Title, want.Title) || !reflect.DeepEqual(got.Description, want.Description) {
		t.Errorf("title or description: got %v %v, want %v %v", got.Title, got.Description, want.Title, want.Description)
	}
	if len(got.Zones) != len(want.Zones) {
		t.Fatalf("%d zones, want %d", len(got.Zones), len(want.Zones))
	}
	for i := range want.Zones {
		g, w := got.Zones[i], want.Zones[i]
		if !jsonEqual(g.ExtendedProperties, w.ExtendedProperties) {
			t.Errorf("%s extendedProperties: %s, want %s", w.Identifier, g.ExtendedProperties, w.ExtendedProperties)
		}
		if !samePeriods(g.Applicability, w.Applicability) {
			t.Errorf("%s applicability: %+v, want %+v", w.Identifier, g.Applicability, w.Applicability)
		}
		g.ExtendedProperties, w.ExtendedProperties = nil, nil
		g.Applicability, w.Applicability = nil, nil
		if !reflect.DeepEqual(g, w) {
			t.Errorf("zone %s:\n got %+v\nwant %+v", w.Identifier, g, w)
		}
	}
}

func jsonEqual(a, b json.RawMessage) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

func samePeriods(a, b []ed269.Period) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		p, q := a[i], b[i]
		if p.Permanent != q.Permanent || !sameInstant(p.Start, q.Start) || !sameInstant(p.End, q.End) || len(p.Schedule) != len(q.Schedule) {
			return false
		}
		for k := range p.Schedule {
			d, e := p.Schedule[k], q.Schedule[k]
			if !reflect.DeepEqual(d.Days, e.Days) || !d.Start.Equal(e.Start) || !d.End.Equal(e.End) || d.Offset.String() != e.Offset.String() {
				return false
			}
		}
	}
	return true
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// ed269 -> ED-318 -> ed269 is the identity on the mappable zones of the
// valid ED-269 fixture: a polygon with a hole and a permanent period
// (TST001), a circle in feet AMSL with a list of restriction conditions,
// uSpaceClass and extendedProperties (TST002), and a circle with an ANY
// schedule in +04:00 and references without limits (TST004). The ED-318
// form is itself accepted by Parse after Export.
func TestED269MapsBothWays(t *testing.T) {
	doc := only(validED269(t), "TST001", "TST002", "TST004")
	fc, err := FromED269(doc, Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Export(fc)
	if err != nil {
		t.Fatal(err)
	}
	reread, probs := Parse(raw, Limits{})
	if probs != nil {
		t.Fatalf("the mapped ED-318 is refused: %v\n%s", probs, raw)
	}
	back, err := ToED269(reread)
	if err != nil {
		t.Fatal(err)
	}
	sameED269(t, back, doc)

	// What the ED-318 form says, field by field.
	z2 := reread.Features[1]
	if z2.Properties.Type != core.ZoneReqAuthorization {
		t.Errorf("REQ_AUTHORISATION became %q", z2.Properties.Type)
	}
	if z2.Geometry.Type != GeometryPoint || *z2.Geometry.RadiusM != 1640*core.FeetToMetres || *z2.Geometry.Layer.Uom != UomFeet {
		t.Errorf("TST002 circle: %+v", z2.Geometry)
	}
	if string(z2.Properties.ExtendedProperties[ED269Key]) != `{"restrictionConditions":["Notify 24 h before"],"uSpaceClass":"EUROCONTROL"}` {
		t.Errorf("kept ED-269 fields: %s", z2.Properties.ExtendedProperties[ED269Key])
	}
	if reread.Features[0].Properties.LimitedApplicability != nil {
		t.Error("a single permanent period became a limitedApplicability")
	}
	if s := *reread.Features[2].Properties.LimitedApplicability[0].Schedule[0].StartTime; s != "08:00:00+04:00" {
		t.Errorf("08:00+04:00 became %q", s)
	}
}

// TST003 has no zone authority, which ED-318 requires: refused, not given
// one (E-01 twin of the identity above). With an authority it maps both
// ways too, its title and night window included.
func TestED269WithoutAuthorityRefused(t *testing.T) {
	doc := only(validED269(t), "TST003")
	_, err := FromED269(doc, Metadata{}, "en-GB")
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "features[0].zoneAuthority" || !strings.Contains(fe.Reason, "requires at least one") {
		t.Fatalf("no authority: %v", err)
	}
	purpose := ed269.PurposeInformation
	doc.Zones[0].ZoneAuthority = []ed269.Authority{{Purpose: &purpose}}
	fc, err := FromED269(doc, Metadata{}, "ka-GE")
	if err != nil {
		t.Fatal(err)
	}
	back, err := ToED269(fc)
	if err != nil {
		t.Fatal(err)
	}
	sameED269(t, back, doc)
}

func TestFromED269Refusals(t *testing.T) {
	base := only(validED269(t), "TST001")
	mutate := func(f func(z *ed269.GeoZone)) *ed269.Document {
		d := *base
		d.Zones = []ed269.GeoZone{base.Zones[0]}
		f(&d.Zones[0])
		return &d
	}
	cases := map[string]struct {
		doc   *ed269.Document
		lang  string
		field string
	}{
		"foreign territory": {mutate(func(z *ed269.GeoZone) { z.Reason = []ed269.Reason{ed269.ReasonForeignTerritory} }), "en", "features[0].reason[0]"},
		"no purpose":        {mutate(func(z *ed269.GeoZone) { z.ZoneAuthority = []ed269.Authority{{}} }), "en", "features[0].zoneAuthority[0].purpose"},
		"extended string":   {mutate(func(z *ed269.GeoZone) { z.ExtendedProperties = json.RawMessage(`"x"`) }), "en", "features[0].extendedProperties"},
		"extended clash":    {mutate(func(z *ed269.GeoZone) { z.ExtendedProperties = json.RawMessage(`{"ed269":1}`) }), "en", "features[0].extendedProperties.ed269"},
		"bad restriction":   {mutate(func(z *ed269.GeoZone) { z.Restriction = "REQ_AUTHORIZATION" }), "en", "features[0].restriction"},
		"two volumes":       {mutate(func(z *ed269.GeoZone) { z.Geometry = append(z.Geometry, z.Geometry[0]) }), "en", "features[0].geometry"},
		"no lang":           {base, "", "lang"},
		"long lang":         {base, "en-GB-x", "lang"},
		"no document":       {nil, "en", "$"},
	}
	for name, c := range cases {
		_, err := FromED269(c.doc, Metadata{}, c.lang)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
	// E-01: the base zone maps, with the metadata given.
	issued := DateTime{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Text: "2026-10-01T00:00:00Z"}
	fc, err := FromED269(base, Metadata{Issued: &issued}, "en")
	if err != nil || fc.Metadata == nil || fc.Metadata.Issued.Text != issued.Text {
		t.Errorf("base zone: %v, %+v", err, fc)
	}
}

// What ED-269 cannot express is refused by ToED269, each beside the
// mappable collection it differs from.
func TestToED269Refusals(t *testing.T) {
	doc := only(validED269(t), "TST001", "TST002")
	good, err := FromED269(doc, Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ToED269(good); err != nil {
		t.Fatalf("the mappable collection: %v", err)
	}
	event := EventSR
	two := "second"
	cases := map[string]struct {
		mut   func(fc *FeatureCollection)
		field string
	}{
		"uspace": {func(fc *FeatureCollection) { fc.Features[0].Properties.Type = core.ZoneUSpace }, "features[0].properties.type"},
		"dar":    {func(fc *FeatureCollection) { fc.Features[0].Properties.Reason = []string{"DAR"} }, "features[0].properties.reason[0]"},
		"two names": {func(fc *FeatureCollection) {
			fc.Features[0].Properties.Name = append(fc.Features[0].Properties.Name, Text{Text: &two, Lang: "ka"})
		}, "features[0].properties.name"},
		"lang only": {func(fc *FeatureCollection) { fc.Features[0].Properties.Message = []Text{{Lang: "en"}} }, "features[0].properties.message[0].text"},
		"start event": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.LimitedApplicability[0].Schedule = []DailyPeriod{{Day: []string{"ANY"}, StartEvent: &event, EndEvent: &event}}
		}, "features[1].properties.limitedApplicability[0].schedule[0].startEvent"},
		"end event": {func(fc *FeatureCollection) {
			s := "08:00:00Z"
			fc.Features[1].Properties.LimitedApplicability[0].Schedule = []DailyPeriod{{Day: []string{"ANY"}, StartTime: &s, EndEvent: &event}}
		}, "features[1].properties.limitedApplicability[0].schedule[0].endEvent"},
		"two layers": {func(fc *FeatureCollection) {
			g := fc.Features[0].Geometry
			fc.Features[0].Geometry = Geometry{Type: GeometryCollection, Geometries: []Geometry{g, g}}
		}, "features[0].geometry.geometries"},
		"no layer": {func(fc *FeatureCollection) { fc.Features[0].Geometry.Layer = nil }, "features[0].geometry.layer"},
		"bad kept field": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.ExtendedProperties[ED269Key] = json.RawMessage(`{"other":1}`)
		}, "features[1].properties.extendedProperties.ed269.other"},
		"kept not object": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.ExtendedProperties[ED269Key] = json.RawMessage(`[]`)
		}, "features[1].properties.extendedProperties.ed269"},
		"bad uSpaceClass": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.ExtendedProperties[ED269Key] = json.RawMessage(`{"uSpaceClass":1}`)
		}, "features[1].properties.extendedProperties.ed269.uSpaceClass"},
		"bad list": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.ExtendedProperties[ED269Key] = json.RawMessage(`{"restrictionConditions":"x"}`)
		}, "features[1].properties.extendedProperties.ed269.restrictionConditions"},
		"negative limit": {func(fc *FeatureCollection) {
			v := -5.0
			fc.Features[0].Geometry.Layer.Lower = &v
		}, "features[0].geometry[0].lowerLimit"},
	}
	for name, c := range cases {
		fc, err := FromED269(doc, Metadata{}, "en-GB")
		if err != nil {
			t.Fatal(err)
		}
		c.mut(fc)
		_, err = ToED269(fc)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
	if _, err := ToED269(nil); err == nil {
		t.Error("a nil collection mapped")
	}
}

// An edited restrictionConditions text wins over the kept ED-269 list,
// which no longer stands for it.
func TestToED269EditedConditionsWin(t *testing.T) {
	fc, err := FromED269(only(validED269(t), "TST002"), Metadata{}, "en")
	if err != nil {
		t.Fatal(err)
	}
	edited := "Notify 48 h before"
	fc.Features[0].Properties.RestrictionConditions = &edited
	doc, err := ToED269(fc)
	if err != nil {
		t.Fatal(err)
	}
	z := doc.Zones[0]
	if !z.RestrictionConditionsIsText || len(z.RestrictionConditions) != 1 || z.RestrictionConditions[0] != edited {
		t.Errorf("conditions %v (text %v)", z.RestrictionConditions, z.RestrictionConditionsIsText)
	}
}

func TestShortestFeet(t *testing.T) {
	for _, ft := range []float64{1640, 3500, 250.5, 1, 12345.678, 0.1} {
		if got := shortestFeet(ft * core.FeetToMetres); got != ft {
			t.Errorf("%v ft -> %v m -> %v ft", ft, ft*core.FeetToMetres, got)
		}
	}
}
