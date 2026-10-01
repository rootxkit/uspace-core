// Package ed269 reads and writes EUROCAE ED-269 geo-zone documents
// strictly, so that export(parse(f)) == f (LESSONS Z-01 to Z-06): unknown
// fields, wrong types, values outside an enumeration, over-length strings,
// unclosed rings and unevaluable periods are refused with a JSON path and
// a reason, all or nothing, capped at 100 problems. It also evaluates zone
// applicability (TimePeriod, DailyPeriod, weekdays in the schedule offset,
// overnight periods; Z-07, T-09), which ed318 and zones reuse.
//
// Vectors: vectors/testdata/ed269_parse.json (52) and
// zones_applicability.json (32). Owned by WP-5.
package ed269
