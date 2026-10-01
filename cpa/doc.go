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
// # Latitude limit
//
// The mid-latitude tangent plane bends near a pole and overstates
// distances, which reads as clear. Evaluate refuses (ReasonOutOfRange) a
// pair with either aircraft, before or after the advance, within 10 x
// (NeighbourRadiusM + the pair's top horizontal speed x TCPAMaxS) of a
// pole: 8 km for two hovering aircraft under DefaultPolicy (latitude
// above about 89.93 degrees), 26 km at 30 m/s (about 89.77). Inside the
// limit the projection is within 0.05 % of the geodesic. The Grid has no
// such limit: it covers the poles exactly.
//
// # Fail-safe on bad numbers (C-09)
//
// A NaN or infinite coordinate, velocity, altitude or time that the
// judgement uses, a position out of range, a policy value that is NaN,
// infinite or negative, or arithmetic that leaves the finite domain makes
// the result Judged false with a Reason, never a judged "no conflict".
// Not judged is not clear: the caller keeps whatever it had and counts
// the reason. The altitude and vertical velocity of a state whose
// vertical is unknown take no part in the judgement and are not checked,
// so a NaN there never hides a horizontal conflict: the pair is judged on
// the horizontal alone.
//
// # Neighbour grid (C-15)
//
// Grid selects the pairs to judge. It cuts the sphere into latitude bands
// cellM metres high and, per band, into longitude columns at least cellM
// metres wide at the band's poleward edge (per-band longitude scaling, so
// it works at any latitude, not only near a reference one); the columns
// divide 360 degrees exactly and wrap across the antimeridian, and a band
// at a pole has one column. Near returns every id that may be within a
// radius, a superset the caller filters by exact distance; it is checked
// against brute force. Size the cells at least one
// Policy.NeighbourRadiusM wide, so a lookup visits the 3x3 ring; a larger
// radius is still answered correctly, by visiting more cells, or by
// scanning the occupied cells when that is cheaper. Non-finite positions
// are refused before they reach the index and counted. A Grid is not
// safe for concurrent use.
//
// It depends on core and geodesy. Vectors: vectors/testdata/cpa.json (27
// cases), run by TestVectorsCPA. Owned by WP-9. The alert lifecycle built
// on it is package alerting.
package cpa
