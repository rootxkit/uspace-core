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

## [0.1.0] - unreleased (G-M1)

Tagged when every file in `vectors/testdata/` passes against its package:
`odid`, `geodesy`, `terrain`/`geoid`, `ed269`, `regnum`/`serial`,
`identify`, `zones`, `cpa`/`alerting`, `timeplace`/`rid`, `sources`,
`auth` (receiver HMAC).
