// Package identify resolves every track to one of the four identification
// statuses with a stable reason code and a mismatch flag (spec 04 section
// 3.2, LESSONS G-01, G-02, G-04, G-05, I-05): a broadcast serial and
// operator number, a direct Remote ID identity block, a track bound by an
// authenticated session, and the serial_conflict verdict. It also holds the
// spoofing guard that judges a broadcast of one of our serials against the
// authenticated telemetry of that aircraft (I-08, I-09).
//
// It depends on core, regnum, serial and geodesy (haversine). Vectors:
// vectors/testdata/identification_status.json (37) and fleet_match.json
// (10). Owned by WP-7.
package identify
