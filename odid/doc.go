// Package odid decodes and encodes Open Drone ID messages (ASTM F3411 /
// ASD-STAN EN 4709-002 broadcast): Basic ID, Location, System, Operator
// ID, Self-ID, Authentication and Message Packs, the 25-byte frames
// carried over Bluetooth 4/5 and Wi-Fi NAN/Beacon.
//
// The message types in types.go are frozen by docs/PLAN.md (WP-0) so that
// the rid package can be built against them. Unknown (decode.go) is the
// one type the codec adds: the raw bytes of an undefined message type,
// returned only with DecodeOptions.KeepSkipped.
//
// # Reference
//
// The layout is read from opendroneid-core-c at commit
// 6484f26545d4f012682524e2d843fab0fbdc0b34 (2026-09-08):
// libopendroneid/opendroneid.h (the packed structs ODID_BasicID_encoded,
// ODID_Location_encoded, ODID_System_encoded, ODID_OperatorID_encoded,
// ODID_SelfID_encoded, ODID_Auth_encoded and ODID_MessagePack_encoded)
// and libopendroneid/opendroneid.c (encodeDirection, encodeSpeedHorizontal,
// encodeAltitude, encodeTimeStamp, encodeAreaRadius, the matching decoders
// and checkPackContent). The offsets are named in wire.go, never written
// from memory (LESSONS E-03), and pinned twice: by the 170 reference
// frames in vectors/testdata/odid_decode.json, which the library encoded,
// and by tests that encode two messages differing in one field and assert
// which bytes changed. Multi-byte fields are little-endian; bit fields
// are least significant bit first.
//
// # Decoding
//
// Decode takes one 25-byte message or one pack. The standard's "unknown"
// encodings (direction 361, horizontal speed 255 m/s, vertical speed
// 63 m/s, altitude -1000 m, timestamp 0xFFFF, position 0, 0) decode to nil
// (R-01); latitude 0 alone is the equator. Height keeps its reference flag
// (R-12). Self-ID, Authentication and undefined types decode to nothing
// unless DecodeOptions.KeepSkipped (R-04). A malformed pack is refused
// whole, before any message in it is decoded (R-03). Every refusal is a
// *core.FieldError on "frame" or "pack[i]". Nothing indexes the input
// before its length is checked; FuzzDecode holds that.
//
// Directions decode as the reference library decodes them: the raw byte
// plus 180 when the east/west bit is set, so a malformed frame can carry
// 360 to 435 (361 aside, which is unknown). They are not folded into
// [0, 360) here; the encoder writes 360 as 0.
//
// Strings lose their trailing NUL padding only; case and every other byte
// are kept as broadcast, so a decoded message always re-encodes to the
// same field. A frame that is neither 25 bytes nor a pack of exactly the
// length its header gives is refused (the predecessor read the first 25
// bytes and ignored the rest).
//
// # Encoding
//
// Encode and EncodePack are the inverse, for the lab's simulator and for
// tests: every reference frame re-encodes byte for byte (R-02), a direction
// that rounds to 360 is written as 0, and EncodePack refuses what Decode
// refuses. Encode refuses a value the wire cannot carry or that would read
// back as an unknown sentinel; it does not judge plausibility.
//
// # What this package has not seen (R-17)
//
// No frame received from a real aircraft in the air has been decoded by
// this package or by its predecessor. Every check so far is against bytes
// the reference library encoded. Treat the first real receiver session as
// a test of the decoder: keep the raw frames (R-15) and compare the decode
// with a second decoder.
//
// A broadcast is not authenticated. Nothing decoded here is evidence that
// the aircraft is who it says it is, or where it says it is.
//
// Vectors: vectors/testdata/odid_decode.json (187 cases).
package odid
