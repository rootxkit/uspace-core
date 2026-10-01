package ed318

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// member is one name and value of an ordered object being written.
type member struct {
	k string
	v any
}

// object is a JSON object that keeps its member order on export.
type object []member

func (o *object) set(k string, v any) { *o = append(*o, member{k, v}) }

// number is a JSON number written by value (5.0 as 5).
type number float64

func setString(o *object, k string, s *string) {
	if s != nil {
		o.set(k, *s)
	}
}

func setTime(o *object, k string, d *DateTime) {
	if d != nil {
		o.set(k, d.text())
	}
}

// text is the date-time as published, or its RFC 3339 form when it was
// built in code or changed since.
func (d *DateTime) text() string {
	if d.Text != "" {
		if t, err := time.Parse(time.RFC3339Nano, d.Text); err == nil && t.Equal(d.Time) {
			return d.Text
		}
	}
	return d.Time.Format(time.RFC3339Nano)
}

// setExtra appends the extra members in name order.
func setExtra(o *object, extra map[string]json.RawMessage) {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.set(k, extra[k])
	}
}

func textList(ts []Text) []any {
	out := make([]any, 0, len(ts))
	for _, t := range ts {
		out = append(out, textObject(t))
	}
	return out
}

func textObject(t Text) object {
	var o object
	setString(&o, "text", t.Text)
	o.set("lang", t.Lang)
	setExtra(&o, t.Extra)
	return o
}

func setTexts(o *object, k string, ts []Text) {
	if ts != nil {
		o.set(k, textList(ts))
	}
}

func numbers(fs []float64) []any {
	out := make([]any, 0, len(fs))
	for _, f := range fs {
		out = append(out, number(f))
	}
	return out
}

// Export writes fc as an ED-318 FeatureCollection, so that
// Export(Parse(f)) equals f by value: members in schema order with the
// members this package keeps but does not interpret (extras,
// extendedProperties) after them as published, null and absent optional
// members absent, numbers by value, date-times and clock times as
// published. It fails, naming the field with a *core.FieldError, only
// for a collection Parse could not have produced (a non-finite number, an
// unknown geometry type, extras that are not JSON).
func Export(fc *FeatureCollection) ([]byte, error) {
	if fc == nil {
		return nil, core.Fieldf("$", "no feature collection")
	}
	var o object
	o.set("type", "FeatureCollection")
	setString(&o, "name", fc.Name)
	setString(&o, "title", fc.Title)
	setString(&o, "description", fc.Description)
	if fc.BBox != nil {
		o.set("bbox", numbers(fc.BBox))
	}
	if fc.Metadata != nil {
		o.set("metadata", metadataObject(fc.Metadata))
	}
	features := make([]any, 0, len(fc.Features))
	for i := range fc.Features {
		f, err := featureObject(&fc.Features[i], index("features", i))
		if err != nil {
			return nil, err
		}
		features = append(features, f)
	}
	o.set("features", features)
	setExtra(&o, fc.Extra)
	var b bytes.Buffer
	if err := encode(o, &b, "$"); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func metadataObject(m *Metadata) object {
	var o object
	setTime(&o, "validFrom", m.ValidFrom)
	setTime(&o, "validTo", m.ValidTo)
	setTime(&o, "issued", m.Issued)
	setTexts(&o, "provider", m.Provider)
	setTexts(&o, "description", m.Description)
	setString(&o, "otherGeoid", m.OtherGeoid)
	setTexts(&o, "technicalLimitations", m.TechnicalLimitations)
	setExtra(&o, m.Extra)
	return o
}

func featureObject(f *Feature, path string) (object, error) {
	var o object
	o.set("type", "Feature")
	if f.ID != nil {
		o.set("id", f.ID)
	}
	g, err := geometryObject(&f.Geometry, path+".geometry")
	if err != nil {
		return nil, err
	}
	o.set("geometry", g)
	o.set("properties", zoneObject(&f.Properties))
	if f.BBox != nil {
		o.set("bbox", numbers(f.BBox))
	}
	setExtra(&o, f.Extra)
	return o, nil
}

func geometryObject(g *Geometry, path string) (object, error) {
	var o object
	o.set("type", g.Type)
	switch g.Type {
	case GeometryPolygon:
		rings := make([]any, 0, len(g.Rings))
		for _, r := range g.Rings {
			ring := make([]any, 0, len(r))
			for _, pt := range r {
				ring = append(ring, lonLat(pt))
			}
			rings = append(rings, ring)
		}
		o.set("coordinates", rings)
	case GeometryPoint:
		if g.Center == nil || g.RadiusM == nil {
			return nil, core.Fieldf(path, "a Point zone needs a centre and a radius")
		}
		o.set("coordinates", lonLat(*g.Center))
		var e object
		e.set("subType", "Circle")
		e.set("radius", number(*g.RadiusM))
		setExtra(&e, g.ExtentExtra)
		o.set("extent", e)
	case GeometryCollection:
		ms := make([]any, 0, len(g.Geometries))
		for i := range g.Geometries {
			m, err := geometryObject(&g.Geometries[i], index(path+".geometries", i))
			if err != nil {
				return nil, err
			}
			ms = append(ms, m)
		}
		o.set("geometries", ms)
	default:
		return nil, core.Fieldf(path+".type", "%q is not Polygon, Point or GeometryCollection", g.Type)
	}
	if g.Layer != nil {
		o.set("layer", layerObject(g.Layer))
	}
	if g.BBox != nil {
		o.set("bbox", numbers(g.BBox))
	}
	setExtra(&o, g.Extra)
	return o, nil
}

func layerObject(l *Layer) object {
	var o object
	if l.Upper != nil {
		o.set("upper", number(*l.Upper))
	}
	if l.UpperReference != "" {
		o.set("upperReference", string(l.UpperReference))
	}
	if l.Lower != nil {
		o.set("lower", number(*l.Lower))
	}
	if l.LowerReference != "" {
		o.set("lowerReference", string(l.LowerReference))
	}
	setString(&o, "uom", l.Uom)
	setExtra(&o, l.Extra)
	return o
}

// lonLat writes a position in GeoJSON order.
func lonLat(p core.LatLon) []any { return []any{number(p.LonDeg), number(p.LatDeg)} }

func zoneObject(z *UASZone) object {
	var o object
	o.set("identifier", z.Identifier)
	o.set("country", z.Country)
	setTexts(&o, "name", z.Name)
	o.set("type", string(z.Type))
	o.set("variant", z.Variant)
	setString(&o, "restrictionConditions", z.RestrictionConditions)
	if z.Region != nil {
		o.set("region", *z.Region)
	}
	if z.Reason != nil {
		rs := make([]any, 0, len(z.Reason))
		for _, r := range z.Reason {
			rs = append(rs, r)
		}
		o.set("reason", rs)
	}
	setTexts(&o, "otherReasonInfo", z.OtherReasonInfo)
	setString(&o, "regulationExemption", z.RegulationExemption)
	setTexts(&o, "message", z.Message)
	as := make([]any, 0, len(z.ZoneAuthority))
	for i := range z.ZoneAuthority {
		as = append(as, authorityObject(&z.ZoneAuthority[i]))
	}
	o.set("zoneAuthority", as)
	if z.LimitedApplicability != nil {
		ps := make([]any, 0, len(z.LimitedApplicability))
		for i := range z.LimitedApplicability {
			ps = append(ps, periodObject(&z.LimitedApplicability[i]))
		}
		o.set("limitedApplicability", ps)
	}
	if z.DataSource != nil {
		o.set("dataSource", sourceObject(z.DataSource))
	}
	if z.ExtendedProperties != nil {
		var x object
		setExtra(&x, z.ExtendedProperties)
		o.set("extendedProperties", x)
	}
	return o
}

func authorityObject(a *Authority) object {
	var o object
	setTexts(&o, "name", a.Name)
	setTexts(&o, "service", a.Service)
	setTexts(&o, "contactName", a.ContactName)
	setString(&o, "siteURL", a.SiteURL)
	setString(&o, "email", a.Email)
	setString(&o, "phone", a.Phone)
	o.set("purpose", a.Purpose)
	setString(&o, "intervalBefore", a.IntervalBefore)
	setExtra(&o, a.Extra)
	return o
}

func periodObject(p *TimePeriod) object {
	var o object
	setTime(&o, "startDateTime", p.StartDateTime)
	setTime(&o, "endDateTime", p.EndDateTime)
	if p.Schedule != nil {
		ss := make([]any, 0, len(p.Schedule))
		for i := range p.Schedule {
			ss = append(ss, dailyObject(&p.Schedule[i]))
		}
		o.set("schedule", ss)
	}
	return o
}

func dailyObject(d *DailyPeriod) object {
	var o object
	days := make([]any, 0, len(d.Day))
	for _, s := range d.Day {
		days = append(days, s)
	}
	o.set("day", days)
	setString(&o, "startTime", d.StartTime)
	setString(&o, "startEvent", d.StartEvent)
	setString(&o, "endTime", d.EndTime)
	setString(&o, "endEvent", d.EndEvent)
	return o
}

func sourceObject(d *DataSource) object {
	var o object
	setTime(&o, "creationDate", d.CreationDate)
	setTime(&o, "creationDateTime", d.CreationDateTime)
	setTime(&o, "updateDateTime", d.UpdateDateTime)
	if d.Originator != nil {
		o.set("originator", textObject(*d.Originator))
	}
	setExtra(&o, d.Extra)
	return o
}

// formatNumber writes f by value: a whole number without a fraction, any
// other in the shortest form that reads back as f.
func formatNumber(f float64, path string) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", core.Fieldf(path, "is not a finite number")
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

// encode writes v as compact JSON; path names the value for errors.
func encode(v any, b *bytes.Buffer, path string) error {
	switch t := v.(type) {
	case object:
		b.WriteByte('{')
		for i, m := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, m.k)
			b.WriteByte(':')
			sub := m.k
			if path != "$" {
				sub = path + "." + m.k
			}
			if err := encode(m.v, b, sub); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encode(e, b, index(path, i)); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case string:
		writeString(b, t)
	case number:
		s, err := formatNumber(float64(t), path)
		if err != nil {
			return err
		}
		b.WriteString(s)
	case int:
		b.WriteString(strconv.Itoa(t))
	case json.RawMessage:
		if err := json.Compact(b, t); err != nil {
			return core.Fieldf(path, "is not valid JSON")
		}
	}
	return nil
}
