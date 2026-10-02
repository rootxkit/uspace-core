# Changelog

All notable changes to `uspace-core`. The format follows Keep a Changelog;
versions follow spec `00 §6.3`: within a major only additive changes; a
behavioural change to a judgement is a major even when the Go signature is
unchanged, and ships with the changed vector and the regulation or standard
clause behind it; two majors are maintained in parallel for six months.
`docs/RELEASING.md` says how a release is cut and how a behaviour change
is made.

Each entry names the work package and the vectors it affects. A pull
request that changes `vectors/testdata/*.json` adds one line per file,
`vectors: <file> (<clause>)`, naming the regulation or standard clause;
for a behaviour change the line sits under the heading of the next major
(`scripts/semver-gate.sh` checks both).

## [Unreleased]

## [1.3.0] (WP-18)

Additive to 1.2.0: the direct geodesic problem in `geodesy`, for the
CISP's circle outline in place of its local solver. No vector changed;
`uspace-lab@6b5b286` remains the pin.

### API declared stable

From `1.3.0` `geodesy.Destination` is stable: its signature, the
agreement with `Inverse` to under 0.1 mm, the bearing taken modulo 360,
the longitude wrapped into [-180, 180], and NaN for an invalid input. No
counter is added.

### Added

- `geodesy`: `Destination` solves the direct geodesic problem on WGS84
  with Vincenty's formulae, the counterpart of `Inverse` to under 0.1 mm
  both ways; a bearing is taken modulo 360, the longitude is wrapped
  across the antimeridian, and an invalid input gives NaN. Nothing that
  exists changes its output. No vector changed. [WP-18]

## [1.2.0] (WP-17)

Additive to 1.1.0: one shared signer for console and portal session
tokens in `auth`, for the authority and the USSP in place of their own. No
vector changed; `uspace-lab@6b5b286` remains the pin.

### API declared stable

From `1.2.0` the new identifiers of `auth` are stable:
`Issuer.IssueSession`, `SessionClaims` and `SessionScope`, and the wire
form of the session token (header `alg RS256`, `kid`, `typ JWT`;
payload exactly `iss`, `aud`, `sub`, `scope`, `roles`, `realm`, `iat`,
`exp`, `jti`, cross-plan Appendix A). No counter is added.

### Added

- `auth`: `Issuer.IssueSession`, `SessionClaims` and `SessionScope`
  (`"session"`) sign a console or portal session token with exactly the
  cross-plan Appendix A claims (iss, aud, sub, scope, roles, realm, iat,
  exp, jti; RS256, kid); every token it issues verifies under
  `StrictSessionClaims`. `Issue` and the verifier are unchanged. No
  vector changed. [WP-17]

## [1.1.0] (C1)

Additive to 1.0.0 for the cross-plan reconciliation C1: JWS helpers and
audience lists in `auth`, grid cells in `geodesy/cell`, the provider
identification basis in `core` and `SkipConflicts` in `alerting`.
No vector changed; `uspace-lab@6b5b286` remains the pin.

### API declared stable

From `1.1.0` the new package `geodesy/cell` is stable, with the cell
names `c5:<lat_idx>:<lon_idx>` and `c3:<lat_idx>:<lon_idx>` and the
index formula as wire form. So are the new identifiers of:

- `auth`: `KeyRing`, `SigningKey`, `MaxRingKeys`, `SignDetached`,
  `DetachedHeader`, `ParseDetachedHeader`, `DetachedConfig`,
  `DetachedVerifier`, `Signature`, `SignCompact`, `CompactClaims`,
  `CompactConfig`, `CompactVerifier`, `DefaultDetachedMaxAge`,
  `DefaultMaxDetachedPayloadBytes`, `Config.Audiences`,
  `Config.StrictSessionClaims`, `Claims.Roles`, `Claims.Realm`, and the
  wire forms of the detached (`X-JWS-Signature`) and compact delivery
  JWS;
- `core`: `BasisProvider`;
- `alerting`: `Config.SkipConflicts`.

The new counter names are stable too: `rejected_b64`, `rejected_crit`,
`rejected_publisher`, `rejected_iat`, `rejected_too_large`,
`key_ring_full` and `conflict_checks_skipped`. The `-kind jws_detached`
and `-kind jws_compact` modes of `auth/internal/genvectors` are not
stable, like the rest of that command.

### Added

- `core`: `BasisProvider` (`"provider"`), the identification basis of a
  flight a USSP reports through the F3411 network or an F3548 peer
  (Q-A8); nothing in `identify` sets it. [WP-16 C1]
- `alerting`: `Config.SkipConflicts` (default false) and the counter
  `conflict_checks_skipped`: a monitor whose owner discards conflict
  alerts skips the neighbour grid and `cpa.Evaluate` and still judges
  zones, the height limit and identification (Q-A9). [WP-16 C1]
- auth: `KeyRing` holds a publisher's signing keys (one active, retired
  keys kept in its JWKS for a two-key overlap; at most `MaxRingKeys`,
  counted as `key_ring_full`), with `Issuer` sharing the ring's JWKS.
  No vector changed [WP-14 C1]
- auth: `SignDetached` and `DetachedVerifier` for `X-JWS-Signature`
  (RFC 7515 Appendix F, RFC 7797 `b64:false`, `crit:["b64"]`, RS256,
  `iat` within `DefaultDetachedMaxAge`), `ParseDetachedHeader`, and the
  counters `rejected_b64`, `rejected_crit`, `rejected_publisher`,
  `rejected_iat`, `rejected_too_large`. No vector changed [WP-14 C1]
- auth: `SignCompact` and `CompactVerifier` for the compact delivery JWS
  (M19: a JWT carrying `iss`, `aud`, `sub`, `iat`, `jti` and the message
  as `body`, returned byte for byte). No vector changed [WP-14 C1]
- auth: `Config.Audiences`, `Claims.Roles`, `Claims.Realm` and
  `Config.StrictSessionClaims` (M18, M20). With `Audiences` empty the
  verifier behaves as in 1.0.0. `StrictSessionClaims` (default off)
  refuses a token whose `roles` is not an array of strings, or whose
  `realm` is not a string, as `rejected_claims`; off, such a claim is
  ignored as in 1.0.0, so no judgement changes. Every uspace system
  enables it. No vector changed [WP-14 C1]
- auth/internal/genvectors: `-kind jws_detached` and `-kind jws_compact`
  write the JWS vector files proposed to uspace-lab (not vendored here)
  [WP-14 C1]
- `geodesy/cell`: the `c5` (0.1 degree) and `c3` (1 degree) partition
  cells of spec `05 §3` (M35): `Of`, the names `c5:<lat_idx>:<lon_idx>`
  with a strict `Parse`, `Parent`, `Children`, `Ring1`, `BBox`,
  `Centre` and a bounded `Cover` (`MaxCoverDefault`). No vector file
  changes; the cell cases are proposed to the lab. [WP-15 C1]

## [1.0.0] (G-M3)

The first stable release. Every vector file comes from `uspace-lab`
(`uspace-lab@6b5b286`, 18 files, 682 cases; `local_files` is empty), and
the semver rules of `00 §6.3` are enforced in CI.

### API declared stable

From `1.0.0` the exported API of these packages is stable: within `v1`
only additive changes (new functions, new optional fields, new constants,
new vector cases that existing behaviour passes); a change to what a
judgement returns is a new major even when no signature changes.

`core`, `vectors`, `geodesy`, `terrain`, `geoid`, `odid`, `regnum`,
`serial`, `sources`, `ed269`, `timeplace`, `rid`, `identify`, `zones`,
`cpa`, `alerting`, `auth`, `ed318`, `f3411`, `f3548`.

This is the API of `docs/PLAN.md` §3 together with what the work packages
added to it (Go's compatibility rule covers every exported identifier of a
`v1` module, listed in §3 or not). It includes the names that cross a
boundary as data: counter names (E-09), `core.FieldError.Field` paths,
the enumerations of `core` (reasons, statuses, severities, zone types,
time and altitude sources), and the meaning of every vector case.

Deliberately not stable (may change in any release):

- Packages under `internal/` (`internal/pgm`, `f3411/internal/oapialias`)
  and the commands `auth/internal/genvectors`,
  `ed318/internal/genvectors` and `scripts/manifest-list`.
- The wording of error and problem texts (`core.FieldError.Reason`,
  `error` strings), except the phrases a vector pins (`reason_contains`,
  `must_include`). Match on fields, sentinels and counters, not on text.
- The full ED-269 problem list beyond each case's `must_include`
  (plan §11 gap 6): only `must_include` binds.
- Orders documented as unspecified, such as the neighbour order of
  `cpa.Grid`.
- Performance: the ns/op figures in `docs/bench-targets.txt` are design
  budgets, reported and never gated.
- The repository's scripts, Make targets and CI jobs.

Stable as pinned today, but expected to be revisited; each change would
be a new major with its vector and clause:

- `ed318`: the vertical-limit names (`layer`), an absent `uom` read as
  metres, and a circle's radius in metres are unverified against the
  EUROCAE ED-318 text (`ed318/doc.go`).
- `f3411`, `f3548`: types generated by oapi-codegen v2.8.0 from F3411-22a
  and F3548-21; a new standard version is a major (`04 §4`).
- `identify.Unavailable` and the version and epoch rule of
  `sources.Follower` have no vector yet (plan §11 gaps 5 and 10); a lab
  vector that disagrees would make a major.

### Added

- The semver gate: `scripts/semver-gate.sh` and the `semver-gate`
  workflow on pull requests. A changed vector file needs a CHANGELOG
  line with its clause and a new lab pin; a behaviour change (a changed
  input or expected value, a removed case or file, a new file, a changed
  header) also needs a new major heading and the label
  `behaviour-change`; added cases alone are additive. Fixture tests in
  `scripts/test-release-gates.sh`, each failing case with its passing
  twin. [WP-13]
- The `tag` job (ci.yml, `v*` tags only): `scripts/release-check.sh`
  (tag equals the top CHANGELOG heading, nothing left under
  Unreleased, module path fits the major, `local_files` empty from v1,
  the tag on `main` or `release/vN`, every manifest file has a vector
  test, the vendored vectors match the lab pin), then
  `scripts/consumer-check.sh` on the published module, then the GitHub
  release from this file. [WP-13]
- `scripts/consumer-check.sh`: a scratch module requires
  `uspace-core`, runs its own `RunOwned(t, "ussp", ...)` test on
  `cpa.json` and every `TestVectors*` of the module from inside it,
  proving that the vectors and their testdata travel with the module
  (plan §6, §11 gap 12). [WP-13]
- `scripts/manifest-coverage.sh`, the manifest coverage step of the
  vectors job as a script, strict on tags. [WP-13]
- `docs/RELEASING.md`: cutting a release, making a behaviour change,
  the `release/vN` branches, how a system upgrades, the `/v2` module
  path rule, and the checks branch protection requires. [WP-13]

### Changed

- Vectors synced from `uspace-lab@6b5b286`. `ed318_roundtrip.json` is now
  a lab file (lab PR #5); its bytes are unchanged, and `local_files` is
  empty. [WP-13]
- `scripts/sync-vectors.sh` retires a local file the lab now carries and
  writes `SHA256SUMS` in text mode on every platform. [WP-13]

### Vectors

The two vector files G-M3 adds to the knowledge set, both shipped
earlier and both from the lab from this release:

- vectors: jwt_verify.json (spec 00 §6.2 and 06 §3: RS256 only, `kid` from the allow-listed issuer's JWKS, `aud`, `exp`/`nbf` with 30 s skew, `jti`, scopes), 16 cases, added in 0.1.0 by WP-11
- vectors: ed318_roundtrip.json (EUROCAE ED-318 JSON schema at UASGeoZones/ED-318 `e98b292`; spec 02 F1-F3, 04 §3.4; Regulation (EU) 2021/664 Art. 3(4)), 22 cases, added in 0.2.0 by WP-12

## [0.2.0] - 2026-10-01 (G-M2)

The standards types: ED-318 zones, F3411-22a network Remote ID and
F3548-21 strategic coordination. Adds the local knowledge vector
`ed318_roundtrip.json` (22 cases; 682 in all with the 660 of
`uspace-lab@aa5187e`).

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
  `ed318_roundtrip.json` (22/22), which `ed318/internal/genvectors` writes.
  [WP-12]
- `geoid` workflow: the 30 GeographicLib reference cases of
  `terrain_geoid.json` run against the real EGM96 and EGM2008 grids
  (`scripts/fetch-geoid.sh` pins them by SHA-256 and caches them) and
  fail if any is skipped; on changes to geoid, terrain, pgm or vectors
  and on release tags. [WP-2]

## [0.1.0] - 2026-10-01 (G-M1)

Every file in `vectors/testdata/` passes against its package
(`uspace-lab@aa5187e`, 17 files, 660 cases); `go test ./...` is the
proof. Packages: `core`, `vectors`, `geodesy`, `internal/pgm`,
`terrain`, `geoid`, `odid`, `regnum`, `serial`, `sources`, `ed269`,
`timeplace`, `rid`, `identify`, `zones`, `cpa`, `alerting`, `auth`.

| Vector file | Package | Cases |
|---|---|---|
| `alert_lifecycle.json` | `alerting` | 35 |
| `cpa.json` | `cpa` | 37 |
| `ed269_parse.json` | `ed269` | 54 |
| `fleet_match.json` | `identify` | 14 |
| `geodesy.json` | `geodesy` | 17 |
| `identification_status.json` | `identify` | 43 |
| `jwt_verify.json` | `auth` | 16 |
| `odid_decode.json` | `odid` | 187 |
| `pressure_altitude.json` | `rid` | 16 |
| `rid_identity.json` | `rid` | 24 |
| `rid_receiver_auth.json` | `auth` | 14 |
| `rid_time.json` | `timeplace` | 25 |
| `serials_and_registration.json` | `serial` (with `regnum`) | 41 |
| `source_control.json` | `sources` | 8 |
| `terrain_geoid.json` | `terrain` (with `geoid`) | 48 |
| `zones_applicability.json` | `ed269` | 32 |
| `zones_vertical.json` | `zones` | 49 |

The case counts in the entries below are those each work package met
when it merged, before the lab sync under Changed; the table is the
release.

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
