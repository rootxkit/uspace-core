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
// when a silence longer than MaxGapS drops the state. With MaxGapS
// below IdentifyWithinS, the case of DefaultSettings in 1.x (3 s under
// a 4 s hold, kept because a changed default is a major), a transmitter
// heard less often than every MaxGapS is forgotten before each hold
// ends and never published at all; each Location dropped that way is
// counted (held_forgotten), and Settings.Validate refuses the settings.
//
// # Publishing what was heard (PublishHeld)
//
// A system runs PublishHeldSettings: the defaults with
// Settings.PublishHeld, and TakeAll and Sweep in place of Take and
// Forget. The two rules are then separate. MaxGapS (3 s) says how long
// an identity survives a silence, and stays short. A Location that was
// heard is published whatever happens to its state: one still held,
// unpublished, when its state is forgotten after a silence, when its
// address is evicted from the full table, or when another Basic ID of
// the same ID type takes the address, is returned as an unidentified
// observation by the call that discards it, with the receive time of
// its own frame (held_flushed, and unidentified). So a transmitter is
// published however rarely it is heard, and no identity is carried
// across a silence longer than MaxGapS: a Location is identified only
// by a Basic ID from a hearing of the address that no such silence has
// broken, its own receiver's or the lending receiver's (I-03). The
// one held Location still not published is one replaced by a newer
// Location of the same state before its hold ended (held_replaced):
// the newer one is published in its place.
//
// The flushed observations come back from TakeAll, before the frame's
// own, and from Sweep. Any frame is the clock that ends every silence,
// so TakeAll returns the flushed Locations of other addresses too. A
// caller without a periodic Sweep gets a flushed Location only when the
// next frame of any transmitter arrives; with a tick it gets it about
// MaxGapS plus one tick after it was heard. The observations of one
// (receiver, address) are returned in the order their Locations were
// taken; across receivers the caller orders by Observation.RxTS, as it
// must without the flag.
//
// RecommendedSettings (MaxGapS 9 s, from 1.15.0) is deprecated: it
// publishes a transmitter heard at least once every 9 s by keeping the
// identity for 9 s of silence too, which widens the known limit below
// from 3 s to 9 s, and one heard less often is still dropped.
//
// A held Location that a later Basic ID completes can be up to MaxGapS
// old, and a flushed one is older than MaxGapS: Observation.RxTS is the
// receive time of the frame that carried the Location, so the caller
// places it at its true age.
//
// DroneID of an identified aircraft is AircraftID: uuid5 of NamespaceUUID
// and "<id_type>:<ua_id>", exactly utm's ids, the same across receivers
// and restarts (I-06).
//
// The table is bounded by Settings.MaxTransmitters addresses; a new
// address at the bound evicts the one heard longest ago (evicted, E-10);
// a Location it still held is counted (held_evicted) or, with
// PublishHeld, published unidentified.
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
