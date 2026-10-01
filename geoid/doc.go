// Package geoid reads GeographicLib geoid grids (binary PGM P5 with
// Offset and Scale comments, 16-bit big-endian samples, row 0 at +90
// latitude, column 0 at 0 longitude, bilinear interpolation wrapping in
// longitude; LESSONS D-08) and converts between height above the WGS84
// ellipsoid and AMSL: alt_amsl_m = alt_hae_m - N(lat, lon). EGM2008 is the
// default because the DEM is on it (R-07). Without a geoid there is no
// AMSL altitude and the aircraft is not judged vertically.
//
// Vectors: vectors/testdata/terrain_geoid.json, the geoid_undulation
// function (synthetic grid always; GeographicLib reference values are
// skipped visibly when the real grid file is absent). Owned by WP-2.
package geoid
