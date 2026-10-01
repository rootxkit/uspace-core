package zones

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
)

// Limit is one vertical limit of a zone in metres, in its own reference.
// A nil *Limit is unbounded: no lower limit is from the ground (or below
// it), no upper limit is unlimited (LESSONS Z-08).
type Limit struct {
	ValueM float64
	Ref    core.VerticalRef
}

// Zone is a UAS geographical zone as the judgement uses it: one volume,
// limits in metres, the published shape and its bounding box, and the
// applicability periods. Exactly one of Polygon and Circle is set; a
// circle is its published centre and radius, never a polygon drawn for
// it (LESSONS Z-11). Build it with FromED269; a Zone built in code is
// judged with the same fail-safe rules (see ContainsHorizontally,
// AppliesAt and JudgeVertical).
type Zone struct {
	Identifier string
	Country    string
	// Type is the ED-318 zone type the restriction maps to (PROHIBITED,
	// REQ_AUTHORIZATION, CONDITIONAL, NO_RESTRICTION). The ED-269 `type`
	// (COMMON or the customised type) does not affect the judgement and is not
	// carried.
	Type core.ZoneType
	// Restriction is the ED-269 spelling (REQ_AUTHORISATION), as alert
	// details carry it.
	Restriction ed269.Restriction
	Lower       *Limit
	Upper       *Limit
	Polygon     *geodesy.Polygon
	Circle      *geodesy.Circle
	BBox        geodesy.BBox
	Periods     []ed269.Period
}

// maxRingVertices bounds one polygon ring, as ed269 does on import
// (LESSONS Z-06).
var maxRingVertices = ed269.DefaultLimits.MaxRingVertices

// FromED269 builds the judgement's view of a parsed ED-269 zone. The zone
// must hold exactly one volume (Z-04); limits are converted to metres
// with core.FeetToMetres exactly (2000 FT is 609.6 m, Z-08), as is a
// circle's radius. A zone it cannot judge safely is refused with a
// *core.FieldError naming the field: a missing or unknown restriction,
// a limit that is not finite or whose reference is not AGL, AMSL or
// WGS84, a ring that ValidRing refuses, a circle without a valid centre
// or a finite positive radius, and an empty applicability list.
func FromED269(z *ed269.GeoZone) (*Zone, error) {
	if z == nil {
		return nil, core.Fieldf("zone", "is nil")
	}
	vol, ok := z.Volume()
	if !ok {
		return nil, core.Fieldf("geometry", "has %d volumes; one volume per zone is supported", len(z.Geometry))
	}
	t := z.Restriction.ZoneType()
	if t == "" {
		return nil, core.Fieldf("restriction", "%q is not an ED-269 restriction", string(z.Restriction))
	}
	if len(z.Applicability) == 0 {
		return nil, core.Fieldf("applicability", "has no period; a zone needs at least one")
	}
	out := &Zone{
		Identifier:  z.Identifier,
		Country:     z.Country,
		Type:        t,
		Restriction: z.Restriction,
		Periods:     slices.Clone(z.Applicability),
	}
	var err error
	if out.Lower, err = limitOf(vol.LowerM(), vol.LowerRef, "geometry[0].lowerLimit"); err != nil {
		return nil, err
	}
	if out.Upper, err = limitOf(vol.UpperM(), vol.UpperRef, "geometry[0].upperLimit"); err != nil {
		return nil, err
	}
	proj := vol.Projection
	switch proj.Type {
	case ed269.ShapePolygon:
		p, err := polygonOf(proj.Rings)
		if err != nil {
			return nil, err
		}
		out.Polygon = p
		out.BBox = p.BBox()
	case ed269.ShapeCircle:
		c, err := circleOf(proj.Center, vol.RadiusM())
		if err != nil {
			return nil, err
		}
		out.Circle = c
		out.BBox = c.BBox()
	default:
		return nil, core.Fieldf("geometry[0].horizontalProjection.type", "%q is neither Polygon nor Circle", proj.Type)
	}
	return out, nil
}

// limitOf checks one converted limit.
func limitOf(valueM *float64, ref core.VerticalRef, field string) (*Limit, error) {
	if valueM == nil {
		return nil, nil
	}
	if !core.IsFinite(*valueM) {
		return nil, core.Fieldf(field, "is not finite (%v)", *valueM)
	}
	if !ref.Valid() {
		return nil, core.Fieldf(field, "reference %q is not AGL, AMSL or WGS84", string(ref))
	}
	return &Limit{ValueM: *valueM, Ref: ref}, nil
}

// polygonOf copies and validates the rings of a polygon projection.
func polygonOf(rings [][]ed269.Position) (*geodesy.Polygon, error) {
	if len(rings) == 0 {
		return nil, core.Fieldf("geometry[0].horizontalProjection.coordinates", "has no ring")
	}
	p := &geodesy.Polygon{Rings: make([]geodesy.Ring, len(rings))}
	for i, r := range rings {
		ring := geodesy.Ring(slices.Clone(r))
		if err := geodesy.ValidRing(ring, maxRingVertices); err != nil {
			return nil, core.Fieldf(fmt.Sprintf("geometry[0].horizontalProjection.coordinates[%d]", i), "%v", err)
		}
		p.Rings[i] = ring
	}
	return p, nil
}

// circleOf validates a circle projection with its radius in metres.
func circleOf(center *ed269.Position, radiusM *float64) (*geodesy.Circle, error) {
	if center == nil || !center.Valid() {
		return nil, core.Fieldf("geometry[0].horizontalProjection.center", "is missing or not a valid WGS84 position")
	}
	if radiusM == nil || !core.IsFinite(*radiusM) || *radiusM <= 0 {
		return nil, core.Fieldf("geometry[0].horizontalProjection.radius", "must be finite and positive")
	}
	return &geodesy.Circle{Center: *center, RadiusM: *radiusM}, nil
}

// ContainsHorizontally reports whether p is inside the zone's horizontal
// projection: the bounding box first (four comparisons for a zone far
// away, Z-06), then the polygon with its holes by ray casting, or the
// circle by the geodesic distance from its published centre (Z-11, D-09).
// A polygon is a closed set: a point on its boundary is inside, and so is
// a point on the edge of a circle. Polygons and boxes that cross the
// antimeridian are handled (geodesy).
//
// An error means "not judged", never "outside": an invalid point
// (non-finite or out of range, C-09) returns false with a
// *core.FieldError naming "point", a zone with no shape or an invalid
// circle returns false with the error that says so, and a nearly
// antipodal circle centre returns geodesy.ErrNoConvergence. The caller
// counts such a check as not evaluated.
func (z *Zone) ContainsHorizontally(p core.LatLon) (bool, error) {
	if !p.Valid() {
		return false, core.Fieldf("point", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", p.LatDeg, p.LonDeg)
	}
	switch {
	case z.Polygon != nil:
		if !bboxContains(z, p) {
			return false, nil
		}
		return z.Polygon.Contains(p), nil
	case z.Circle != nil:
		if !bboxContains(z, p) {
			return false, nil
		}
		in, _, err := z.Circle.Contains(p)
		if err != nil {
			return false, err
		}
		return in, nil
	}
	return false, core.Fieldf("zone", "%q has neither a polygon nor a circle", z.Identifier)
}

// AppliesAt reports whether the zone applies at at, through
// ed269.Applies (any period applies; both ends included; compared in
// UTC; LESSONS Z-07, T-09). The caller passes the aircraft's captured_at
// (its placed time), never wall time or arrival time: an aircraft
// captured inside a window that ended before its message arrived was in
// the zone.
//
// Fail-safe towards enforcing the restriction, as ed269's zero-value
// Period is: a zone with no period (built in code; FromED269 refuses
// it) applies always, and so does a zero at, which says the caller does
// not know when the aircraft was there.
func (z *Zone) AppliesAt(at time.Time) bool {
	if len(z.Periods) == 0 || at.IsZero() {
		return true
	}
	return ed269.Applies(z.Periods, at)
}

// bboxContains is the zone's bounding box test, with longitude -180 and
// 180 taken as the one meridian they are.
func bboxContains(z *Zone, p core.LatLon) bool {
	if z.BBox.Contains(p) {
		return true
	}
	if math.Abs(p.LonDeg) == 180 {
		return z.BBox.Contains(core.LatLon{LatDeg: p.LatDeg, LonDeg: -p.LonDeg})
	}
	return false
}

// needsHeight reports whether a limit needs the aircraft's height in its
// reference to be judged. A lower AGL limit at or below 0 is met by any
// airborne aircraft and needs nothing (Z-08).
func needsHeight(l *Limit, lower bool) bool {
	if l == nil {
		return false
	}
	return !(lower && l.Ref == core.RefAGL && l.ValueM <= 0)
}

// NeedsTerrain reports whether judging the zone vertically needs the DEM:
// an AGL ceiling, or an AGL floor above the ground. A floor at or below
// it is met by any airborne aircraft. A service logs at startup the
// PROHIBITED zones that need terrain when none is configured (Z-09).
func (z *Zone) NeedsTerrain() bool {
	return (needsHeight(z.Lower, true) && z.Lower.Ref == core.RefAGL) ||
		(needsHeight(z.Upper, false) && z.Upper.Ref == core.RefAGL)
}

// NeedsGeoid reports whether the zone has a WGS84 (height above the
// ellipsoid) limit, which is judged as AMSL plus the geoid undulation.
func (z *Zone) NeedsGeoid() bool {
	return (z.Lower != nil && z.Lower.Ref == core.RefWGS84) ||
		(z.Upper != nil && z.Upper.Ref == core.RefWGS84)
}
