package rid

import (
	"crypto/sha1" //nolint:gosec // UUIDv5 is defined over SHA-1 (RFC 9562 §5.5); it names an aircraft, it secures nothing
	"encoding/hex"
	"strconv"

	"github.com/rootxkit/uspace-core/odid"
)

// NamespaceUUID is the uuid5 namespace of every Remote ID aircraft id. It
// is utm's and never changes: changing it would give every Remote ID
// aircraft a new id (LESSONS I-06).
const NamespaceUUID = "6f1c7d52-4a0b-5c1e-9d3a-2b8e41f07a65"

// namespace is NamespaceUUID as bytes.
var namespace = [16]byte{
	0x6f, 0x1c, 0x7d, 0x52, 0x4a, 0x0b, 0x5c, 0x1e,
	0x9d, 0x3a, 0x2b, 0x8e, 0x41, 0xf0, 0x7a, 0x65,
}

// AircraftID is the id of an identified aircraft: uuid5 of NamespaceUUID
// and "<id_type>:<ua_id>", exactly utm's ids. The same identity is always
// the same id, across receivers and restarts (I-06); it is never derived
// from the transmitter address, which some transmitters randomise.
func AircraftID(idType odid.IDType, uaID string) string {
	return uuid5(strconv.Itoa(int(idType)) + ":" + uaID)
}

// UnidentifiedID is the id of a transmitter heard without a fresh
// identity: uuid5 of NamespaceUUID and "transmitter:<address>". It is per
// transmitter, not per receiver (I-02).
func UnidentifiedID(transmitter string) string {
	return uuid5("transmitter:" + transmitter)
}

// uuid5 returns the RFC 9562 version 5 UUID of name in namespace, in the
// canonical lower-case text form.
func uuid5(name string) string {
	h := sha1.New() //nolint:gosec // see the import
	h.Write(namespace[:])
	h.Write([]byte(name))
	var sum [sha1.Size]byte
	u := h.Sum(sum[:0])[:16]
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 9562 variant
	var out [36]byte
	hex.Encode(out[0:8], u[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], u[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], u[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], u[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], u[10:16])
	return string(out[:])
}
