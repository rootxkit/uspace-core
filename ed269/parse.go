package ed269

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rootxkit/uspace-core/core"
)

// Field lists, from InterUSS uas_standards eurocae_ed269.py.
var (
	featureFields = []string{
		"identifier", "country", "name", "type", "restriction", "reason",
		"message", "applicability", "zoneAuthority", "geometry",
		"restrictionConditions", "region", "otherReasonInfo",
		"regulationExemption", "uSpaceClass", "extendedProperties", "title",
	}
	authorityFields = []string{
		"name", "service", "contactName", "siteURL", "email", "phone",
		"purpose", "intervalBefore",
	}
	volumeFields = []string{
		"uomDimensions", "lowerLimit", "lowerVerticalReference",
		"upperLimit", "upperVerticalReference", "horizontalProjection",
	}
	featuresWrapperFields = []string{"title", "description", "features"}
	listWrapperFields     = []string{"formatVersion", "createdAt", "UASZoneList"}
)

// utf8BOM is the byte order mark Luxembourg's live file starts with.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Parse reads a whole ED-269 document strictly (LESSONS Z-01 to Z-06). It
// accepts a UTF-8 byte order mark and both published wrappers. A document
// is accepted whole or refused whole: on any problem the document is nil
// and every problem is listed (capped at lim.MaxProblems). A zero field
// of lim takes its DefaultLimits value.
func Parse(data []byte, lim Limits) (*Document, *Problems) {
	lim = lim.withDefaults()
	ps := &collector{max: lim.MaxProblems}
	if len(data) > lim.MaxBytes {
		ps.add("$", fmt.Sprintf("is %d bytes; at most %d", len(data), lim.MaxBytes))
		return nil, ps.result()
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		ps.add("$", "not UTF-8: "+firstInvalid(data))
		return nil, ps.result()
	}
	root := decodeTree(data, lim.MaxDepth, "", ps)
	if root == nil {
		return nil, ps.result()
	}
	if root.kind != kindObject {
		ps.add("$", "not a JSON object")
		return nil, ps.result()
	}
	p := &parser{lim: lim, ps: ps}
	doc := p.document(root)
	if r := ps.result(); r != nil {
		return nil, r
	}
	return doc, nil
}

// firstInvalid says where the first invalid UTF-8 byte is.
func firstInvalid(data []byte) string {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			return fmt.Sprintf("invalid byte 0x%02x at offset %d", data[i], i)
		}
		i += size
	}
	return "invalid encoding"
}

// ParseZone reads one `UASZoneVersion` on its own. Problems name paths
// under `zone`.
func ParseZone(raw json.RawMessage, lim Limits) (*GeoZone, *Problems) {
	lim = lim.withDefaults()
	ps := &collector{max: lim.MaxProblems}
	if len(raw) > lim.MaxBytes {
		ps.add("zone", fmt.Sprintf("is %d bytes; at most %d", len(raw), lim.MaxBytes))
		return nil, ps.result()
	}
	if !utf8.Valid(raw) {
		ps.add("zone", "not UTF-8: "+firstInvalid(raw))
		return nil, ps.result()
	}
	v := decodeTree(raw, lim.MaxDepth, "zone", ps)
	if v == nil {
		return nil, ps.result()
	}
	p := &parser{lim: lim, ps: ps}
	z := p.zone(v, "zone")
	if r := ps.result(); r != nil {
		return nil, r
	}
	return z, nil
}

// document reads the wrapper and every zone.
func (p *parser) document(root *value) *Document {
	doc := &Document{}
	var allowed []string
	switch {
	case root.get(WrapperFeatures) != nil:
		doc.Wrapper, allowed = WrapperFeatures, featuresWrapperFields
	case root.get(WrapperUASZoneList) != nil:
		doc.Wrapper, allowed = WrapperUASZoneList, listWrapperFields
	default:
		p.ps.add("features", "missing: an ED-269 document lists its zones here")
		return nil
	}
	p.unknown(root, "", allowed, "unknown field; not part of an ED-269 document")
	if doc.Wrapper == WrapperFeatures {
		doc.Title = p.optionalText(root.get("title"), "title", 0)
		doc.Description = p.optionalText(root.get("description"), "description", 0)
	} else {
		doc.FormatVersion = p.optionalText(root.get("formatVersion"), "formatVersion", 0)
		doc.CreatedAt = p.optionalText(root.get("createdAt"), "createdAt", 0)
	}
	list := root.get(doc.Wrapper)
	if list.kind != kindArray {
		p.ps.add(doc.Wrapper, "must be a list of zones")
		return nil
	}
	doc.Zones = make([]GeoZone, 0, len(list.arr))
	firstSeen := make(map[string]int, len(list.arr))
	for i, raw := range list.arr {
		path := index(doc.Wrapper, i)
		before := p.ps.count()
		z := p.zone(raw, path)
		id, ok := raw.identifierText()
		if ok {
			if first, dup := firstSeen[id]; dup {
				p.ps.add(join(path, "identifier"),
					fmt.Sprintf("%s is also the identifier of %s", quote(id), index(doc.Wrapper, first)))
			} else {
				firstSeen[id] = i
			}
		}
		if z != nil && p.ps.count() == before {
			doc.Zones = append(doc.Zones, *z)
		}
	}
	return doc
}

// identifierText is a zone's identifier when it is a string, for the
// duplicate check.
func (v *value) identifierText() (string, bool) {
	if v.kind != kindObject {
		return "", false
	}
	id := v.get("identifier")
	if id == nil || id.kind != kindString {
		return "", false
	}
	return id.s, true
}

// zone reads one UASZoneVersion; nil when it has any problem.
func (p *parser) zone(v *value, path string) *GeoZone {
	if v.kind != kindObject {
		p.ps.add(path, "a zone must be an object")
		return nil
	}
	before := p.ps.count()
	p.unknown(v, path, featureFields, "unknown field; not part of ED-269")
	z := &GeoZone{}
	identifier, okID := p.requiredText(v, "identifier", path, p.lim.IdentifierMax)
	if okID && identifier != strings.TrimSpace(identifier) {
		p.ps.add(join(path, "identifier"), "has leading or trailing spaces")
	}
	z.Identifier = identifier
	country, okCountry := p.requiredText(v, "country", path, 3)
	if okCountry && !isCountry(country) {
		p.ps.add(join(path, "country"), quote(country)+" is not an ISO 3166-1 alpha-3 code")
	}
	z.Country = country
	z.Name = p.optionalText(v.get("name"), join(path, "name"), p.lim.NameMax)
	if t := v.get("type"); !present(t) {
		p.ps.add(join(path, "type"), "missing: required")
	} else if s, ok := p.enum(t, join(path, "type"), typeValues); ok {
		z.Type = s
	}
	z.Restriction = p.restriction(v.get("restriction"), join(path, "restriction"))
	z.Reason = p.reasons(v.get("reason"), join(path, "reason"))
	z.Message = p.optionalText(v.get("message"), join(path, "message"), p.lim.MessageMax)
	z.ZoneAuthority = p.authorities(v.get("zoneAuthority"), join(path, "zoneAuthority"))
	z.Applicability = p.applicability(v.get("applicability"), join(path, "applicability"))
	if vol, ok := p.geometry(v.get("geometry"), join(path, "geometry")); ok {
		z.Geometry = []Volume{vol}
	}
	p.extra(v, path, z)
	if p.ps.count() > before {
		return nil
	}
	return z
}

// isCountry reports whether s is three ASCII upper-case letters.
func isCountry(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := range 3 {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// requiredText reads a required, non-blank string member of v.
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

// text checks that v is a string of at most maxLen characters (0: no
// limit).
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

// restriction reads the required restriction, refusing the Z spelling of
// REQ_AUTHORISATION by name (LESSONS Z-04).
func (p *parser) restriction(v *value, where string) Restriction {
	if !present(v) {
		p.ps.add(where, "missing: required")
		return ""
	}
	if v.kind == kindString && v.s == "REQ_AUTHORIZATION" {
		p.ps.add(where, "'REQ_AUTHORIZATION' is not an ED-269 value; ED-269 spells it REQ_AUTHORISATION")
		return ""
	}
	s, _ := p.enum(v, where, restrictionValues)
	return Restriction(s)
}

// reasons reads the optional reason list: no repeats, at most ReasonsMax.
func (p *parser) reasons(v *value, where string) []Reason {
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
	out := make([]Reason, 0, len(v.arr))
	for i, item := range v.arr {
		here := index(where, i)
		s, ok := p.enum(item, here, reasonValues)
		if !ok {
			continue
		}
		if slices.Contains(out, Reason(s)) {
			p.ps.add(here, s+" is listed twice")
			continue
		}
		out = append(out, Reason(s))
	}
	return out
}

// authorities reads the required zoneAuthority list (which may be empty).
func (p *parser) authorities(v *value, where string) []Authority {
	if !present(v) {
		p.ps.add(where, "missing: required (a list, which may be empty)")
		return nil
	}
	if v.kind != kindArray {
		p.ps.add(where, "must be a list, not "+describe(v))
		return nil
	}
	out := make([]Authority, 0, len(v.arr))
	for i, raw := range v.arr {
		here := index(where, i)
		if raw.kind != kindObject {
			p.ps.add(here, "an authority must be an object")
			continue
		}
		var a Authority
		for k, name := range raw.keys {
			item := raw.vals[k]
			at := join(here, name)
			if !slices.Contains(authorityFields, name) {
				p.ps.add(at, "unknown field; not part of ED-269")
				continue
			}
			if !present(item) {
				continue
			}
			if name == "purpose" {
				if s, ok := p.enum(item, at, purposeValues); ok {
					pu := Purpose(s)
					a.Purpose = &pu
				}
				continue
			}
			limit := 0
			switch name {
			case "name", "service", "contactName", "phone":
				limit = p.lim.AuthorityTextMax
			}
			s := p.optionalText(item, at, limit)
			switch name {
			case "name":
				a.Name = s
			case "service":
				a.Service = s
			case "contactName":
				a.ContactName = s
			case "siteURL":
				a.SiteURL = s
			case "email":
				a.Email = s
			case "phone":
				a.Phone = s
			case "intervalBefore":
				a.IntervalBefore = s
			}
		}
		out = append(out, a)
	}
	return out
}

// geometry reads the required volume list, which must hold exactly one
// volume (LESSONS Z-04).
func (p *parser) geometry(v *value, where string) (Volume, bool) {
	if !present(v) {
		p.ps.add(where, "missing: required, one airspace volume")
		return Volume{}, false
	}
	if v.kind != kindArray || len(v.arr) == 0 {
		p.ps.add(where, "must be a list of one airspace volume")
		return Volume{}, false
	}
	if len(v.arr) > 1 {
		p.ps.add(where, fmt.Sprintf(
			"has %d volumes; one volume per zone is supported (publish each volume as its own zone)", len(v.arr)))
		return Volume{}, false
	}
	return p.volume(v.arr[0], index(where, 0))
}

// volume reads one UASZoneAirspaceVolume.
func (p *parser) volume(v *value, where string) (Volume, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a volume must be an object")
		return Volume{}, false
	}
	before := p.ps.count()
	p.unknown(v, where, volumeFields, "unknown field; not part of ED-269")
	uom, _ := p.enum(v.get("uomDimensions"), join(where, "uomDimensions"), uomValues)
	lower := p.limit(v.get("lowerLimit"), join(where, "lowerLimit"))
	upper := p.limit(v.get("upperLimit"), join(where, "upperLimit"))
	lowerRef, _ := p.enum(v.get("lowerVerticalReference"), join(where, "lowerVerticalReference"), refValues)
	upperRef, _ := p.enum(v.get("upperVerticalReference"), join(where, "upperVerticalReference"), refValues)
	proj, okProj := p.projection(v.get("horizontalProjection"), join(where, "horizontalProjection"))
	if p.ps.count() > before || !okProj {
		return Volume{}, false
	}
	if why := longitudeSpan(proj, Uom(uom)); why != "" {
		p.ps.add(join(where, "horizontalProjection"), why)
		return Volume{}, false
	}
	if lower != nil && upper != nil && lowerRef == upperRef && *lower >= *upper {
		p.ps.add(join(where, "upperLimit"), "is not above lowerLimit")
		return Volume{}, false
	}
	if lower == nil && upper != nil && *upper == 0 {
		p.ps.add(join(where, "upperLimit"), "is 0 with no lowerLimit; a volume from the surface to 0 is empty")
		return Volume{}, false
	}
	return Volume{
		Uom:        Uom(uom),
		LowerLimit: lower,
		UpperLimit: upper,
		LowerRef:   VerticalRef(lowerRef),
		UpperRef:   VerticalRef(upperRef),
		Projection: proj,
	}, true
}

// limit reads an optional vertical limit: a JSON number, never a string
// (LESSONS Z-01: "0" is refused, not converted), and never negative.
func (p *parser) limit(v *value, where string) *float64 {
	if !present(v) {
		return nil
	}
	if v.kind != kindNumber {
		p.ps.add(where, "must be a number, not "+describe(v))
		return nil
	}
	f, ok := v.float()
	if !ok {
		p.ps.add(where, "must be a number within the range of a double")
		return nil
	}
	if f < 0 {
		p.ps.add(where, "must not be negative")
		return nil
	}
	return &f
}

// projection reads a Polygon or a Circle.
func (p *parser) projection(v *value, where string) (HorizontalProjection, bool) {
	if !present(v) || v.kind != kindObject {
		p.ps.add(where, "missing: required, a Polygon or a Circle")
		return HorizontalProjection{}, false
	}
	t := v.get("type")
	var allowed []string
	switch {
	case t != nil && t.kind == kindString && t.s == ShapePolygon:
		allowed = []string{"type", "coordinates"}
	case t != nil && t.kind == kindString && t.s == ShapeCircle:
		allowed = []string{"type", "center", "radius"}
	default:
		p.ps.add(join(where, "type"), show(t)+" is not Polygon or Circle")
		return HorizontalProjection{}, false
	}
	if !p.unknown(v, where, allowed, "not a field of a "+t.s) {
		return HorizontalProjection{}, false
	}
	if t.s == ShapeCircle {
		center, okCenter := p.position(v.get("center"), join(where, "center"))
		radius, okRadius := v.get("radius").float()
		if !okRadius || radius <= 0 {
			p.ps.add(join(where, "radius"), "must be a number above 0")
			return HorizontalProjection{}, false
		}
		if !okCenter {
			return HorizontalProjection{}, false
		}
		return HorizontalProjection{Type: ShapeCircle, Center: &center, Radius: &radius}, true
	}
	coords := v.get("coordinates")
	if !present(coords) || coords.kind != kindArray || len(coords.arr) == 0 {
		p.ps.add(join(where, "coordinates"), "must be a list of rings")
		return HorizontalProjection{}, false
	}
	rings := make([][]Position, 0, len(coords.arr))
	ok := true
	for i, raw := range coords.arr {
		ring, good := p.ring(raw, index(join(where, "coordinates"), i))
		if !good {
			ok = false
			continue
		}
		rings = append(rings, ring)
	}
	if !ok {
		return HorizontalProjection{}, false
	}
	return HorizontalProjection{Type: ShapePolygon, Rings: rings}, true
}

// position reads a GeoJSON [longitude, latitude] (LESSONS Z-03).
func (p *parser) position(v *value, where string) (Position, bool) {
	if !present(v) || v.kind != kindArray || len(v.arr) != 2 {
		p.ps.add(where, "must be [longitude, latitude]")
		return Position{}, false
	}
	lon, okLon := v.arr[0].float()
	lat, okLat := v.arr[1].float()
	if !okLon || !okLat {
		p.ps.add(where, "must be two numbers, [longitude, latitude]")
		return Position{}, false
	}
	if lon < -180 || lon > 180 || lat < -90 || lat > 90 {
		p.ps.add(where, "["+clip(v.arr[0].s)+", "+clip(v.arr[1].s)+"] is outside longitude and latitude ranges")
		return Position{}, false
	}
	return Position{LatDeg: lat, LonDeg: lon}, true
}

// ring reads one closed ring of at least four positions, at most
// MaxRingVertices (LESSONS Z-06).
func (p *parser) ring(v *value, where string) ([]Position, bool) {
	if v.kind != kindArray {
		p.ps.add(where, "a ring must be a list of positions")
		return nil, false
	}
	if len(v.arr) > p.lim.MaxRingVertices {
		p.ps.add(where, fmt.Sprintf("has %d positions; at most %d per ring", len(v.arr), p.lim.MaxRingVertices))
		return nil, false
	}
	points := make([]Position, 0, len(v.arr))
	for i, raw := range v.arr {
		pt, ok := p.position(raw, index(where, i))
		if !ok {
			return nil, false
		}
		points = append(points, pt)
	}
	if len(points) < 4 {
		p.ps.add(where, "a ring needs at least four positions, closed")
		return nil, false
	}
	if points[0] != points[len(points)-1] {
		p.ps.add(where, "is not closed: the last position must repeat the first")
		return nil, false
	}
	if distinctAtLeast(points, 3) < 3 {
		p.ps.add(where, "needs at least three distinct positions")
		return nil, false
	}
	return points, true
}

// longitudeSpan refuses a shape that spans more than 180 degrees of
// longitude or crosses the antimeridian: geodesy's polygon containment
// would judge it wrongly without an error. A circle's span is its
// radius over the parallel's radius at its centre (a sphere is close
// enough to decide this). It returns the reason, or "" when the shape is
// fine.
func longitudeSpan(hp HorizontalProjection, uom Uom) string {
	const why = "longitude span exceeds 180° or crosses the antimeridian; not supported"
	switch hp.Type {
	case ShapePolygon:
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, r := range hp.Rings {
			for _, pt := range r {
				lo, hi = math.Min(lo, pt.LonDeg), math.Max(hi, pt.LonDeg)
			}
		}
		if hi-lo > 180 {
			return fmt.Sprintf("%s (the rings span %g°)", why, hi-lo)
		}
	case ShapeCircle:
		radiusM := *hp.Radius
		if uom == UomFeet {
			radiusM *= core.FeetToMetres
		}
		parallelM := core.MeanEarthRadiusM * math.Cos(hp.Center.LatDeg*math.Pi/180)
		if parallelM <= 0 {
			return why + " (the circle is centred on a pole)"
		}
		halfDeg := radiusM / parallelM * 180 / math.Pi
		if 2*halfDeg > 180 || hp.Center.LonDeg-halfDeg < -180 || hp.Center.LonDeg+halfDeg > 180 {
			return why + " (the circle reaches past ±180° longitude)"
		}
	}
	return ""
}

// distinctAtLeast counts distinct positions, stopping at n.
func distinctAtLeast(points []Position, n int) int {
	seen := make([]Position, 0, n)
	for _, pt := range points {
		pt.LatDeg += 0 // -0 and 0 are one position
		pt.LonDeg += 0
		if !slices.Contains(seen, pt) {
			seen = append(seen, pt)
			if len(seen) >= n {
				break
			}
		}
	}
	return len(seen)
}

// extra reads the published fields this package carries without
// interpreting them; they are written back unchanged.
func (p *parser) extra(v *value, path string, z *GeoZone) {
	if c := v.get("restrictionConditions"); present(c) {
		where := join(path, "restrictionConditions")
		switch {
		case c.kind == kindString:
			z.RestrictionConditions, z.RestrictionConditionsIsText = []string{c.s}, true
		case c.kind == kindArray && allStrings(c.arr):
			z.RestrictionConditions = make([]string, 0, len(c.arr))
			for _, e := range c.arr {
				z.RestrictionConditions = append(z.RestrictionConditions, e.s)
			}
		default:
			p.ps.add(where, "must be a string or a list of strings")
		}
	}
	if r := v.get("region"); present(r) {
		where := join(path, "region")
		switch n, err := strconv.Atoi(r.s); {
		case r.kind != kindNumber:
			p.ps.add(where, "must be an integer, not "+describe(r))
		case strings.ContainsAny(r.s, ".eE"):
			p.ps.add(where, "must be an integer, not "+r.s)
		case err != nil:
			p.ps.add(where, "must be an integer within the range of an int, not "+r.s)
		default:
			z.Region = &n
		}
	}
	if e := v.get("regulationExemption"); present(e) {
		if s, ok := p.enum(e, join(path, "regulationExemption"), yesNoValues); ok {
			yn := YesNo(s)
			z.RegulationExemption = &yn
		}
	}
	z.OtherReasonInfo = p.optionalText(v.get("otherReasonInfo"), join(path, "otherReasonInfo"), p.lim.OtherReasonMax)
	z.USpaceClass = p.optionalText(v.get("uSpaceClass"), join(path, "uSpaceClass"), p.lim.USpaceClassMax)
	z.Title = p.optionalText(v.get("title"), join(path, "title"), 0)
	if x := v.get("extendedProperties"); present(x) {
		var b bytes.Buffer
		compact(x, &b)
		z.ExtendedProperties = json.RawMessage(b.Bytes())
	}
}

func allStrings(vs []*value) bool {
	for _, v := range vs {
		if v.kind != kindString {
			return false
		}
	}
	return true
}
