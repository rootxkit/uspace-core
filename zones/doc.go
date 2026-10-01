// Package zones judges one aircraft against one zone: horizontal
// containment through geodesy (circle by centre and radius, polygon with
// holes, bounding-box prefilter; LESSONS D-09, Z-06, Z-11), applicability
// at captured_at through ed269 (T-09), each vertical limit in its own
// reference (AMSL, AGL via the DEM, WGS84 via the geoid; feet exact;
// Z-08), the unjudged-limit warning for the zones that matter (Z-09), the
// pressure-altitude margin and within_band (R-09), the severity per zone
// type (Z-10) and the 120 m height limit over the ground (D-04).
//
// It depends on core, geodesy and ed269. Vectors:
// vectors/testdata/zones_vertical.json (38). Owned by WP-8.
package zones
