# WP-6: `timeplace` and `rid`

Branch `feat/WP-6-timeplace-rid`. Milestone G-M1. Owns `timeplace/` and
`rid/` exclusively. Depends on WP-0 (the frozen `odid/types.go`; you do
not need the codec). Consumers: the authority's `rid-ingest`, the USSP's
`dp-poller`/`telemetry-ingest`, `alerting` (indirectly through
`core.Times`).

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.8`, `§6` (stateful files), `§8`, `§11`
   gap 8.
2. `core/time.go`, `core/vertical.go`, `core/counters.go`, `odid/types.go`.
3. `vectors/testdata/rid_time.json` (header `rule`; all 25 cases; note
   two have `expected: null`), `rid_identity.json` (header
   `description` and `defaults`; all 24 cases), `pressure_altitude.json`
   (16; four are step sequences).
4. LESSONS T-01, T-02, T-05, T-07, T-08, T-12; I-01, I-02, I-03, I-04,
   I-06, I-07; R-07, R-08, R-10, R-11, R-13; E-09, E-10.
5. Spec `04 §2` (envelope times), `04 §3.1` (altitude rules), `02 F9`.
6. Reference only: `utm/gateway/remote_id.py` (`broadcast_time`, `place`,
   `amsl`, `geodetic_usable`, `RemoteIdTracker`), `utm/gateway/network_rid.py`
   (`place`).

## What to build

### `timeplace`

```go
type Fallback string  // "", "clock_ahead", "too_old", "unknown", "invalid"
type BroadcastPolicy struct{ ToleranceS, MaxLatencyS float64 }   // DefaultBroadcastPolicy{1, 5}
type Placement struct{ TS, CapturedAt time.Time; Source core.TimeSource; Fallback Fallback }
func PlaceBroadcast(timestampTenths uint16, tsAccuracyCode uint8, receivedAt time.Time, pol BroadcastPolicy) Placement
type NetworkPolicy struct{ MaxAgeS, ToleranceS, MaxLatencyS float64 }   // DefaultNetworkPolicy{60, 1, 5}
type NetworkNote string  // "", "ahead_of_response", "clock_ahead", "too_old"
func PlaceNetwork(stateTS time.Time, responseTS *time.Time, receivedAt time.Time, pol NetworkPolicy) (p Placement, note NetworkNote, shown bool)
func PlaceBatch(rxTS time.Time, ts []time.Time, maxSpacing time.Duration) (capturedAt []time.Time, clamped int)
func AccuracyS(code uint8) float64   // MAV_ODID_TIME_ACC: 0 unknown -> 0; k -> k/10
```

Broadcast rule (header `rule`, T-07, T-08): `0xFFFF` -> `unknown`;
tenths/10 >= 3600 -> `invalid`; otherwise `moment` = the latest instant
at `seconds_after_hour` into some UTC hour that is **not after**
`received_at + tolerance + accuracy`; `age = received_at - moment`;
`age > 1800 s` -> `clock_ahead` with `ts = moment + 1 h`; `age >
max_latency + accuracy` -> `too_old`; else `Source = broadcast`,
`CapturedAt = TS = moment`. All four fallbacks place at receipt
(`CapturedAt = received_at`, `Source = receiver`) and keep `TS` as the
claim (for unknown/invalid, `TS` = received_at: check the vectors
`unknown-timestamp-0xffff`, `timestamp-past-the-hour-is-invalid`). The
boundaries are inside: `clock-ahead-exactly-at-tolerance`,
`latency-bound-exactly`. Microsecond exactness: do the arithmetic in
integer microseconds (tenths are exact in µs), never float seconds.

Network rule (T-02): with a response timestamp, `captured_at = received_at
- (response_ts - state_ts)`; a state newer than its response is
`ahead_of_response`, placed at receipt (`Source receiver`); a state more
than `MaxAgeS` behind its response is not shown (`shown=false`, the
vector's `expected: null`). Without a response timestamp
(`network-no-response-time-*`), place by the state timestamp against our
clock with the broadcast tolerance/latency rules and the same notes
(`clock_ahead`, `too_old`), and drop beyond `MaxAgeS`. `time_source` in
the vectors is `broadcast` or `receiver`; map `Source` accordingly (the
spec's `provider` is for a system-level field; document the mapping).

Batch (T-02): `captured_at[i] = rx - (max(ts) - ts[i])`; a spacing that
is negative (ts after the newest? impossible by construction; guard) or
greater than `maxSpacing` (120 s) is clamped to the bound and counted.

### `rid`

```go
const NamespaceUUID = "6f1c7d52-4a0b-5c1e-9d3a-2b8e41f07a65"
func AircraftID(idType odid.IDType, uaID string) string        // uuid5(ns, fmt.Sprintf("%d:%s", idType, uaID)); utm's exact ids
func UnidentifiedID(transmitter string) string                 // uuid5(ns, "transmitter:"+address)
type Settings struct{ IdentityTTLS, MaxGapS, IdentifyWithinS float64; MaxTransmitters int }   // DefaultSettings{15, 3, 4, 50000}
type Frame struct{ Receiver, Transmitter string; Messages []odid.Message; NowS float64; RxTS time.Time }
type Observation struct{ DroneID, Label string; Identified bool; UAID string; IDType odid.IDType; OperatorID *string; Location odid.Location; System *odid.System; Receiver, Transmitter string; RxTS time.Time }
type Tracker struct{...};  func NewTracker(s Settings) *Tracker
func (t *Tracker) Take(f Frame) *Observation
func (t *Tracker) Forget(nowS float64) int
func (t *Tracker) Counters() *core.Counters                    // identity_changes, silences, unidentified, address_conflicts, evicted
func (t *Tracker) Transmitters() int
type AltInput struct{ AltHAEM, AltPressureM *float64; VertAccuracyCode uint8; UndulationM *float64 }
type AltPolicy struct{ MinVerticalAccuracy uint8; HoldPressure bool; PressureHoldS float64 }   // DefaultAltPolicy{2, true, 10}
type AltResult struct{ AltAMSLM *float64; Source core.AltSource }
func SelectAltitude(in AltInput, pol AltPolicy) AltResult
type AltitudeSelector struct{...};  func NewAltitudeSelector(pol AltPolicy) *AltitudeSelector
func (s *AltitudeSelector) Select(in AltInput, nowS float64) AltResult
func VelocityNED(speedMS, trackDeg, climbMS *float64) (vn, ve, vd *float64)
func Airborne(st odid.Status) bool
```

Tracker rules (I-01..I-04, R-13; the 24 vectors): state is per
transmitter address across receivers. A Basic ID is usable while heard
within `IdentityTTLS`, the address not silent for more than `MaxGapS`,
and no other Basic ID **of the same ID type** has arrived from the
address since (a second ID type is another identity, kept beside:
`second-id-type-is-another-identity`; a serial wins over a registration
id: `serial-wins-over-registration-id`; an empty identity does not
replace a serial). A different Basic ID of the same type drops everything
known about the address (System and Operator ID too), counts
`identity_changes` and `address_conflicts` as the vectors show (read
`two-serials-alternating-on-one-address-are-an-anomaly` for the exact
counting). A Location publishes when it arrives with a fresh identity, or
when the identity it was held for arrives within `IdentifyWithinS`; a
Location with no identity after `IdentifyWithinS` publishes as
unidentified (`DroneID = UnidentifiedID(address)`, `Label = address`,
`UAID ""`, `IDType 0`, counts `unidentified`); when the serial then
arrives the unidentified track is not merged. A repeated Basic ID or
Operator ID alone publishes nothing (R-13). A receiver that hears only
Locations borrows another receiver's fresh identity for the same address
(I-03), own first. Silence longer than `MaxGapS` forgets the serial and
counts `silences`; a Basic ID after a silence identifies a held Location
and is not an anomaly. The table is bounded by `MaxTransmitters` with
oldest-eviction counted (E-10). `Observation.RxTS` is the frame's
`RxTS`; the vector test derives it as `2026-09-29T12:00:00Z + now_s`.

Altitude rules (R-07, R-08; 16 vectors): `alt_amsl = hae - N` when HAE is
present, N known and accuracy not poor; "poor" means code known (`> 0`)
and `< MinVerticalAccuracy`; poor or missing HAE with a pressure altitude
-> pressure (`Source pressure`, value = the pressure altitude as is, not
AMSL); no N -> nil/none even with a good HAE and a pressure altitude
(`pressure-is-not-a-substitute-for-a-missing-geoid`); neither -> nil.
With `HoldPressure`, once on pressure a track stays on it until
`PressureHoldS` after the last poor fix (`hold-*`,
`accuracy-at-threshold-does-not-flip-the-source`,
`without-hold-it-flips-every-message`), and the hold needs a pressure
altitude to hold.

`VelocityNED`: `vn = v cos(track)`, `ve = v sin(track)`, `vd = -climb`;
all three nil unless both speed and track are known (R-10); `vd` nil when
climb is nil but vn/ve known? Read R-10: "leave all three null" applies
to a speed without direction; with speed and track but no climb return
vn, ve and nil vd. Document.

## Vector tests

- `timeplace/vectors_test.go` (`rid_time.json`): broadcast cases have
  `timestamp_tenths`, `ts_accuracy_code`, `received_at`,
  `time_tolerance_s`, `max_latency_s` -> `ts`, `captured_at`,
  `time_source`, `fallback`; network cases have `state_timestamp`,
  `response_timestamp` (nullable), `received_at`, `max_age_s`,
  `time_tolerance_s`, `max_latency_s` -> `ts`, `captured_at`,
  `time_source`, `note`, or `expected` null -> `shown == false`. Times
  with `EqualTime`.
- `rid/vectors_test.go`: `rid_identity.json`: `settings` (partial
  overrides of the defaults), `steps[]{now_s, messages[], receiver?,
  transmitter?}` with header `defaults` for receiver/transmitter; messages
  are `{type: basic_id|location|operator_id, ...}` with Location defaults
  `lat_deg 41.7151` (the header says "defaults of the generator's
  location_with(); only identity matters": set lat from the message or
  41.7151, lon 44.8271, status airborne); build `odid` values directly.
  Compare `per_step[i]` (`null` means `Take` returned nil) on `drone_id`,
  `label`, `identified`, `ua_id`, `id_type`, `operator_id`, `lat_deg`,
  `rx_ts`; then `counters`. `pressure_altitude.json`: stateless cases ->
  `SelectAltitude`; `steps` cases -> one `AltitudeSelector` fed in order.

## Other tests

- E-01 pairs at every boundary of the time rules (the vectors have most;
  add `PlaceBatch` clamping both ways and T-12 missing `rx_ts`
  documented as the caller's "place at arrival").
- `FuzzPlaceBroadcast(tenths uint16, acc uint8, rxUnixMicro int64)`:
  never panics; `CapturedAt <= received_at + tolerance + accuracy`.
- Tracker bound: 50001 transmitters evicts and counts; concurrency: one
  tracker per goroutine documented, race test with a mutex-guarded
  wrapper.
- Benchmarks `BenchmarkPlaceBroadcast`, `BenchmarkPlaceNetwork`,
  `BenchmarkTrackerTake`, `BenchmarkSelectAltitude`.

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 25 + 24 + 16 vector cases pass; `-race -shuffle=on` green; coverage >= 90 % in both packages.
- [ ] Fuzz clean; lint clean; benchmarks reported.
- [ ] Both `doc.go` rewritten (I-07 limit documented in `rid`); CHANGELOG line; PR with outputs.

## Commits

`feat(timeplace): reconstruct broadcast time and place network states on the ingest clock [WP-6 G-M1]`,
`feat(rid): join Basic ID and Location by transmitter while the identity is fresh [WP-6 G-M1]`,
`feat(rid): select the AMSL altitude with the pressure fallback and hold [WP-6 G-M1]`,
`test(timeplace): run the 25 time placement vectors [WP-6 G-M1]`,
`test(rid): run the identity and pressure altitude vectors [WP-6 G-M1]`.
