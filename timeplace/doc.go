// Package timeplace places a record in time on the ingest clock (LESSONS
// T-01 to T-08, T-12): the broadcast time of a direct Remote ID Location
// reconstructed from tenths after the hour with tolerance and declared
// accuracy (T-07), the four fallbacks clock_ahead, too_old, unknown and
// invalid (T-08), the placement of a network Remote ID state against its
// response timestamp so the provider clock skew cancels (T-02), and the
// placement of a batch at rx_ts minus its spacing with clamping (T-02).
// Output is core.Times.
//
// Vectors: vectors/testdata/rid_time.json (25 cases). Owned by WP-6.
package timeplace
