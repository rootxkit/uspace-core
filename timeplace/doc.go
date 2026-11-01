// Package timeplace places a record in time on the ingest clock (LESSONS
// T-01, T-02, T-04, T-07, T-08, T-11, T-12).
//
// Every record carries three times (core.Times): ts on the source's
// clock, rx_ts on ours, and captured_at, where the record sits on our
// clock. All geometry-time reasoning uses captured_at.
//
// # Direct Remote ID (T-07, T-08)
//
// A Location carries only tenths of a second after the full UTC hour.
// PlaceBroadcast reconstructs the hour from the receive time: the moment
// is the latest instant at that offset into some UTC hour that is not
// after received_at + tolerance + declared accuracy (that window is kept
// below half an hour, so no tolerance can defeat the clock_ahead rule; an
// accuracy code above the 4-bit field's 15 is unknown). Then:
//
//   - 0xFFFF is FallbackUnknown and 36000 tenths or more is
//     FallbackInvalid; TS is the receive time;
//   - a moment more than half an hour before the receive time is a clock
//     ahead of ours by more than the tolerance (FallbackClockAhead); TS is
//     the time the broadcast claims, moment + 1 h;
//   - a moment older than max_latency + accuracy is FallbackTooOld; TS is
//     the moment;
//   - otherwise TS = CapturedAt = moment and Source is core.TimeBroadcast.
//
// Every fallback places the record at receipt (CapturedAt = received_at,
// Source core.TimeReceiver); the caller counts it by its Fallback. Both
// boundaries (exactly at the tolerance, exactly at the latency bound) are
// inside. The arithmetic is in integer time.Duration (tenths and
// microseconds are exact), never in float seconds.
//
// # Network Remote ID (T-02)
//
// PlaceNetwork places a provider's flight state against the timestamp of
// the response that carried it, captured_at = received_at - (response_ts
// - state_ts), so the provider's clock skew cancels. A state newer than
// its response is NoteAheadOfResponse and placed at receipt; a state more
// than MaxAgeS behind its response is not shown at all (shown false).
// Without a response timestamp the state's own time is compared with
// ours, with the broadcast tolerance and latency bound (NoteClockAhead,
// NoteTooOld), and a state older than MaxAgeS is not shown.
//
// Source mapping: the vectors (from utm) name the time source of a
// network state "broadcast" when the state's own time is believed and
// "receiver" when it is placed at receipt. PlaceNetwork returns
// core.TimeBroadcast and core.TimeReceiver accordingly. core.TimeProvider
// is reserved for a system-level field that says a record came from a
// provider at all; a system that wants it maps TimeBroadcast onto it at
// its own boundary.
//
// # Batches (T-02)
//
// PlaceBatch places each row of a batch at rx_ts - (newest ts - its ts),
// clamping a spacing that is negative or longer than the bound (120 s in
// the lessons) and counting the clamps.
//
// # Input read late (T-04, T-11)
//
// A reader that stalls and then drains what waited for it (in the
// operating system, a proxy or a queue) reads old input at once, and the
// batch rule alone would stamp it with the read. PlaceAnchored is the
// batch rule plus the anchor of the USSP's operator-telemetry ingest: the
// placement of the source's newest unflagged row, carrying the smallest
// delay seen, past which no later row is placed. A row is then placed by
// the source's own time base, and a row placed more than BacklogAfterS
// behind the read is backlog, as is every row of a delivery the source
// flagged. AnchorMaxAgeS after it was learnt the anchor follows a drifted
// or stepped source clock: a timely delivery relearns it, a delivery it
// calls late keeps it (AnchorNoteKept), and one read offset kept over
// AnchorMaxAgeS of reads replaces it (AnchorNoteRelearnt). The caller
// keeps one Anchor per source; the function is pure.
//
// # Missing times (T-12)
//
// A record without a receive time is placed at this system's arrival and
// counted, never dropped: the caller passes its arrival time where rx_ts
// would go, or uses PlaceArrival for a record that has no source time
// either. Times builds the core.Times of a placement.
//
// Vectors: vectors/testdata/rid_time.json (25 cases). Every function is
// pure and safe for concurrent use. Owned by WP-6.
package timeplace
