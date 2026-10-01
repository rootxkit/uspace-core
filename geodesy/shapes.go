package geodesy

import (
	"fmt"
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// Ring is a closed or open sequence of positions. GeoJSON stores rings as
// [lon, lat]; convert at the parser boundary with RingFromLonLat.
type Ring []core.LatLon

// Polygon is an outer ring (Rings[0]) followed by holes.
type Polygon struct{ Rings []Ring }

// Circle is a centre and a radius in metres on the ground, judged by
// geodesic distance on WGS84 (Z-11). A radius given in feet is converted
// by the caller with core.FeetToMetres before it gets here.
type Circle struct {
	Center  core.LatLon
	RadiusM float64
}

// BBox is a latitude/longitude box in degrees, used as a prefilter. When
// MinLon > MaxLon the box crosses the antimeridian: it holds longitudes
// >= MinLon or <= MaxLon. An empty box (from an empty polygon or an
// invalid circle) has MinLat > MaxLat and contains nothing.
type BBox struct{ MinLat, MinLon, MaxLat, MaxLon float64 }

// emptyBBox contains no point.
var emptyBBox = BBox{MinLat: math.Inf(1), MinLon: math.Inf(1), MaxLat: math.Inf(-1), MaxLon: math.Inf(-1)}

func (b BBox) empty() bool { return !(b.MinLat <= b.MaxLat) }

// Contains reports whether p is inside the box, edges included. An
// invalid point is never inside.
func (b BBox) Contains(p core.LatLon) bool {
	if !p.Valid() || b.empty() {
		return false
	}
	if p.LatDeg < b.MinLat || p.LatDeg > b.MaxLat {
		return false
	}
	if b.MinLon <= b.MaxLon {
		return p.LonDeg >= b.MinLon && p.LonDeg <= b.MaxLon
	}
	return p.LonDeg >= b.MinLon || p.LonDeg <= b.MaxLon
}

// PadM grows the box by at least m metres on every side, so that a point
// within m metres of the box is inside the padded box. The pad is
// conservative: latitude uses the smallest meridional radius (at the
// equator) and longitude the semi-major axis at the padded box's highest
// latitude. Latitude is clamped to [-90, 90]; a box that reaches a pole
// or wraps the whole globe gets the full longitude range. A non-positive
// or non-finite m, or an empty box, returns b unchanged.
func (b BBox) PadM(m float64) BBox {
	if !core.IsFinite(m) || m <= 0 || b.empty() {
		return b
	}
	const minMeridionalM = core.WGS84SemiMajorM * (1 - eccentricitySq)
	dLatDeg := degrees(m / minMeridionalM)
	out := BBox{
		MinLat: math.Max(-90, b.MinLat-dLatDeg),
		MaxLat: math.Min(90, b.MaxLat+dLatDeg),
	}
	phiMaxDeg := math.Max(math.Abs(out.MinLat), math.Abs(out.MaxLat))
	cosPhi := math.Cos(radians(phiMaxDeg))
	widthDeg := b.MaxLon - b.MinLon
	if b.MinLon > b.MaxLon {
		widthDeg += 360
	}
	full := cosPhi <= 1e-9
	var dLonDeg float64
	if !full {
		dLonDeg = degrees(m / (core.WGS84SemiMajorM * cosPhi))
		full = widthDeg+2*dLonDeg >= 360
	}
	if full {
		out.MinLon, out.MaxLon = -180, 180
		return out
	}
	out.MinLon = core.WrapLonDeg(b.MinLon - dLonDeg)
	out.MaxLon = core.WrapLonDeg(b.MaxLon + dLonDeg)
	return out
}

// unwrapDeg brings a longitude difference of two in-range longitudes
// (within [-360, 360]) into [-180, 180] without math.Mod, for the inner
// loop of ray casting.
func unwrapDeg(d float64) float64 {
	if d > 180 {
		return d - 360
	}
	if d < -180 {
		return d + 360
	}
	return d
}

// openLen is the number of distinct positions of r: a closed ring (first
// == last) drops its repeated last position.
func openLen(r Ring) int {
	n := len(r)
	if n > 1 && r[0] == r[n-1] {
		n--
	}
	return n
}

// inRing is even-odd ray casting on lon/lat in degrees with straight
// edges. Longitudes are unwrapped relative to the first vertex, so a ring
// narrower than 180 degrees that crosses the antimeridian is handled.
// onBoundary reports a point exactly on an edge or a vertex.
func inRing(r Ring, pt core.LatLon) (inside, onBoundary bool) {
	n := openLen(r)
	if n < 3 {
		return false, false
	}
	lon0 := r[0].LonDeg
	x := lon0 + unwrapDeg(pt.LonDeg-lon0)
	y := pt.LatDeg
	x1 := lon0
	y1 := r[0].LatDeg
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		x2 := lon0 + unwrapDeg(r[j].LonDeg-lon0)
		y2 := r[j].LatDeg
		if onSegment(x, y, x1, y1, x2, y2) {
			return false, true
		}
		if (y1 > y) != (y2 > y) {
			crossX := x1 + (y-y1)*(x2-x1)/(y2-y1)
			if x < crossX {
				inside = !inside
			}
		}
		x1, y1 = x2, y2
	}
	return inside, false
}

// onSegment reports whether (x, y) lies exactly on the segment from
// (x1, y1) to (x2, y2).
func onSegment(x, y, x1, y1, x2, y2 float64) bool {
	if x < math.Min(x1, x2) || x > math.Max(x1, x2) || y < math.Min(y1, y2) || y > math.Max(y1, y2) {
		return false
	}
	return (x2-x1)*(y-y1)-(y2-y1)*(x-x1) == 0
}

// Contains reports whether pt is inside the polygon: inside the outer ring
// and not inside any hole. It casts rays on lon/lat in degrees with
// straight edges (metres of error for zones kilometres across), the way
// the old monitor judged zones.
//
// The polygon is a closed set: a point exactly on the outer ring, or on a
// hole's ring, counts as inside (the vectors do not test the boundary; a
// position fix cannot resolve it anyway, so the cautious answer is
// chosen). Rings may be closed or open. A ring that crosses the
// antimeridian is handled when it is narrower than 180 degrees of
// longitude; wider rings are unsupported and judged wrongly. An invalid
// point, or a polygon without an outer ring of at least three distinct
// positions, is never inside. It never panics.
func (p Polygon) Contains(pt core.LatLon) bool {
	if !pt.Valid() || len(p.Rings) == 0 {
		return false
	}
	in, edge := inRing(p.Rings[0], pt)
	if !in && !edge {
		return false
	}
	for _, hole := range p.Rings[1:] {
		if in, edge := inRing(hole, pt); in && !edge {
			return false
		}
	}
	return true
}

// BBox is the bounding box of the outer ring. A ring that crosses the
// antimeridian gives a box with MinLon > MaxLon; a polygon without
// positions gives an empty box.
func (p Polygon) BBox() BBox {
	if len(p.Rings) == 0 || len(p.Rings[0]) == 0 {
		return emptyBBox
	}
	r := p.Rings[0]
	lon0 := r[0].LonDeg
	b := emptyBBox
	minX, maxX := math.Inf(1), math.Inf(-1)
	for _, v := range r {
		x := lon0 + unwrapDeg(v.LonDeg-lon0)
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		b.MinLat = math.Min(b.MinLat, v.LatDeg)
		b.MaxLat = math.Max(b.MaxLat, v.LatDeg)
	}
	switch {
	case minX < -180:
		b.MinLon, b.MaxLon = minX+360, maxX
	case maxX > 180:
		b.MinLon, b.MaxLon = minX, maxX-360
	default:
		b.MinLon, b.MaxLon = minX, maxX
	}
	return b
}

// Contains reports whether pt is within the circle, judged by the
// geodesic distance from the centre on WGS84 (D-09, Z-11): inside when
// the distance is at most RadiusM. It returns that distance. An invalid
// centre, radius or point returns false with a *core.FieldError naming
// "center", "radius_m" or "point"; nearly antipodal points return
// ErrNoConvergence. It never panics.
func (c Circle) Contains(pt core.LatLon) (inside bool, distanceM float64, err error) {
	if !c.Center.Valid() {
		return false, 0, core.Fieldf("center", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", c.Center.LatDeg, c.Center.LonDeg)
	}
	if !core.IsFinite(c.RadiusM) || c.RadiusM < 0 {
		return false, 0, core.Fieldf("radius_m", "must be finite and not negative, got %v", c.RadiusM)
	}
	if !pt.Valid() {
		return false, 0, core.Fieldf("point", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", pt.LatDeg, pt.LonDeg)
	}
	d, err := DistanceM(c.Center, pt)
	if err != nil {
		return false, 0, err
	}
	return d <= c.RadiusM, d, nil
}

// BBox is a box that holds the whole circle (conservatively larger). An
// invalid centre or radius gives an empty box.
func (c Circle) BBox() BBox {
	if !c.Center.Valid() || !core.IsFinite(c.RadiusM) || c.RadiusM < 0 {
		return emptyBBox
	}
	pt := BBox{MinLat: c.Center.LatDeg, MinLon: c.Center.LonDeg, MaxLat: c.Center.LatDeg, MaxLon: c.Center.LonDeg}
	return pt.PadM(c.RadiusM)
}

// ValidRing checks a polygon ring before it is used (Z-06, C-09): at most
// maxVertices positions (callers pass 5000; checked first, so an
// oversized ring is refused before it is walked), at least 4 positions,
// every position valid (finite and in range), and closed (first == last).
// The error is a *core.FieldError naming "ring" or "ring[i]".
func ValidRing(r Ring, maxVertices int) error {
	if len(r) > maxVertices {
		return core.Fieldf("ring", "%d positions, more than the maximum of %d", len(r), maxVertices)
	}
	if len(r) < 4 {
		return core.Fieldf("ring", "%d positions, a closed ring needs at least 4", len(r))
	}
	for i, p := range r {
		if !p.Valid() {
			return core.Fieldf(fmt.Sprintf("ring[%d]", i), "not a valid WGS84 position (lat_deg %v, lon_deg %v)", p.LatDeg, p.LonDeg)
		}
	}
	if r[0] != r[len(r)-1] {
		return core.Fieldf("ring", "not closed: the first position differs from the last (ring[%d])", len(r)-1)
	}
	return nil
}

// RingFromLonLat converts GeoJSON positions ([lon, lat]) into a Ring
// (LatLon). It only reorders; validate the result with ValidRing.
func RingFromLonLat(coords [][2]float64) Ring {
	r := make(Ring, len(coords))
	for i, c := range coords {
		r[i] = core.LatLon{LatDeg: c[1], LonDeg: c[0]}
	}
	return r
}
