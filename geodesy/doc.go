// Package geodesy holds the WGS84 computations every judgement rests on:
// the Vincenty inverse (geodesic distance for circular zones, LESSONS D-09),
// the haversine on the 6,371,008.8 m sphere (spoof distance only, D-11),
// the local tangent-plane projection about a mid-latitude with the WGS84
// meridional and prime-vertical radii (CPA, D-10), longitude wrapping
// across the antimeridian, point-in-circle and point-in-polygon with holes
// (ray casting on lon/lat), and bounding boxes for prefiltering (Z-06).
//
// It depends on core only. H3 helpers are deferred to a later work
// package (docs/PLAN.md, spec gap 3).
//
// Vectors: vectors/testdata/geodesy.json (17 cases). Owned by WP-1.
package geodesy
