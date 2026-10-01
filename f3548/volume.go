package f3548

import (
	"fmt"
	"math"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// MaxVolumeVertices bounds the vertices of one outline polygon read by
// Volume4DToZonesEnvelope (E-10): OiMaxVertices, the most an operational
// intent may have (a constraint may have CstrMaxVertices, fewer).
const MaxVolumeVertices = OiMaxVertices

// HAEM is the altitude as height above the WGS84 ellipsoid in metres. Only
// the one reference and unit F3548 allows are accepted (W84, M); anything
// else is refused with a *core.FieldError rather than converted. The USSP
// derives AMSL from it through the geoid and keeps both (spec 04 section
// 3.1).
func (a Altitude) HAEM() (float64, error) {
	if a.Reference != W84 {
		return 0, core.Fieldf("reference", "%q is not W84, the only altitude reference F3548 allows", string(a.Reference))
	}
	if a.Units != AltitudeUnitsM {
		return 0, core.Fieldf("units", "%q is not M, the only altitude unit F3548 allows", string(a.Units))
	}
	if !core.IsFinite(a.Value) {
		return 0, core.Fieldf("value", "is not finite")
	}
	return a.Value, nil
}

// Volume4DToZonesEnvelope returns a horizontal box that holds v's outline
// and v's time window, for prefiltering (which zones, ISAs or intents can
// touch v at all). The box is conservative: a polygon's box is padded by
// the most a geodesic edge can bow poleward of its vertices, and a
// circle's box is geodesy.Circle.BBox. An absent time_start or time_end is
// the zero time.Time: unbounded on that side.
//
// A volume the envelope cannot be computed for is refused with a
// *core.FieldError, never returned as an empty box, which a prefilter
// would read as "touches nothing": no outline or both outlines, an
// invalid vertex or centre, fewer than three or more than
// MaxVolumeVertices vertices, a radius that is not a positive finite
// number of metres (units M), a time whose format is not RFC3339, or an
// end before the start.
func Volume4DToZonesEnvelope(v Volume4D) (geodesy.BBox, time.Time, time.Time, error) {
	var start, end time.Time
	if v.TimeStart != nil {
		if v.TimeStart.Format != RFC3339 {
			return geodesy.BBox{}, start, end, core.Fieldf("time_start.format", "%q is not RFC3339", string(v.TimeStart.Format))
		}
		start = v.TimeStart.Value
	}
	if v.TimeEnd != nil {
		if v.TimeEnd.Format != RFC3339 {
			return geodesy.BBox{}, start, end, core.Fieldf("time_end.format", "%q is not RFC3339", string(v.TimeEnd.Format))
		}
		end = v.TimeEnd.Value
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return geodesy.BBox{}, start, end, core.Fieldf("time_end", "is before time_start")
	}
	box, err := outlineBBox(v.Volume)
	if err != nil {
		return geodesy.BBox{}, start, end, err
	}
	return box, start, end, nil
}

// outlineBBox is the conservative box of a volume's outline.
func outlineBBox(v Volume3D) (geodesy.BBox, error) {
	switch {
	case v.OutlineCircle != nil && v.OutlinePolygon != nil:
		return geodesy.BBox{}, core.Fieldf("volume", "has both outline_circle and outline_polygon; one is allowed")
	case v.OutlineCircle != nil:
		return circleBBox(*v.OutlineCircle)
	case v.OutlinePolygon != nil:
		return polygonBBox(*v.OutlinePolygon)
	}
	return geodesy.BBox{}, core.Fieldf("volume", "has neither outline_circle nor outline_polygon")
}

func circleBBox(c Circle) (geodesy.BBox, error) {
	if c.Center == nil || !c.Center.LatLon().Valid() {
		return geodesy.BBox{}, core.Fieldf("volume.outline_circle.center", "is missing or not a valid WGS84 position")
	}
	if c.Radius == nil {
		return geodesy.BBox{}, core.Fieldf("volume.outline_circle.radius", "is missing")
	}
	if c.Radius.Units != RadiusUnitsM {
		return geodesy.BBox{}, core.Fieldf("volume.outline_circle.radius.units", "%q is not M", string(c.Radius.Units))
	}
	r := wire(c.Radius.Value)
	if !core.IsFinite(r) || r <= 0 {
		return geodesy.BBox{}, core.Fieldf("volume.outline_circle.radius.value", "must be a positive number of metres")
	}
	return geodesy.Circle{Center: c.Center.LatLon(), RadiusM: r}.BBox(), nil
}

func polygonBBox(p Polygon) (geodesy.BBox, error) {
	n := len(p.Vertices)
	if n < 3 {
		return geodesy.BBox{}, core.Fieldf("volume.outline_polygon.vertices", "has %d vertices; at least 3", n)
	}
	if n > MaxVolumeVertices {
		return geodesy.BBox{}, core.Fieldf("volume.outline_polygon.vertices", "has %d vertices; at most %d", n, MaxVolumeVertices)
	}
	ring := make(geodesy.Ring, 0, n+1)
	for i, v := range p.Vertices {
		pt := v.LatLon()
		if !pt.Valid() {
			return geodesy.BBox{}, core.Fieldf(fmt.Sprintf("volume.outline_polygon.vertices[%d]", i), "is not a valid WGS84 position")
		}
		ring = append(ring, pt)
	}
	ring = append(ring, ring[0]) // the last vertex joins the first (F3548 Polygon)
	box := geodesy.Polygon{Rings: []geodesy.Ring{ring}}.BBox()
	return box.PadM(edgeBulgeM(ring)), nil
}

// edgeBulgeM bounds how far poleward of its end points a geodesic edge of
// the ring can reach: for an edge of length L at latitude phi the great
// circle bows by about L^2 tan(phi) / (8 R). Lengths are equirectangular
// estimates, which overstate a short edge; the result is padded by one
// metre for rounding.
func edgeBulgeM(r geodesy.Ring) float64 {
	const radToDeg = 180 / math.Pi
	worst := 0.0
	for i := 1; i < len(r); i++ {
		a, b := r[i-1], r[i]
		phi := math.Max(math.Abs(a.LatDeg), math.Abs(b.LatDeg))
		phi = math.Min(phi, 89) / radToDeg
		dLat := (b.LatDeg - a.LatDeg) / radToDeg
		dLon := core.WrapLonDeg(b.LonDeg-a.LonDeg) / radToDeg * math.Cos(math.Min(math.Abs(a.LatDeg), math.Abs(b.LatDeg))/radToDeg)
		l := core.MeanEarthRadiusM * math.Hypot(dLat, dLon)
		worst = math.Max(worst, l*l*math.Tan(phi)/(8*core.MeanEarthRadiusM))
	}
	return worst + 1
}
