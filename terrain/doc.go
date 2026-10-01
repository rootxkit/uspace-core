// Package terrain gives ground elevation for a position from locally
// installed DEM tiles (Copernicus GLO-30 / GLO-90 converted to the PGM tile
// format of internal/pgm), with 1x1 degree cell naming by south-west
// corner, bilinear interpolation with edge clamping, nodata handling, a
// "sea" dataset at 0 m, unknown (not zero) for anything outside the index
// (LESSONS D-04), a bounded tile cache whose lock never covers a disk read
// (B-06) and a once-a-minute retry for unreadable tiles.
//
// Height above ground is never stored; it is AMSL minus this (D-02). The
// DEM is a surface model and carries the Copernicus attribution (D-05).
//
// Vectors: vectors/testdata/terrain_geoid.json, the cell_name and
// terrain_tile_elevation functions. Owned by WP-2 with geoid.
package terrain
