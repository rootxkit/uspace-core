package core

import "time"

// TimeSource says which rule produced CapturedAt (04 §2).
type TimeSource string

const (
	// TimeSourceClock is captured_at derived from the source's own ts
	// placed within its batch (T-02).
	TimeSourceClock TimeSource = "source_clock"
	// TimeBroadcast is a Remote ID broadcast time within tolerance (T-07).
	TimeBroadcast TimeSource = "broadcast"
	// TimeReceiver is captured_at placed at the receiver's receipt time (fallback).
	TimeReceiver TimeSource = "receiver"
	// TimeProvider is captured_at placed from a network provider's response (T-02).
	TimeProvider TimeSource = "provider"
	// TimeSystem is captured_at placed at this system's arrival time (T-12).
	TimeSystem TimeSource = "system"
)

// Times are the three times every record carries and the verdicts that
// come with them (LESSONS T-01, T-04).
//
// TS is the source's own clock; it orders a source's own records and is
// never compared across sources. It is nil when the record carried none
// (T-12: "do not order within the source").
//
// RxTS is when this system received the record, on its own clock.
//
// CapturedAt is where the record sits in time on this system's clock. All
// geometry-time reasoning uses it.
//
// Backlog is the ingest's verdict that the record is history; consumers
// record it and never raise a live alert from it.
type Times struct {
	TS         *time.Time `json:"ts,omitempty"`
	RxTS       time.Time  `json:"rx_ts"`
	CapturedAt time.Time  `json:"captured_at"`
	Source     TimeSource `json:"time_source"`
	Backlog    bool       `json:"backlog"`
}

// LagS is the delivery lag rx_ts - captured_at in seconds; it is the only
// legitimate use of rx_ts in a judgement (T-05: the lateness bound covers
// the leg you control).
func (t Times) LagS() float64 {
	return t.RxTS.Sub(t.CapturedAt).Seconds()
}
