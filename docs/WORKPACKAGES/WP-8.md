# WP-8: `zones`

Branch `feat/WP-8-zones`. Milestone G-M1. Owns `zones/` exclusively.
Depends on WP-1 (`geodesy`) and WP-5 (`ed269` model and `Applies`);
start when both PRs are open. Consumers: `alerting` (WP-10), the
authority's `detect`, the USSP's `monitor` and `intent` checks.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.10`, `§6`, `§8.6` (the zone budget).
2. `core/enums.go` (ZoneType, Severity), `core/vertical.go`,
   `geodesy` (Polygon, Circle, BBox), `ed269` (GeoZone, Volume, Period,
   Applies), `terrain.Ground` and `geoid.Undulator` (interfaces only;
   `zones` takes resolved values in `Env`, not the interfaces, so it stays
   free of I/O).
3. `vectors/testdata/zones_vertical.json`: header `description` (how
   `terrain` is encoded), all 38 cases and their `why`.
4. LESSONS Z-06, Z-08, Z-09, Z-10, Z-11, R-09, D-01, D-02, D-04, T-09,
   C-09, INV-03.
5. Spec `04 §3.1` (altitude rules), `04 §3.3` (`zone_incursion`,
   `height_120m` fields), `09 §2` (120 m at the authority only).
6. Reference only: `utm/airspace/zones.py` (`Zone`, `needs_terrain`,
   `unjudgeable`), `utm/airspace/monitor.py` (`_judge_vertical`,
   `_check_height`).

## What to build

```go
type Limit struct{ ValueM float64; Ref core.VerticalRef }
type Zone struct{ Identifier, Country string; Type core.ZoneType; Restriction ed269.Restriction; Lower, Upper *Limit; Polygon *geodesy.Polygon; Circle *geodesy.Circle; BBox geodesy.BBox; Periods []ed269.Period }
func FromED269(z *ed269.GeoZone) (*Zone, error)              // feet converted exactly; one volume
func (z *Zone) ContainsHorizontally(p core.LatLon) (bool, error)
func (z *Zone) AppliesAt(at time.Time) bool
func (z *Zone) NeedsTerrain() bool                            // an AGL limit that is not a lower limit <= 0
func (z *Zone) NeedsGeoid() bool                              // any WGS84 limit
type GroundKind int  // GroundNotConfigured, GroundUnknown, GroundKnown
type Env struct{ Ground GroundKind; GroundM float64; UndulationM *float64 }
type Aircraft struct{ AltAMSLM *float64; AltSource core.AltSource }
type Policy struct{ PressureUncertaintyM float64; ConditionalSeverity core.Severity; MaxHeightAGLM *float64 }   // DefaultPolicy{250, warning, nil}
type Detail struct{ Identifier, Restriction string; VerticalKnown, WithinBand, LimitNotJudged *bool; NotJudged []string; HeightAGLM, AltHAEM, MaxHeightAGLM *float64 }
type Raise struct{ Kind string; Severity core.Severity; Detail Detail }
type Result struct{ Raise *Raise; NotEvaluated, LimitNotJudged bool }
func JudgeVertical(z *Zone, ac Aircraft, env Env, pol Policy) Result
func JudgeHeightLimit(ac Aircraft, env Env, pol Policy) Result
func Severity(t core.ZoneType, pol Policy) (core.Severity, bool)
type Index struct{...};  func NewIndex(zs []*Zone) *Index;  func (i *Index) Candidates(p core.LatLon) []*Zone
```

Rules (derive the exact decision table from the 38 cases; these are the
load-bearing ones):

- **Severity** (Z-10): PROHIBITED critical; REQ_AUTHORISATION warning;
  CONDITIONAL `pol.ConditionalSeverity` (warning in the vectors);
  NO_RESTRICTION raises nothing (`restriction-NO_RESTRICTION`). An
  authorisation lifting REQ_AUTHORISATION is the caller's concern
  (document; not in this package).
- **Each limit in its own reference** (Z-08): AMSL vs `AltAMSLM`; AGL vs
  `AltAMSLM - GroundM` (needs `GroundKnown`); WGS84 vs `AltAMSLM +
  UndulationM` (needs the undulation); feet already converted in
  `FromED269` (`feet-converted-*`: 2000 ft = 609.6 m); a missing limit is
  unbounded; a lower AGL limit `<= 0` is met by any airborne aircraft and
  needs no DEM (`agl-floor-at-ground-needs-no-terrain`); a lower AGL limit
  above ground needs terrain (`agl-floor-above-ground-needs-terrain`).
  Inclusive bounds as the vectors show (`amsl-band-500-700-at-*`: check
  450 out, 600 in, 750 out; the band test is `lower <= alt <= upper`).
- **A judged limit that excludes decides** whatever an unjudged one would
  say (`agl-floor-at-ground-amsl-ceiling-excludes`).
- **Unjudgeable** (Z-09): a PROHIBITED or REQ_AUTHORISATION zone whose
  only unjudged limit is AGL (no terrain configured, or unknown here)
  raises a **warning** with `vertical_known false`, `limit_not_judged
  true`, `not_judged ["AGL"]`, and counts `zone_limits_not_judged`
  (`prohibited-agl-ceiling-no-terrain-warns`,
  `req_authorisation-agl-ceiling-no-terrain-warns`). Otherwise
  (CONDITIONAL with AGL, or any zone with a WGS84 limit and no geoid) the
  zone is **not evaluated**: no raise, `NotEvaluated true`, counts
  `zone_checks_not_evaluated` (`conditional-agl-no-terrain-not-evaluated`,
  `conditional-agl-ground-unknown-not-evaluated`,
  `prohibited-wgs84-no-geoid-not-evaluated`).
- **Pressure altitude** (R-09): with `AltSource pressure`, inside the band
  as indicated keeps the zone's severity with `vertical_known false`,
  `within_band true`; inside only the band widened by
  `PressureUncertaintyM` each way is a warning with `within_band false`;
  outside the widened band nothing (`pressure-agl-zone-at-100m-agl`,
  `-300m-agl`, `-400m-agl`; `pressure-wgs84-zone-through-geoid-and-margin`);
  a zone without limits is judged as for anyone
  (`pressure-zone-without-limits-judged-as-for-anyone`); pressure plus an
  unjudged AGL carries both flags (`pressure-and-unjudged-agl-carry-both-flags`).
- **Detail numbers**: `height_agl_m` when an AGL limit was judged with
  known ground (`agl-ceiling-100m-above-ground`: 100.0); `alt_hae_m`
  when a WGS84 limit was judged (`wgs84-ceiling-inside`: 595.0). Details
  are rounded to 0.1 by the vectors' tolerance; return full precision and
  let the test compare with 0.1.
- **No altitude** (`AltAMSLM nil`, `AltSource none`): a zone with any
  limit is not evaluated and counted; a zone without limits is judged by
  horizontal containment alone (document; no vector; unit test).
- **Height limit** (D-04, `height-limit-*`): only with `pol.MaxHeightAGLM`
  set and `GroundKnown`; `height = alt - ground`; strictly greater than
  the limit raises `kind height`, warning, `height_agl_m`,
  `max_height_agl_m` (620.0 is allowed, 620.5 raises); unknown ground or
  no terrain -> not evaluated with **no counter** (the vector counters stay
  0 for the height limit: check `height-limit-ground-unknown-not-evaluated`);
  a pressure altitude is judged as indicated and flagged
  (`vertical_known false`).
- **Horizontal** (Z-06, Z-11, D-09): `ContainsHorizontally` checks the
  bbox first, then the polygon or the circle by geodesic distance from
  the published centre and radius; `Index.Candidates` returns the zones
  whose bbox contains the point (a simple grid of 0.1 deg cells or a sort
  on min-lat; 300 zones, sub-microsecond). Invalid positions return false
  (C-09).
- **Applicability** (T-09): `AppliesAt(capturedAt)` through
  `ed269.Applies`; the caller passes `captured_at`, never wall time
  (document).

## Vector test (`zones/vectors_test.go`)

Input: `zone` (nullable; an ED-269 feature -> `ed269.ParseZone` ->
`FromED269`), `aircraft{alt_amsl_m, alt_source}`, `terrain` (`"none"` ->
NotConfigured, `"unknown here"` -> Unknown, `{"ground_m": x}` -> Known;
decode as `json.RawMessage` and branch), `geoid_undulation_m` (nullable),
`max_height_agl_m` (nullable), `pressure_uncertainty_m`. Expected:
`raised[]{kind, severity, aircraft ["A"], detail{...}}` and `counters`.
With a zone: assume horizontal containment and applicability (the header
says so) and call `JudgeVertical`; with `zone null` call
`JudgeHeightLimit`. Compare detail floats with 0.1, flags exactly, and
absent detail keys as nil pointers (strict decode of `detail` into the
`Detail` struct with pointer fields; a key the vector does not have must
be nil in the result: make `Detail` carry pointers so this is testable).

## Other tests

- E-01 pairs for every branch above that the vectors leave implicit (no
  altitude, circle containment, `Index` hit and miss, `NeedsTerrain` /
  `NeedsGeoid` table).
- `FromED269` refuses two volumes and converts feet exactly.
- Benchmarks `BenchmarkJudgeZone` (containment + vertical for a
  100-vertex polygon), `BenchmarkJudgeHeight`, and an `Index` lookup over
  300 zones.

## Done when

- [ ] 38/38 vector cases pass; `-race -shuffle=on` green; coverage >= 90 %.
- [ ] Lint clean; benchmarks reported; `doc.go` rewritten; CHANGELOG line; PR with outputs.

## Commits

`feat(zones): build zones from ED-269 and judge horizontal containment and applicability [WP-8 G-M1]`,
`feat(zones): judge each vertical limit in its own reference with the pressure margin [WP-8 G-M1]`,
`feat(zones): judge the height limit over known ground only [WP-8 G-M1]`,
`test(zones): run the 38 vertical judgement vectors [WP-8 G-M1]`.
