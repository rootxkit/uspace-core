// Package geodesy holds the WGS84 computations the zone, CPA and
// identification judgements rest on. It depends on core and the standard
// library only, keeps no state and never panics on any float input.
//
// Distances:
//
//   - Inverse and DistanceM solve the inverse geodesic problem on WGS84
//     with Vincenty's formulae (D-09): iterated to 1e-12 rad on lambda, at
//     most 200 iterations, else ErrNoConvergence (nearly antipodal points
//     only). Coincident points give 0 without iterating. Geoscience
//     Australia's worked example agrees to 1 mm. Every zone edge and
//     circle uses this.
//   - HaversineM is the great circle on the 6,371,008.8 m sphere, for the
//     spoof-distance check only (D-11). It is up to 0.56 % off the
//     ellipsoid: never use it for zone edges.
//
// Local tangent plane (D-10): LocalOffsetM gives north and east metres
// from an origin with the WGS84 meridional and prime-vertical radii at
// the origin's latitude; LocalOffsetAboutMidLatM takes the radii at the
// mid-latitude of the pair, as the CPA judgement does. The longitude
// difference is wrapped, so a pair straddling the antimeridian is metres
// apart, not 40,000 km.
//
// Shapes: Circle.Contains judges by geodesic distance from the centre
// (Z-11) and returns it; a radius in feet is converted by the caller.
// Polygon.Contains is even-odd ray casting on lon/lat in degrees with
// straight edges, outer ring then holes; the polygon is a closed set (a
// point on the outer ring or on a hole's ring is inside); rings may be
// closed or open; a ring narrower than 180 degrees that crosses the
// antimeridian is handled by unwrapping longitudes relative to its first
// vertex, and wider rings are unsupported. An invalid point is never
// inside anything. BBox, with PadM, is a conservative prefilter; a box
// crossing the antimeridian has MinLon > MaxLon. ValidRing checks a ring
// before use (Z-06, C-09) and names the offending "ring[i]";
// RingFromLonLat converts GeoJSON [lon, lat] positions.
//
// H3 helpers are deferred to a later work package (docs/PLAN.md, spec
// gap 3).
//
// Vectors: vectors/testdata/geodesy.json (17 cases), run by
// TestVectorsGeodesy. Owned by WP-1.
package geodesy
