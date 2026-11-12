// Package rid holds the Remote ID observation rules that sit between the
// ODID codec and the picture.
//
// # Identity (I-01 to I-04, I-06, R-13)
//
// Tracker joins Basic ID and Location by transmitter address, because a
// Bluetooth 4 broadcast sends them separately and only the address ties
// them together. An address can be reused (a randomising transmitter, a
// module rebooting with another identity, a spoofer), so an identity is
// used only while it is fresh:
//
//   - its Basic ID was heard within IdentityTTLS (15 s);
//   - the address has not been silent for longer than MaxGapS (3 s). After
//     such a silence everything known about it is dropped (silences), a
//     Location still held for an identity included (held_forgotten);
//   - no other Basic ID of the same ID type has arrived from the address
//     since. If one has, everything known about it is dropped, System and
//     Operator ID too (identity_changes). A second ID type is another
//     identity kept beside the first; a serial is preferred over any other
//     type. ID type 0 with an empty UAS ID is no identity.
//
// State is per (receiver, address). A receiver with no fresh identity of
// its own for an address borrows one another receiver holds, under the
// same TTL (I-03). A Basic ID naming a second fresh identity of one ID
// type for an address, from any receiver, is the anomaly "one
// transmitter, two identities" (address_conflicts, I-04). The caller logs
// it, rate-limited per address; the tracker only counts.
//
// A Location without a fresh identity is held for IdentifyWithinS (4 s),
// counted from when the receiver's own view of the address lost its own
// identity (TTL expiry) or first heard the address without one. An
// identity borrowed from another receiver does not move that start: when
// the lent identity expires, a receiver that never had its own publishes
// unidentified at once (rid_identity.json
// #borrowed-identity-must-be-fresh-too). A Basic ID arriving while the
// Location is held publishes it, placed by the frame that carried the
// Location; a held Location replaced by a newer one is counted
// (held_replaced). After
// that it is published unidentified: DroneID is UnidentifiedID(address),
// Label the address, UAID empty and IDType 0 (unidentified). It is never
// attached to an earlier serial, and when the serial then arrives the
// unidentified track is not merged: it goes stale. Each Location is
// published once (R-13); a repeated Basic ID, Operator ID or System
// message publishes nothing.
//
// The hold runs inside the (receiver, address) state, so it restarts
// when a silence longer than MaxGapS drops the state: MaxGapS must not
// be below IdentifyWithinS (Settings.Validate), or a transmitter heard
// less often than every MaxGapS is never published at all. That is the
// case of DefaultSettings in 1.x (3 s under a 4 s hold), kept because a
// changed default is a major: a system runs RecommendedSettings (9 s),
// under which a transmitter heard at least once every 9 s is published,
// within 4 s plus one gap of the first frame; one heard less often is
// not, and each Location dropped that way is counted (held_forgotten).
// A held Location that a later Basic ID completes can be up to MaxGapS
// old: Observation.RxTS is the receive time of the frame that carried
// the Location, so the caller places it at its true age.
//
// DroneID of an identified aircraft is AircraftID: uuid5 of NamespaceUUID
// and "<id_type>:<ua_id>", exactly utm's ids, the same across receivers
// and restarts (I-06).
//
// The table is bounded by Settings.MaxTransmitters addresses; a new
// address at the bound evicts the one heard longest ago (evicted, E-10).
//
// # Known limit (I-07)
//
// A different aircraft that takes over an address within MaxGapS, while
// the old identity is still fresh, and whose own Basic ID is lost, cannot
// be told apart from the old aircraft: its Locations join the old serial
// until its own Basic ID arrives (then identity_changes counts it and the
// old identity is dropped). Nothing in the broadcast distinguishes the
// two; the rule bounds the exposure to IdentityTTLS.
//
// # Altitude (R-07, R-08)
//
// SelectAltitude and AltitudeSelector choose the AMSL altitude: HAE minus
// the geoid undulation when the geodetic altitude is present and its
// declared accuracy is not poor (known and below MinVerticalAccuracy);
// the pressure altitude, as broadcast and labelled core.AltPressure,
// when the geodetic one is missing or poor; nothing without a geoid even
// with a good HAE and a pressure altitude. AltitudeSelector adds the hold:
// once on pressure a track stays on it until PressureHoldS after the last
// poor fix, while there is a pressure altitude to hold.
//
// # Velocity and status (R-10, R-11)
//
// VelocityNED converts track and speed to north-east-down; Airborne is
// false only for status GROUND.
//
// It depends on core and the frozen odid message types. Vectors:
// vectors/testdata/rid_identity.json (24) and pressure_altitude.json (16).
// Owned by WP-6.
package rid
