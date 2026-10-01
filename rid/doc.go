// Package rid holds the Remote ID observation rules that sit between the
// ODID codec and the picture: the identity-per-transmitter Tracker that
// joins Basic ID and Location by address only while the identity is fresh
// (LESSONS I-01 to I-04, I-06, R-13), the stable aircraft id derived from
// the identity (uuid5, I-06), the AMSL altitude selection with the pressure
// fallback and its 10 s hold (R-07, R-08), the NED velocity from track and
// speed (R-10) and the airborne rule (R-11).
//
// It depends on core and the frozen odid message types. Vectors:
// vectors/testdata/rid_identity.json (24) and pressure_altitude.json (16).
// Owned by WP-6.
package rid
