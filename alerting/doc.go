// Package alerting is the alert state machine of a monitor (LESSONS C-05
// to C-09, C-14, T-03 to T-06, T-10, B-11, G-02, G-03, I-02, I-04): raise
// once, refresh silently, clear as resolved after hysteresis, as stale on
// silence, as source_disabled on a switch; reject and count backlog, late
// and out-of-order samples; alert only on flying aircraft; a severity
// change is a new raise; zone, identification and identification_mismatch
// alerts beside conflicts; the transmitter pairing rules of Remote ID.
//
// It depends on core, cpa, zones, ed269 and sources. Vectors:
// vectors/testdata/alert_lifecycle.json (28). Owned by WP-10.
package alerting
