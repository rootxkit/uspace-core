package ed269

import (
	"bytes"
	"encoding/json"
	"math"
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

// number is a JSON number to be written by value (LESSONS Z-01: 5.0 is
// written as 5).
type number float64

// Feature returns z as an ED-269 `UASZoneVersion`, ready for
// json.Marshal: numbers are json.Number written by value, null and absent
// optional fields are left out, and extendedProperties is decoded as
// published. Feature(zone of parse(f)) equals the zone of f by value.
func Feature(z *GeoZone) (map[string]any, error) {
	o, err := featureObject(z, "zone")
	if err != nil {
		return nil, err
	}
	m, err := toPlain(o)
	if err != nil {
		return nil, err
	}
	out, _ := m.(map[string]any)
	return out, nil
}

// Export writes doc as an ED-269 document in the wrapper it was read
// with (`features` when Wrapper is empty), so that Export(Parse(f)) equals
// f by value: members are in ED-269 order, null and absent optional
// fields are absent and numbers are written by value. It fails, naming
// the field, only for a document that Parse could not have produced (a
// non-finite number, an unknown shape, invalid extendedProperties).
func Export(doc *Document) ([]byte, error) {
	if doc == nil {
		return nil, core.Fieldf("$", "no document")
	}
	wrapper := doc.Wrapper
	if wrapper == "" {
		wrapper = WrapperFeatures
	}
	if wrapper != WrapperFeatures && wrapper != WrapperUASZoneList {
		return nil, core.Fieldf("$", "unknown wrapper %q; features or UASZoneList", wrapper)
	}
	var o object
	if wrapper == WrapperFeatures {
		setText(&o, "title", doc.Title)
		setText(&o, "description", doc.Description)
	} else {
		setText(&o, "formatVersion", doc.FormatVersion)
		setText(&o, "createdAt", doc.CreatedAt)
	}
	zones := make([]any, 0, len(doc.Zones))
	for i := range doc.Zones {
		f, err := featureObject(&doc.Zones[i], index(wrapper, i))
		if err != nil {
			return nil, err
		}
		zones = append(zones, f)
	}
	o.set(wrapper, zones)
	var b bytes.Buffer
	if err := encode(o, &b, "$"); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func setText(o *object, k string, s *string) {
	if s != nil {
		o.set(k, *s)
	}
}

func setNumber(o *object, k string, f *float64) {
	if f != nil {
		o.set(k, number(*f))
	}
}

// featureObject builds one zone in ED-269 member order.
func featureObject(z *GeoZone, path string) (object, error) {
	var o object
	o.set("identifier", z.Identifier)
	o.set("country", z.Country)
	setText(&o, "name", z.Name)
	o.set("type", z.Type)
	o.set("restriction", string(z.Restriction))
	if z.Reason != nil {
		rs := make([]any, 0, len(z.Reason))
		for _, r := range z.Reason {
			rs = append(rs, string(r))
		}
		o.set("reason", rs)
	}
	setText(&o, "message", z.Message)
	ps := make([]any, 0, len(z.Applicability))
	for i := range z.Applicability {
		ps = append(ps, periodObject(&z.Applicability[i]))
	}
	o.set("applicability", ps)
	as := make([]any, 0, len(z.ZoneAuthority))
	for i := range z.ZoneAuthority {
		as = append(as, authorityObject(&z.ZoneAuthority[i]))
	}
	o.set("zoneAuthority", as)
	vs := make([]any, 0, len(z.Geometry))
	for i := range z.Geometry {
		v, err := volumeObject(&z.Geometry[i], index(join(path, "geometry"), i))
		if err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	o.set("geometry", vs)
	if z.RestrictionConditions != nil {
		if z.RestrictionConditionsIsText && len(z.RestrictionConditions) == 1 {
			o.set("restrictionConditions", z.RestrictionConditions[0])
		} else {
			cs := make([]any, 0, len(z.RestrictionConditions))
			for _, c := range z.RestrictionConditions {
				cs = append(cs, c)
			}
			o.set("restrictionConditions", cs)
		}
	}
	if z.Region != nil {
		o.set("region", *z.Region)
	}
	setText(&o, "otherReasonInfo", z.OtherReasonInfo)
	if z.RegulationExemption != nil {
		o.set("regulationExemption", string(*z.RegulationExemption))
	}
	setText(&o, "uSpaceClass", z.USpaceClass)
	if z.ExtendedProperties != nil {
		if !json.Valid(z.ExtendedProperties) {
			return nil, core.Fieldf(join(path, "extendedProperties"), "is not valid JSON")
		}
		o.set("extendedProperties", z.ExtendedProperties)
	}
	setText(&o, "title", z.Title)
	return o, nil
}

func authorityObject(a *Authority) object {
	var o object
	setText(&o, "name", a.Name)
	setText(&o, "service", a.Service)
	setText(&o, "contactName", a.ContactName)
	setText(&o, "siteURL", a.SiteURL)
	setText(&o, "email", a.Email)
	setText(&o, "phone", a.Phone)
	if a.Purpose != nil {
		o.set("purpose", string(*a.Purpose))
	}
	setText(&o, "intervalBefore", a.IntervalBefore)
	return o
}

func volumeObject(v *Volume, path string) (object, error) {
	var o object
	o.set("uomDimensions", string(v.Uom))
	setNumber(&o, "lowerLimit", v.LowerLimit)
	o.set("lowerVerticalReference", string(v.LowerRef))
	setNumber(&o, "upperLimit", v.UpperLimit)
	o.set("upperVerticalReference", string(v.UpperRef))
	var hp object
	hp.set("type", v.Projection.Type)
	switch v.Projection.Type {
	case ShapeCircle:
		if v.Projection.Center == nil || v.Projection.Radius == nil {
			return nil, core.Fieldf(join(path, "horizontalProjection"), "a Circle needs a center and a radius")
		}
		hp.set("center", lonLat(*v.Projection.Center))
		hp.set("radius", number(*v.Projection.Radius))
	case ShapePolygon:
		rings := make([]any, 0, len(v.Projection.Rings))
		for _, r := range v.Projection.Rings {
			ring := make([]any, 0, len(r))
			for _, pt := range r {
				ring = append(ring, lonLat(pt))
			}
			rings = append(rings, ring)
		}
		hp.set("coordinates", rings)
	default:
		return nil, core.Fieldf(join(path, "horizontalProjection.type"), "%q is not Polygon or Circle", v.Projection.Type)
	}
	o.set("horizontalProjection", hp)
	return o, nil
}

// lonLat writes a position in GeoJSON order (LESSONS Z-03).
func lonLat(p Position) []any { return []any{number(p.LonDeg), number(p.LatDeg)} }

func periodObject(p *Period) object {
	var o object
	if p.Permanent {
		o.set("permanent", string(Yes))
	} else {
		o.set("permanent", string(No))
	}
	if p.Start != nil {
		o.set("startDateTime", textOr(p.startText, p.Start.Format(time.RFC3339Nano)))
	}
	if p.End != nil {
		o.set("endDateTime", textOr(p.endText, p.End.Format(time.RFC3339Nano)))
	}
	if p.Schedule != nil {
		ss := make([]any, 0, len(p.Schedule))
		for i := range p.Schedule {
			ss = append(ss, dailyObject(&p.Schedule[i]))
		}
		o.set("schedule", ss)
	}
	return o
}

const clockLayout = "15:04:05.999999Z07:00"

func dailyObject(d *DailyPeriod) object {
	var o object
	days := make([]any, 0, 7)
	switch {
	case d.dayText != nil:
		for _, s := range d.dayText {
			days = append(days, s)
		}
	case len(d.Days) == 7:
		days = append(days, anyDay)
	default:
		for _, name := range dayNames {
			for _, wd := range d.Days {
				if weekdayOf[name] == wd {
					days = append(days, name)
					break
				}
			}
		}
	}
	o.set("day", days)
	o.set("startTime", textOr(d.startText, d.Start.Format(clockLayout)))
	o.set("endTime", textOr(d.endText, d.End.Format(clockLayout)))
	return o
}

func textOr(text, fallback string) string {
	if text != "" {
		return text
	}
	return fallback
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

// encode writes v as compact JSON. path names the value for errors.
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
			if err := encode(m.v, b, join(trimRoot(path), m.k)); err != nil {
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

func trimRoot(path string) string {
	if path == "$" {
		return ""
	}
	return path
}

// toPlain converts an ordered object tree into maps and slices for
// json.Marshal.
func toPlain(v any) (any, error) {
	switch t := v.(type) {
	case object:
		m := make(map[string]any, len(t))
		for _, e := range t {
			x, err := toPlain(e.v)
			if err != nil {
				return nil, err
			}
			m[e.k] = x
		}
		return m, nil
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			x, err := toPlain(e)
			if err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, nil
	case number:
		s, err := formatNumber(float64(t), "zone")
		if err != nil {
			return nil, err
		}
		return json.Number(s), nil
	case json.RawMessage:
		dec := json.NewDecoder(bytes.NewReader(t))
		dec.UseNumber()
		var x any
		if err := dec.Decode(&x); err != nil {
			return nil, core.Fieldf("zone.extendedProperties", "is not valid JSON")
		}
		return x, nil
	}
	return v, nil
}
