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
- `odid` codec: `Decode`, `DecodeMessage`, `Encode`, `EncodePack`,
  `TypeOf` and the wire sentinels, from the opendroneid-core-c layout;
  passes `odid_decode.json` (187/187, every accepted frame re-encoded byte
  for byte); `FuzzDecode`, `FuzzEncodeLocation` and the three codec
  benchmarks. Adds `odid.Unknown` for undefined types kept with
  `KeepSkipped`. [WP-3]
- `serial` (CTA-2063-A and the class rule), `regnum` (configurable
  registration format, public part and compare key) and `sources`
  (source switches, follower by version and epoch).
  `serials_and_registration.json` 33/33 and `source_control.json` 8/8,
  with `public-part-GEO-OP-ABC` a known deviation: the EU secret part is
  stripped only after a valid public number, so `GEO-OP-ABC` no longer
  compares as `GEO-OP` (LESSONS G-04). [WP-4]
- `auth`: Remote ID receiver authentication (HMAC-SHA256 over the exact
  report bytes, +-30 s window, per-receiver bounded nonce memory) passing
  the 14 `rid_receiver_auth.json` cases, and the RS256-only JWT verifier
  with allow-listed issuers, kid-selected cached JWKS with a rate-limited
  refresh, and the token issuer, on `lestrrat-go/jwx/v3`. [WP-11 G-M1]
- `vectors/testdata/jwt_verify.json` (16 cases, local until the lab merges
  it): the ecosystem JWT knowledge vector, written by
  `auth/internal/genvectors` with a discarded key, and run by `auth`.
  [WP-11 G-M3]
- `internal/pgm` (bounded binary PGM P5 parser), `geoid` (GeographicLib
  grids, `UndulationM`, `AMSLFromHAE`/`HAEFromAMSL`) and `terrain`
  (`CellName`, DEM tiles, `ParseIndex`, a bounded LRU `Store` that counts
  unknown, nodata and unreadable-tile answers and retries a failed tile
  once per `RetryAfter`). Vectors: `terrain_geoid.json`, 18 synthetic
  cases pass, 30 GeographicLib cases skip without `USPACE_GEOID_DIR`.
  [WP-2]
- `timeplace`: Remote ID broadcast time reconstruction with the four
  fallbacks, network state and batch placement on the ingest clock;
  `rid`: the identity-per-transmitter Tracker, utm's uuid5 aircraft ids,
  AMSL altitude selection with the pressure fallback and hold, NED
  velocity and the airborne rule. Vectors `rid_time.json` (25),
  `rid_identity.json` (24), `pressure_altitude.json` (16). [WP-6]
- `ed269`: strict ED-269 parse and export (both wrappers, UTF-8 BOM,
  every problem with its JSON path, capped at 100; bounded bytes, depth
  and ring vertices; shapes past 180 degrees of longitude refused) and
  zone applicability; passes `ed269_parse.json` (52 cases) and
  `zones_applicability.json` (32 cases). [WP-5]
- `identify`: registry `Snapshot` with exact-then-unambiguous-fold serial
  lookup, `ResolveBroadcast`, `ResolveRemoteID`, `ResolveBound`,
  `SerialConflict`, `Unavailable` (no vector yet), `IsOurs` and the
  spoofing guard `JudgeFleet`. `identification_status.json` 37/37 and
  `fleet_match.json` 10/10, with the predecessor's reasons `fleet` and
  `relay_binding` read as the spec's `matched` and `session_binding`
  (04 §3.2). Decisions without a vector: an unrecognised registration
  status is `unknown_operator` (`not_in_registry` for the UAS,
  `owner_unknown` for its owner) so it raises an identification
  incident; a bound aircraft whose owner is not projected is
  `owner_unknown`; a bound track without a registry is
  `registry_unavailable`; a serial shared by two aircraft, or a replaced
  row's serial, matches nothing; `JudgeFleet` withholds with a
  `FieldError` on an unusable live window or spoof distance. `serial.FoldKey`
  folds ASCII letters only, so a look-alike such as U+017F never folds
  onto one of our serials; `regnum` matches the head before the EU secret
  part ignoring its case, so `geoabcd1234efgh-x9z` compares as
  `GEOABCD1234EFGH`. [WP-7]
- `zones`: zones from ED-269 (one volume, feet exact), horizontal
  containment (bounding box, polygon with holes, circle by geodesic
  distance, antimeridian), applicability at captured_at, each vertical
  limit in its own reference with the unjudged-AGL warning and the
  pressure margin (capped at the zone's own severity), the height limit
  over known ground, USPACE at info, a bounding-box grid `Index` bounded
  per zone and in total (`IndexLimits`), every missing reference in
  `Result.Reasons`, and the counters `zone_checks_not_evaluated`,
  `zone_limits_not_judged`, `height_checks_not_evaluated`; passes
  `zones_vertical.json` (38 cases). [WP-8]
- `cpa`: closest point of approach in the mid-latitude tangent plane
  with the older sample advanced, `t_cpa` clamped to >= 0, the vertical
  gap at `t_cpa`, a conflict as a loss of separation anywhere in the
  window (closed-form intervals, with `LoSStartS`), inside the minima
  now as a conflict and pressure
  tracks as unknown vertical; non-finite inputs, an invalid policy and
  overflow are not judged (with a reason), never judged clear; results
  are identical in either order; a pair within the polar limit (10 x
  the neighbour radius plus the window's travel, from a pole) is not
  judged. Neighbour `Grid` with per-band longitude columns, safe across
  the antimeridian and at the poles, checked against brute force.
  `cpa.json` 27/27 in both orders. [WP-9]
- `alerting`: the monitor's alert state machine. Admission counts and
  ignores backlog, late (on the ingest-to-monitor leg only), out-of-order
  (per source clock), disabled-source and invalid samples; conflicts are
  judged by `cpa` against the grid's neighbours and every active partner,
  zones and the height limit by `zones`, `identification` beside an
  incident zone's alert and `identification_mismatch` on every live
  sample. Raise once, refresh silently, a severity change raised again;
  clears are `resolved` (hysteresis, only on a judgement), `stale`,
  `source_disabled` (through a `sources.Follower`, by version and epoch),
  `landed` and `flight_ended` (`Drop`); the aircraft cap evicts only
  aircraft without an active alert and otherwise refuses new ids
  (`rejected_capacity`, `CapacityExceeded`), and no one source adds new
  ids past `MaxSourceShare` of it in alert holders (`rejected_source_share`), with refusals
  reported in `Events.Refused` at a limited rate; a resolved conflict carries the clearing
  judgement's separation in `Cleared.ClearingDetail` beside the last
  in-conflict detail (C-14). Zone alerts are keyed by country and
  identifier, with a counted fallback for a true duplicate. Nothing unjudged, missing or non-finite clears an
  alert. `Active` ranks by severity and `LoSStartS`. Passes
  `alert_lifecycle.json` 28/28 with one counted override:
  `disarming-clears-as-stale` clears as `landed` (owner decision, plan
  §11 gap 4). [WP-10]

### Changed

- Vectors re-vendored from `uspace-lab@aa5187e` (17 files, 660 cases),
  which records the wave 1-3 decisions in the vectors themselves: spec
  04 §3.2 reason codes, the G-04 strip and ASCII-only folding (G-12),
  thresholds that withhold (E-15), the ED-269 type enumeration, every
  missing zone reason, the widened band capped at the zone's severity,
  U-space at info, `landed` on disarm, placements behind or ahead
  refused (T-13), the aircraft cap and source share (C-18), and loss of
  separation over the half-open window (C-19). `jwt_verify.json` now
  comes from the lab and leaves `local_files`. Every override, rename
  and known deviation in the vector tests is removed; each test passes
  its file as written. [WP-0]
- `zones`: a PROHIBITED or REQ_AUTHORISATION zone with a WGS84 limit and
  no geoid now warns with `limit_not_judged`, as for an AGL limit with
  no DEM, and keeps the `no_geoid` reason (S-37, owner decision; LESSONS
  Z-09). A CONDITIONAL zone stays not evaluated. Behaviour change to a
  judgement at `v0.x`, pinned by `zones_vertical.json`. [WP-8]

## [0.2.0] - unreleased (G-M2)

The standards types: ED-318 zones, F3411-22a network Remote ID and
F3548-21 strategic coordination. Adds the local knowledge vector
`ed318_roundtrip.json` (21 cases; 681 in all with the 660 of the lab).

### Added

- `f3411`: the F3411-22a types generated, types only, by oapi-codegen
  v2.8.0 from uastech/standards `remoteid/updated.yaml` at `dd4016b` (the
  file uas_standards `6e182f4` generates its v22a module from), with the
  one-element anyOf wrappers made aliases so the package needs only the
  standard library; the Net* and data-field constants and the
  `rid.service_provider` / `rid.display_provider` scopes; special values
  decoding to nil (speed 255, track 361, vertical speed 63, height, alt
  and pressure altitude -1000) with `SpeedIsMax` for 254.25; the airborne
  rule; `LatLon`, `Altitude.HAEM` (W84 and M only),
  `Volume4DToZonesEnvelope` and a bounded `UnmarshalRIDFlight`. [WP-12]
- `f3548`: the F3548-21 types generated the same way from
  interuss/astm-utm-protocol `utm.yaml` at `1d3d8fb`; the five `utm.*`
  scopes and every uas_standards constant; `DSSStates`;
  `Altitude.HAEM`, `Volume4DToZonesEnvelope` and a bounded
  `UnmarshalOperationalIntent`. [WP-12]
- `ed318`: parse and validate ED-318 FeatureCollections on receipt
  (never repair) with field names from the ED-318 JSON schema
  (UASGeoZones/ED-318 `e98b292`) and uas_standards, export equal by value,
  the ED-269 mapping both ways with what each side cannot hold refused by
  name, applicability with the daylight events BMCT, SR, SS and EECT
  (`NOAADaylight`, within 7 s of astropy in the tests; `FixedDaylight`),
  an unresolvable event reported as not evaluated, and `ToZones`. The
  geometry's vertical-limit names (`layer`) are taken from both sources
  and remain unverified against the EUROCAE text. Passes
  `ed318_roundtrip.json` (21/21), which `ed318/internal/genvectors` writes.
  [WP-12]

## [0.1.0] - unreleased (G-M1)

Tagged when every file in `vectors/testdata/` passes against its package:
`odid`, `geodesy`, `terrain`/`geoid`, `ed269`, `regnum`/`serial`,
`identify`, `zones`, `cpa`/`alerting`, `timeplace`/`rid`, `sources`,
`auth` (receiver HMAC).
