package timeplace

import (
	"math"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// Fallback says why a broadcast time was not used for captured_at. The
// empty value means it was used.
type Fallback string

const (
	// FallbackNone is a broadcast time that was believed.
	FallbackNone Fallback = ""
	// FallbackClockAhead is a broadcast time ahead of ours by more than
	// the tolerance (the hour choice put it more than half an hour back).
	FallbackClockAhead Fallback = "clock_ahead"
	// FallbackTooOld is a broadcast time older than the latency bound plus
	// the declared accuracy.
	FallbackTooOld Fallback = "too_old"
	// FallbackUnknown is the standard's unknown timestamp, 0xFFFF.
	FallbackUnknown Fallback = "unknown"
	// FallbackInvalid is a timestamp of 3600.0 s or more: it fits the
	// field but is not inside any hour.
	FallbackInvalid Fallback = "invalid"
)

// TimestampUnknown is the wire value of an unknown Location timestamp.
const TimestampUnknown uint16 = 0xFFFF

// tenthsPerHour is the first timestamp value past the hour.
const tenthsPerHour = 36000

// clockAheadWrap is the age beyond which a reconstructed moment is read
// as a clock ahead of ours rather than an old broadcast: half the hour
// the timestamp covers (T-08). It is a property of the encoding, not a
// tunable threshold.
const clockAheadWrap = 30 * time.Minute

// maxPolicyDuration bounds a policy value so that time arithmetic cannot
// overflow. No tolerance, latency bound or age limit is anywhere near it.
const maxPolicyDuration = 24 * time.Hour

// BroadcastPolicy holds the tolerances of PlaceBroadcast (T-07, T-08).
//
// The zero value is not a usable policy: with no tolerance and no latency
// bound, every broadcast older than its declared accuracy is too_old and
// every one ahead of our clock is clock_ahead, so nearly everything is
// placed at receipt. Start from DefaultBroadcastPolicy.
type BroadcastPolicy struct {
	// ToleranceS is how far ahead of our clock a broadcast time may be
	// and still be believed. Default 1 s.
	ToleranceS float64
	// MaxLatencyS is how old a broadcast may be when we receive it and
	// still be placed at its own time. Default 5 s.
	MaxLatencyS float64
}

// DefaultBroadcastPolicy returns the defaults of utm: 1 s tolerance, 5 s
// latency bound.
func DefaultBroadcastPolicy() BroadcastPolicy {
	return BroadcastPolicy{ToleranceS: 1, MaxLatencyS: 5}
}

// Placement is where a record sits in time.
type Placement struct {
	// TS is the time the source claims, on its own clock.
	TS time.Time
	// CapturedAt is where the record is placed on our clock.
	CapturedAt time.Time
	// Source says which rule produced CapturedAt: core.TimeBroadcast when
	// the source's time was believed, core.TimeReceiver when the record
	// was placed at receipt.
	Source core.TimeSource
	// Fallback says why a broadcast time was not believed; empty when it
	// was. PlaceNetwork leaves it empty and returns a NetworkNote.
	Fallback Fallback
}

// maxAccuracyCode is the largest MAV_ODID_TIME_ACC code: the wire field
// is 4 bits.
const maxAccuracyCode = 15

// AccuracyS converts a MAV_ODID_TIME_ACC code to seconds: 0 is unknown
// and gives 0, code k in 1..15 gives k/10 s. A code above 15 cannot come
// off the 4-bit wire field and is read as unknown (0), never trusted to
// widen a bound.
func AccuracyS(code uint8) float64 {
	return accuracy(code).Seconds()
}

// accuracy is AccuracyS as an exact duration.
func accuracy(code uint8) time.Duration {
	if code > maxAccuracyCode {
		return 0
	}
	return time.Duration(code) * 100 * time.Millisecond
}

// seconds converts a policy value in seconds to an exact duration,
// rounded to the microsecond. A NaN or negative value is 0 and a value
// beyond maxPolicyDuration is clamped to it, so that no input can make the
// time arithmetic overflow.
func seconds(s float64) time.Duration {
	if math.IsNaN(s) || s <= 0 {
		return 0
	}
	if s >= maxPolicyDuration.Seconds() {
		return maxPolicyDuration
	}
	return time.Duration(math.Round(s*1e6)) * time.Microsecond
}

// atReceipt is a fallback placement.
func atReceipt(ts, receivedAt time.Time, fb Fallback) Placement {
	return Placement{TS: ts, CapturedAt: receivedAt, Source: core.TimeReceiver, Fallback: fb}
}

// PlaceBroadcast places a direct Remote ID Location from its wire
// timestamp (tenths of a second after the full UTC hour, 0xFFFF unknown)
// and its declared timestamp accuracy code, received at receivedAt (our
// clock). See the package documentation for the rule. It never fails:
// every input yields a placement, and CapturedAt is never after
// receivedAt + tolerance + accuracy.
func PlaceBroadcast(timestampTenths uint16, tsAccuracyCode uint8, receivedAt time.Time, pol BroadcastPolicy) Placement {
	if timestampTenths == TimestampUnknown {
		return atReceipt(receivedAt, receivedAt, FallbackUnknown)
	}
	if timestampTenths >= tenthsPerHour {
		return atReceipt(receivedAt, receivedAt, FallbackInvalid)
	}
	acc := accuracy(tsAccuracyCode)
	// How far ahead of our clock a broadcast may be. Kept below the
	// clock-ahead wrap, or a large tolerance would believe a clock half an
	// hour or more ahead and T-08 could never fire.
	ahead := min(seconds(pol.ToleranceS)+acc, clockAheadWrap-time.Microsecond)
	limit := receivedAt.UTC().Add(ahead)
	// time.Truncate counts from the zero time, which is on a UTC hour.
	moment := limit.Truncate(time.Hour).Add(time.Duration(timestampTenths) * 100 * time.Millisecond)
	if moment.After(limit) {
		moment = moment.Add(-time.Hour)
	}
	age := receivedAt.Sub(moment)
	if age > clockAheadWrap {
		return atReceipt(moment.Add(time.Hour), receivedAt, FallbackClockAhead)
	}
	if age > seconds(pol.MaxLatencyS)+acc {
		return atReceipt(moment, receivedAt, FallbackTooOld)
	}
	return Placement{TS: moment, CapturedAt: moment, Source: core.TimeBroadcast}
}

// NetworkPolicy holds the bounds of PlaceNetwork (T-02).
type NetworkPolicy struct {
	// MaxAgeS is the oldest a state may be, behind its response or behind
	// our clock, and still be shown. Default 60 s.
	MaxAgeS float64
	// ToleranceS is how far ahead of our clock a state without a response
	// timestamp may be. Default 1 s.
	ToleranceS float64
	// MaxLatencyS is how old a state without a response timestamp may be
	// and still be placed at its own time. Default 5 s.
	MaxLatencyS float64
}

// DefaultNetworkPolicy returns the defaults of utm: 60 s, 1 s, 5 s.
func DefaultNetworkPolicy() NetworkPolicy {
	return NetworkPolicy{MaxAgeS: 60, ToleranceS: 1, MaxLatencyS: 5}
}

// NetworkNote says why a network state was placed at receipt; empty when
// its own time was used.
type NetworkNote string

const (
	// NoteNone is a state placed by its own time.
	NoteNone NetworkNote = ""
	// NoteAheadOfResponse is a state newer than the response carrying it:
	// impossible on one clock.
	NoteAheadOfResponse NetworkNote = "ahead_of_response"
	// NoteClockAhead is a state without a response timestamp ahead of our
	// clock by more than the tolerance.
	NoteClockAhead NetworkNote = "clock_ahead"
	// NoteTooOld is a state without a response timestamp older than the
	// latency bound (but not older than MaxAgeS).
	NoteTooOld NetworkNote = "too_old"
)

// PlaceNetwork places a network Remote ID flight state whose own
// timestamp is stateTS, carried by a response stamped responseTS (nil
// when the response had none) and received at receivedAt on our clock.
// shown is false when the state is older than MaxAgeS and must not be
// shown as current; the placement is then the zero value. The bounds are
// inside: a state exactly MaxAgeS old is shown.
func PlaceNetwork(stateTS time.Time, responseTS *time.Time, receivedAt time.Time, pol NetworkPolicy) (p Placement, note NetworkNote, shown bool) {
	maxAge := seconds(pol.MaxAgeS)
	if responseTS != nil {
		behind := responseTS.Sub(stateTS)
		if behind > maxAge {
			return Placement{}, NoteNone, false
		}
		if behind < 0 {
			return atReceipt(stateTS, receivedAt, FallbackNone), NoteAheadOfResponse, true
		}
		return Placement{TS: stateTS, CapturedAt: receivedAt.Add(-behind), Source: core.TimeBroadcast}, NoteNone, true
	}
	age := receivedAt.Sub(stateTS)
	if age > maxAge {
		return Placement{}, NoteNone, false
	}
	if age < -seconds(pol.ToleranceS) {
		return atReceipt(stateTS, receivedAt, FallbackNone), NoteClockAhead, true
	}
	if age > seconds(pol.MaxLatencyS) {
		return atReceipt(stateTS, receivedAt, FallbackNone), NoteTooOld, true
	}
	return Placement{TS: stateTS, CapturedAt: stateTS, Source: core.TimeBroadcast}, NoteNone, true
}

// DefaultMaxBatchSpacing is the longest spacing a batch row keeps before
// it is clamped (T-02: 120 s).
const DefaultMaxBatchSpacing = 120 * time.Second

// PlaceBatch places the rows of one batch received at rxTS whose source
// times are ts: capturedAt[i] = rxTS - (max(ts) - ts[i]), so the source's
// skew cancels within the batch and the rows keep their spacing. A
// spacing longer than maxSpacing (or negative, which cannot happen by
// construction and is guarded anyway) is clamped to the bound and
// counted in clamped. A negative maxSpacing is read as 0. An empty batch
// returns nil.
func PlaceBatch(rxTS time.Time, ts []time.Time, maxSpacing time.Duration) (capturedAt []time.Time, clamped int) {
	if len(ts) == 0 {
		return nil, 0
	}
	if maxSpacing < 0 {
		maxSpacing = 0
	}
	newest := ts[0]
	for _, t := range ts[1:] {
		if t.After(newest) {
			newest = t
		}
	}
	capturedAt = make([]time.Time, len(ts))
	for i, t := range ts {
		spacing := newest.Sub(t)
		switch {
		case spacing < 0:
			spacing = 0
			clamped++
		case spacing > maxSpacing:
			spacing = maxSpacing
			clamped++
		}
		capturedAt[i] = rxTS.Add(-spacing)
	}
	return capturedAt, clamped
}

// PlaceArrival places a record that carried neither a usable source time
// nor a receive time at this system's arrival time (T-12: placed, counted
// by the caller, never dropped). Source is core.TimeSystem.
func PlaceArrival(arrivedAt time.Time) Placement {
	return Placement{TS: arrivedAt, CapturedAt: arrivedAt, Source: core.TimeSystem}
}

// Times builds the core.Times of a placement received at rxTS. The
// source time is left nil when the record carried none: a broadcast
// whose timestamp was unknown or invalid, or a record placed at arrival
// (T-12: such a record is not ordered within its source).
func Times(p Placement, rxTS time.Time, backlog bool) core.Times {
	t := core.Times{RxTS: rxTS, CapturedAt: p.CapturedAt, Source: p.Source, Backlog: backlog}
	switch {
	case p.Fallback == FallbackUnknown || p.Fallback == FallbackInvalid:
	case p.Source == core.TimeSystem:
	default:
		ts := p.TS
		t.TS = &ts
	}
	return t
}
