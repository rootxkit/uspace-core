package ed318

import (
	"fmt"
	"math"
	"slices"

	"github.com/rootxkit/uspace-core/core"
)

var (
	polygonFields    = []string{"type", "coordinates", "layer", "bbox"}
	pointFields      = []string{"type", "coordinates", "extent", "layer", "bbox"}
	collectionGFlds  = []string{"type", "geometries", "layer", "bbox"}
	extentFields     = []string{"subType", "radius"}
	layerFields      = []string{"upper", "upperReference", "lower", "lowerReference", "uom"}
	verticalRefs     = []string{"AGL", "AMSL", "WGS84"}
	uomValues        = []string{UomMetres, UomFeet}
	unsupportedTypes = []string{"LineString", "MultiPoint", "MultiLineString"}
)

// geometry reads a zone's geometry: a Polygon, a Point with a Circle
// extent, or (at the top only) a GeometryCollection of those.
func (p *parser) geometry(v *value, where string, member bool) (Geometry, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a geometry must be an object")
		return Geometry{}, false
	}
	t := v.get("type")
	if !present(t) || t.kind != kindString {
		p.ps.add(join(where, "type"), "missing: Polygon, Point or GeometryCollection")
		return Geometry{}, false
	}
	before := p.ps.count()
	var g Geometry
	switch {
	case t.s == GeometryPolygon:
		g = Geometry{Type: t.s, Extra: extras(v, polygonFields)}
		g.Rings = p.rings(v.get("coordinates"), join(where, "coordinates"))
	case t.s == GeometryPoint:
		g = Geometry{Type: t.s, Extra: extras(v, pointFields)}
		p.circle(v, where, &g)
	case t.s == GeometryCollection && !member:
		g = Geometry{Type: t.s, Extra: extras(v, collectionGFlds)}
		list := v.get("geometries")
		if !present(list) || list.kind != kindArray || len(list.arr) == 0 {
			p.ps.add(join(where, "geometries"), "must be a list of at least one geometry")
			return Geometry{}, false
		}
		for i, raw := range list.arr {
			if m, ok := p.geometry(raw, index(join(where, "geometries"), i), true); ok {
				g.Geometries = append(g.Geometries, m)
			}
		}
	case t.s == GeometryCollection:
		p.ps.add(join(where, "type"), "a GeometryCollection inside a GeometryCollection is not allowed")
		return Geometry{}, false
	case t.s == "MultiPolygon":
		// Not in this release (owner decision on PR #16): refused whole,
		// never imported part by part.
		p.ps.add(join(where, "type"), "'MultiPolygon' is not supported in this release; the zone is refused whole, not imported part by part: publish each polygon as its own feature")
		return Geometry{}, false
	case slices.Contains(unsupportedTypes, t.s):
		p.ps.add(join(where, "type"), quote(t.s)+" is not supported for a UAS zone; give a Polygon, a Point with a Circle extent, or a GeometryCollection of those")
		return Geometry{}, false
	default:
		p.ps.add(join(where, "type"), quote(t.s)+" is not a GeoJSON geometry type")
		return Geometry{}, false
	}
	g.BBox = p.bbox(v.get("bbox"), join(where, "bbox"))
	if l := v.get("layer"); present(l) {
		g.Layer = p.layer(l, join(where, "layer"))
	}
	if g.Type == GeometryCollection && g.Layer != nil {
		for i := range g.Geometries {
			if g.Geometries[i].Layer != nil {
				p.ps.add(index(join(where, "geometries"), i)+".layer",
					"is given with the collection's layer; give the layer once, on the collection or on each geometry")
			}
		}
	}
	if p.ps.count() > before {
		return Geometry{}, false
	}
	if why := longitudeSpan(g); why != "" {
		p.ps.add(where, why)
		return Geometry{}, false
	}
	return g, true
}

// circle reads a Point's position and its Circle extent.
func (p *parser) circle(v *value, where string, g *Geometry) {
	if c, ok := p.position(v.get("coordinates"), join(where, "coordinates")); ok {
		g.Center = &c
	}
	e := v.get("extent")
	if !present(e) || e.kind != kindObject {
		p.ps.add(join(where, "extent"), "missing: a Point zone needs an extent {subType: Circle, radius}")
		return
	}
	g.ExtentExtra = extras(e, extentFields)
	st := e.get("subType")
	if !present(st) || st.kind != kindString || st.s != "Circle" {
		p.ps.add(join(where, "extent.subType"), "must be 'Circle', not "+show(st))
	}
	r, ok := e.get("radius").float()
	if !ok || r <= 0 {
		p.ps.add(join(where, "extent.radius"), "must be a number above 0")
		return
	}
	if r > MaxCircleRadiusM {
		p.ps.add(join(where, "extent.radius"), fmt.Sprintf("%v m is above %v m, too large for a UAS zone", r, float64(MaxCircleRadiusM)))
		return
	}
	g.RadiusM = &r
}

// MaxCircleRadiusM caps a circle's radius (1000 km): a larger one is not
// a plausible UAS zone and is refused rather than judged. The radius is
// always in metres, whatever the layer's uom (which governs the vertical
// limits only); see doc.go, UNVERIFIED.
const MaxCircleRadiusM = 1_000_000

// position reads a GeoJSON [longitude, latitude]. A third member
// (an altitude) is refused: a zone's vertical extent is its layer.
func (p *parser) position(v *value, where string) (core.LatLon, bool) {
	if !present(v) || v.kind != kindArray || len(v.arr) != 2 {
		p.ps.add(where, "must be [longitude, latitude]")
		return core.LatLon{}, false
	}
	lon, okLon := v.arr[0].float()
	lat, okLat := v.arr[1].float()
	if !okLon || !okLat {
		p.ps.add(where, "must be two numbers, [longitude, latitude]")
		return core.LatLon{}, false
	}
	if lon < -180 || lon > 180 || lat < -90 || lat > 90 {
		p.ps.add(where, fmt.Sprintf("[%v, %v] is outside longitude and latitude ranges", lon, lat))
		return core.LatLon{}, false
	}
	return core.LatLon{LatDeg: lat, LonDeg: lon}, true
}

// rings reads a Polygon's coordinates: one or more closed rings.
func (p *parser) rings(v *value, where string) [][]core.LatLon {
	if !present(v) || v.kind != kindArray || len(v.arr) == 0 {
		p.ps.add(where, "must be a list of rings")
		return nil
	}
	out := make([][]core.LatLon, 0, len(v.arr))
	for i, raw := range v.arr {
		if r, ok := p.ring(raw, index(where, i)); ok {
			out = append(out, r)
		}
	}
	return out
}

// ring reads one closed ring of at least four positions, at most
// MaxRingVertices, with three distinct (LESSONS Z-06).
func (p *parser) ring(v *value, where string) ([]core.LatLon, bool) {
	if v.kind != kindArray {
		p.ps.add(where, "a ring must be a list of positions")
		return nil, false
	}
	if len(v.arr) > p.lim.MaxRingVertices {
		p.ps.add(where, fmt.Sprintf("has %d positions; at most %d per ring", len(v.arr), p.lim.MaxRingVertices))
		return nil, false
	}
	pts := make([]core.LatLon, 0, len(v.arr))
	for i, raw := range v.arr {
		pt, ok := p.position(raw, index(where, i))
		if !ok {
			return nil, false
		}
		pts = append(pts, pt)
	}
	if len(pts) < minRingCount {
		p.ps.add(where, "a ring needs at least four positions, closed")
		return nil, false
	}
	if pts[0] != pts[len(pts)-1] {
		p.ps.add(where, "is not closed: the last position must repeat the first")
		return nil, false
	}
	if distinct(pts) < 3 {
		p.ps.add(where, "needs at least three distinct positions")
		return nil, false
	}
	return pts, true
}

// distinct counts distinct positions, stopping at three.
func distinct(pts []core.LatLon) int {
	seen := make([]core.LatLon, 0, 3)
	for _, pt := range pts {
		pt.LatDeg += 0 // -0 and 0 are one position
		pt.LonDeg += 0
		if !slices.Contains(seen, pt) {
			seen = append(seen, pt)
			if len(seen) == 3 {
				break
			}
		}
	}
	return len(seen)
}

// layer reads a geometry's vertical layer.
func (p *parser) layer(v *value, where string) *Layer {
	if v.kind != kindObject {
		p.ps.add(where, "must be an object, not "+describe(v))
		return nil
	}
	before := p.ps.count()
	l := &Layer{Extra: extras(v, layerFields)}
	if u := v.get("uom"); present(u) {
		if s, ok := p.requiredEnum(u, join(where, "uom"), uomValues); ok {
			l.Uom = &s
		}
	}
	l.Upper, l.UpperReference = p.limit(v, where, "upper", "upperReference")
	l.Lower, l.LowerReference = p.limit(v, where, "lower", "lowerReference")
	if p.ps.count() > before {
		return nil
	}
	if l.Lower != nil && l.Upper != nil && l.LowerReference == l.UpperReference && *l.Lower >= *l.Upper {
		p.ps.add(join(where, "upper"), "is not above lower")
		return nil
	}
	if l.Lower == nil && l.Upper != nil && *l.Upper == 0 {
		p.ps.add(join(where, "upper"), "is 0 with no lower; a layer from the surface to 0 is empty")
		return nil
	}
	return l
}

// limit reads one vertical limit and its reference. A value without a
// reference is refused: a limit is judged in its datum (D-01).
func (p *parser) limit(v *value, where, key, refKey string) (*float64, core.VerticalRef) {
	var ref core.VerticalRef
	if r := v.get(refKey); present(r) {
		if s, ok := p.requiredEnum(r, join(where, refKey), verticalRefs); ok {
			ref = core.VerticalRef(s)
		}
	}
	n := v.get(key)
	if !present(n) {
		return nil, ref
	}
	f, ok := n.float()
	if !ok {
		p.ps.add(join(where, key), "must be a finite number, not "+show(n))
		return nil, ref
	}
	if !present(v.get(refKey)) {
		p.ps.add(join(where, refKey), "missing: "+key+" is given without its reference (AGL, AMSL or WGS84)")
	}
	return &f, ref
}

// longitudeSpan refuses a shape that spans more than 180 degrees of
// longitude or a circle that reaches past +-180 degrees, as ed269 does:
// geodesy's containment would judge it wrongly without an error.
func longitudeSpan(g Geometry) string {
	const why = "longitude span exceeds 180° or crosses the antimeridian; not supported"
	switch g.Type {
	case GeometryPolygon:
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, r := range g.Rings {
			for _, pt := range r {
				lo, hi = math.Min(lo, pt.LonDeg), math.Max(hi, pt.LonDeg)
			}
		}
		if hi-lo > 180 {
			return fmt.Sprintf("%s (the rings span %g°)", why, hi-lo)
		}
	case GeometryPoint:
		if g.Center == nil || g.RadiusM == nil {
			return ""
		}
		// At a pole the parallel is 0 m round (or rounding's 1e-10 m) and
		// the half-width in longitude huge: refused below.
		parallelM := core.MeanEarthRadiusM * math.Cos(g.Center.LatDeg*math.Pi/180)
		halfDeg := *g.RadiusM / parallelM * 180 / math.Pi
		if 2*halfDeg > 180 || g.Center.LonDeg-halfDeg < -180 || g.Center.LonDeg+halfDeg > 180 {
			return why + " (the circle reaches past ±180° longitude)"
		}
	case GeometryCollection:
		for i := range g.Geometries {
			if s := longitudeSpan(g.Geometries[i]); s != "" {
				return s
			}
		}
	}
	return ""
}

// parts are the geometry's zone parts: itself, or each member of a
// collection, each with the layer that applies to it.
func (g Geometry) parts() []Geometry {
	if g.Type != GeometryCollection {
		return []Geometry{g}
	}
	out := make([]Geometry, 0, len(g.Geometries))
	for i := range g.Geometries {
		m := g.Geometries[i]
		if m.Layer == nil {
			m.Layer = g.Layer
		}
		out = append(out, m)
	}
	return out
}

// toM converts a layer value to metres (feet with core.FeetToMetres
// exactly, LESSONS Z-08).
func (l *Layer) toM(v *float64) *float64 {
	if v == nil {
		return nil
	}
	m := *v
	if l.Uom != nil && *l.Uom == UomFeet {
		m *= core.FeetToMetres
	}
	return &m
}

// LowerM is the lower limit in metres, nil when absent (the surface).
func (l *Layer) LowerM() *float64 { return l.toM(l.Lower) }

// UpperM is the upper limit in metres, nil when absent (unlimited).
func (l *Layer) UpperM() *float64 { return l.toM(l.Upper) }
