// Package geoid reads GeographicLib geoid grids and converts between
// height above the WGS84 ellipsoid (HAE) and height above mean sea level
// (AMSL): alt_amsl_m = alt_hae_m - N(lat, lon) (LESSONS R-07).
//
// The layout is GeographicLib's (LESSONS D-08): a binary PGM P5 with
// "# Offset" and "# Scale" comment lines and 16-bit big-endian samples;
// row 0 is latitude +90 and rows run south (an odd number, so the equator
// is one), column 0 is longitude 0 and columns run east through 360 (an
// even number); N = Offset + Scale*raw, interpolated bilinearly, wrapping
// in longitude. Latitude outside [-90, 90] is an error.
//
// EGM2008 (egm2008-2_5.pgm) is the default because the Copernicus DEM is
// on it; EGM96 differs by -2.3 to +4.7 m over Georgia. For orientation,
// EGM2008 gives N = 15.9 m at Tbilisi and 22.5 m at Batumi: a separation
// computed on HAE as if it were AMSL is off by that much. Without a geoid
// there is no AMSL altitude and the aircraft is not judged vertically.
//
// Load reads the whole file into the process's heap (egm2008-2_5.pgm is
// 74.7 MB). LoadMapped maps it read-only instead, on linux and darwin, so
// that every process on a host shares one copy in the page cache; on other
// platforms it reads the file as Load does, and Grid.Mapped says which.
// Both go through Parse's checks and answer bit for bit the same. A mapped
// file must not be truncated or rewritten in place while a process maps it
// (the process faults with SIGBUS): install a new grid by renaming it over
// the old one.
//
// Vectors: vectors/testdata/terrain_geoid.json, function geoid_undulation,
// run from terrain/vectors_test.go: the synthetic grid always, the
// GeographicLib reference values only when USPACE_GEOID_DIR holds
// egm2008-2_5.pgm and egm96-15.pgm (skipped visibly otherwise).
package geoid
