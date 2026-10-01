// Package zones judges one aircraft against UAS geographical zones. It
// does no I/O: the caller resolves the ground (terrain.Ground) and the
// geoid undulation (geoid.Undulator) at the aircraft's position and
// passes the values in an Env.
//
// A Zone is built from a parsed ED-269 zone with FromED269: one volume
// (Z-04), limits converted to metres with 0.3048 m per foot exactly
// (Z-08), the published polygon (with holes) or circle centre and radius
// (Z-11), its bounding box and its periods.
//
// The judgement for one aircraft and one zone runs in this order; each
// step is the caller's call, so that a step it cannot take is visible:
//
//  1. Index.Candidates: the zones whose bounding box contains the
//     position (a sparse 0.1 degree grid; zones past IndexLimits, per
//     zone or in total, are checked by box on every lookup). Every containing zone is
//     a candidate. An invalid position has none (C-09).
//  2. Zone.ContainsHorizontally: the box (Z-06), then ray casting on the
//     polygon or the geodesic distance from the circle's centre (D-09).
//     Boundaries are inside. The antimeridian is handled, and -180 and
//     180 are one meridian. An error means "not judged", never "outside".
//  3. Zone.AppliesAt(captured_at) through ed269.Applies (T-09): the
//     aircraft's placed time, never wall time. A zone with no periods, or
//     a zero time, applies (fail-safe, as ed269's zero-value Period).
//  4. JudgeVertical: each limit in its own reference (Z-08, D-01): AMSL
//     against the AMSL altitude, AGL against the altitude less known
//     ground, WGS84 against the altitude plus the undulation; inclusive
//     bounds; a missing limit is unbounded; a lower AGL limit at or below
//     0 needs no DEM; a judged limit that excludes decides. Severity per
//     type (Z-10): PROHIBITED critical, REQ_AUTHORISATION warning,
//     CONDITIONAL as Policy.ConditionalSeverity, USPACE info (so that
//     presence in U-space airspace is visible), NO_RESTRICTION nothing.
//     A PROHIBITED or REQ_AUTHORISATION zone whose only unjudged limit is
//     AGL warns with limit_not_judged (Z-09); any other unjudged limit
//     leaves the zone not evaluated. A pressure altitude is widened by
//     Policy.PressureUncertaintyM each way (R-09): inside as indicated
//     keeps the severity, inside only the widened band warns (capped at
//     the zone's own severity: an info zone stays info), both flagged
//     vertical_known false with within_band.
//
// JudgeHeightLimit is the height limit over the ground (D-04; the
// authority's 120 m rule, spec 09 §2): strictly greater raises a warning;
// unknown ground or no terrain is not evaluated, never judged against 0.
//
// Lifting a REQ_AUTHORISATION zone for an aircraft authorised there is
// the caller's concern (U-05); nothing lifts PROHIBITED.
//
// Every Result is exactly one of raised, not evaluated (with its Reasons:
// every reference that was missing), or judged clear, and is never clear
// because something was unknown: a
// non-finite altitude, ground or undulation is unknown, a non-finite
// limit makes the zone not evaluated, and an invalid pressure margin is
// unbounded. Result.Count adds the outcome to core.Counters as
// zone_checks_not_evaluated, zone_limits_not_judged or
// height_checks_not_evaluated (E-09).
//
// Thresholds (the pressure margin, the conditional severity, the height
// limit) are Policy fields with DefaultPolicy (INV-03). The judgements
// are stateless; an Index is immutable and safe for concurrent use.
//
// It depends on core, geodesy and ed269. Vectors:
// vectors/testdata/zones_vertical.json (38 cases), run by
// TestVectorsZonesVertical. Owned by WP-8.
package zones
