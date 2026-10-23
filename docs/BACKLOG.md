# Backlog

Ranked table of open items.

| # | Severity (high/medium/low) | Item | Where (file:line if given) | Source (the review that found it) |
|---|---|---|---|---|
| 1 | low | `alerting`: `ac.registrationMismatch` is not reset when the sweep clears `identification_mismatch` as stale while the aircraft stays tracked (identity silent for `StaleAfterS`, track still heard). Later samples that compare no UA registration then count `registration_not_compared` with no alert held. Counter inflation only: no alert is raised, held or cleared differently. Fix: reset the flag where the identity goes stale and its alert is cleared; pair it with a test that raises the mismatch, lets the identity go stale with the track live, sends an omitted-registration sample and checks the counter stays put (and one where the alert is still held and it counts). | alerting/monitor.go:526-528 (sweep, `ac.hasIdentity = false`); alerting/monitor.go:76 (`registrationMismatch`) | review of PR #57 (UA registration), 2026-10-23 |
