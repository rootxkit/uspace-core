// Package alerting is the alert state machine of a monitor: it takes one
// track sample at a time and returns the alerts raised and cleared, for
// conflicts between aircraft (cpa), zone incursions and the height limit
// (zones), and the two identification alerts (G-02, G-03). It does no
// I/O, keeps no clock of its own and never logs: the caller publishes,
// audits and logs (E-09: every refusal is a named counter instead).
//
// # The lifecycle
//
// An alert is raised once per condition and refreshed silently while it
// holds (C-06); a severity change is raised again under the same key
// (C-07). It clears with a reason:
//
//   - resolved: messages showed the condition false for more than
//     Config.ClearAfterS since it was last true. Only a judgement shows
//     false: a pair cpa.Evaluate did not judge (a stale neighbour, bad
//     numbers), a zone zones.JudgeVertical did not evaluate, or a sample
//     without an identification neither refreshes nor shows false (C-04,
//     C-09, hysteresis_rule). An active pair is judged on every sample of
//     either aircraft wherever the other now is, so it is never shown
//     false by falling outside the neighbour search. A resolved conflict
//     carries both the last numbers that showed it true (Detail) and the
//     separation of the judgement that cleared it (ClearingDetail, C-14).
//   - stale: an aircraft involved was not heard for Config.StaleAfterS
//     (T-10). The mismatch alert goes stale with the aircraft's identity,
//     the others with its flying track. When an alert could be cleared
//     both ways at once, evidence outranks silence (resolved).
//   - landed: a sample said the aircraft is not flying (C-05, C-14; the
//     owner's decision on plan §11 gap 4 replaces the old stale). An
//     unknown flying state is not flying and not a landing: the track is
//     held, its alerts too, and they go stale unless it flies on.
//   - source_disabled: SwitchSource took a state that disables the source
//     of the aircraft's last admitted sample (B-11). The drop goes by that
//     last sample only: an aircraft also heard through a source that is
//     still enabled is dropped all the same, and its next sample from the
//     enabled source starts a new track and raises its alerts again (as
//     the old monitor did).
//   - flight_ended (or any reason the caller names): Drop.
//
// Nothing missing, stale, NaN or unjudged clears an alert by itself, and
// neither does the aircraft cap (E-10): past Config.MaxAircraft only an
// aircraft without an active alert is evicted, and when every one holds
// an alert a new id is refused (rejected_capacity, CapacityExceeded). No
// one source may add a new id while it holds Config.MaxSourceShare of the
// cap in alert-holding aircraft (rejected_source_share), so a receiver
// flooding alert holders leaves room for the others; free and evictable
// slots are open to every source. Every refusal is counted in total and per source and reported
// in Events.Refused, at most once per source per
// Config.RefusalEventIntervalS: the caller alerts on it.
//
// # Admission
//
// Backlog (T-04), late on the ingest-to-monitor leg (T-05), out of order
// on the sample's own source clock (T-03), from a disabled source (B-11)
// and invalid samples are counted and judge nothing. Placement and
// staleness use Track.CapturedAtS, the ingest's clock (T-01); zone
// applicability is judged at it in UTC (T-09). A sample placed or
// received ahead of the wall by more than Config.AheadToleranceS is
// refused, and alert times use the placement capped at the wall time.
//
// wallS, on every call, is the monitor's own monotonic clock on the
// ingest's time base. The caller never derives it from a sample's times.
//
// Source keys (Track.Source and Track.Station) are the ingest's
// authenticated station and receiver ids. The source share
// (Config.MaxSourceShare) and the per-source counters assume a source
// cannot mint new keys at will: how many keys there can be is bounded
// only by the upstream authentication (the receivers' HMAC keys, auth),
// and the refusal rate limit by maxRefusalSources.
//
// # What the caller must do
//
//   - One Monitor per cell set (spec 05 §3), owned by one goroutine: a
//     Monitor is not safe for concurrent use.
//   - Call Tick about once a second: the end of a condition can be the
//     absence of telemetry.
//   - Republish Active every second with its current numbers, and replay
//     it to a console that connects later (C-08). Active ranks critical
//     first, then conflicts by time to loss of separation
//     (cpa.Result.LoSStartS, detail los_start_s).
//   - Send every raise and clear with the policy_version of the Config in
//     force (spec 04 §3.3); the monitor does not know it.
//   - Call Drop when a flight ends (flight_ended) or the caller knows the
//     aircraft landed (landed); feed every source-control state to
//     SwitchSource.
//   - Treat an identification raise or clear as the incident seam (G-03).
//   - Watch the counters: an invalid separation policy is counted as
//     config_invalid and every pair it refuses as not judged.
//
// It depends on core, cpa, zones and sources (ed269 in its tests).
// Vectors: vectors/testdata/alert_lifecycle.json (35 cases), run by
// TestVectorsAlertLifecycle as written. Owned by WP-10.
package alerting
