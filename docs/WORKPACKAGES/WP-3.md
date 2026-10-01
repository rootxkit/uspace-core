# WP-3: `odid` codec

Branch `feat/WP-3-odid-codec`. Milestone G-M1. Owns `odid/` except
`types.go` (frozen; if a field is missing, propose the change in the PR
with the vector that needs it). Depends on WP-0 only. Consumers: `rid`
(WP-6, built against the frozen types in parallel), the authority's
`rid-ingest`, the lab's simulator encoder.

## Read first

1. `CLAUDE.md`, `docs/PLAN.md §3.5`, `§8.4`, `§8.5`.
2. `odid/types.go` (every struct and enum; the JSON names are the vector
   names).
3. `vectors/testdata/odid_decode.json`: header (`units`, `byte_order`),
   the 10 `refuse-*` cases, `accept-basic-id-and-location-pack`, the
   `skip-*`, `position-zero-zero-is-unknown`,
   `latitude-zero-alone-is-a-position`, `direction-rounding-to-360-is-north`,
   and at least one `reference-*` of each type and one `reference-pack-*`.
4. LESSONS E-03, R-01, R-02, R-03, R-04, R-15, R-17.
5. opendroneid-core-c `libopendroneid/opendroneid.h` (the packed structs
   `ODID_BasicID_encoded`, `ODID_Location_encoded`, `ODID_System_encoded`,
   `ODID_OperatorID_encoded`, `ODID_MessagePack_encoded`) and
   `opendroneid.c` (`encodeLocationMessage`, `decodeLocationMessage`,
   the speed, direction and altitude helpers). Fetch it; do not recall it.
   Reference only: `utm/gateway/odid.py`.

## What to build

```go
func Decode(frame []byte, opts DecodeOptions) ([]Message, error)
func DecodeMessage(b [MessageSize]byte, opts DecodeOptions) (Message, error)   // nil, nil for a skipped type
func Encode(m Message) ([MessageSize]byte, error)
func EncodePack(ms []Message) ([]byte, error)
func TypeOf(frame []byte) (MessageType, error)
const (SpecialDirection = 361; SpecialSpeedH = 255; SpecialSpeedV = 63; SpecialAltitudeM = -1000.0; SpecialTimestamp = 0xFFFF)
```

Wire rules (E-03: take every offset from the C structs and pin it with
the reference frames; derive any you are unsure of by diffing two frames
that differ in one field):

- Byte 0: `(type << 4) | protocol_version`. Little-endian multi-byte
  fields; bit fields LSB first.
- **Basic ID**: byte 1 `(id_type << 4) | ua_type`; bytes 2-21 `ua_id`,
  NUL-padded; trailing NULs removed, case kept.
- **Location**: byte 1 `(status << 4) | reserved(1) | height_type(1) |
  ew_dir(1) | speed_mult(1)` (check the bit order in the struct!); byte 2
  direction; byte 3 speed; byte 4 vspeed (int8); 5-8 lat int32 x 1e-7;
  9-12 lon; 13-14 baro alt uint16; 15-16 geo alt; 17-18 height; 19
  `(vert_acc << 4) | horiz_acc`; 20 `(baro_acc << 4) | speed_acc`; 21-22
  timestamp uint16 tenths; 23 `(reserved << 4) | ts_acc`; 24 reserved.
  Decoding: `direction = raw + 180 if ew bit` and 361 -> nil; speed `raw *
  0.25` when mult 0, `raw * 0.75 + 63.75` when mult 1, 255 -> nil; vspeed
  `raw * 0.5`, 63 -> nil; altitudes `raw * 0.5 - 1000`, -1000 -> nil;
  timestamp `raw / 10`, 0xFFFF -> nil; lat 0 and lon 0 together -> both
  nil, lat 0 alone is the equator (`latitude-zero-alone-is-a-position`).
  `height_reference` kept as the flag (R-12).
- **System**: byte 1 flags `(reserved << 3) | classification_type(3) |
  operator_location_type(2)` (verify), 2-5 operator lat, 6-9 lon, 10-11
  area count uint16, 12 area radius uint8 x 10, 13-14 ceiling, 15-16 floor,
  17 `(category_eu << 4) | class_eu`, 18-19 operator altitude, 20-23
  timestamp uint32 (seconds since 2019-01-01), 24 reserved. Sentinels as
  for Location (`reference-pack-001` shows ceiling, floor and operator
  altitude nil, timestamp 0).
- **Operator ID**: byte 1 type, 2-21 id NUL-padded, 22-24 reserved.
- **Message pack** (R-03): byte 1 message size (must be 25), byte 2 count
  (1-9), then `count` messages. Refuse the whole pack, with these exact
  phrases in the error (the vectors match `error_contains`): "empty";
  "pack message size 24, expected 25"; "pack of 0 messages"; "pack of 10
  messages"; "a pack inside a pack"; "too many Basic ID messages in a
  pack" (more than 2); "more than one LOCATION message in a pack" (same
  for SYSTEM, SELF_ID, OPERATOR_ID); "pack shorter than it says"; "N
  bytes, a message is 25" for a lone message shorter than 25. Return
  `*core.FieldError{Field: "frame" | "pack[i]", Reason: <phrase>}`.
- **Skip** (R-04): Self-ID, Authentication and unknown types decode to
  nothing (`expected.messages: []`) unless `opts.KeepSkipped`.
- **Encode** (R-02): the inverse, byte for byte on every reference frame;
  direction rounds 359.6 to 360 then wraps to 0 (encodes 0 with ew bit 0);
  nil fields encode the sentinel; speed picks the multiplier; strings are
  NUL-padded and refused if longer than 20 bytes; a pack with more than 9
  messages or a nested pack is refused.
- Never panic: every frame length is checked before indexing. A frame
  longer than one message that is not a pack is refused ("N bytes, a
  message is 25").

## Vector test (`odid/vectors_test.go`)

```go
type expMsg struct { Type string `json:"type"`; /* union of all fields as pointers */ }
```

Decode `input.hex`; if `expected.error` is set assert `err != nil` and
`strings.Contains(err.Error(), expected.error_contains)`; else compare
the message list in order: `type`, then per type every field with the
header tolerances (lat/lon 1e-9, other floats 1e-6, integers and strings
exact; nil vs null). Then **round-trip**: for every accepted single
message `Encode(msg) == frame`; for every accepted pack `EncodePack ==
frame` (R-02) except `direction-rounding-to-360-is-north` whose
`encoded_from.direction_deg` 359.6 must encode to that frame.

## Other tests

- E-01 pairs for every refusal (the vectors have the accept twin for the
  pack; add one per lone-message refusal).
- Offsets by differencing: a test that encodes two Locations differing in
  one field and asserts which bytes changed (E-03), for direction, speed
  multiplier, height type, timestamp.
- `FuzzDecode(frame []byte)` seeded with all 187 vector frames: never
  panics; if `Decode` succeeds, `EncodePack`/`Encode` of the result
  decodes to an equal value (idempotent round trip; byte equality only
  where reserved bits were zero: check reserved bits and assert byte
  equality when they are).
- `FuzzEncodeLocation` over random field values: `Decode(Encode(x))`
  within one quantum.
- Benchmarks `BenchmarkDecodeMessage` (Location), `BenchmarkDecodePack`
  (4 messages), `BenchmarkEncodeMessage`. Zero allocations is the goal
  for `DecodeMessage`; report allocs/op.

## Done when

- [ ] Lint run locally before every push, with the pinned linters:
  `make tools` (once; installs golangci-lint v2.14.0 and staticcheck
  v0.8.1, the versions CI runs) then `make lint` (gofmt, vet,
  staticcheck, golangci-lint; it refuses any other golangci-lint
  version) prints no issue. Paste its last lines into the PR.
- [ ] 187/187 vector cases pass including round trips; `-race -shuffle=on` green.
- [ ] Coverage >= 95 % (a codec is small; cover every branch).
- [ ] Fuzz targets run 10 s clean; corpus seeded; lint clean.
- [ ] `doc.go` rewritten (list the reference commit of opendroneid-core-c you read; R-17 note that no real broadcast has been seen).
- [ ] CHANGELOG line; PR with commands and outputs.

## Commits

`feat(odid): decode Basic ID, Location, System and Operator ID from reference layouts [WP-3 G-M1]`,
`feat(odid): decode and refuse message packs whole [WP-3 G-M1]`,
`feat(odid): encode messages and packs byte for byte [WP-3 G-M1]`,
`test(odid): run the 187 decode vectors with round trips [WP-3 G-M1]`,
`test(odid): fuzz the decoder and benchmark it [WP-3 G-M1]`.
