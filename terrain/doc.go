// Package terrain gives the ground elevation at a position from locally
// installed DEM tiles: Copernicus GLO-30 / GLO-90 converted by the lab's
// fetch tool into one PGM tile per 1 x 1 degree cell (internal/pgm,
// elevation = -500 m + 0.2 m * raw, 65535 nodata), plus an index.json
// naming the dataset of every fetched cell, or "sea".
//
//   - CellName names a cell after its south-west corner, by floor:
//     "N41E044", "S01W001".
//   - Tile.ElevationM interpolates bilinearly, clamps to the tile's edge
//     samples and returns nil when any of the four corners is nodata.
//   - Store answers Elevation for any position. A cell absent from the
//     index, a nodata corner or an unreadable tile is unknown (nil, nil),
//     never zero (LESSONS D-04), and counted; a "sea" cell is 0 m with no
//     tile read. A tile that failed is not read again before RetryAfter
//     (one minute by default, not once per message). Tiles are cached up
//     to MaxTiles, least recently used out first, and the cache lock is
//     never held across a read (B-06).
//
// Height above ground is never stored; it is AMSL minus this elevation
// (D-02), both on EGM2008. The DEM is a surface model (roofs, canopy) and
// carries the Copernicus attribution wherever its numbers are shown
// (D-05: Attribution); Elevation carries the dataset and sample spacing
// to show beside the number.
//
// Vectors: vectors/testdata/terrain_geoid.json, all 48 cases, run by
// terrain/vectors_test.go (cell_name, terrain_tile_elevation, and the
// geoid package's geoid_undulation).
package terrain
