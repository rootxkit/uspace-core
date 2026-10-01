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
  pressure margin, the height limit over known ground, a bounding-box
  grid `Index`, and the counters `zone_checks_not_evaluated`,
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

## [0.1.0] - unreleased (G-M1)

Tagged when every file in `vectors/testdata/` passes against its package:
`odid`, `geodesy`, `terrain`/`geoid`, `ed269`, `regnum`/`serial`,
`identify`, `zones`, `cpa`/`alerting`, `timeplace`/`rid`, `sources`,
`auth` (receiver HMAC).
