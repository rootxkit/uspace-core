// Package core holds the shared base types of uspace-core: positions,
// vertical references and altitude sources, the three times every record
// carries, trust classes, severities, zone types, the identification block
// and small helpers for counters and field-naming errors.
//
// These types are frozen by docs/PLAN.md (WP-0). Every other package in the
// module imports them and nothing here imports another package of the
// module, so the dependency graph has one root. A change here is a change
// to every package's API and needs the plan updated first.
//
// Rules the types embody (knowledge/LESSONS.md): E-13 (units and datums in
// every name), T-01 (ts, rx_ts, captured_at on one clock), R-05 (a
// broadcast is never authenticated; the basis says so), G-01 (four
// identification statuses with stable reasons), E-09 (everything refused
// or degraded is counted), C-09 (non-finite numbers are rejected before
// they reach any spatial code).
package core
