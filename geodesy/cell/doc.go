// Package cell is the named partition grid of spec 05 §3 (as amended by
// lab WP-L4, reconciliation M35): a fixed latitude/longitude grid whose
// cells are the stable keys that cross processes, in NATS subjects, KV
// keys and the monitor ownership map.
//
// What a cell is: one of two levels of equal-angle cells on WGS84
// latitude and longitude.
//
//   - Level5, named "c5": 0.1 x 0.1 degrees, 1800 rows by 3600 columns.
//   - Level3, named "c3": 1 x 1 degree, 180 rows by 360 columns. Each c3
//     holds exactly 100 c5 cells (Parent, Children).
//
// What a cell is not: it is not H3 (plan §11 gap 3), not an equal-area
// cell (a c5 is about 11 km high and 11 km wide at the equator, 8 km wide
// at Tbilisi, a sliver at the poles), and not the metric neighbour grid
// of cpa.Grid, which is private to one monitor and sized in metres. A
// cell judges nothing: it is a key. Spec 05 §3's rule stands that the
// partition never crosses an external interface; 04 messages carry
// positions, not cells.
//
// Index formula (fixed; a change is a major). With n cells per degree
// (10 for c5, 1 for c3) and the longitude first brought into
// [-180, 180) (180 itself is -180; a finite longitude outside the range
// is wrapped with core.WrapLonDeg):
//
//	lat_idx = floor((lat_deg + 90) * n)    in [0, 180n - 1]
//	lon_idx = floor((lon_deg + 180) * n)   in [0, 360n - 1]
//
// Latitude +90 belongs to the last row. The cell edges are the decimal
// values (lat_idx - 90n) / n and (lon_idx - 180n) / n, each the nearest
// float64 to the decimal edge, and a cell holds [south, north) x
// [west, east). The floor is corrected by one where floating point puts
// a position outside its cell's own edges, so a position exactly on an
// edge such as 41.7 lands in the cell whose south edge is 41.7, and every
// valid position has exactly one cell at each level.
//
// Name grammar (the wire form; String and Parse are an exact pair):
//
//	name    = level ":" index ":" index
//	level   = "c5" / "c3"
//	index   = "0" / nonzero *DIGIT       ; no sign, no leading zero, no space
//
// with both indexes in range for the level, so "c5:1317:2248" is
// Tbilisi. Parse refuses every spelling String never produces.
//
// Neighbours: Ring1 gives the up-to-eight cells sharing an edge or a
// corner, wrapping in longitude (the last column neighbours the first)
// and not in latitude (the first and last rows have five). Cover gives
// every cell a geodesy.BBox intersects, with a box crossing the
// antimeridian (MinLon > MaxLon) split in two and a hard bound on the
// count checked before anything is allocated (E-10). Every position
// inside a box has its cell in the cover: a box reaching MaxLon == 180
// also holds column 0, where Of places the 180 meridian.
//
// No I/O, no goroutines, no state; nothing here panics on any input.
// Invalid positions, levels, names and boxes are refused with a
// *core.FieldError naming "lat_deg", "lon_deg", "level", "cell", "bbox"
// or "max".
//
// Vectors: none yet. The table in cell_test.go is the pin until the lab
// adds cases of kind "cell" to geodesy.json. Owned by WP-15.
package cell
