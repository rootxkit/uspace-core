# WP-1: `geodesy`

Branch `feat/WP-1-geodesy`. Milestone G-M1. Owns `geodesy/` exclusively.
Depends on WP-0 only. On the critical path: `identify`, `zones`, `cpa`
and `ed318` import it, so open the PR as soon as the vectors pass.

## Read first

1. `CLAUDE.md` (rules), `docs/PLAN.md §3.3` (your API), `§6` (harness),
   `§8` (standards).
2. `core/geo.go` (LatLon, WrapLonDeg, constants you must use).
3. `vectors/testdata/geodesy.json` (all 17 cases; read every `why`).
4. `uspace-lab/knowledge/LESSONS.md` D-09, D-10, D-11, Z-06, Z-11, C-09.
5. Reference only: `utm/airspace/geodesy.py`, `utm/airspace/zones.py`
   (`_in_ring`, `contains_horizontally`), `utm/airspace/cpa.py`
   (`local_offset_m`). Do not copy structure; match numbers.

## What to build

Package `geodesy`, depends on `core` and the standard library only.

```go
var ErrNoConvergence = errors.New("vincenty did not converge")
func Inverse(a, b core.LatLon) (distanceM, initialBearingDeg, finalBearingDeg float64, err error)
func DistanceM(a, b core.LatLon) (float64, error)
func HaversineM(a, b core.LatLon) float64                 // core.MeanEarthRadiusM
func LocalOffsetM(origin, p core.LatLon) (northM, eastM float64)
func LocalOffsetAboutMidLatM(a, b core.LatLon) (northM, eastM float64)
type Ring []core.LatLon
type Polygon struct{ Rings []Ring }
type Circle struct{ Center core.LatLon; RadiusM float64 }
type BBox struct{ MinLat, MinLon, MaxLat, MaxLon float64 }
func (b BBox) Contains(p core.LatLon) bool
func (b BBox) PadM(m float64) BBox
func (p Polygon) Contains(pt core.LatLon) bool
func (p Polygon) BBox() BBox
func (c Circle) Contains(pt core.LatLon) (inside bool, distanceM float64, err error)
func (c Circle) BBox() BBox
func ValidRing(r Ring, maxVertices int) error              // *core.FieldError
func RingFromLonLat(coords [][2]float64) Ring              // GeoJSON order in, LatLon out
```

Rules:

- **Vincenty** (D-09): WGS84 `a = 6378137`, `f = 1/298.257223563`
  (`core` constants). Iterate to 1e-12 on lambda, at most 200 iterations,
  else `ErrNoConvergence`. Coincident points return 0 with no error. The
  Geoscience Australia example must agree to 1 mm (`published_reference_m`
  54972.271; the vector's exact value 54972.27113865979 to 0.001).
- **Haversine** (D-11): only for the spoof distance; `haversine-spoof-distance`
  pins 300.00021646671865 to 1e-6. Document "never for zone edges".
- **Tangent plane** (D-10): `northM = dlat_rad * M(phi0)`,
  `eastM = dlon_rad * N(phi0) * cos(phi0)` with `M = a(1-e2)/(1-e2 sin^2)^1.5`,
  `N = a/sqrt(1-e2 sin^2)`; `dlon` wrapped with `core.WrapLonDeg`.
  `LocalOffsetM` uses the origin latitude; `LocalOffsetAboutMidLatM` uses
  `(a.lat+b.lat)/2` as `phi0` (this is what `cpa.json` was generated with;
  check `tangent-plane-*` and `cpa.json#head-on` `d_horizontal_now_m`
  1000.0007861298747 by hand before WP-9 needs it).
  `tangent-plane-across-antimeridian` must give ~223 m, not 40 000 km.
- **Polygon containment**: ray casting on lon/lat in degrees with straight
  edges; outer ring then holes (a point in a hole is outside); a point on
  the boundary counts as inside for the outer ring (document the choice,
  the vectors do not test the boundary). Rings may be closed (first ==
  last) or not; treat both. Antimeridian: a ring narrower than 180 deg
  that crosses it is handled by unwrapping longitudes relative to the
  first vertex; document that wider rings are unsupported.
- **Circle containment** (Z-11): `DistanceM(center, pt) <= radius`; return
  the distance (`circle-500m-inside-at-499m` 498.4287233780669 to 1e-6).
  Feet are converted by the caller (`circle-1000ft-*` give `radius_unit`
  "m" already converted; check).
- **ValidRing** (Z-06, C-09): >= 4 positions, closed, every position
  `core.LatLon.Valid()`, at most `maxVertices` (callers pass 5000).
  Error names `ring[i]`.
- Never panic on NaN or Inf; `Contains` returns false for an invalid
  point.

## Vector test (`geodesy/vectors_test.go`)

Decode `input.function` and switch. Input shapes (strict):

| function | input | expected |
|---|---|---|
| `vincenty_inverse` | `from`, `to` as `[lat, lon]` | `distance_m` (tol 0.001), optional `published_reference_m` (log only) |
| `compare` | `from`, `to` | `vincenty_m` (0.001), `haversine_mean_radius_m` (1e-6), `difference_m` (1e-6) |
| `in_circle` | `center`, `radius`, `radius_unit` ("m"), `point` | `inside` exact, `distance_m` 1e-6 |
| `in_polygon` | `rings_lon_lat` ([[lon,lat]...] rings), `point` ([lat,lon]) | `inside` exact |
| `local_offset_m` | `origin`, `point` | `north_m`, `east_m` 1e-6 |
| `haversine_6371008.8` | `from`, `to` | `distance_m` 1e-6 |

Mirror the header tolerances as constants and assert them with
`f.FloatTolerance`. Decode positions as `[2]float64`.

## Other tests

- E-01 pairs: inside/outside for circle and polygon (vectors have them);
  convergence and `ErrNoConvergence` (nearly antipodal: (0,0) to
  (0.5,179.7)); `ValidRing` accept and each refusal; `WrapLonDeg` use
  across the antimeridian for `LocalOffsetM` both directions.
- Symmetry: `Inverse(a,b)` distance equals `Inverse(b,a)` to 1e-9
  (`tbilisi-short-there/back`).
- Property test (no fuzz needed: no decoder): random points within 10 km
  agree between haversine and Vincenty within 0.5 %.
- Benchmarks: `BenchmarkVincentyInverse`, `BenchmarkHaversine`,
  `BenchmarkLocalOffset`, `BenchmarkInPolygon100` (100-vertex ring with
  one hole), `BenchmarkInCircle`. Targets in `docs/bench-targets.txt`.

## Done when

- [ ] `go test -race -shuffle=on ./geodesy/` green; 17/17 vector cases pass.
- [ ] Coverage >= 90 % (`go test -cover ./geodesy/`).
- [ ] gofmt, vet, staticcheck, golangci-lint clean.
- [ ] Benchmarks present and reported.
- [ ] `geodesy/doc.go` rewritten to describe what exists.
- [ ] `CHANGELOG.md` Unreleased: one line `geodesy: ... [WP-1]`.
- [ ] PR body lists the commands run and their last lines (E-04).

## Commits

`feat(geodesy): add the WGS84 Vincenty inverse and haversine [WP-1 G-M1]`,
`feat(geodesy): add the local tangent plane and shape containment [WP-1 G-M1]`,
`test(geodesy): run the 17 geodesy vectors [WP-1 G-M1]`,
`perf(geodesy): add benchmarks for the hot functions [WP-1 G-M1]`,
`docs(geodesy): describe the package [WP-1 G-M1]`.
