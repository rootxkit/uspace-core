# WP-2: `terrain`, `geoid`, `internal/pgm`

Branch `feat/WP-2-terrain-geoid`. Milestone G-M1. Owns `terrain/`,
`geoid/`, `internal/pgm/` exclusively. Depends on WP-0 only. Consumers:
`zones` (through the `terrain.Ground` and `geoid.Undulator` interfaces),
`rid` (undulation value passed in), the systems.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.4`, `§6`, `§8`, `§11` gap 2.
2. `core/geo.go`, `core/counters.go`, `core/errors.go`.
3. `vectors/testdata/terrain_geoid.json`: the header `description` (it
   defines the synthetic grid and tile exactly), all 48 cases.
4. LESSONS D-02, D-04, D-05, D-07, D-08, R-07, B-06, E-10.
5. Reference only: `utm/common/pgm.py`, `utm/common/geoid.py`,
   `utm/common/terrain.py` (the tile format and the index rules;
   `tools/terrain_fetch.py` for what writes the tiles).

## What to build

### `internal/pgm`

Binary PGM P5 parser shared by both grids.

```go
type Grid struct { Width, Height, MaxVal int; Header map[string]string /* "# Key value" comments */ ; data []byte }
const DefaultMaxBytes = 512 << 20
func Parse(data []byte, maxBytes int) (*Grid, error)   // *core.FieldError{Field:"pgm[offset]"}
func (g *Grid) Raw(ix, iy int) uint16                  // big-endian; ix in [0,Width), iy in [0,Height); out of range -> 0 and never panics (callers clamp first)
func (g *Grid) Number(key string) (float64, error)
```

Layout (GeographicLib `.pgm`, D-08): `P5\n`, zero or more `# Key value`
comment lines (Offset, Scale, Description, and for tiles Dataset,
LatFirst, LonFirst, LatStep, LonStep), `width height\n`, `65535\n`, then
`width*height*2` bytes big-endian. Refuse: wrong magic, maxval other than
65535, non-positive or absurd dimensions (width*height*2 > maxBytes),
truncated data, a comment without a key. Name the byte offset.

### `geoid`

```go
type Grid struct{...}
func Parse(data []byte) (*Grid, error);  func Load(path string) (*Grid, error)
func (g *Grid) UndulationM(p core.LatLon) (float64, error)
func (g *Grid) Description() string
func AMSLFromHAE(altHAEM, undulationM float64) float64   // alt_hae_m - N
func HAEFromAMSL(altAMSLM, undulationM float64) float64
type Undulator interface { UndulationM(core.LatLon) (float64, error) }
```

Rules (D-08, pinned by the synthetic cases): row 0 is +90 latitude, rows
go south, an odd number of rows so the equator is one; column 0 is 0
longitude, columns go east, an even number of columns spanning 360 deg;
`value = Offset + Scale * raw`; bilinear interpolation; longitude wraps
(column `Width` is column 0; `geoid-synthetic-0.0-355.0` and
`0.0--5.0` pin it); latitude outside [-90, 90] is an error; latitude
exactly +-90 uses the edge row. Synthetic grid from the header: 36 x 19
samples at 10 deg, Offset -100, Scale 0.01, `sample(row, col) = 1000 +
100*row + col`. Build it in the test from that description (write a tiny
PGM encoder in the test), never from a file.

GeographicLib reference cases (`geoid-egm2008-2_5-*`, `geoid-egm96-15-*`,
30 cases): skip with `t.Skip("needs <file>")` unless the environment
variable `USPACE_GEOID_DIR` points at a directory holding
`egm2008-2_5.pgm` / `egm96-15.pgm` (GeographicLib's distribution). When
present, compare to 1e-9... but note the header tolerance 1e-9 applies to
the generator's own numbers; GeographicLib's bilinear result should match
to 1e-9 if the layout is identical, which is the point of D-08. Report
the skip count in the PR (E-04).

### `terrain`

```go
func CellName(p core.LatLon) string                        // floor; "N41E044", "S01W001", "N00E000", "S34E151"
const (OffsetM = -500.0; ScaleM = 0.2; NoData = 0xFFFF; SeaDataset = "sea")
type Tile struct{...}
func ParseTile(data []byte) (*Tile, error)                 // requires Offset/Scale as above, a Dataset, positive steps
func (t *Tile) ElevationM(p core.LatLon) *float64
func (t *Tile) SpacingM() float64;  func (t *Tile) Dataset() string
type Elevation struct{ ElevationM float64; Dataset string; SpacingM float64 }
type Index map[string]string                               // cell -> dataset or "sea"
func ParseIndex(data []byte) (Index, error)                // index.json {"N41E044":"COP-DEM GLO-30","N40E044":"sea",...}
type StoreOptions struct{ MaxTiles int (16); RetryAfter time.Duration (1 min); Open func(name string) ([]byte, error); Now func() time.Time }
type Store struct{...};  func NewStore(index Index, opts StoreOptions) *Store
func (s *Store) Elevation(p core.LatLon) (*Elevation, error)
func (s *Store) Counters() *core.Counters                  // tiles_loaded, tiles_evicted, tile_read_failed, tile_read_retried, unknown_cell, nodata
type Ground interface { Elevation(core.LatLon) (*Elevation, error) }
const Attribution = "Produced using Copernicus WorldDEM-30 (c) DLR e.V. 2010-2014 and (c) Airbus Defence and Space GmbH 2014-2018 provided under COPERNICUS by the European Union and ESA; all rights reserved."
```

Rules pinned by the six synthetic tile cases (header: 5 x 5 at 0.25 deg,
first sample 42.0N 44.0E, rows south, `elevation = 400 + 10*row + col`,
sample (1,1) nodata): `fy = (LatFirst - lat)/LatStep`, `fx = (lon -
LonFirst)/LonStep`, both clamped into `[0, n-1]`; the cell index clamped
to `n-2` so the four corners exist; past the last sample the edge sample
is used (`terrain-synthetic-40.9000-45.3000` = 444); before the first
sample clamped to it (`42.2000-43.9000` is nil in the vector: read the
case: the clamped corners include the nodata sample, hence nil);
any nodata corner gives nil (`41.8750-44.1250`); away from it the value
(`41.1250-44.8750` = 438.5). Store: a position whose cell is not in the
index is unknown (`nil, nil`, counted `unknown_cell`); a `"sea"` cell is 0
m with dataset "sea" and no tile read; an unreadable or malformed tile is
counted, treated as unknown, and not retried before `RetryAfter`
(D-04: once a minute, not once per message); the cache holds at most
`MaxTiles` (LRU) and **the cache lock is never held across `Open`**
(B-06): lock, check, unlock, read, lock, insert; a concurrent duplicate
read is acceptable and counted.

## Vector test (`terrain/vectors_test.go`)

One test loads `terrain_geoid.json` and dispatches on `function`:
`cell_name` -> `terrain.CellName`; `terrain_tile_elevation` ->
synthetic tile built in the test -> `Tile.ElevationM` (nil vs
`expected.elevation_m` null; tol 1e-9); `geoid_undulation` -> the
synthetic grid from the test's encoder or the real file (skip). The
`geoid` package has its own unit tests; the vector test lives in
`terrain` (the manifest names `terrain`) and imports `geoid`.

## Other tests

- pgm: accept a minimal grid; refuse each malformed form (E-01 pairs);
  `FuzzParsePGM` seeded with the synthetic grid and tile.
- geoid: wrap at 360, both poles, `Description`, `AMSLFromHAE` /
  `HAEFromAMSL` inverse; Tbilisi N = 15.9 m and Batumi 22.5 m documented
  (R-07) but only asserted with the real file.
- terrain: `Store` with a fake `Open`: sea, unknown cell, nodata, read
  failure then retry only after `RetryAfter` (drive `Now`), eviction past
  `MaxTiles` (E-10), concurrent `Elevation` calls under `-race`, and the
  degraded output read back (E-02): the counters after each path.
- `FuzzParseTile`, `FuzzParseGrid`.
- Benchmarks `BenchmarkTileElevation`, `BenchmarkUndulation`.

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 18 synthetic cases pass, 30 GeographicLib cases skip visibly (or pass with the file); `-race -shuffle=on` green.
- [ ] Coverage >= 90 % in each of the three packages.
- [ ] Lint clean; fuzz targets run 10 s without a crasher.
- [ ] `doc.go` files rewritten; CHANGELOG line; PR lists commands and outputs, including the skip count.

## Commits

`feat(pgm): parse the binary PGM container with bounds [WP-2 G-M1]`,
`feat(geoid): read GeographicLib grids and convert HAE to AMSL [WP-2 G-M1]`,
`feat(terrain): read DEM tiles, name cells and serve elevations with a bounded cache [WP-2 G-M1]`,
`test(terrain): run the 48 terrain and geoid vectors [WP-2 G-M1]`,
`test(terrain): fuzz the three parsers and benchmark lookups [WP-2 G-M1]`.
