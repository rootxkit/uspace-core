// Package cpa computes the closest point of approach of two aircraft in a
// local tangent plane and the conflict test (LESSONS C-01 to C-04, C-15):
// t_cpa from the horizontal relative motion only, vertical separation at
// t_cpa, the diverging and zero-relative-velocity cases decided
// explicitly, "inside the minima now" as a conflict whatever t_cpa says,
// the older sample advanced to the newer one and a pair not judged on a
// stale sample, pressure tracks as unknown vertical, and a neighbour grid
// index at least one radius wide checked against brute force.
//
// It depends on core and geodesy. Vectors: vectors/testdata/cpa.json (27).
// Owned by WP-9. The alert lifecycle built on it is package alerting.
package cpa
