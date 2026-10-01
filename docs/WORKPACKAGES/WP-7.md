# WP-7: `identify`

Branch `feat/WP-7-identify`. Milestone G-M1. Owns `identify/`
exclusively. Depends on WP-1 (`geodesy.HaversineM`) and WP-4 (`regnum`,
`serial`); start when both PRs are open, rebase when they merge.
Consumers: the authority's `detect`, the USSP's ingest and monitor,
`alerting` (consumes `core.Identification` only, no import of this
package).

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.9`, `§6`, `§8`, `§11` gaps 1, 5.
2. `core/ident.go` (statuses, reasons, basis, the block), `odid/types.go`
   (`IDType`), `regnum` (`CompareKey`), `serial` (`Normalize`, `FoldKey`),
   `geodesy` (`HaversineM`).
3. `vectors/testdata/identification_status.json`: header `description`,
   `fixtures.registry` (operators, uas, notes), `incident_statuses`, all
   37 cases (each kind). `fleet_match.json`: all 10.
4. LESSONS G-01, G-02, G-04, G-05, G-08 (document for callers), G-09,
   I-05, I-06, I-08, I-09, D-11.
5. Spec `04 §3.2` (the block, who resolves, the `serial_conflict` rule),
   `06 §2` T1, T3.
6. Reference only: `utm/gateway/identification.py` (`resolve`,
   `resolve_bound`, `resolve_remote_id`, `serial_conflict`,
   `_inactive_reason`), `utm/gateway/registry_projection.py`
   (`RegistrySnapshot.find_uas`), `utm/gateway/remote_id_match.py`
   (`judge`, `LinkFreshness`).

## What to build

```go
type OperatorFacts struct{ OperatorID, RegistrationNumber, Status string }
type UASFacts struct{ DroneID, Label, Serial, RegistrationStatus string; OperatorID *string; InRegistry bool }
type Match int  // MatchNone, MatchExact, MatchFolded, MatchAmbiguous
type Lookup interface {
    UASBySerial(serial string) (UASFacts, Match)
    UASByID(droneID string) (UASFacts, bool)
    Operator(operatorID string) (OperatorFacts, bool)
}
type Snapshot struct{...}
func NewSnapshot(ops []OperatorFacts, uas []UASFacts) *Snapshot     // implements Lookup
func ResolveBroadcast(reg Lookup, serial, operatorReg *string) core.Identification
type RemoteIDIdentity struct{ Identified bool; UAID string; IDType odid.IDType; OperatorID *string }
func ResolveRemoteID(reg Lookup, id RemoteIDIdentity) core.Identification
func ResolveBound(reg Lookup, droneID string) core.Identification
func SerialConflict(serial string, operatorReg *string) core.Identification
func Unavailable(serial, operatorReg *string) core.Identification
type AuthRow struct{ HeardAtS float64; Pos *core.LatLon; Backlog bool; BehindS float64; Source string }
type FleetInput struct{ SerialIsOurs bool; Rows []AuthRow; Broadcast core.LatLon; NowS, LiveForS, SpoofDistanceM float64 }
type Verdict string  // "stranger", "withhold", "as_ours", "conflict"
type FleetResult struct{ Verdict Verdict; ApartM *float64; IgnoredHistoryRows int }
func JudgeFleet(in FleetInput) FleetResult
```

Resolution rules (G-01, G-02, G-04, G-05, I-05; derive the exact table
from the 37 cases, these are the load-bearing ones):

- Inputs are cleaned: `serial.Normalize` (trim, case kept); operator
  number trimmed; empty after trim is nil. `expected.serial` and
  `expected.operator_reg` echo the cleaned inputs (`" GEOX "` -> `GEOX`;
  `"GEOabcd1234efgh-x9z"` stays as received, only the comparison strips
  the suffix).
- No serial -> `unidentified / no_serial` (`unidentified-no-serial`,
  `-empty-serial`).
- `UASBySerial`: exact match wins; else a case-folded match only when
  exactly one aircraft folds to it (`serial-case-folded-when-unambiguous`,
  `ambiguous-case-fold-is-unknown`, `ambiguous-exact-match-wins`).
- Unknown serial -> `unknown_operator / serial_unknown`, whatever the
  operator number says (`unknown-serial-with-active-operator`).
- Known serial with `InRegistry false` -> `unknown_operator /
  not_in_registry` (`in-projection-but-not-registry`).
- UAS status suspended/revoked -> `suspended / uas_suspended|uas_revoked`;
  owner status suspended/revoked -> `suspended / operator_suspended|
  operator_revoked`; suspension outranks a mismatch but the mismatch is
  still flagged (`suspended-outranks-operator-id-mismatch-still-flagged`:
  `mismatch true`, `registered_operator_reg` set).
- Owner id set but not in the projection -> `unknown_operator /
  owner_unknown` (`owner-not-in-projection`).
- Own fleet (owner nil, in registry) -> `registered / matched` on the
  serial alone, operator number ignored (`fleet-serial-alone`,
  `fleet-ignores-operator-id`).
- Operator number: absent -> `unknown_operator / operator_absent`;
  present and `regnum.CompareKey` differs from the owner's ->
  `unknown_operator / operator_mismatch`, `mismatch true`,
  `registered_operator_reg` = owner's number; equal -> `registered /
  matched` (`registered-case-insensitive-and-trimmed`,
  `eu-secret-suffix-stripped`).
- `remote_id_block`: `Identified false` -> `unidentified / no_serial`;
  `IDType != IDTypeSerial` -> `unknown_operator / not_a_serial` with
  `serial nil`, `operator_reg` as broadcast (`remote-id-block-caa-registration-is-not-a-serial`,
  `-session-id-is-not-a-serial`); an empty non-serial is `unidentified`;
  else as broadcast.
- `bound`: `UASByID`; missing -> `unknown_operator / not_in_registry`?
  Read `relay-not-yet-projected` and `relay-not-in-registry` for the
  reasons; found and active -> `registered / session_binding`;
  suspended/revoked as above; `Basis authenticated`.
- `serial_conflict`: `unknown_operator / serial_conflict`, `mismatch true`.
- `Unavailable`: `unknown_operator / registry_unavailable` (spec `04 §3.2`;
  no vector: unit test and note in the PR).
- `RegistryUASID` = the matched `DroneID` (the vector's `drone_id`; null
  when none). `Basis`: `as_broadcast` for broadcast and remote_id_block,
  `authenticated` for bound; the vectors do not carry it (omitempty), so
  test it in unit tests.

Spoofing guard (I-08, I-09; 10 cases): not ours -> `stranger`, `ApartM`
nil. Ours: a row is live when not `Backlog`, `BehindS <= LiveForS`
(captured no more than `LiveForS` before receipt), `Source == ""`
(authenticated telemetry; `remote_id` rows never vouch) and `NowS -
HeardAtS <= LiveForS` (exactly at the bound is live). History rows are
counted in `IgnoredHistoryRows` (backlog and behind rows; the
`remote_id` row is not counted: read `remote-id-rows-do-not-make-the-link-live`
`ignored_history_rows: 0`). No live row -> `as_ours` (the broadcast
speaks for the aircraft). A live row without a position -> `withhold`
(`relay-live-no-position-withhold`). With a position: `ApartM =
geodesy.HaversineM(row, broadcast)` (the newest live row); `<=
SpoofDistanceM` -> `withhold`; `>` -> `conflict` with `ApartM`
(500.0003607781002 to 0.01).

## Vector tests

`identify/vectors_test.go`: build the `Snapshot` from
`fixtures.registry` (strict decode of operators and uas with
`in_registry`), or from `input.registry_override` when present; switch on
`kind`; compare the seven expected fields with `EqualStrPtr` for the
pointers. `TestVectorsFleetMatch`: `relay_rows[]{heard_at_s, lat_deg?,
lon_deg?, backlog?, behind_s?, source?}`, `broadcast_position [lat,lon]`,
`now_s`, `live_for_s`, `spoof_distance_m` -> `verdict`, `apart_m` (0.01,
nullable), `ignored_history_rows`.

## Other tests

- E-01 pairs per reason (37 cases cover most; add `Unavailable` and the
  `Basis` values).
- `Snapshot` lookups: exact beats fold; two folds ambiguous; a snapshot
  from an empty registry knows nobody.
- Bound: `NewSnapshot` with 100 000 aircraft builds the fold index without
  quadratic time (benchmark).
- Benchmarks `BenchmarkResolveBroadcast`, `BenchmarkResolveRemoteID`,
  `BenchmarkJudgeFleet`.

## Done when

- [ ] 37 + 10 vector cases pass; `-race -shuffle=on` green; coverage >= 90 %.
- [ ] Lint clean; benchmarks reported; `doc.go` rewritten (who resolves, G-08 projection rules for callers); CHANGELOG line; PR with outputs.

## Commits

`feat(identify): resolve broadcast, Remote ID and bound tracks to a status and reason [WP-7 G-M1]`,
`feat(identify): judge a broadcast of our serial against live authenticated rows [WP-7 G-M1]`,
`test(identify): run the identification and fleet match vectors [WP-7 G-M1]`.
