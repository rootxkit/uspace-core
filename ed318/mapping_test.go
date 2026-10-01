package ed318

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
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
	back, err := ToED269(reread, "en-GB")
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
	if string(z2.Properties.ExtendedProperties[ED269Key]) != `{"radius":{"value":1640,"uom":"FT"},"restrictionConditions":["Notify 24 h before"],"uSpaceClass":"EUROCONTROL"}` {
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
	back, err := ToED269(fc, "")
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
	if _, err := ToED269(good, ""); err != nil {
		t.Fatalf("the mappable collection: %v", err)
	}
	event := EventSR
	cases := map[string]struct {
		mut   func(fc *FeatureCollection)
		field string
	}{
		"uspace":    {func(fc *FeatureCollection) { fc.Features[0].Properties.Type = core.ZoneUSpace }, "features[0].properties.type"},
		"dar":       {func(fc *FeatureCollection) { fc.Features[0].Properties.Reason = []string{"DAR"} }, "features[0].properties.reason[0]"},
		"lang only": {func(fc *FeatureCollection) { fc.Features[0].Properties.Message = []Text{{Lang: "en"}} }, "features[0].properties.message"},
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
		_, err = ToED269(fc, "")
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
	if _, err := ToED269(nil, ""); err == nil {
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
	doc, err := ToED269(fc, "")
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

func txt(s, lang string) Text { return Text{Text: &s, Lang: lang} }

// ToED269 writes the text in the preferred language, else English, else
// the first given.
func TestToED269PicksALanguage(t *testing.T) {
	for _, c := range []struct {
		names []Text
		lang  string
		want  string
	}{
		{[]Text{txt("ka", "ka-GE"), txt("en", "en-GB"), txt("fr", "fr-FR")}, "ka-GE", "ka"},
		{[]Text{txt("ka", "ka-GE"), txt("en", "en-GB"), txt("fr", "fr-FR")}, "FR-fr", "fr"},
		{[]Text{txt("ka", "ka-GE"), txt("en", "en-GB"), txt("fr", "fr-FR")}, "de", "en"},
		{[]Text{txt("ka", "ka-GE"), txt("en", "en"), txt("fr", "fr-FR")}, "", "en"},
		{[]Text{txt("ka", "ka-GE"), txt("fr", "fr-FR")}, "de", "ka"},
		{[]Text{{Lang: "de"}, txt("fr", "fr-FR")}, "de", "fr"},
	} {
		got := pickText(c.names, c.lang)
		if got == nil || *got != c.want {
			t.Errorf("%v in %q: %v, want %q", c.names, c.lang, got, c.want)
		}
	}
	if pickText([]Text{{Lang: "de"}}, "de") != nil {
		t.Error("a text picked from an entry without one")
	}
}

// Texts in several languages map to ED-269 without being refused and
// come back whole: the other languages travel in the ED-269 zone's
// extendedProperties.ed269.texts.
func TestToED269CarriesOtherLanguages(t *testing.T) {
	fc, err := FromED269(only(validED269(t), "TST001"), Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	u := &fc.Features[0].Properties
	u.Name = []Text{txt("სატესტო", "ka-GE"), txt("Test prohibited square with a hole", "en-GB")}
	u.ZoneAuthority[0].Name = []Text{txt("Test authority", "en-GB"), txt("უწყება", "ka-GE")}
	doc, err := ToED269(fc, "en-GB")
	if err != nil {
		t.Fatalf("several languages refused: %v", err)
	}
	z := doc.Zones[0]
	if *z.Name != "Test prohibited square with a hole" || *z.ZoneAuthority[0].Name != "Test authority" {
		t.Errorf("chosen texts %q, %q", *z.Name, *z.ZoneAuthority[0].Name)
	}
	if !strings.Contains(string(z.ExtendedProperties), `"ed269":{"texts":{`) || !strings.Contains(string(z.ExtendedProperties), "სატესტო") {
		t.Errorf("carried texts: %s", z.ExtendedProperties)
	}
	// Through ED-269 JSON and back: every language returns, and the
	// carrier does not stay in extendedProperties.
	raw, err := ed269.Export(doc)
	if err != nil {
		t.Fatal(err)
	}
	reread, probs := ed269.Parse(raw, ed269.Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	back, err := FromED269(reread, Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	b := back.Features[0].Properties
	if !reflect.DeepEqual(b.Name, u.Name) || !reflect.DeepEqual(b.ZoneAuthority[0].Name, u.ZoneAuthority[0].Name) {
		t.Errorf("languages lost: %+v / %+v", b.Name, b.ZoneAuthority[0].Name)
	}
	if b.ExtendedProperties != nil {
		t.Errorf("the carrier stayed: %v", b.ExtendedProperties)
	}
	// E-01: one language carries nothing.
	one, err := FromED269(only(validED269(t), "TST001"), Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	single, err := ToED269(one, "")
	if err != nil {
		t.Fatal(err)
	}
	if single.Zones[0].ExtendedProperties != nil {
		t.Errorf("a single language was carried: %s", single.Zones[0].ExtendedProperties)
	}
}

// A carried list that no longer holds the ED-269 text, carries a member
// the zone does not have, or is not what ToED269 writes, is refused.
func TestFromED269CarriedTextsRefusals(t *testing.T) {
	base := only(validED269(t), "TST001")
	cases := map[string]struct {
		ext   string
		field string
	}{
		"edited text":     {`{"ed269":{"texts":{"name":[{"text":"old","lang":"en"},{"text":"ძველი","lang":"ka"}]}}}`, "features[0].extendedProperties.ed269.texts.name"},
		"duplicate lang":  {`{"ed269":{"texts":{"name":[{"text":"Test prohibited square with a hole","lang":"en"},{"text":"y","lang":"EN"}]}}}`, "features[0].extendedProperties.ed269.texts.name[1].lang"},
		"long lang":       {`{"ed269":{"texts":{"name":[{"text":"Test prohibited square with a hole","lang":"en"},{"text":"y","lang":"en-GB-x"}]}}}`, "features[0].extendedProperties.ed269.texts.name[1].lang"},
		"long text":       {`{"ed269":{"texts":{"name":[{"text":"Test prohibited square with a hole","lang":"en"},{"text":"` + strings.Repeat("y", 201) + `","lang":"ka"}]}}}`, "features[0].extendedProperties.ed269.texts.name[1].text"},
		"empty list":      {`{"ed269":{"texts":{"name":[]}}}`, "features[0].extendedProperties.ed269.texts.name"},
		"absent member":   {`{"ed269":{"texts":{"zoneAuthority[3].name":[{"text":"x","lang":"en"}]}}}`, "features[0].extendedProperties.ed269.texts.zoneAuthority[3].name"},
		"other member":    {`{"ed269":{"names":{}}}`, "features[0].extendedProperties.ed269.names"},
		"texts not lists": {`{"ed269":{"texts":[]}}`, "features[0].extendedProperties.ed269.texts"},
		"no lang":         {`{"ed269":{"texts":{"name":[{"text":"x"}]}}}`, "features[0].extendedProperties.ed269.texts.name[0].lang"},
		"not an object":   {`{"ed269":1}`, "features[0].extendedProperties.ed269"},
	}
	for name, c := range cases {
		d := *base
		d.Zones = []ed269.GeoZone{base.Zones[0]}
		d.Zones[0].ExtendedProperties = json.RawMessage(c.ext)
		_, err := FromED269(&d, Metadata{}, "en")
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
}

// An FT circle round-trips exactly through ED-318: TST002 (1640 ft) and a
// radius whose metres do not divide back to it (3500 ft), each written
// back in the feet published, carried in extendedProperties.ed269.radius;
// the radius in ED-318 is metres under the feet layer (E-01 twin: an
// edited ED-318 radius is written as the shortest feet that convert to
// it, not as the stale published value).
func TestFTCircleRoundTripsExactly(t *testing.T) {
	for _, ft := range []float64{1640, 3500} {
		doc := only(validED269(t), "TST002")
		r := ft
		doc.Zones[0].Geometry[0].Projection.Radius = &r
		fc, err := FromED269(doc, Metadata{}, "en-GB")
		if err != nil {
			t.Fatal(err)
		}
		g := fc.Features[0].Geometry
		if *g.RadiusM != ft*core.FeetToMetres || *g.Layer.Uom != UomFeet {
			t.Errorf("%v ft: radius %v m under %s", ft, *g.RadiusM, *g.Layer.Uom)
		}
		raw, err := Export(fc)
		if err != nil {
			t.Fatal(err)
		}
		back, err := ToED269(parseOrFail(t, raw), "en-GB")
		if err != nil {
			t.Fatal(err)
		}
		if got := *back.Zones[0].Geometry[0].Projection.Radius; got != ft {
			t.Errorf("%v ft came back as %v", ft, got)
		}
		sameED269(t, back, doc)
	}
	fc, err := FromED269(only(validED269(t), "TST002"), Metadata{}, "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	edited := 600.0
	fc.Features[0].Geometry.RadiusM = &edited
	back, err := ToED269(fc, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := *back.Zones[0].Geometry[0].Projection.Radius; got*core.FeetToMetres != edited || got == 1640 {
		t.Errorf("an edited radius came back as %v ft", got)
	}
}

// A carried radius that is not {value, uom: FT} with a positive value is
// refused.
func TestKeptRadiusRefused(t *testing.T) {
	for _, bad := range []string{`{"value":1640,"uom":"M"}`, `{"value":-1,"uom":"FT"}`, `"1640"`} {
		fc, err := FromED269(only(validED269(t), "TST002"), Metadata{}, "en-GB")
		if err != nil {
			t.Fatal(err)
		}
		fc.Features[0].Properties.ExtendedProperties[ED269Key] = json.RawMessage(`{"radius":` + bad + `}`)
		_, err = ToED269(fc, "")
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != "features[0].properties.extendedProperties.ed269.radius" {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// The carried-text limits match Parse's: at MaxTexts entries a list is
// restored and the result parses; past it, it is refused.
func TestCarriedTextsAtTheBound(t *testing.T) {
	base := only(validED269(t), "TST001")
	list := func(n int) string {
		var es []string
		es = append(es, `{"text":"Test prohibited square with a hole","lang":"en"}`)
		for i := 1; i < n; i++ {
			es = append(es, fmt.Sprintf(`{"text":"t%d","lang":"x%d"}`, i, i))
		}
		return `{"ed269":{"texts":{"name":[` + strings.Join(es, ",") + `]}}}`
	}
	for _, c := range []struct {
		n  int
		ok bool
	}{{MaxTexts, true}, {MaxTexts + 1, false}} {
		d := *base
		d.Zones = []ed269.GeoZone{base.Zones[0]}
		d.Zones[0].ExtendedProperties = json.RawMessage(list(c.n))
		fc, err := FromED269(&d, Metadata{}, "en")
		if (err == nil) != c.ok {
			t.Errorf("%d entries: %v", c.n, err)
		}
		if err == nil && len(fc.Features[0].Properties.Name) != c.n {
			t.Errorf("%d entries restored as %d", c.n, len(fc.Features[0].Properties.Name))
		}
	}
}

// Property: whatever FromED269 returns, Parse accepts after Export. ED-269
// zones are built from the valid fixture with random texts, languages,
// carried lists, identifiers and free text, some within ED-318's bounds
// and some past them; a refusal is fine, an output Parse refuses is not.
func TestFromED269OutputAlwaysParses(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := only(validED269(t), "TST001")
	str := func(most int) string {
		n := rng.IntN(most + 1)
		var b strings.Builder
		for range n {
			b.WriteRune([]rune("aZ ა/\"é")[rng.IntN(7)])
		}
		return b.String()
	}
	langs := []string{"en", "en-GB", "ka", "ka-GE", "EN", "", "fr-FR-x", " "}
	accepted, refused := 0, 0
	for range 3000 {
		d := *base
		z := base.Zones[0]
		name := str(260)
		z.Name = &name
		if rng.IntN(3) == 0 {
			id := str(7)
			z.Identifier = id
		}
		if rng.IntN(3) == 0 {
			m := str(260)
			z.Message = &m
		}
		if rng.IntN(3) == 0 {
			z.RestrictionConditions = []string{str(1200), str(1200)}
		}
		if rng.IntN(3) == 0 {
			title := str(2500)
			d.Title = &title
		}
		if rng.IntN(2) == 0 {
			var es []string
			n := 1 + rng.IntN(MaxTexts+3)
			for i := range n {
				text := name
				if i > 0 || rng.IntN(5) == 0 {
					text = str(230)
				}
				tb, _ := json.Marshal(text)
				lb, _ := json.Marshal(langs[rng.IntN(len(langs))])
				es = append(es, `{"text":`+string(tb)+`,"lang":`+string(lb)+`}`)
			}
			z.ExtendedProperties = json.RawMessage(`{"ed269":{"texts":{"name":[` + strings.Join(es, ",") + `]}}}`)
		}
		d.Zones = []ed269.GeoZone{z}
		fc, err := FromED269(&d, Metadata{}, langs[rng.IntN(4)])
		if err != nil {
			refused++
			continue
		}
		accepted++
		raw, err := Export(fc)
		if err != nil {
			t.Fatalf("FromED269 output does not export: %v", err)
		}
		if _, probs := Parse(raw, Limits{}); probs != nil {
			t.Fatalf("FromED269 output refused by Parse: %v\n%s", probs, raw)
		}
	}
	if accepted == 0 || refused == 0 {
		t.Errorf("accepted %d, refused %d: the property ran one branch only", accepted, refused)
	}
	t.Logf("%d accepted and parsed, %d refused by FromED269", accepted, refused)
}
