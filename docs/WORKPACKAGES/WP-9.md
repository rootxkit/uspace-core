# WP-9: `cpa`

Branch `feat/WP-9-cpa`. Milestone G-M1. Owns `cpa/` exclusively. Depends
on WP-1 (`geodesy.LocalOffsetAboutMidLatM`, `core.WrapLonDeg`); the pure
math can be written against the signature in `docs/PLAN.md §3.3` while
WP-1 is in review. Consumers: `alerting` (WP-10), the USSP's `monitor`
(traffic information), the authority's `detect`.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.11`, `§8.6` (the CPA budget: the first
   thing that breaks at 5000 drones, spec `05 §1`).
2. `core/geo.go`, `geodesy` (tangent plane).
3. `vectors/testdata/cpa.json`: header (`description` is the full
   algorithm, `policy`), all 27 cases.
4. LESSONS C-01, C-02, C-03, C-04, C-09, C-10, C-15, D-01, D-10, R-09.
5. Spec `04 §3.3` (proximity alert fields, policy 60 s / 60 m / 20 m /
   800 m), `05 §1`, `05 §5` (CPA workers' budget and `evaluation_period_s`).
6. Reference only: `utm/airspace/cpa.py` (docstring and `cpa()`,
   `advance()`), `utm/airspace/neighbours.py`.

## What to build

```go
type State struct{ Pos core.LatLon; AltAMSLM float64; VerticalKnown bool; VNMS, VEMS, VDMS float64; CapturedAtS float64 }
type Policy struct{ TCPAMaxS, DHorizontalMinM, DVerticalMinM, NeighbourRadiusM, NeighbourMaxAgeS float64 }
var DefaultPolicy = Policy{60, 60, 20, 800, 10}
type Result struct{ Judged bool; TCPAS, DCPAHorizontalM, DAltAtCPAM, DHorizontalNowM, DAltNowM float64; VerticalKnown, Conflict bool }
func Advance(s State, toS float64) State
func Evaluate(a, b State, pol Policy) Result
type Grid struct{...}
func NewGrid(cellM float64) *Grid
func (g *Grid) Upsert(id string, p core.LatLon)
func (g *Grid) Remove(id string)
func (g *Grid) Near(p core.LatLon, radiusM float64) []string
func (g *Grid) Len() int
```

Algorithm (header `description`, C-01..C-04):

1. If `|a.CapturedAtS - b.CapturedAtS| > NeighbourMaxAgeS`: `Judged
   false`, nothing else (`stale-neighbour-not-judged`; exactly at the
   bound is judged: `neighbour-at-max-age-is-judged`).
2. Advance the older state to the newer one's time along its NED velocity
   (`older-sample-advanced-*`): positions move by `vn*dt` north, `ve*dt`
   east in the tangent plane about the state's own latitude, altitude by
   `-vd*dt` (down positive). Wrap the longitude.
3. Project both onto the tangent plane about the pair's mid-latitude
   (`geodesy.LocalOffsetAboutMidLatM`, D-10): `rel_pos = p_b - p_a`,
   `rel_vel = v_b - v_a` (north, east).
4. `t_cpa = -(rel_pos . rel_vel) / |rel_vel|^2` if `|rel_vel| >
   1e-6 m/s`, else 0; clamp to `>= 0` (C-02; `diverging-*`,
   `both-hovering`, `parallel-same-speed`).
5. `d_cpa_h = |rel_pos + rel_vel * t_cpa|`; `d_h_now = |rel_pos|`;
   `d_alt_now = |alt_b - alt_a|`; `d_alt_at_cpa = |(alt_b - alt_a) +
   (vd_a - vd_b) * t_cpa|` (C-01; `climb-closes-vertical-gap`,
   `vertical-separation-at-cpa`). Vertical is known only when both
   states have `VerticalKnown` (`one-pressure-track-makes-the-pair-unknown`,
   R-09).
6. `conflict = (d_h_now < d_h_min AND (vertical unknown OR d_alt_now <
   d_v_min)) OR (t_cpa < t_max AND d_cpa_h < d_h_min AND (vertical
   unknown OR d_alt_at_cpa < d_v_min))` (C-03: `hovering-inside-minima-with-velocity-noise`,
   `diverging-but-already-too-close`, `conflict-beyond-window`,
   `pressure-tracks-100m-apart-vertically-still-conflict`,
   `horizontally-clear-pressure-tracks`).
7. Symmetry: `Evaluate(a,b)` and `Evaluate(b,a)` give equal numbers
   (`order-a-b`, `order-b-a`); the sign conventions above make the
   magnitudes identical; test it for every vector pair.

Precision: the vectors pin `t_cpa_s` and distances to 0.01; `head-on`
expects `t_cpa 50.00003930649373`, `d_horizontal_now_m
1000.0007861298747`: the tangent-plane radii at the **mid-latitude** of
the pair make the difference from a flat 1000 m; if your numbers are off
by ~0.001 m you used the origin latitude or a sphere.

Non-finite inputs (C-09): `Evaluate` returns `Judged false` when any
coordinate, altitude or velocity is NaN or Inf; never reaches the grid.

Grid (C-15): square cells of `cellM` metres in a local equirectangular
frame about a reference latitude set at `NewGrid` (or per-cell-row
longitude scaling; document); `Near` returns ids in the 3x3 (or more,
when `radiusM > cellM`) ring; the caller computes exact distances. A test
compares `Near` with brute force over 300 random aircraft and 100 random
queries (zero misses); `cellM < radius` is rejected by `NewGrid` (error or
clamp; document). Bounded by construction (one entry per id); `Remove`
frees. Not safe for concurrent use; document.

## Vector test (`cpa/vectors_test.go`)

Input `a`, `b` with `lat_deg`, `lon_deg`, `alt_amsl_m`, `vn_ms`, `ve_ms`,
`vd_ms`, `captured_at_s`, `vertical_known`, `described_as{north_m,
east_m}` (ignore, but decode it so strict decoding passes),
`neighbour_max_age_s`; policy from the header `policy` (`t_cpa_max_s`,
`d_horizontal_min_m`, `d_vertical_min_m`, `neighbour_radius_m`).
Expected: `judged`; when judged `t_cpa_s` (0.01), `d_cpa_horizontal_m`,
`d_alt_at_cpa_m`, `d_horizontal_now_m`, `d_alt_now_m` (0.01),
`vertical_known`, `conflict` exact. Also assert symmetry per case.

## Other tests

- E-01 pairs: `opening-from-40m/50m/60m` (boundary of the horizontal
  minimum: strictly less); conflict true/false twins exist in the vectors.
- `Advance` across the antimeridian; `Advance` with `dt 0` is identity.
- Grid vs brute force (property, seeded RNG); `Near` with the 800 m
  radius and 1000 m cells; eviction via `Remove`.
- Non-finite inputs rejected (table).
- Benchmarks `BenchmarkEvaluate` (target 2 µs; aim for ~200 ns and zero
  allocations), `BenchmarkGridNeighbours` (1000 aircraft in 10 km^2).

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 27/27 vector cases pass with symmetry; `-race -shuffle=on` green; coverage >= 95 %.
- [ ] Lint clean; benchmarks reported with allocs/op; `doc.go` rewritten (C-10: the prediction is a straight line); CHANGELOG line; PR with outputs.

## Commits

`feat(cpa): compute the closest point of approach in a local tangent plane [WP-9 G-M1]`,
`feat(cpa): add the neighbour grid checked against brute force [WP-9 G-M1]`,
`test(cpa): run the 27 CPA vectors with symmetry [WP-9 G-M1]`.
