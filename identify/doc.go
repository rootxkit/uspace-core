// Package identify resolves every track to one of the four identification
// statuses (registered, suspended, unknown_operator, unidentified) with a
// stable reason code, a mismatch flag and a basis (spec 04 section 3.2,
// LESSONS G-01, G-02, G-04, G-05, I-05), and holds the spoofing guard that
// judges a broadcast of one of our serials against that aircraft's
// authenticated telemetry (I-08, I-09, S-10). It keeps no state, never
// logs and never panics on any input. It depends on core, odid, regnum,
// serial and geodesy (haversine).
//
// # Who resolves
//
// Each source adapter resolves its own tracks against its own registry
// snapshot as it publishes them, and publishes each track once with its
// identification (G-09); there is no separate resolver. The USSP resolves
// the flights of its authenticated sessions with ResolveBound (basis
// authenticated: the binding is the proof, not anything the aircraft
// broadcasts) and every broadcast or peer track with ResolveBroadcast or
// ResolveRemoteID (basis as_broadcast: "as broadcast and unverified", and
// every display of such a status says so). The authority resolves every
// track in its picture against the registry itself, on the broadcast
// basis. alerting consumes core.Identification and does not import this
// package.
//
// # The resolution table
//
// Inputs are cleaned first: the serial trimmed with its case kept (the
// serial package), the operator number trimmed; empty after the trim is nil.
// The cleaned values are echoed; the operator number keeps any EU secret
// suffix as received, which regnum.CompareKey removes only for the
// comparison (G-04). In order:
//
//   - no serial: unidentified / no_serial;
//   - no aircraft for the serial (exact match first, else a case-folded
//     match only when exactly one aircraft has it, G-05), or more than one
//     candidate: unknown_operator / serial_unknown, whatever operator is
//     claimed;
//   - a projection row the registry has no aircraft for: unknown_operator
//     / not_in_registry;
//   - the UAS revoked or suspended, then its owner revoked or suspended:
//     suspended with the matching reason; suspension outranks a mismatch,
//     which is still flagged. A status other than active (or empty, read
//     as active) fails safe and counts as suspended;
//   - our own fleet (no UAS operator): registered / matched on the serial
//     alone, the operator number not compared;
//   - an owner the projection does not hold: unknown_operator /
//     owner_unknown;
//   - no operator number: unknown_operator / operator_absent;
//   - an operator number that is not the owner's: unknown_operator /
//     operator_mismatch, mismatch true, the owner's number in
//     RegisteredOperatorReg; a mismatch is never registered (G-02);
//   - otherwise registered / matched.
//
// ResolveRemoteID looks up only a serial (Basic ID type 1, I-05): another
// identity type is unknown_operator / not_a_serial and never names a
// registry aircraft. RegistryUASID is the matched aircraft's id, nil when
// none matched. A nil Lookup is Unavailable (registry_unavailable).
//
// # The projection a caller reads (G-08)
//
// A Lookup is a read of a projection, never an authority. Callers must:
//
//   - build a Snapshot from one read of the projection that the registry
//     writes in the same transaction as the change, and that a full
//     re-projection repairs at startup and every 300 s;
//   - mark a projected aircraft the registry does not hold as InRegistry
//     false, never leave it looking registered;
//   - refresh the snapshot every 5 s and, when a read fails, keep the
//     snapshot already held: a database hiccup must not turn every
//     aircraft unknown. Only when no snapshot was ever read is the
//     registry unavailable (Unavailable, or a nil Lookup).
//
// A Snapshot is immutable once built; replace it whole on refresh.
//
// # The spoofing guard
//
// JudgeFleet decides what a broadcast of one of our serials is: withheld
// within the spoof distance of where live authenticated telemetry places
// the aircraft (the authenticated track is better), a separate unverified
// track beyond it (SerialConflict), and the broadcast speaking for our
// aircraft when that telemetry is quiet. Only live authenticated rows
// vouch: backlog rows, rows captured more than the live window before
// receipt, and every broadcast row are ignored (I-09). The distance is the
// haversine (D-11), never the ellipsoid.
//
// Vectors: vectors/testdata/identification_status.json (37) and
// fleet_match.json (10). Owned by WP-7.
package identify
