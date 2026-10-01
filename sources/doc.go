// Package sources is the source-control model (predecessor U-15, LESSONS
// B-09, B-11): switches by source type and by instance, default deny, the
// question "is this source enabled and if not why", and the follower rule
// that applies a state only if its version is strictly higher within the
// same epoch and takes any state from a new epoch. It never fails closed:
// with no state, everything is enabled.
//
// Vectors: vectors/testdata/source_control.json (8). Owned by WP-4.
package sources
