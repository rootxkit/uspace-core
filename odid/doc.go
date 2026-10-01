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
// encodings (direction 361, and 360 or more, which only a malformed
// frame carries; horizontal speed 255 m/s, vertical speed
// 63 m/s, altitude -1000 m, timestamp 0xFFFF, position 0, 0) decode to nil
// (R-01); latitude 0 alone is the equator. Height keeps its reference flag
// (R-12). Self-ID, Authentication and undefined types decode to nothing
// unless DecodeOptions.KeepSkipped (R-04), alone or inside a pack: an
// undefined type does not spoil the pack around it. A malformed pack is refused
// whole, before any message in it is decoded (R-03). Every refusal is a
// *core.FieldError on "frame" or "pack[i]". Nothing indexes the input
// before its length is checked; FuzzDecode holds that.
//
// Strings lose their trailing NUL padding only; case and every other byte
// are kept as broadcast, so a decoded message always re-encodes to the
// same field. A lone message must be exactly 25 bytes (the receiver layer
// strips its transport framing; the predecessor read the first 25 bytes
// and ignored the rest). A pack must hold at least the messages its
// header declares; bytes after them are transport padding (BLE, Wi-Fi
// NAN) and are ignored.
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
