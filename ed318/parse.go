package ed318

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
)

// Member names and enumerations, from the ED-318 JSON schema
// (UASGeoZones/ED-318 schema/, commit in doc.go) and uas_standards
// eurocae_ed318.py.
var (
	zoneFields = []string{
		"identifier", "country", "name", "type", "variant",
		"restrictionConditions", "region", "reason", "otherReasonInfo",
		"regulationExemption", "message", "zoneAuthority",
		"limitedApplicability", "dataSource", "extendedProperties",
	}
	collectionFields = []string{"type", "name", "title", "description", "bbox", "metadata", "features"}
	featureFields    = []string{"type", "id", "properties", "geometry", "bbox"}
	metadataFields   = []string{"validFrom", "validTo", "issued", "provider", "description", "otherGeoid", "technicalLimitations"}
	sourceFields     = []string{"creationDate", "creationDateTime", "updateDateTime", "originator"}
	authorityFields  = []string{"name", "service", "contactName", "siteURL", "email", "phone", "purpose", "intervalBefore"}
	textFields       = []string{"text", "lang"}
	periodFields     = []string{"startDateTime", "endDateTime", "schedule"}
	dailyFields      = []string{"day", "startTime", "startEvent", "endTime", "endEvent"}

	zoneTypeValues = []string{"USPACE", "PROHIBITED", "REQ_AUTHORIZATION", "CONDITIONAL", "NO_RESTRICTION"}
	variantValues  = []string{"COMMON", variantCustomized}
	reasonValues   = []string{"AIR_TRAFFIC", "SENSITIVE", "PRIVACY", "POPULATION", "NATURE", "NOISE", "EMERGENCY", "DAR", "OTHER"}
	purposeValues  = []string{"AUTHORIZATION", "NOTIFICATION", "INFORMATION"}
	yesNoValues    = []string{"YES", "NO"}
	eventValues    = []string{EventBMCT, EventSR, EventSS, EventEECT}
	dayValues      = []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN", anyDay}
)

// variantCustomized is the ED-318 variant spelt with a Z, as published.
const variantCustomized = "CUSTOMIZED" //nolint:misspell // the ED-318 enumeration value, kept exactly

const anyDay = "ANY"

// MaxFreeTextChars bounds every free-text member the schema leaves
// unbounded (restrictionConditions, an authority's siteURL, email and
// intervalBefore, metadata otherGeoid, the collection's title and
// description), in characters, so that one member cannot carry an
// arbitrarily large string into every consumer (E-10). The schema's own
// bounds (identifier 7, texts 200, phone 200, name 200) apply where it
// sets them.
const MaxFreeTextChars = 2000

// Lengths the schema sets.
const (
	countryLen   = 3
	langMax      = 5
	collNameMax  = 200
	phoneMax     = 200
	minRingCount = 4
)

// parser carries the limits and the problems of one input.
type parser struct {
	lim Limits
	ps  *collector
}

// defaults fills every zero or negative field of l from
// ed269.DefaultLimits.
func defaults(l Limits) Limits {
	d := ed269.DefaultLimits
	fill := func(v *int, def int) {
		if *v <= 0 {
			*v = def
		}
	}
	fill(&l.IdentifierMax, d.IdentifierMax)
	fill(&l.NameMax, d.NameMax)
	fill(&l.MessageMax, d.MessageMax)
	fill(&l.ReasonsMax, d.ReasonsMax)
	fill(&l.MaxRingVertices, d.MaxRingVertices)
	fill(&l.MaxProblems, d.MaxProblems)
	fill(&l.MaxDepth, d.MaxDepth)
	fill(&l.MaxBytes, d.MaxBytes)
	return l
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse reads a whole ED-318 FeatureCollection and validates it on
// receipt, never repairing it (spec 06 T9): it is accepted whole or
// refused whole. On any problem the collection is nil and every problem
// is listed with its JSON path (`features[2].properties.type`) and a
// reason, capped at lim.MaxProblems. A UTF-8 byte order mark is accepted.
// A zero field of lim takes its ed269.DefaultLimits value. Parse never
// panics (fuzzed).
func Parse(data []byte, lim Limits) (*FeatureCollection, *ed269.Problems) {
	lim = defaults(lim)
	ps := &collector{max: lim.MaxProblems}
	if len(data) > lim.MaxBytes {
		ps.add("$", fmt.Sprintf("is %d bytes; at most %d", len(data), lim.MaxBytes))
		return nil, ps.result()
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		ps.add("$", "not UTF-8")
		return nil, ps.result()
	}
	root := decodeTree(data, lim.MaxDepth, ps)
	if root == nil {
		return nil, ps.result()
	}
	if root.kind != kindObject {
		ps.add("$", "not a JSON object")
		return nil, ps.result()
	}
	p := &parser{lim: lim, ps: ps}
	fc := p.collection(root)
	if r := ps.result(); r != nil {
		return nil, r
	}
	return fc, nil
}

// extras keeps the members of v that are not in known, as published.
func extras(v *value, known []string) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	for i, k := range v.keys {
		if slices.Contains(known, k) {
			continue
		}
		if out == nil {
			out = make(map[string]json.RawMessage)
		}
		out[k] = v.vals[i].raw()
	}
	return out
}

// unknown refuses every member of v not in allowed.
func (p *parser) unknown(v *value, path string, allowed []string, reason string) {
	for _, k := range v.keys {
		if !slices.Contains(allowed, k) {
			p.ps.add(join(path, k), reason)
		}
	}
}

func (p *parser) collection(root *value) *FeatureCollection {
	fc := &FeatureCollection{Extra: extras(root, collectionFields)}
	fc.Type = p.literal(root.get("type"), "type", "FeatureCollection")
	fc.Name = p.optionalText(root.get("name"), "name", collNameMax)
	fc.Title = p.optionalText(root.get("title"), "title", MaxFreeTextChars)
	fc.Description = p.optionalText(root.get("description"), "description", MaxFreeTextChars)
	fc.BBox = p.bbox(root.get("bbox"), "bbox")
	if m := root.get("metadata"); present(m) {
		fc.Metadata = p.metadata(m, "metadata")
	}
	list := root.get("features")
	if !present(list) || list.kind != kindArray {
		p.ps.add("features", "missing: required, a list of features")
		return nil
	}
	fc.Features = make([]Feature, 0, len(list.arr))
	firstSeen := make(map[string]int, len(list.arr))
	for i, raw := range list.arr {
		path := index("features", i)
		before := p.ps.count()
		f := p.feature(raw, path)
		if id, ok := raw.identifier(); ok {
			if first, dup := firstSeen[id]; dup {
				p.ps.add(join(path, "properties.identifier"),
					fmt.Sprintf("%s is also the identifier of %s", quote(id), index("features", first)))
			} else {
				firstSeen[id] = i
			}
		}
		if f != nil && p.ps.count() == before {
			fc.Features = append(fc.Features, *f)
		}
	}
	return fc
}

// identifier is a feature's properties.identifier when it is a string.
func (v *value) identifier() (string, bool) {
	if v.kind != kindObject {
		return "", false
	}
	props := v.get("properties")
	if props == nil || props.kind != kindObject {
		return "", false
	}
	id := props.get("identifier")
	if id == nil || id.kind != kindString {
		return "", false
	}
	return id.s, true
}

func (p *parser) feature(v *value, path string) *Feature {
	if v.kind != kindObject {
		p.ps.add(path, "a feature must be an object")
		return nil
	}
	f := &Feature{Extra: extras(v, featureFields)}
	f.Type = p.literal(v.get("type"), join(path, "type"), "Feature")
	if id := v.get("id"); present(id) {
		if id.kind != kindString && id.kind != kindNumber {
			p.ps.add(join(path, "id"), "must be a string or a number, not "+describe(id))
		} else {
			f.ID = id.raw()
		}
	}
	f.BBox = p.bbox(v.get("bbox"), join(path, "bbox"))
	props := v.get("properties")
	if !present(props) {
		p.ps.add(join(path, "properties"), "missing: a UAS zone feature needs its UASZone properties")
	} else if z, ok := p.zone(props, join(path, "properties")); ok {
		f.Properties = z
	}
	geom := v.get("geometry")
	if !present(geom) {
		p.ps.add(join(path, "geometry"), "missing: a UAS zone feature needs a geometry")
	} else if g, ok := p.geometry(geom, join(path, "geometry"), false); ok {
		f.Geometry = g
	}
	return f
}

// literal reads a required string that must equal want.
func (p *parser) literal(v *value, where, want string) string {
	if !present(v) {
		p.ps.add(where, "missing: required, "+quote(want))
		return ""
	}
	if v.kind != kindString || v.s != want {
		p.ps.add(where, "must be "+quote(want)+", not "+show(v))
		return ""
	}
	return v.s
}

func (p *parser) zone(v *value, path string) (UASZone, bool) {
	if v.kind != kindObject {
		p.ps.add(path, "UASZone properties must be an object")
		return UASZone{}, false
	}
	before := p.ps.count()
	p.unknown(v, path, zoneFields, "unknown property; not part of the ED-318 UASZone")
	var z UASZone
	id, okID := p.requiredText(v, "identifier", path, p.lim.IdentifierMax)
	if okID && id != strings.TrimSpace(id) {
		p.ps.add(join(path, "identifier"), "has leading or trailing spaces")
	}
	if okID && strings.Contains(id, "/") {
		p.ps.add(join(path, "identifier"), quote(id)+" contains '/', which ToZones reserves for the layers of a zone (<identifier>/L<index>)")
	}
	z.Identifier = id
	country, okC := p.requiredText(v, "country", path, countryLen)
	if okC && !isCountry(country) {
		p.ps.add(join(path, "country"), quote(country)+" is not an ISO 3166-1 alpha-3 code")
	}
	z.Country = country
	z.Name = p.texts(v.get("name"), join(path, "name"), p.lim.NameMax)
	z.Type = p.zoneType(v.get("type"), join(path, "type"))
	if s, ok := p.requiredEnum(v.get("variant"), join(path, "variant"), variantValues); ok {
		z.Variant = s
	}
	z.RestrictionConditions = p.optionalText(v.get("restrictionConditions"), join(path, "restrictionConditions"), MaxFreeTextChars)
	z.Region = p.integer(v.get("region"), join(path, "region"))
	z.Reason = p.reasons(v.get("reason"), join(path, "reason"))
	z.OtherReasonInfo = p.texts(v.get("otherReasonInfo"), join(path, "otherReasonInfo"), p.lim.NameMax)
	if e := v.get("regulationExemption"); present(e) {
		if s, ok := p.requiredEnum(e, join(path, "regulationExemption"), yesNoValues); ok {
			z.RegulationExemption = &s
		}
	}
	z.Message = p.texts(v.get("message"), join(path, "message"), p.lim.MessageMax)
	z.ZoneAuthority = p.authorities(v.get("zoneAuthority"), join(path, "zoneAuthority"))
	z.LimitedApplicability = p.periods(v.get("limitedApplicability"), join(path, "limitedApplicability"))
	if d := v.get("dataSource"); present(d) {
		z.DataSource = p.dataSource(d, join(path, "dataSource"))
	}
	if x := v.get("extendedProperties"); present(x) {
		if x.kind != kindObject {
			p.ps.add(join(path, "extendedProperties"), "must be an object, not "+describe(x))
		} else {
			z.ExtendedProperties = make(map[string]json.RawMessage, len(x.keys))
			for i, k := range x.keys {
				z.ExtendedProperties[k] = x.vals[i].raw()
			}
		}
	}
	return z, p.ps.count() == before
}

// zoneType reads the required type, refusing ED-269's spelling by name.
func (p *parser) zoneType(v *value, where string) core.ZoneType {
	if present(v) && v.kind == kindString && v.s == "REQ_AUTHORISATION" {
		p.ps.add(where, "'REQ_AUTHORISATION' is the ED-269 spelling; ED-318 spells it REQ_AUTHORIZATION")
		return ""
	}
	s, _ := p.requiredEnum(v, where, zoneTypeValues)
	return core.ZoneType(s)
}

func isCountry(s string) bool {
	if len(s) != countryLen {
		return false
	}
	for i := range countryLen {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// requiredText reads a required, non-blank string member.
func (p *parser) requiredText(v *value, key, path string, maxLen int) (string, bool) {
	where := join(path, key)
	m := v.get(key)
	if !present(m) {
		p.ps.add(where, "missing: required")
		return "", false
	}
	if m.kind == kindString && strings.TrimSpace(m.s) == "" {
		p.ps.add(where, "must not be empty")
		return "", false
	}
	return p.text(m, where, maxLen)
}

// optionalText reads an optional string; nil when absent, null or refused.
func (p *parser) optionalText(v *value, where string, maxLen int) *string {
	if !present(v) {
		return nil
	}
	s, ok := p.text(v, where, maxLen)
	if !ok {
		return nil
	}
	return &s
}

// text checks that v is a string of at most maxLen characters (0: any).
func (p *parser) text(v *value, where string, maxLen int) (string, bool) {
	if v.kind != kindString {
		p.ps.add(where, "must be a string, not "+describe(v))
		return "", false
	}
	if maxLen > 0 {
		if n := utf8.RuneCountInString(v.s); n > maxLen {
			p.ps.add(where, fmt.Sprintf("is %d characters; at most %d", n, maxLen))
			return "", false
		}
	}
	return v.s, true
}

// requiredEnum reads a required string from allowed.
func (p *parser) requiredEnum(v *value, where string, allowed []string) (string, bool) {
	list := strings.Join(allowed, ", ")
	if !present(v) {
		p.ps.add(where, "missing: required, one of "+list)
		return "", false
	}
	if v.kind != kindString {
		p.ps.add(where, "must be one of "+list+", not "+describe(v))
		return "", false
	}
	if !slices.Contains(allowed, v.s) {
		p.ps.add(where, quote(v.s)+" is not one of "+list)
		return "", false
	}
	return v.s, true
}

// integer reads an optional JSON integer.
func (p *parser) integer(v *value, where string) *int {
	if !present(v) {
		return nil
	}
	if v.kind != kindNumber || strings.ContainsAny(v.s, ".eE") {
		p.ps.add(where, "must be an integer, not "+show(v))
		return nil
	}
	n, err := strconv.Atoi(v.s)
	if err != nil {
		p.ps.add(where, "must be an integer within the range of an int, not "+v.s)
		return nil
	}
	return &n
}

// bbox reads an optional GeoJSON bbox: at least four numbers.
func (p *parser) bbox(v *value, where string) []float64 {
	if !present(v) {
		return nil
	}
	if v.kind != kindArray || len(v.arr) < 4 {
		p.ps.add(where, "must be a list of at least four numbers")
		return nil
	}
	out := make([]float64, 0, len(v.arr))
	for i, e := range v.arr {
		f, ok := e.float()
		if !ok {
			p.ps.add(index(where, i), "must be a finite number, not "+show(e))
			return nil
		}
		out = append(out, f)
	}
	return out
}

// texts reads an optional list of textShortType.
func (p *parser) texts(v *value, where string, maxLen int) []Text {
	if !present(v) {
		return nil
	}
	if v.kind != kindArray {
		p.ps.add(where, "must be a list of {text, lang}, not "+describe(v))
		return nil
	}
	out := make([]Text, 0, len(v.arr))
	for i, e := range v.arr {
		if t, ok := p.textObject(e, index(where, i), maxLen); ok {
			out = append(out, t)
		}
	}
	return out
}

// textObject reads one textShortType: lang required (at most five
// characters), text optional.
func (p *parser) textObject(v *value, where string, maxLen int) (Text, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "must be an object {text, lang}, not "+describe(v))
		return Text{}, false
	}
	before := p.ps.count()
	t := Text{Extra: extras(v, textFields)}
	t.Text = p.optionalText(v.get("text"), join(where, "text"), maxLen)
	lang, _ := p.requiredText(v, "lang", where, langMax)
	t.Lang = lang
	return t, p.ps.count() == before
}

// reasons reads the optional reason list: no repeats, at most ReasonsMax.
func (p *parser) reasons(v *value, where string) []string {
	if !present(v) {
		return nil
	}
	if v.kind != kindArray {
		p.ps.add(where, "must be a list, not "+describe(v))
		return nil
	}
	if len(v.arr) > p.lim.ReasonsMax {
		p.ps.add(where, fmt.Sprintf("has %d reasons; at most %d", len(v.arr), p.lim.ReasonsMax))
		return nil
	}
	out := make([]string, 0, len(v.arr))
	for i, item := range v.arr {
		here := index(where, i)
		if item.kind == kindString && item.s == "FOREIGN_TERRITORY" {
			p.ps.add(here, "'FOREIGN_TERRITORY' is an ED-269 reason; ED-318 has no such value")
			continue
		}
		s, ok := p.requiredEnum(item, here, reasonValues)
		if !ok {
			continue
		}
		if slices.Contains(out, s) {
			p.ps.add(here, s+" is listed twice")
			continue
		}
		out = append(out, s)
	}
	return out
}

// authorities reads the required zoneAuthority list: at least one.
func (p *parser) authorities(v *value, where string) []Authority {
	if !present(v) {
		p.ps.add(where, "missing: required, at least one authority")
		return nil
	}
	if v.kind != kindArray || len(v.arr) == 0 {
		p.ps.add(where, "must be a list of at least one authority")
		return nil
	}
	out := make([]Authority, 0, len(v.arr))
	for i, raw := range v.arr {
		here := index(where, i)
		if raw.kind != kindObject {
			p.ps.add(here, "an authority must be an object")
			continue
		}
		a := Authority{Extra: extras(raw, authorityFields)}
		a.Name = p.texts(raw.get("name"), join(here, "name"), p.lim.NameMax)
		a.Service = p.texts(raw.get("service"), join(here, "service"), p.lim.NameMax)
		a.ContactName = p.texts(raw.get("contactName"), join(here, "contactName"), p.lim.NameMax)
		a.SiteURL = p.optionalText(raw.get("siteURL"), join(here, "siteURL"), MaxFreeTextChars)
		a.Email = p.optionalText(raw.get("email"), join(here, "email"), MaxFreeTextChars)
		a.Phone = p.optionalText(raw.get("phone"), join(here, "phone"), phoneMax)
		if s, ok := p.requiredEnum(raw.get("purpose"), join(here, "purpose"), purposeValues); ok {
			a.Purpose = s
		}
		a.IntervalBefore = p.optionalText(raw.get("intervalBefore"), join(here, "intervalBefore"), MaxFreeTextChars)
		out = append(out, a)
	}
	return out
}

func (p *parser) metadata(v *value, where string) *Metadata {
	if v.kind != kindObject {
		p.ps.add(where, "must be an object, not "+describe(v))
		return nil
	}
	m := &Metadata{Extra: extras(v, metadataFields)}
	m.ValidFrom = p.dateTime(v.get("validFrom"), join(where, "validFrom"))
	m.ValidTo = p.dateTime(v.get("validTo"), join(where, "validTo"))
	m.Issued = p.dateTime(v.get("issued"), join(where, "issued"))
	m.Provider = p.texts(v.get("provider"), join(where, "provider"), p.lim.NameMax)
	m.Description = p.texts(v.get("description"), join(where, "description"), p.lim.NameMax)
	m.OtherGeoid = p.optionalText(v.get("otherGeoid"), join(where, "otherGeoid"), MaxFreeTextChars)
	m.TechnicalLimitations = p.texts(v.get("technicalLimitations"), join(where, "technicalLimitations"), p.lim.NameMax)
	return m
}

func (p *parser) dataSource(v *value, where string) *DataSource {
	if v.kind != kindObject {
		p.ps.add(where, "must be an object, not "+describe(v))
		return nil
	}
	d := &DataSource{Extra: extras(v, sourceFields)}
	d.CreationDate = p.dateTime(v.get("creationDate"), join(where, "creationDate"))
	d.CreationDateTime = p.dateTime(v.get("creationDateTime"), join(where, "creationDateTime"))
	d.UpdateDateTime = p.dateTime(v.get("updateDateTime"), join(where, "updateDateTime"))
	if o := v.get("originator"); present(o) {
		if t, ok := p.textObject(o, join(where, "originator"), p.lim.NameMax); ok {
			d.Originator = &t
		}
	}
	return d
}

// naiveLayouts are date-times without an offset: refused by name.
var naiveLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"}

// dateTime reads an optional RFC 3339 date-time with an offset.
func (p *parser) dateTime(v *value, where string) *DateTime {
	if !present(v) {
		return nil
	}
	if v.kind != kindString {
		p.ps.add(where, "must be an RFC 3339 date-time, not "+describe(v))
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v.s); err == nil {
		return &DateTime{Time: t, Text: v.s}
	}
	for _, layout := range naiveLayouts {
		if _, err := time.Parse(layout, v.s); err == nil {
			p.ps.add(where, quote(v.s)+" has no offset; give Z or +hh:mm")
			return nil
		}
	}
	p.ps.add(where, quote(v.s)+" is not an RFC 3339 date-time")
	return nil
}
