package ed318

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
)

// ED269Key is the extendedProperties member that carries, in each
// direction, what the other format has no member for (owner decision on
// PR #16):
//
//   - in ED-318, written by FromED269 and read back by ToED269: the ED-269
//     fields `uSpaceClass`, the zone-level `title`, and
//     `restrictionConditions` when ED-269 published it as a list
//     (ED-318's is one string; the list is joined with newlines there
//     and kept whole here), and `radius` {value, uom: "FT"} for a circle
//     ED-269 published in feet (ED-318's radius is metres);
//   - in ED-269, written by ToED269 and read back by FromED269: `texts`,
//     the whole ED-318 text lists of the zone whose other languages
//     ED-269's single string cannot hold, by member (`name`, `message`,
//     `otherReasonInfo`, `zoneAuthority[k].name`, `.service`,
//     `.contactName`), each as published ([{text, lang}]).
const ED269Key = "ed269"

// textsKey is the ED269Key member that carries ED-318 text lists in an
// ED-269 document.
const textsKey = "texts"

var ed269KeyFields = []string{"uSpaceClass", "title", "restrictionConditions", "radius"}

// keptRadius is a circle radius as ED-269 published it in feet, carried
// under ED269Key so that ToED269 writes it back exactly.
type keptRadius struct {
	Value float64 `json:"value"`
	Uom   string  `json:"uom"`
}

// FromED269 maps a parsed ED-269 document onto ED-318 (spec 02 F1):
// restriction becomes type (REQ_AUTHORISATION becomes REQ_AUTHORIZATION),
// the ED-269 type (COMMON or the customised type) becomes variant,
// applicability becomes limitedApplicability (a document of one
// permanent period has none; any other permanent period becomes an empty
// TimePeriod, which applies always), each text becomes a one-entry list
// in lang, uomDimensions M and FT become the layer's m and ft, a Circle
// becomes a Point with a Circle extent (its radius always in metres, feet
// converted with core.FeetToMetres, the published feet kept in
// extendedProperties.ed269.radius for ToED269), and the document's title and
// description are kept. meta becomes the collection's metadata (nil when
// it is the zero value).
//
// Text lists that ToED269 carried in extendedProperties.ed269.texts are
// restored whole, every language back; the carried list must still hold
// the ED-269 text, or the zone is refused (the text was edited since).
//
// What ED-318 cannot hold is refused with a *core.FieldError naming the
// zone, never dropped or invented: a FOREIGN_TERRITORY reason (ED-318 has
// none), a zone with no zoneAuthority (ED-318 requires one) or an
// authority with no purpose (required in ED-318), extendedProperties that
// are not an object, and an ED269Key member that holds anything but the
// texts ToED269 writes. Date-times and clock
// times are kept as published when they are RFC 3339 and rewritten in RFC
// 3339 when ED-269 allowed a shorter form (08:00+04:00 becomes
// 08:00:00+04:00). The UASZoneList wrapper's formatVersion and createdAt
// are not carried.
func FromED269(doc *ed269.Document, meta Metadata, lang string) (*FeatureCollection, error) {
	if doc == nil {
		return nil, core.Fieldf("$", "no document")
	}
	if lang == "" || len(lang) > langMax {
		return nil, core.Fieldf("lang", "%q must be a language tag of 1 to %d characters (en-GB)", lang, langMax)
	}
	fc := &FeatureCollection{Type: "FeatureCollection", Title: doc.Title, Description: doc.Description}
	if !reflect.DeepEqual(meta, Metadata{}) {
		m := meta
		fc.Metadata = &m
	}
	fc.Features = make([]Feature, 0, len(doc.Zones))
	for i := range doc.Zones {
		f, err := fromZone(&doc.Zones[i], lang, index("features", i))
		if err != nil {
			return nil, err
		}
		fc.Features = append(fc.Features, *f)
	}
	// What FromED269 returns, Parse accepts: an ED-269 value ED-318
	// bounds more tightly (a longer title, a "/" in an identifier, deeper
	// extendedProperties) is refused here, naming the ED-318 field, never
	// handed on to be refused later.
	raw, err := Export(fc)
	if err != nil {
		return nil, err
	}
	if _, probs := Parse(raw, Limits{}); probs != nil {
		first := probs.List[0]
		return nil, core.Fieldf(first.Field, "the ED-318 this mapping writes would be refused: %s", first.Reason)
	}
	return fc, nil
}

func oneText(s *string, lang string) []Text {
	if s == nil {
		return nil
	}
	v := *s
	return []Text{{Text: &v, Lang: lang}}
}

func fromZone(z *ed269.GeoZone, lang, path string) (*Feature, error) {
	t := z.Restriction.ZoneType()
	if t == "" {
		return nil, core.Fieldf(path+".restriction", "%q is not an ED-269 restriction", string(z.Restriction))
	}
	carried, rest, err := carriedTexts(z.ExtendedProperties, path)
	if err != nil {
		return nil, err
	}
	tx := &restorer{lang: lang, carried: carried, path: path}
	u := UASZone{
		Identifier:      z.Identifier,
		Country:         z.Country,
		Name:            tx.texts("name", z.Name),
		Type:            t,
		Variant:         z.Type,
		Region:          z.Region,
		OtherReasonInfo: tx.texts("otherReasonInfo", z.OtherReasonInfo),
		Message:         tx.texts("message", z.Message),
	}
	if z.RegulationExemption != nil {
		s := string(*z.RegulationExemption)
		u.RegulationExemption = &s
	}
	if z.Reason != nil {
		u.Reason = make([]string, 0, len(z.Reason))
		for k, r := range z.Reason {
			if r == ed269.ReasonForeignTerritory {
				return nil, core.Fieldf(index(path+".reason", k), "FOREIGN_TERRITORY has no ED-318 reason")
			}
			u.Reason = append(u.Reason, string(r))
		}
	}
	keep := map[string]any{}
	if z.RestrictionConditions != nil {
		joined := strings.Join(z.RestrictionConditions, "\n")
		u.RestrictionConditions = &joined
		if !z.RestrictionConditionsIsText {
			keep["restrictionConditions"] = z.RestrictionConditions
		}
	}
	if z.USpaceClass != nil {
		keep["uSpaceClass"] = *z.USpaceClass
	}
	if z.Title != nil {
		keep["title"] = *z.Title
	}
	if len(z.Geometry) == 1 && z.Geometry[0].Uom == ed269.UomFeet && z.Geometry[0].Projection.Radius != nil {
		keep["radius"] = keptRadius{Value: *z.Geometry[0].Projection.Radius, Uom: string(ed269.UomFeet)}
	}
	ext, err := fromExtended(rest, keep, path)
	if err != nil {
		return nil, err
	}
	u.ExtendedProperties = ext
	if len(z.ZoneAuthority) == 0 {
		return nil, core.Fieldf(path+".zoneAuthority", "is empty; ED-318 requires at least one zone authority")
	}
	for k, a := range z.ZoneAuthority {
		if a.Purpose == nil {
			return nil, core.Fieldf(index(path+".zoneAuthority", k)+".purpose", "is missing; ED-318 requires it")
		}
		key := index("zoneAuthority", k)
		u.ZoneAuthority = append(u.ZoneAuthority, Authority{
			Name:           tx.texts(key+".name", a.Name),
			Service:        tx.texts(key+".service", a.Service),
			ContactName:    tx.texts(key+".contactName", a.ContactName),
			SiteURL:        a.SiteURL,
			Email:          a.Email,
			Phone:          a.Phone,
			Purpose:        string(*a.Purpose),
			IntervalBefore: a.IntervalBefore,
		})
	}
	if err := tx.finish(); err != nil {
		return nil, err
	}
	periods, err := fromApplicability(z, path)
	if err != nil {
		return nil, err
	}
	u.LimitedApplicability = periods
	vol, ok := z.Volume()
	if !ok {
		return nil, core.Fieldf(path+".geometry", "has %d volumes; one is mapped", len(z.Geometry))
	}
	g, err := fromVolume(vol, path+".geometry[0]")
	if err != nil {
		return nil, err
	}
	return &Feature{Type: "Feature", Geometry: g, Properties: u}, nil
}

// carriedTexts splits ED-269's extendedProperties (an object) into the
// text lists ToED269 carried under ED269Key and the rest.
func carriedTexts(raw json.RawMessage, path string) (map[string][]Text, map[string]json.RawMessage, error) {
	if raw == nil {
		return nil, nil, nil
	}
	var ext map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&ext); err != nil || ext == nil {
		return nil, nil, core.Fieldf(path+".extendedProperties", "is not a JSON object; ED-318 extendedProperties is one")
	}
	block, ok := ext[ED269Key]
	if !ok {
		return nil, ext, nil
	}
	where := path + ".extendedProperties." + ED269Key
	var members map[string]json.RawMessage
	if err := json.Unmarshal(block, &members); err != nil || members == nil {
		return nil, nil, core.Fieldf(where, "must be an object holding only %s, which ToED269 writes", textsKey)
	}
	carried := map[string][]Text{}
	for k, v := range members {
		if k != textsKey {
			return nil, nil, core.Fieldf(where+"."+k, "is not %s, the only member ToED269 writes here", textsKey)
		}
		var lists map[string][]wireText
		if err := json.Unmarshal(v, &lists); err != nil || lists == nil {
			return nil, nil, core.Fieldf(where+"."+textsKey, "must map members to lists of {text, lang}")
		}
		for member, list := range lists {
			ts, err := restoredList(list, member, where+"."+textsKey+"."+member)
			if err != nil {
				return nil, nil, err
			}
			carried[member] = ts
		}
	}
	rest := make(map[string]json.RawMessage, len(ext))
	for k, v := range ext {
		if k != ED269Key {
			rest[k] = v
		}
	}
	if len(rest) == 0 {
		rest = nil
	}
	return carried, rest, nil
}

// restoredList checks a carried list against the limits Parse applies to
// the same member, so that FromED269 never produces a list Parse refuses:
// at most MaxTexts entries, each lang 1 to 5 characters and given once,
// each text within the member's length (messages MessageMax, the rest
// NameMax, both of ed269.DefaultLimits).
func restoredList(list []wireText, member, where string) ([]Text, error) {
	if len(list) == 0 || len(list) > MaxTexts {
		return nil, core.Fieldf(where, "has %d entries; 1 to %d", len(list), MaxTexts)
	}
	maxLen := ed269.DefaultLimits.NameMax
	if member == "message" {
		maxLen = ed269.DefaultLimits.MessageMax
	}
	seen := make(map[string]bool, len(list))
	out := make([]Text, 0, len(list))
	for i, w := range list {
		here := index(where, i)
		if w.Lang == "" || utf8.RuneCountInString(w.Lang) > langMax || strings.TrimSpace(w.Lang) == "" {
			return nil, core.Fieldf(here+".lang", "must be 1 to %d characters", langMax)
		}
		key := strings.ToLower(w.Lang)
		if seen[key] {
			return nil, core.Fieldf(here+".lang", "%q is given twice", w.Lang)
		}
		seen[key] = true
		if w.Text != nil && utf8.RuneCountInString(*w.Text) > maxLen {
			return nil, core.Fieldf(here+".text", "is %d characters; at most %d", utf8.RuneCountInString(*w.Text), maxLen)
		}
		out = append(out, Text{Text: w.Text, Lang: w.Lang})
	}
	return out, nil
}

// wireText is one carried text as JSON.
type wireText struct {
	Text *string `json:"text,omitempty"`
	Lang string  `json:"lang"`
}

// restorer gives each ED-269 text its ED-318 list: the carried list when
// one was carried for that member, else one entry in lang.
type restorer struct {
	lang    string
	carried map[string][]Text
	used    map[string]bool
	path    string
	err     error
}

func (r *restorer) texts(member string, s *string) []Text {
	list, ok := r.carried[member]
	if !ok {
		return oneText(s, r.lang)
	}
	if r.used == nil {
		r.used = map[string]bool{}
	}
	r.used[member] = true
	for _, t := range list {
		if s != nil && t.Text != nil && *t.Text == *s {
			return list
		}
	}
	if r.err == nil {
		r.err = core.Fieldf(r.path+".extendedProperties."+ED269Key+"."+textsKey+"."+member,
			"no longer holds the ED-269 text of %s; the text was edited after ToED269 carried the languages", member)
	}
	return nil
}

// finish reports a stale carried list, or one for a member the zone does
// not have.
func (r *restorer) finish() error {
	if r.err != nil {
		return r.err
	}
	for member := range r.carried {
		if !r.used[member] {
			return core.Fieldf(r.path+".extendedProperties."+ED269Key+"."+textsKey+"."+member,
				"is carried for a member this zone does not have")
		}
	}
	return nil
}

// fromExtended carries the rest of ED-269's extendedProperties and the
// kept ED-269 fields under ED269Key.
func fromExtended(ext map[string]json.RawMessage, keep map[string]any, path string) (map[string]json.RawMessage, error) {
	if len(keep) > 0 {
		b, err := json.Marshal(keep)
		if err != nil {
			return nil, core.Fieldf(path, "%v", err)
		}
		if ext == nil {
			ext = map[string]json.RawMessage{}
		}
		ext[ED269Key] = b
	}
	return ext, nil
}

// fromApplicability maps ED-269 periods, reading their published texts
// through ed269.Feature.
func fromApplicability(z *ed269.GeoZone, path string) ([]TimePeriod, error) {
	if len(z.Applicability) == 1 && z.Applicability[0].Permanent {
		return nil, nil
	}
	feature, err := ed269.Feature(z)
	if err != nil {
		return nil, core.Fieldf(path, "%v", err)
	}
	list, _ := feature["applicability"].([]any)
	out := make([]TimePeriod, 0, len(list))
	for k, item := range list {
		here := index(path+".applicability", k)
		m, _ := item.(map[string]any)
		var tp TimePeriod
		if s, ok := m["startDateTime"].(string); ok {
			d, err := rfc3339DateTime(s, here+".startDateTime")
			if err != nil {
				return nil, err
			}
			tp.StartDateTime = d
		}
		if s, ok := m["endDateTime"].(string); ok {
			d, err := rfc3339DateTime(s, here+".endDateTime")
			if err != nil {
				return nil, err
			}
			tp.EndDateTime = d
		}
		sched, _ := m["schedule"].([]any)
		for j, sd := range sched {
			dm, _ := sd.(map[string]any)
			where := index(here+".schedule", j)
			var d DailyPeriod
			days, _ := dm["day"].([]any)
			for _, day := range days {
				s, _ := day.(string)
				d.Day = append(d.Day, s)
			}
			for _, end := range []struct {
				key string
				dst **string
			}{{"startTime", &d.StartTime}, {"endTime", &d.EndTime}} {
				s, _ := dm[end.key].(string)
				c, err := rfc3339Clock(s, where+"."+end.key)
				if err != nil {
					return nil, err
				}
				*end.dst = &c
			}
			tp.Schedule = append(tp.Schedule, d)
		}
		out = append(out, tp)
	}
	return out, nil
}

// ed269DateLayouts are the date-time forms ed269 accepts.
var ed269DateLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z0700",
}

// rfc3339DateTime keeps an RFC 3339 text and rewrites ED-269's shorter
// forms in RFC 3339 with the same offset.
func rfc3339DateTime(s, where string) (*DateTime, error) {
	for i, layout := range ed269DateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			if i == 0 {
				return &DateTime{Time: t, Text: s}, nil
			}
			return &DateTime{Time: t, Text: t.Format(time.RFC3339Nano)}, nil
		}
	}
	return nil, core.Fieldf(where, "%q is not a date-time", s)
}

// rfc3339Clock keeps an RFC 3339 full-time and rewrites ED-269's shorter
// forms (no seconds, an offset without a colon) in RFC 3339.
func rfc3339Clock(s, where string) (string, error) {
	if _, ok := parseClock(s); ok {
		return s, nil
	}
	// hh:mm, then an optional :ss and fraction, then the offset.
	if len(s) < 6 || s[2] != ':' {
		return "", core.Fieldf(where, "%q is not a time of day", s)
	}
	body, off := s, ""
	for i := 5; i < len(s); i++ {
		if s[i] == 'Z' || s[i] == '+' || s[i] == '-' {
			body, off = s[:i], s[i:]
			break
		}
	}
	if len(body) == 5 {
		body += ":00"
	}
	if len(off) == 5 && off[0] != 'Z' {
		off = off[:3] + ":" + off[3:]
	}
	out := body + off
	if _, ok := parseClock(out); !ok {
		return "", core.Fieldf(where, "%q is not a time of day with an offset", s)
	}
	return out, nil
}

// fromVolume maps an ED-269 volume onto a layered geometry.
func fromVolume(v ed269.Volume, path string) (Geometry, error) {
	l := &Layer{Upper: v.UpperLimit, Lower: v.LowerLimit, UpperReference: v.UpperRef, LowerReference: v.LowerRef}
	uom := UomMetres
	if v.Uom == ed269.UomFeet {
		uom = UomFeet
	}
	l.Uom = &uom
	switch v.Projection.Type {
	case ed269.ShapePolygon:
		rings := make([][]core.LatLon, 0, len(v.Projection.Rings))
		for _, r := range v.Projection.Rings {
			rings = append(rings, append([]core.LatLon(nil), r...))
		}
		return Geometry{Type: GeometryPolygon, Rings: rings, Layer: l}, nil
	case ed269.ShapeCircle:
		r := v.RadiusM()
		if v.Projection.Center == nil || r == nil {
			return Geometry{}, core.Fieldf(path+".horizontalProjection", "a Circle needs a center and a radius")
		}
		c := *v.Projection.Center
		return Geometry{Type: GeometryPoint, Center: &c, RadiusM: r, Layer: l}, nil
	}
	return Geometry{}, core.Fieldf(path+".horizontalProjection.type", "%q is not Polygon or Circle", v.Projection.Type)
}

// ToED269 maps an ED-318 collection onto an ED-269 document for legacy
// consumers, the inverse of FromED269 (a document written by FromED269
// maps back to the one it came from). The result is built as ED-269 JSON
// and read back with ed269.Parse, so it is a valid ED-269 document or an
// error.
//
// ED-269 holds one string where ED-318 holds a list of texts in several
// languages. ToED269 writes the text in lang when there is one, else in
// English (en or en-*), else the first text given; when a list has more
// than one entry, the whole list is carried in the ED-269 zone's
// extendedProperties.ed269.texts, so that FromED269 restores every
// language (owner decision on PR #16). A list none of whose entries has
// a text is refused.
//
// What ED-269 cannot express is refused with a *core.FieldError naming the
// field, never dropped or approximated: a USPACE zone (ED-269 has no such
// restriction), a DAR reason, a daylight event (ED-269 times are clock
// times), a GeometryCollection of more than one layer (ED-269 holds one
// volume per zone), a layer without both references. The collection's
// metadata, a zone's dataSource and the extras ED-269 has no member for
// are not carried; extendedProperties is, with ED-318's ED269Key fields
// returned to their ED-269 members.
func ToED269(fc *FeatureCollection, lang string) (*ed269.Document, error) {
	if fc == nil {
		return nil, core.Fieldf("$", "no feature collection")
	}
	doc := map[string]any{}
	if fc.Title != nil {
		doc["title"] = *fc.Title
	}
	if fc.Description != nil {
		doc["description"] = *fc.Description
	}
	features := make([]any, 0, len(fc.Features))
	for i := range fc.Features {
		z, err := toZone(&fc.Features[i], index("features", i), lang)
		if err != nil {
			return nil, err
		}
		features = append(features, z)
	}
	doc["features"] = features
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, core.Fieldf("$", "%v", err)
	}
	out, probs := ed269.Parse(raw, ed269.Limits{})
	if probs != nil {
		first := probs.List[0]
		return nil, core.Fieldf(first.Field, "the mapped ED-269 is refused: %s", first.Reason)
	}
	return out, nil
}

// pickText chooses the text ED-269 gets from a list: the one in lang,
// else English (en or en-*), else the first, among entries with a text.
func pickText(ts []Text, lang string) *string {
	var en, first *string
	for _, t := range ts {
		if t.Text == nil {
			continue
		}
		switch {
		case lang != "" && strings.EqualFold(t.Lang, lang):
			return t.Text
		case en == nil && (strings.EqualFold(t.Lang, "en") || strings.HasPrefix(strings.ToLower(t.Lang), "en-")):
			en = t.Text
		case first == nil:
			first = t.Text
		}
	}
	if en != nil {
		return en
	}
	return first
}

// carrier writes ED-269 strings from ED-318 text lists and keeps the
// lists that hold more than one entry.
type carrier struct {
	lang  string
	lists map[string]any
}

// set writes member k of m from ts; member names the list in the carrier.
func (c *carrier) set(m map[string]any, k, member string, ts []Text, where string) error {
	if ts == nil {
		return nil
	}
	s := pickText(ts, c.lang)
	if s == nil {
		return core.Fieldf(where, "has no text in any of its %d entries; ED-269 holds the text, not only its language", len(ts))
	}
	m[k] = *s
	if len(ts) > 1 {
		if c.lists == nil {
			c.lists = map[string]any{}
		}
		list := make([]any, 0, len(ts))
		for _, t := range ts {
			e := map[string]any{"lang": t.Lang}
			if t.Text != nil {
				e["text"] = *t.Text
			}
			list = append(list, e)
		}
		c.lists[member] = list
	}
	return nil
}

func toZone(f *Feature, path, lang string) (map[string]any, error) {
	z := &f.Properties
	here := path + ".properties"
	if z.Type == core.ZoneUSpace {
		return nil, core.Fieldf(here+".type", "USPACE has no ED-269 restriction")
	}
	m := map[string]any{
		"identifier":  z.Identifier,
		"country":     z.Country,
		"type":        z.Variant,
		"restriction": z.Type.ED269(),
	}
	c := &carrier{lang: lang}
	for _, t := range []struct {
		k  string
		ts []Text
	}{{"name", z.Name}, {"message", z.Message}, {"otherReasonInfo", z.OtherReasonInfo}} {
		if err := c.set(m, t.k, t.k, t.ts, here+"."+t.k); err != nil {
			return nil, err
		}
	}
	if z.Reason != nil {
		rs := make([]string, 0, len(z.Reason))
		for k, r := range z.Reason {
			if r == "DAR" {
				return nil, core.Fieldf(index(here+".reason", k), "DAR has no ED-269 reason")
			}
			rs = append(rs, r)
		}
		m["reason"] = rs
	}
	if z.Region != nil {
		m["region"] = *z.Region
	}
	if z.RegulationExemption != nil {
		m["regulationExemption"] = *z.RegulationExemption
	}
	var kept *keptRadius
	rest, err := toExtended(m, z, here, &kept)
	if err != nil {
		return nil, err
	}
	auths := make([]any, 0, len(z.ZoneAuthority))
	for k := range z.ZoneAuthority {
		a, err := toAuthority(&z.ZoneAuthority[k], index(here+".zoneAuthority", k), index("zoneAuthority", k), c)
		if err != nil {
			return nil, err
		}
		auths = append(auths, a)
	}
	m["zoneAuthority"] = auths
	if c.lists != nil {
		block, err := json.Marshal(map[string]any{textsKey: c.lists})
		if err != nil {
			return nil, core.Fieldf(here, "%v", err)
		}
		rest[ED269Key] = block
	}
	if len(rest) > 0 {
		m["extendedProperties"] = rest
	}
	periods, err := toApplicability(z.LimitedApplicability, here+".limitedApplicability")
	if err != nil {
		return nil, err
	}
	m["applicability"] = periods
	vol, err := toVolume(f.Geometry, path+".geometry", kept)
	if err != nil {
		return nil, err
	}
	m["geometry"] = []any{vol}
	return m, nil
}

// toExtended restores the ED269Key fields and returns the rest of
// extendedProperties.
func toExtended(m map[string]any, z *UASZone, here string, radius **keptRadius) (map[string]json.RawMessage, error) {
	if z.RestrictionConditions != nil {
		m["restrictionConditions"] = *z.RestrictionConditions
	}
	rest := map[string]json.RawMessage{}
	if z.ExtendedProperties == nil {
		return rest, nil
	}
	for k, v := range z.ExtendedProperties {
		if k != ED269Key {
			rest[k] = v
		}
	}
	if raw, ok := z.ExtendedProperties[ED269Key]; ok {
		where := here + ".extendedProperties." + ED269Key
		var kept map[string]json.RawMessage
		if err := json.Unmarshal(raw, &kept); err != nil || kept == nil {
			return nil, core.Fieldf(where, "must be an object")
		}
		for k, v := range kept {
			switch k {
			case "uSpaceClass", "title":
				var s string
				if err := json.Unmarshal(v, &s); err != nil {
					return nil, core.Fieldf(where+"."+k, "must be a string")
				}
				m[k] = s
			case "radius":
				var r keptRadius
				if err := json.Unmarshal(v, &r); err != nil || r.Uom != string(ed269.UomFeet) || !core.IsFinite(r.Value) || r.Value <= 0 {
					return nil, core.Fieldf(where+"."+k, "must be {value, uom: FT} with a positive value")
				}
				*radius = &r
			case "restrictionConditions":
				var list []string
				if err := json.Unmarshal(v, &list); err != nil || list == nil {
					return nil, core.Fieldf(where+"."+k, "must be a list of strings")
				}
				// The list stands for restrictionConditions only while the
				// ED-318 text is still its join; an edited text wins.
				if z.RestrictionConditions != nil && strings.Join(list, "\n") == *z.RestrictionConditions {
					m["restrictionConditions"] = list
				}
			default:
				return nil, core.Fieldf(where+"."+k, "is not one of %s", strings.Join(ed269KeyFields, ", "))
			}
		}
	}
	return rest, nil
}

func toAuthority(a *Authority, where, member string, c *carrier) (map[string]any, error) {
	m := map[string]any{"purpose": a.Purpose}
	for _, t := range []struct {
		k  string
		ts []Text
	}{{"name", a.Name}, {"service", a.Service}, {"contactName", a.ContactName}} {
		if err := c.set(m, t.k, member+"."+t.k, t.ts, where+"."+t.k); err != nil {
			return nil, err
		}
	}
	for k, s := range map[string]*string{"siteURL": a.SiteURL, "email": a.Email, "phone": a.Phone, "intervalBefore": a.IntervalBefore} {
		if s != nil {
			m[k] = *s
		}
	}
	return m, nil
}

func toApplicability(tps []TimePeriod, where string) ([]any, error) {
	if len(tps) == 0 {
		return []any{map[string]any{"permanent": "YES"}}, nil
	}
	out := make([]any, 0, len(tps))
	for i, tp := range tps {
		here := index(where, i)
		if tp.StartDateTime == nil && tp.EndDateTime == nil && len(tp.Schedule) == 0 {
			out = append(out, map[string]any{"permanent": "YES"})
			continue
		}
		p := map[string]any{"permanent": "NO"}
		if tp.StartDateTime != nil {
			p["startDateTime"] = tp.StartDateTime.text()
		}
		if tp.EndDateTime != nil {
			p["endDateTime"] = tp.EndDateTime.text()
		}
		if tp.Schedule != nil {
			ss := make([]any, 0, len(tp.Schedule))
			for j, d := range tp.Schedule {
				dw := index(here+".schedule", j)
				if d.StartEvent != nil {
					return nil, core.Fieldf(dw+".startEvent", "%s is a daylight event; ED-269 has clock times only", *d.StartEvent)
				}
				if d.EndEvent != nil {
					return nil, core.Fieldf(dw+".endEvent", "%s is a daylight event; ED-269 has clock times only", *d.EndEvent)
				}
				ss = append(ss, map[string]any{"day": d.Day, "startTime": deref(d.StartTime), "endTime": deref(d.EndTime)})
			}
			p["schedule"] = ss
		}
		out = append(out, p)
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toVolume(g Geometry, where string, kept *keptRadius) (map[string]any, error) {
	if g.Type == GeometryCollection {
		if len(g.Geometries) != 1 {
			return nil, core.Fieldf(where+".geometries", "has %d layers; ED-269 holds one volume per zone", len(g.Geometries))
		}
		where = index(where+".geometries", 0)
		g = g.parts()[0]
	}
	l := g.Layer
	if l == nil || l.LowerReference == "" || l.UpperReference == "" {
		return nil, core.Fieldf(where+".layer", "needs both references; ED-269 requires lowerVerticalReference and upperVerticalReference")
	}
	v := map[string]any{
		"uomDimensions":          "M",
		"lowerVerticalReference": string(l.LowerReference),
		"upperVerticalReference": string(l.UpperReference),
	}
	feet := l.Uom != nil && *l.Uom == UomFeet
	if feet {
		v["uomDimensions"] = "FT"
	}
	if l.Lower != nil {
		v["lowerLimit"] = json.Number(strconv.FormatFloat(*l.Lower, 'g', -1, 64))
	}
	if l.Upper != nil {
		v["upperLimit"] = json.Number(strconv.FormatFloat(*l.Upper, 'g', -1, 64))
	}
	switch g.Type {
	case GeometryPolygon:
		rings := make([]any, 0, len(g.Rings))
		for _, r := range g.Rings {
			ring := make([]any, 0, len(r))
			for _, pt := range r {
				ring = append(ring, []float64{pt.LonDeg, pt.LatDeg})
			}
			rings = append(rings, ring)
		}
		v["horizontalProjection"] = map[string]any{"type": ed269.ShapePolygon, "coordinates": rings}
	case GeometryPoint:
		if g.Center == nil || g.RadiusM == nil {
			return nil, core.Fieldf(where, "a Point zone needs a centre and a radius")
		}
		radius := *g.RadiusM
		if feet {
			// The feet ED-269 published, while the metres still convert
			// from them exactly; else the shortest feet that do.
			if kept != nil && kept.Value*core.FeetToMetres == radius {
				radius = kept.Value
			} else {
				radius = shortestFeet(radius)
			}
		}
		v["horizontalProjection"] = map[string]any{
			"type":   ed269.ShapeCircle,
			"center": []float64{g.Center.LonDeg, g.Center.LatDeg},
			"radius": radius,
		}
	default:
		return nil, core.Fieldf(where+".type", "%q is not Polygon or Point", g.Type)
	}
	return v, nil
}

// shortestFeet is the shortest decimal number of feet that converts to
// exactly radiusM with core.FeetToMetres, so that a radius FromED269
// converted from feet returns to the feet it was published in (1640 ft is
// 499.872 m and returns as 1640; 3500 ft is 1066.8 m, whose quotient by
// 0.3048 is 3499.9999999999995, and returns as 3500).
func shortestFeet(radiusM float64) float64 {
	q := radiusM / core.FeetToMetres
	for prec := 1; prec <= 17; prec++ {
		f, err := strconv.ParseFloat(strconv.FormatFloat(q, 'g', prec, 64), 64)
		if err == nil && f*core.FeetToMetres == radiusM {
			return f
		}
	}
	return q
}
