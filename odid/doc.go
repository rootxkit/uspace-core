// Package odid decodes and encodes Open Drone ID messages (ASTM F3411 /
// ASD-STAN EN 4709-002 broadcast): Basic ID, Location, System, Operator
// ID, Self-ID, Authentication and Message Packs, from the 25-byte frames
// carried over Bluetooth 4/5 and Wi-Fi NAN/Beacon.
//
// The message types in types.go are frozen by docs/PLAN.md (WP-0) so that
// the rid package can be built against them while the codec (WP-3) is
// written. The codec takes its layout from opendroneid-core-c, never from
// memory (LESSONS E-03), decodes the standard's "unknown" encodings to nil
// (R-01), refuses malformed packs whole (R-03), skips Self-ID and
// Authentication (R-04) and re-encodes to the same bytes (R-02).
//
// Vectors: vectors/testdata/odid_decode.json (187 cases).
package odid
