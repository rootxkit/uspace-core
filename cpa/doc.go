// Package cpa computes the closest point of approach (CPA) of two
// aircraft and the conflict verdict.
//
// # The prediction is a straight line (C-10)
//
// Evaluate assumes both aircraft hold their current velocity. It is a
// conservative linear prediction: it cannot know that an aircraft will
// stop short, turn or level off, so expect alerts that clear when one
// aircraft stops, and know that an aircraft slowing to hover near another
// can hide a conflict until the "inside the minima now" clause catches it
// (scenario SC-21). A turn is seen one sample later, not predicted.
//
// # The judgement (C-01 to C-04, cpa.json)
//
//   - The pair is not judged when the two samples are more than
//     Policy.NeighbourMaxAgeS apart (exactly at the bound is judged):
//     the caller neither raises, refreshes nor clears on it.
//   - The older sample is advanced to the newer one's time along its NED
//     velocity (Advance); "now" is the newer capture time.
//   - Positions are projected onto the tangent plane about the pair's
//     mid-latitude with the WGS84 radii (geodesy.LocalOffsetAboutMidLatM,
//     D-10); longitudes are wrapped, so the antimeridian is not a wall.
//   - t_cpa comes from the horizontal relative motion only; the vertical
//     gap is evaluated at t_cpa (C-01). Velocity down is positive.
//   - Diverging pairs (t_cpa < 0) and pairs with no relative motion
//     (|rel_vel| < 1e-6 m/s) have t_cpa 0 and today's distances (C-02);
//     nothing divides by zero.
//   - Inside both minima now is a conflict whatever t_cpa says (C-03):
//     two aircraft hovering 30 m apart with centimetres per second of
//     velocity noise stay in conflict, and a diverging pair stays in
//     conflict until it is past the minimum. The minima are strict
//     ("60 m is not < 60").
//   - A pressure altitude is a vertical position of unknown accuracy
//     (R-09): when either state has VerticalKnown false the vertical
//     minimum counts as not met, the pair is judged on the horizontal
//     alone, and the altitude differences are reported as zero and must
//     be published as null.
//   - Evaluate(a, b) and Evaluate(b, a) return identical results: the
//     pair is put in a canonical order before any arithmetic.
//
// # Fail-safe on bad numbers (C-09)
//
// A NaN or infinite coordinate, altitude, velocity or time, a position out
// of range, a policy value that is NaN, infinite or negative, or
// arithmetic that leaves the finite domain makes the result Judged false
// with a Reason, never a judged "no conflict". Not judged is not clear:
// the caller keeps whatever it had and counts the reason.
//
// It depends on core and geodesy. Vectors: vectors/testdata/cpa.json (27
// cases), run by TestVectorsCPA. Owned by WP-9. The alert lifecycle built
// on it is package alerting.
package cpa
