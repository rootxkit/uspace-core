# WP-5: `ed269`

Branch `feat/WP-5-ed269`. Milestone G-M1. Owns `ed269/` exclusively.
Depends on WP-0 only. Consumers: `zones` (WP-8) and `alerting` (WP-10)
use the zone model and `Applies`; `ed318` (WP-12) maps from it; the CISP
imports ED-269 files. On the critical path for WP-8: land the model and
applicability first, the full strict parser second.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.7`, `§6` (tolerance rule for
   `must_include`), `§8.4`, `§8.5`, `§11` gap 6.
2. `core/enums.go` (ZoneType and the ED-269 spelling), `core/vertical.go`,
   `core/errors.go`.
3. `vectors/testdata/ed269_parse.json`: header (`limits`), the three
   accepted cases (`valid-file-round-trips-unchanged`,
   `list-wrapper-and-byte-order-mark`,
   `null-optional-fields-read-absent-export-absent`), every `refuse-*`
   (49) with its `must_include`.
4. `vectors/testdata/zones_applicability.json` (32 cases).
5. LESSONS Z-01 to Z-07, T-09, E-10.
6. Field names: InterUSS `uas_standards` `eurocae_ed269.py` (fetch it),
   the vector documents themselves. Reference only:
   `utm/airspace/ed269.py` (it is the generator of the expected values;
   read its `_zone`, `_volume`, `_period`, `_daily`, `_clock`, `feature`,
   `document` for the exact refusal reasons, then write your own).

## What to build

Types and functions of `docs/PLAN.md §3.7`. Key points:

**Model.** `GeoZone` keeps optional fields as pointers; an unknown field
is a refusal, never kept. `Geometry` is a list on the wire but must hold
exactly one volume (`refuse-two-volumes`: "a zone with more than one
volume is refused rather than half-imported", Z-04). Limits are kept as
written in `Uom` with `LowerM()/UpperM()/RadiusM()` converting feet by
`core.FeetToMetres` exactly. Coordinates are GeoJSON `[lon, lat]` on the
wire and `core.LatLon` in memory (Z-03). `WGS84` as a vertical reference
is accepted and written (Z-05) and the package doc says a file using it
is not a published ED-269 file.

**Applicability** (Z-07, T-09; 32 vectors): a `Period` is `permanent:
YES` (applies always, must carry no dates: `refuse-permanent-with-an-end`)
or `permanent: NO` with `startDateTime`/`endDateTime` (RFC 3339 **with
offset**; a naive instant is refused: `refuse-naive-date`) and/or a
`schedule` of `DailyPeriod{day: [MON..SUN|ANY], startTime, endTime}`
where the clock times carry an offset (`09:00Z`, `08:00+04:00`,
`22:00:00.00Z`), both ends must have the same offset
(`refuse-schedule-with-two-offsets`), differ
(`refuse-schedule-start-equals-end`), and a schedule without any offset
is refused. A non-permanent period with neither dates nor schedule is
refused (`refuse-not-permanent-and-never`). Evaluation: both ends
included; an end before its start runs past midnight and belongs to the
day it starts on (`night-across-midnight-utc-*`); the weekday is judged in
the schedule's own offset (`schedule-in-an-offset-uses-that-offsets-day-*`);
dates bound a schedule; `Applies` is true when any period applies; the
`23:59:59` published end leaves the last second uncovered
(`published-end-23-59-59-*`). `at` must carry a location; `Applies`
converts to UTC and never uses `time.Local`.

**Parse** (Z-01, Z-02, Z-03, Z-06). Accept a UTF-8 BOM; accept both
wrappers (`features` and `UASZoneList`, plus the document-level `title`,
`description`, `formatVersion`, `createdAt` keys as the valid fixtures
show; an unknown document key is refused:
`refuse-document-unknown-document-field`). Decode into `map[string]any`
first? No: use `json.Decoder` with `UseNumber` into an ordered structure
of your own so that numbers round-trip by value (`5.0` written as `5`),
strings are typed strictly (a number as a string, `"0"`, is refused:
`refuse-limit-as-a-string`), and depth is bounded (`refuse-document-
nested-too-deeply`: the input is 100000 `[` then `]`; count depth while
tokenising and refuse past `Limits.MaxDepth` (32) without recursing).
Byte-level refusals: not UTF-8 (`//4=`), not JSON, not an object (`[]`),
no features (`{"zones":[]}`), features not a list. Every problem is
`Problem{Field, Reason}` with the path (`features[0].identifier`, `$`
for the document); report all problems, capped at `MaxProblems` 100 with
`Truncated` counting the rest (`refuse-reports-every-problem-not-only-the-first`);
a duplicate identifier names both places
(`refuse-repeated-identifier-names-both-places`). Enumerations: country
is 3 upper-case letters; `type` COMMON|CUSTOMIZED; restriction the four
ED-269 spellings (the Z spelling refused with a reason naming the
spelling: `refuse-american-spelling-req-authorization`); reasons from
the uas_standards list, no repeats, at most 9; purpose
AUTHORIZATION|NOTIFICATION|INFORMATION (the S spelling refused);
`permanent` YES|NO exactly; days MON..SUN|ANY (`MONDAY` refused); uom
M|FT; references AGL|AMSL|WGS84; shapes Polygon|Circle (`Ellipse`
refused; a circle needs `center` and `radius` and no `coordinates`);
rings closed with >= 4 positions, latitude in range, at most
`MaxRingVertices` 5000; upper above lower when both given; strings
within `identifier 7`, `name 200`, `message 200`; `region` an integer;
`regulationExemption` YES|NO; `zoneAuthority` required and each entry's
fields known (`fax` refused). Limits come from `Limits` with
`DefaultLimits` equal to the header.

**Export**: `Feature(zone)` and `Export(doc)` write back exactly the
input of the accepted cases: key order as in the input is not required
by the vector (compare by value after `json.Unmarshal`), null optionals
absent, numbers by value. Keep unknown-but-accepted document keys
(`title`, `description`) and `extendedProperties` verbatim.

## Vector tests

`ed269/vectors_test.go`:

- `TestVectorsED269Parse`: inputs come as `document` (object), `zone`
  (object; use `ParseZone`), `document_utf8_with_bom_base64` or
  `bytes_base64` (raw bytes, use `Parse`), `bytes_description` ("100000
  '[' then 100000 ']'": build it). Accepted: compare `expected.zones`
  summaries (`identifier`, `restriction`, `shape`, `lower_m`, `upper_m`,
  `lower_reference`, `upper_reference`, `radius_m`, `periods` as the
  vector shows; metres tol 1e-9) and, when `export` is present, `Export`
  decoded equals `export` decoded (`reflect.DeepEqual` on
  `json.Unmarshal` into `any` with `UseNumber`, or a canonicalised
  comparison; log the diff). Refused: `must_include` is binding: when
  `field` is given the problem list contains one with that exact field,
  else one whose field `HasSuffix(field_endswith)`, and its reason
  `Contains(reason_contains)`. The full `problems` list is compared as a
  set; a difference is `t.Logf` with a diff, not a failure (plan §11
  gap 6); the PR reports how many cases differ.
- `TestVectorsZonesApplicability`: `applicability` (raw JSON ->
  `ParseApplicability`), `at` -> `Applies` exact.

## Other tests

- E-01 pairs beyond the vectors: a period exactly at its end (inside), a
  schedule on Sunday 23:00+04:00 (UTC Saturday), `ANY` days, dates
  bounding a schedule at both ends.
- Bounds (E-10): 5001 vertices refused, 5000 accepted; 101 problems
  truncated to 100 with `Truncated == 1`; depth 33 refused, 32 accepted;
  `MaxBytes` exceeded refused before parsing.
- `FuzzParse` seeded with every vector document and the byte cases:
  never panics; when accepted, `Parse(Export(doc))` accepts and equals.
- Benchmarks `BenchmarkParseDocument` (a 1400-vertex zone plus 50 small
  ones), `BenchmarkApplies` (10 periods with schedules).

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 52 + 32 vector cases pass (with `must_include` binding and the full-list diff logged); `-race -shuffle=on` green.
- [ ] Coverage >= 90 %; fuzz clean 10 s; lint clean.
- [ ] `doc.go` rewritten (sources of the field names, the WGS84 extension note); CHANGELOG line; PR with outputs and the count of cases whose full problem list differs.

## Commits

`feat(ed269): add the zone model and applicability evaluation [WP-5 G-M1]`,
`feat(ed269): parse documents strictly with paths and reasons [WP-5 G-M1]`,
`feat(ed269): export zones so that a parsed file round-trips [WP-5 G-M1]`,
`test(ed269): run the 52 parse and 32 applicability vectors [WP-5 G-M1]`,
`test(ed269): fuzz the parser and benchmark it [WP-5 G-M1]`.
