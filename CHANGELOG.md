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

### Changed

- vectors: the pin moves from `uspace-lab@f5e2a53` (the first commit of
  lab PR #58) to `uspace-lab@cc9f5a8`, that pull request's merge
  commit on lab `main` (docs/RELEASING.md section 3.1). No vector file
  changed: the files are byte-identical at both commits. [ED-318 layer]

## [1.12.0] (UA registration)

Minor to 1.11.0: the UA registration of a bound aircraft enters
identification judging (spec 04 section 3.2 errata 2026-10-22, owner
decision 2026-10-22 on uspace-ussp PLAN section 15 Q41, option 1). The
authorised intent's registration is the only source of the one a
Service Provider serves; the one the telemetry declares only validates
against it, and a disagreement is a mismatch, never silently replaced.
Additive: a new function, a new reason, two optional fields, and new
vector cases; `ResolveBound` and every existing case answer as before.
The knowledge vectors are re-synced from `uspace-lab@b707dad`, the
merge commit of rootxkit/uspace-lab#64 on lab `main` (lab #63 merged
before it): new cases only, no gated header key changed, so the semver
gate classifies the change as additive. The pin moves from
`uspace-lab@71c5c18` to `uspace-lab@b707dad`; `utm_commit` stays
`484cd22`. 704 cases in all (694 before).

### Added

- `identify`: `ResolveBoundWith(reg, droneID, BoundInputs)`, with
  `BoundInputs{DeclaredRegistration, IntentRegistration}`. It is
  `ResolveBound` plus the registration rule: the declared registration
  is carried, trimmed, as `Registration`; when both are present and
  differ (trimmed, ASCII letters case-insensitive, G-12) `Mismatch` is
  set, `IntentRegistration` carries the intent's, and the reason
  becomes `registration_mismatch` where `ResolveBound` said
  `session_binding`. Any other reason (a standing problem, or
  `registry_unavailable`) is kept with the mismatch still flagged. With
  zero inputs it is `ResolveBound`. [UA registration]
- `core`: `ReasonRegistrationMismatch` (`registration_mismatch`),
  judged; `Identification.Registration` and
  `Identification.IntentRegistration`, optional (`omitempty`), so a
  block without them encodes as before. [UA registration]
- vectors: identification_status.json (spec 04 section 3.2 errata 2026-10-22; Implementing Regulation (EU) 2021/664 Annex IV item 10): ten new `bound` cases (45 to 54) with the inputs
  `declared_registration` and `intent_registration`: agrees, agrees
  ignoring case and spaces, mismatch, mismatch not yet projected,
  mismatch under a suspended UAS and not in the registry (reason kept,
  mismatch flagged), a Kelvin-sign look-alike, declared without an
  intent registration, the intent's without a declared one, and a blank
  declared one. The vector test also checks that a bound case without
  them gives `ResolveBound`'s answer. [UA registration]

## [1.11.0] (ED-318 uom spelling)

Minor to 1.10.0: `uom` spelt in another case inside an ED-318 layer is
refused by name instead of kept as an extra, which left the layer with
no `uom` and a ceiling in feet read as metres (owner decision
2026-10-22, from the review of PR #54). A zone without `upper` and
`lower` stays valid and applies at every height, as
`zones_vertical.json` defines it (owner decision 2026-10-23). No public
ED-318 source or sample in `ed318/testdata` spells `uom` otherwise, and
no existing vector case's `input` or `expected` changed. The minor
follows 1.10.0's precedent for a refusal of input that was silently
misread. The knowledge vectors are re-synced from `uspace-lab@71c5c18`
(lab branch `docs/WP-L4-ed318-layer-heights`): one new case, no header
key changed, so the semver gate classifies the change as additive. The
pin moves from `uspace-lab@cc9f5a8` to `uspace-lab@71c5c18`;
`utm_commit` stays `484cd22`. 694 cases in all (693 before).

### Added

- vectors: ed318_roundtrip.json (EUROCAE ED-318 JSON schema Schema_LayeredGeoJSON.json at e98b292, uas_standards VerticalLayer at 6e182f4; spec 06 T9): one new case (25 to 26),
  `refuse-uom-in-upper-case-inside-layer`, the accepted base with one
  patch; the base's TSD001 (`uom` ft) is its accepted twin.
  [ED-318 uom spelling]

### Fixed

- `ed318`: `LayerVariants()` also holds the seven spellings of `uom`
  that differ from it in case only (`UOM`, `Uom`, `UoM`, `uOM`, `uOm`,
  `uoM`, `UOm`), each mapped to `uom`; `Parse` refuses them inside a
  layer by name ("is not ED-318's spelling; ED-318 writes uom, in lower
  case"). ED-269's names keep their reason. Other unknown members are
  still kept. [ED-318 uom spelling]

## [1.10.0] (ED-318 layer)

Minor to 1.9.0: an ED-318 layer that does not say its vertical limits
in ED-318's names is refused instead of read as a zone from the surface
to unlimited. `ed318.Parse` kept every member of a `layer` it did not
know as an extra, so a layer written with ED-269's names parsed with
no lower and no upper limit, and `uomDimensions: "FT"` beside `upper`
left the limit read as metres. No public ED-318 source or national
sample read writes those names inside a layer (ED-318 vertical-limit
field-name check, 2026-10-22), and no existing vector case's `input`
or `expected` changed: everything 1.9.0 accepted with ED-318's names
is accepted and written back as before. The minor follows 1.4.0's
precedent for a refusal of input that was silently mis-read (owner
decision 2026-10-22). The knowledge vectors are re-synced from
`uspace-lab@f5e2a53` (lab branch `docs/WP-L4-ed318-layer-names`): new
cases only, no header key changed, so the semver gate classifies the
change as additive. The pin moves from `uspace-lab@3946517` to
`uspace-lab@f5e2a53`; `utm_commit` stays `484cd22`. 693 cases in all
(690 before).

### Added

- `ed318`: one table of a layer's names. `LayerKeyUpper`,
  `LayerKeyUpperReference`, `LayerKeyLower`, `LayerKeyLowerReference`
  and `LayerKeyUom`, with `LayerMembers()` in the schema's order;
  `UomValues()` (`m`, `ft`); `LayerVariants()`, ED-269's names
  (`upperLimit`, `upperVerticalReference`, `lowerLimit`,
  `lowerVerticalReference`, `uomDimensions`) each with the ED-318
  member it stands for; `UomVariants()` (`M`, `FT` to `m`, `ft`). The
  functions return copies. The parser reads, and the exporter writes,
  these names only; a system that writes a layer itself checks its
  names against `LayerMembers()`. [ED-318 layer]
- vectors: ed318_roundtrip.json (EUROCAE ED-318 JSON schema Schema_LayeredGeoJSON.json at e98b292, uas_standards VerticalLayer and eurocae_ed269.py at 6e182f4; spec 06 T9): three new cases (22 to 25),
  `refuse-ed269-names-inside-layer`, `refuse-ed269-unit-name-inside-layer`
  and `refuse-empty-layer`, each the accepted base with one patch; the
  base's TSD001 (`uom` ft) and TSN001 (references without limits) are
  their accepted twins. [ED-318 layer]

### Fixed

- `ed318`: `Parse` refuses each of ED-269's names inside a `layer` at
  its JSON path (`features[0].geometry.layer.upperLimit`: "is ED-269's
  name; ED-318 writes upper"), and a `layer` with none of the five
  ED-318 members (`{}`, or only unknown members) as empty. An ED-269
  unit as the `uom` (`M`, `FT`) is refused as before, its reason now
  also naming ED-318's spelling. Other unknown members beside the
  ED-318 ones are kept, and a layer of references without limits is
  accepted, as before. An absent `uom` is still metres (the schema's
  default, UNVERIFIED), and `Export` still writes `uom` only when it
  was given, so no published document's bytes change. [ED-318 layer]
- `ed318`: the package doc cites each public source of the layer names
  with its URL and commit or version, records that `uas_standards` and
  the Swiss V2 model make `uom` mandatory against the schema's default,
  and that LayeredGeoJSON and Swiss V2 give the circle radius in
  metres. The status stays UNVERIFIED. [ED-318 layer]
## [1.9.0] (GCAA config)

Additive to 1.8.0: the registration-number format is configuration as a
whole, so a national answer to spec 08 Q5 can be applied without code.

### Added

- `regnum`: `Options` and `NewValidatorWith(Options)`: the
  registration-number format as a whole is configuration (spec 08 Q5,
  pending GCAA): the pattern, the secret separator (`SecretSep`, default
  `DefaultSecretSep` = `-`) and the secret length (`SecretLen`, default
  `DefaultSecretLen` = 3). Under a format the EU default does not match,
  a system can now strip that format's own secret part instead of keeping
  it (G-04). The zero `Options` is today's format and returns the same
  validator the package-level functions use; `NewValidator(pattern)` is
  `NewValidatorWith(Options{Pattern: pattern})`. A bad separator (not
  printable ASCII, or a letter, digit or space) or length (outside
  1..62) is a `*core.FieldError` naming `registration_secret`.
  `Validator.SecretSep`, `SecretLen` and `HasChecksum` report the
  effective values, for a system that issues numbers or derives an
  intake rule from the same format. [GCAA config]
- `regnum`: a checksum hook, off by default (AMC1 Art. 14(6), spec 09
  §3). `Options.Checksum` (a `Checksum` func), when set, must accept a
  number for `Validate` to accept it ("the check character does not
  verify"); it runs after the pattern, and `PublicPart` does not consult
  it, so a secret is stripped from a number with a bad check character as
  well. `ChecksumByName` knows only `none` (and the empty name): no
  algorithm is written until the national format is decided, and any
  other name is refused naming `registration_checksum`. [GCAA config]

Nothing that exists changes its output: the package-level functions,
`NewValidator` and the registration vectors run under the same default
format, and nothing exported was removed or renamed. No vector changed;
`uspace-lab@3946517` remains the pin.

## [1.8.0] (DP details)

Additive to 1.7.0: an identification that was not judged. A Display
Provider flight whose Service Provider details were never fetched was
resolved as `unidentified / no_serial`, raised `identification` (G-03)
in an incident zone, and became an `unregistered` violation for a
registered aircraft (authority staging, 2026-10-19). Nothing that exists
changes its output under the default configuration, and nothing exported
was removed or renamed. The knowledge vectors are re-synced from
`uspace-lab@3946517` (lab PR #37, WP-L4 errata): new cases only, no
existing case's `input` or `expected` changed and no header key other
than `description` changed, so the semver gate classifies the change
as additive. The pin moves from `uspace-lab@216fa56` to
`uspace-lab@3946517`; `utm_commit` stays `484cd22`. 690 cases in all
(682 before).

### Added

- `core`: `ReasonDetailsUnavailable` (`details_unavailable`, status
  `unidentified`): the Service Provider's details have not been
  fetched, so the source was never asked who the aircraft is.
  `no_serial` keeps its meaning: the source was asked and gave no
  serial. [DP details]
- `core`: `IdentReason.Judged()` and `(*Identification).Judged()`: an
  identification is judged when its inputs were consulted. False for
  `details_unavailable` and `registry_unavailable` (and for a nil
  identification), true for every other reason. `IdentStatus.IncidentStatus`
  is unchanged and reads the status alone. [DP details]
- `identify`: `DetailsUnavailable(operatorReg)` builds the
  `unidentified / details_unavailable` block on the `as_broadcast`
  basis (a provider-basis caller sets `Basis`), the one place a Display
  Provider gets it from (spec 04 §3.2). [DP details]
- `alerting`: `details_unavailable` is held as an absent identification
  (C-09): it raises and shows false neither `identification` nor
  `identification_mismatch`, does not keep the identity fresh, and does
  not change the aircraft's eviction class. An active alert stays
  active under it and goes stale with the identity, as with no
  identification. New reason, so no earlier behaviour changes.
  [DP details]
- `alerting`: `Config.HoldUnjudgedIdentification` (default false) holds
  `registry_unavailable` the same way. With it off, the 1.x behaviour
  stands: `registry_unavailable` is `unknown_operator` and raises
  `identification` in a PROHIBITED or REQ_AUTHORISATION zone. A system
  sets it to stop raising on a registry it could not read. [DP details]
- vectors: alert_lifecycle.json (spec 04 section 3.2 and 02 F7 errata 2026-10-20; F3411-22a NET0480, NET0490; Regulation (EU) 2021/664 Art. 8(2)): seven new cases (35 to 42).
  `details-unavailable-in-prohibited-zone-holds` and its presence twin
  `details-arrive-as-serial-unknown-raises-identification`;
  `registry-unavailable-holds-when-configured` (case config
  `hold_unjudged_identification: true`) and its twin
  `registry-unavailable-raises-by-default`;
  `mismatch-held-while-details-unavailable`,
  `mismatch-held-while-registry-unavailable` (with the flag) and their
  twin `mismatch-shown-false-by-registry-unavailable-by-default`. The
  vector runner reads the new case config key. [DP details]
- vectors: identification_status.json (spec 04 section 3.2): one new case,
  `details-unavailable` (43 to 44), of the new input kind
  `details_unavailable`, run through `identify.DetailsUnavailable`.
  [DP details]

### Planned for 2.0.0

- `alerting`: `Config.HoldUnjudgedIdentification` becomes true by
  default, so `registry_unavailable` no longer raises `identification`.
  That changes a judgement, so it waits for the next major (`00 §6.3`,
  `docs/RELEASING.md` §3). It is listed here, not under a `## [2.0.0]`
  heading, because a version heading above `[1.8.0]` would fail the
  v1.8.0 tag's release check; the first behaviour-change pull request
  for 2.0.0 adds that heading and moves this line under it.

## [1.7.0] (ANSP gap 3)

Additive to 1.6.0: the ground over a box. No judgement changed, no
vector changed, and nothing exported was removed or renamed;
`uspace-lab@216fa56` remains the pin.

### Added

- `terrain`: `Store.RangeM(bbox, maxSamples)` gives the least and
  greatest ground elevation over a box, for a caller that holds a limit
  above ground for a whole area: the ANSP's AGL dynamic restriction,
  made an absolute F3548 volume with the least ground under the lower
  limit and the greatest under the upper, so the volume never shrinks
  (uspace-ansp `docs/PLAN.md` section 15 gap 3). The range bounds every
  answer `Store.Elevation` gives inside the box: it reads every sample a
  bilinear interpolation there can read, so a peak between the box's
  corners is in it. A box any part of which is unknown to `Elevation`
  (a cell outside the index, an unreadable tile, a NoData sample) is
  `(nil, nil)`, counted `range_unknown`, never the range of the part
  that is known (LESSONS D-04). Bounded by `MaxRangeCells` (64) and a
  sample bound (`DefaultMaxRangeSamples`); a box across the
  antimeridian is refused. Nothing that exists changes its output; no
  vector changed. [ANSP gap 3]

## [1.6.0] (lab finding C3)

Additive to 1.5.0: an issuer that writes a single audience as a JSON
string. No judgement changed, no vector changed, and nothing exported
was removed or renamed; `uspace-lab@216fa56` remains the pin.

### Added

- `auth.Issuer.WithStringAudience` returns an Issuer with the same iss,
  key and JWKS whose `Issue` writes `aud` as a string (`"aud":"host"`)
  instead of a one-element array. RFC 7519 §4.1.3 allows both, and
  `auth.Verifier` accepts both, as before; the string form is for a
  verifier that compares `aud` as a string only, the InterUSS mock USS
  of monitoring v0.36.0 that the lab's uss_qualifier runs use
  (uspace-lab `docs/decisions/2026-10-05-conformance-findings.md` C3).
  `Issue` on any other Issuer is unchanged (an array), and
  `IssueSession` writes a string as it always did. [C3]

## [1.5.0] (WP-L4 re-sync)

Minor to 1.4.0: the knowledge vectors re-synced from
`uspace-lab@216fa56` (lab PR #17, WP-L4 spec errata), which changed one
file. No code changed, no exported identifier was added or removed, and
no judgement changed: every case keeps its `input` and `expected`, and
no header key changed, so the semver gate classifies the change as
editorial. The pin moves from `uspace-lab@6b5b286` to
`uspace-lab@216fa56`; `utm_commit` stays `484cd22`.

### Changed

- vectors: alert_lifecycle.json (cross-plan decision 2026-10-02 cisp Q18, lab WP-L4): the
  cases `zone-stops-applying-clears` and
  `applicability-at-placed-time-not-arrival` no longer list `cisp` in
  their `owner`, matching the file's header `owners`
  (`[authority, ussp]`); the CISP runs no alert monitor. Core runs every
  case whatever its `owner` (PLAN D4), so its tests are unchanged. A
  system calling `RunOwned(t, "cisp", ...)` on this file now runs no
  case; no system on `main` does. Case counts are unchanged. [WP-L4]

## [1.4.0] (WP-19)

Additive to 1.3.0: memory-mapped geoid and DEM grid loaders, so the
processes on one host share one copy of EGM2008 and of each tile in the
page cache; `terrain.Tile.ElevationM` answering the same on every
architecture; and the retro-audit changes merged since 1.3.0. No vector
changed; `uspace-lab@6b5b286` remains the pin.

### API declared stable

From `1.4.0` `geoid.LoadMapped`, `geoid.Grid.Mapped`,
`terrain.MappedDirOpener`, `terrain.Tile.Mapped` and
`terrain.StoreOptions.OpenTile` are stable: their signatures; answers
bit for bit those of `Load` and `ParseTile` on the same file; a
read-only memory map on linux and darwin and a read into memory
elsewhere, reported by `Mapped`; a file whose bytes `Parse` or
`ParseTile` refuses is refused with the same `*core.FieldError`; a
mapping released only when its grid or tile is unreachable, with no
`Close`. No counter is added.

### Added

- `auth`: `Config.MaxSessionTTL` and `Config.Realms`, honoured under
  `StrictSessionClaims`: a session token (scope `session`) whose `exp`
  is more than `MaxSessionTTL` after its `iat`, or that has no `iat`,
  and a present `realm` not in `Realms`, are `rejected_claims`. Both are
  off by default, so no judgement of 1.3.0 changes; `NewVerifier`
  refuses either without `StrictSessionClaims`, where it would do
  nothing. SECURITY.md names where the 12 h limit is enforced.
  [audit N1]
- `geoid`, `terrain`: memory-mapped grid loaders. `geoid.LoadMapped`
  maps a GeographicLib grid read-only on linux and darwin, so processes
  on one host share its pages in the page cache instead of each holding
  a heap copy (74.7 MB for EGM2008 2.5'); `terrain.MappedDirOpener` with
  the new optional `StoreOptions.OpenTile` does the same for DEM tiles.
  Elsewhere they read the file as `Load` and `DirOpener` do;
  `Grid.Mapped` and `Tile.Mapped` say which. They parse through the same
  checks and answer bit for bit what `Load` and `ParseTile` answer; a
  truncated file is refused as before. A mapped file must be replaced by
  renaming, never rewritten in place. Nothing that exists changes its
  output; no counter is added. No vector changed. [WP-19]

### Fixed

- `geodesy`: `Destination` refuses, as NaN, a distance longer than one
  equatorial circumference (`MaxDestinationDistanceM`, about 40 075 km).
  It returned a valid-looking position for any finite distance, also
  where the arc had no significant bits left (1e300 m); every distance
  up to one turn gives what it gave before. No vector changed. [audit S8]
- `auth`: `IssueSession` refuses a role with surrounding white space
  (`" operator"`, an em space included) and a role listed twice, as a
  `*core.FieldError` on `roles`; they were signed as given, confusing
  display and audit. The verifier's judgement is unchanged. [audit N2]
- `terrain`: `Tile.ElevationM` gives the same answer on every
  architecture. Go lets a compiler fuse `x*y + z` into one multiply-add;
  arm64 did, in the bilinear sum and in `OffsetM + ScaleM*...`, and
  answered an ulp off amd64 (400.00000000000006 for 400). Each product
  is now rounded by an explicit conversion, which forbids the fusion; on
  amd64, which never fused, every answer is bit for bit what it was.
  CI runs the vectors and the loaders on linux/arm64 and no longer skips
  five terrain tests there. No vector changed. [WP-19]
- `auth`: the package doc, `ReceiverVerifier`'s doc and the text of
  `NewReceiverVerifier`'s no-keys `*core.FieldError` (still on `keys`)
  no longer say an ingest without receiver keys binds to loopback only;
  they say it refuses every report and reports itself not ready, as
  uspace-authority's rid-ingest does since its #36. No behaviour
  changes. [WP-11]

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
