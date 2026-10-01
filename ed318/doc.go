// Package ed318 holds the EUROCAE ED-318 UASZone model as a GeoJSON
// FeatureCollection (field names per uas_standards: identifier, country,
// name, type, variant, restrictionConditions, region, reason,
// otherReasonInfo, regulationExemption, message, extendedProperties,
// limitedApplicability, zoneAuthority, dataSource; Metadata with
// creationDateTime, updateDateTime, originator), parse and validate on
// receipt (never repair, 06 T9), the ED-269 -> ED-318 mapping and the
// applicability evaluation shared with ed269 (daylight events BMCT, SR,
// SS, EECT added).
//
// Milestone G-M2. Owned by WP-12. An ED-318 round-trip vector is added to
// the knowledge set by that work package.
package ed318
