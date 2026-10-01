# Changelog

All notable changes to `uspace-core`. The format follows Keep a Changelog;
versions follow spec `00 §6.3`: within a major only additive changes; a
behavioural change to a judgement is a major even when the Go signature is
unchanged, and ships with the changed vector and the regulation or standard
clause behind it; two majors are maintained in parallel for six months.

Each entry names the work package and the vectors it affects.

## [Unreleased]

### Added

- Plan and scaffolding: `docs/PLAN.md`, work package briefs, frozen base
  types in `core`, frozen ODID message types, the `vectors` harness with
  the 16 knowledge vector files (596 cases) vendored from
  `uspace-lab@c6b7f33` (generated from `utm@484cd22`), CI, lint and
  Makefile. [WP-0]
- `geodesy`: WGS84 Vincenty inverse, haversine (spoof distance only),
  local tangent plane with antimeridian wrapping, circle and polygon
  containment with holes, bounding boxes and ring validation; passes
  `geodesy.json` (17 cases). [WP-1]
- `serial` (CTA-2063-A and the class rule), `regnum` (configurable
  registration format, public part and compare key) and `sources`
  (source switches, follower by version and epoch).
  `serials_and_registration.json` 33/33 and `source_control.json` 8/8,
  with `public-part-GEO-OP-ABC` a known deviation: the EU secret part is
  stripped only after a valid public number, so `GEO-OP-ABC` no longer
  compares as `GEO-OP` (LESSONS G-04). [WP-4]
- `internal/pgm` (bounded binary PGM P5 parser), `geoid` (GeographicLib
  grids, `UndulationM`, `AMSLFromHAE`/`HAEFromAMSL`) and `terrain`
  (`CellName`, DEM tiles, `ParseIndex`, a bounded LRU `Store` that counts
  unknown, nodata and unreadable-tile answers and retries a failed tile
  once per `RetryAfter`). Vectors: `terrain_geoid.json`, 18 synthetic
  cases pass, 30 GeographicLib cases skip without `USPACE_GEOID_DIR`.
  [WP-2]
- `identify`: registry `Snapshot` with exact-then-unambiguous-fold serial
  lookup, `ResolveBroadcast`, `ResolveRemoteID`, `ResolveBound`,
  `SerialConflict`, `Unavailable` (no vector yet) and the spoofing guard
  `JudgeFleet`. `identification_status.json` 37/37 and `fleet_match.json`
  10/10, with the predecessor's reasons `fleet` and `relay_binding` read
  as the spec's `matched` and `session_binding` (04 §3.2). [WP-7]

## [0.1.0] - unreleased (G-M1)

Tagged when every file in `vectors/testdata/` passes against its package:
`odid`, `geodesy`, `terrain`/`geoid`, `ed269`, `regnum`/`serial`,
`identify`, `zones`, `cpa`/`alerting`, `timeplace`/`rid`, `sources`,
`auth` (receiver HMAC).
