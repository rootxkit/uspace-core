package timeplace

import (
	"math"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

var rx = time.Date(2026, 10, 1, 12, 34, 56, 700_000_000, time.UTC)

// tenthsAt returns the wire timestamp of an instant.
func tenthsAt(t time.Time) uint16 {
	t = t.UTC()
	return uint16((t.Minute()*60+t.Second())*10 + t.Nanosecond()/100_000_000)
}

func TestAccuracyS(t *testing.T) {
	for code, want := range map[uint8]float64{0: 0, 1: 0.1, 10: 1, 15: 1.5} {
		if got := AccuracyS(code); math.Abs(got-want) > 1e-12 {
			t.Errorf("AccuracyS(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestDefaults(t *testing.T) {
	if p := DefaultBroadcastPolicy(); p.ToleranceS != 1 || p.MaxLatencyS != 5 {
		t.Errorf("DefaultBroadcastPolicy = %+v", p)
	}
	if p := DefaultNetworkPolicy(); p.MaxAgeS != 60 || p.ToleranceS != 1 || p.MaxLatencyS != 5 {
		t.Errorf("DefaultNetworkPolicy = %+v", p)
	}
}

// E-01: each boundary of the broadcast rule from both sides.
func TestPlaceBroadcastBoundaries(t *testing.T) {
	pol := DefaultBroadcastPolicy()
	cases := []struct {
		name   string
		offset time.Duration // broadcast moment relative to rx
		acc    uint8
		fb     Fallback
	}{
		{"ahead-at-tolerance-believed", time.Second, 0, FallbackNone},
		{"ahead-past-tolerance-clock-ahead", 1100 * time.Millisecond, 0, FallbackClockAhead},
		{"ahead-within-tolerance-plus-accuracy", 1500 * time.Millisecond, 5, FallbackNone},
		{"behind-at-latency-believed", -5 * time.Second, 0, FallbackNone},
		{"behind-past-latency-too-old", -5100 * time.Millisecond, 0, FallbackTooOld},
		{"behind-past-latency-within-accuracy", -5100 * time.Millisecond, 1, FallbackNone},
		{"behind-29m59s-too-old", -(29*time.Minute + 59*time.Second), 0, FallbackTooOld},
		{"ahead-29m59s-clock-ahead", 29*time.Minute + 59*time.Second, 0, FallbackClockAhead},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := rx.Add(c.offset)
			p := PlaceBroadcast(tenthsAt(at), c.acc, rx, pol)
			if p.Fallback != c.fb {
				t.Fatalf("fallback %q, want %q", p.Fallback, c.fb)
			}
			if !p.TS.Equal(at) {
				t.Errorf("ts %s, want %s", p.TS, at)
			}
			if c.fb == FallbackNone {
				if p.Source != core.TimeBroadcast || !p.CapturedAt.Equal(at) {
					t.Errorf("placement %+v, want broadcast at %s", p, at)
				}
			} else if p.Source != core.TimeReceiver || !p.CapturedAt.Equal(rx) {
				t.Errorf("placement %+v, want receiver at %s", p, rx)
			}
		})
	}
}

func TestPlaceBroadcastSentinels(t *testing.T) {
	pol := DefaultBroadcastPolicy()
	for _, c := range []struct {
		tenths uint16
		fb     Fallback
	}{
		{TimestampUnknown, FallbackUnknown},
		{36000, FallbackInvalid},
		{36001, FallbackInvalid},
		{65534, FallbackInvalid},
	} {
		p := PlaceBroadcast(c.tenths, 0, rx, pol)
		if p.Fallback != c.fb || p.Source != core.TimeReceiver || !p.TS.Equal(rx) || !p.CapturedAt.Equal(rx) {
			t.Errorf("tenths %d: %+v, want %q at receipt", c.tenths, p, c.fb)
		}
	}
	// The presence pair: the last tenth of the hour is a time.
	p := PlaceBroadcast(35999, 0, rx, pol)
	if p.Fallback == FallbackInvalid || p.Fallback == FallbackUnknown {
		t.Errorf("35999: %+v, want a placed time", p)
	}
}

// A receive time in another zone and with a monotonic reading places the
// same instant: the hour is a UTC hour.
func TestPlaceBroadcastZoneIndependent(t *testing.T) {
	tz := time.FixedZone("UTC+0430", 4*3600+1800)
	local := rx.In(tz)
	a := PlaceBroadcast(20963, 0, rx, DefaultBroadcastPolicy())
	b := PlaceBroadcast(20963, 0, local, DefaultBroadcastPolicy())
	if !a.TS.Equal(b.TS) || !a.CapturedAt.Equal(b.CapturedAt) || a.Fallback != b.Fallback {
		t.Errorf("UTC %+v, zoned %+v", a, b)
	}
}

// Degenerate policies never overflow or panic.
func TestPolicySeconds(t *testing.T) {
	for _, c := range []struct {
		s    float64
		want time.Duration
	}{
		{math.NaN(), 0},
		{-1, 0},
		{0, 0},
		{1.5, 1500 * time.Millisecond},
		{0.0000004, 0},
		{0.0000006, time.Microsecond},
		{math.Inf(1), maxPolicyDuration},
		{1e300, maxPolicyDuration},
	} {
		if got := seconds(c.s); got != c.want {
			t.Errorf("seconds(%v) = %v, want %v", c.s, got, c.want)
		}
	}
	p := PlaceBroadcast(20963, 15, rx, BroadcastPolicy{ToleranceS: math.Inf(1), MaxLatencyS: math.NaN()})
	if p.CapturedAt.After(rx.Add(maxPolicyDuration + 1500*time.Millisecond)) {
		t.Errorf("placement %+v beyond the bound", p)
	}
}

func TestPlaceNetworkWithResponse(t *testing.T) {
	pol := DefaultNetworkPolicy()
	resp := time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)
	recv := time.Date(2026, 10, 1, 12, 0, 10, 0, time.UTC)
	// Exactly MaxAgeS behind is shown (E-01 pair of the drop).
	p, note, shown := PlaceNetwork(resp.Add(-60*time.Second), &resp, recv, pol)
	if !shown || note != NoteNone || p.Source != core.TimeBroadcast || !p.CapturedAt.Equal(recv.Add(-60*time.Second)) {
		t.Errorf("at max age: %+v %q %v", p, note, shown)
	}
	if _, _, shown := PlaceNetwork(resp.Add(-60*time.Second-time.Microsecond), &resp, recv, pol); shown {
		t.Errorf("past max age shown")
	}
	// Equal to the response is not ahead of it.
	p, note, _ = PlaceNetwork(resp, &resp, recv, pol)
	if note != NoteNone || !p.CapturedAt.Equal(recv) || p.Source != core.TimeBroadcast {
		t.Errorf("equal to response: %+v %q", p, note)
	}
	p, note, _ = PlaceNetwork(resp.Add(time.Microsecond), &resp, recv, pol)
	if note != NoteAheadOfResponse || p.Source != core.TimeReceiver || !p.CapturedAt.Equal(recv) {
		t.Errorf("ahead of response: %+v %q", p, note)
	}
}

func TestPlaceNetworkWithoutResponse(t *testing.T) {
	pol := DefaultNetworkPolicy()
	recv := time.Date(2026, 10, 1, 12, 0, 10, 0, time.UTC)
	cases := []struct {
		name   string
		offset time.Duration
		note   NetworkNote
		shown  bool
	}{
		{"ahead-at-tolerance", time.Second, NoteNone, true},
		{"ahead-past-tolerance", time.Second + time.Microsecond, NoteClockAhead, true},
		{"behind-at-latency", -5 * time.Second, NoteNone, true},
		{"behind-past-latency", -5*time.Second - time.Microsecond, NoteTooOld, true},
		{"behind-at-max-age", -60 * time.Second, NoteTooOld, true},
		{"behind-past-max-age", -60*time.Second - time.Microsecond, NoteNone, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := recv.Add(c.offset)
			p, note, shown := PlaceNetwork(state, nil, recv, pol)
			if note != c.note || shown != c.shown {
				t.Fatalf("note %q shown %v, want %q %v", note, shown, c.note, c.shown)
			}
			if !shown {
				return
			}
			if !p.TS.Equal(state) {
				t.Errorf("ts %s, want %s", p.TS, state)
			}
			want := state
			if c.note != NoteNone {
				want = recv
			}
			if !p.CapturedAt.Equal(want) {
				t.Errorf("captured_at %s, want %s", p.CapturedAt, want)
			}
		})
	}
}

func TestPlaceBatch(t *testing.T) {
	recv := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	src := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // a skewed source clock
	ts := []time.Time{src.Add(-8 * time.Second), src, src.Add(-3 * time.Second)}
	got, clamped := PlaceBatch(recv, ts, DefaultMaxBatchSpacing)
	if clamped != 0 {
		t.Errorf("clamped %d, want 0", clamped)
	}
	want := []time.Time{recv.Add(-8 * time.Second), recv, recv.Add(-3 * time.Second)}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("row %d: %s, want %s", i, got[i], want[i])
		}
	}
}

// E-01 and E-10: a spacing at the bound is kept, one past it is clamped
// and counted.
func TestPlaceBatchClamps(t *testing.T) {
	recv := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	src := recv.Add(time.Hour)
	ts := []time.Time{src, src.Add(-120 * time.Second), src.Add(-121 * time.Second), src.Add(-time.Hour)}
	got, clamped := PlaceBatch(recv, ts, DefaultMaxBatchSpacing)
	if clamped != 2 {
		t.Errorf("clamped %d, want 2", clamped)
	}
	want := []time.Time{recv, recv.Add(-120 * time.Second), recv.Add(-120 * time.Second), recv.Add(-120 * time.Second)}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Errorf("row %d: %s, want %s", i, got[i], want[i])
		}
	}
	// A negative bound reads as 0: every row but the newest is clamped.
	got, clamped = PlaceBatch(recv, ts[:2], -time.Second)
	if clamped != 1 || !got[1].Equal(recv) {
		t.Errorf("negative bound: %v clamped %d", got, clamped)
	}
	if got, clamped := PlaceBatch(recv, nil, DefaultMaxBatchSpacing); got != nil || clamped != 0 {
		t.Errorf("empty batch: %v %d", got, clamped)
	}
}

// T-12: a record with no receive time is placed at arrival, by the
// caller passing its arrival time; one with no time at all uses
// PlaceArrival. Neither is dropped.
func TestMissingTimesPlacedAtArrival(t *testing.T) {
	arrived := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	p := PlaceArrival(arrived)
	if p.Source != core.TimeSystem || !p.CapturedAt.Equal(arrived) {
		t.Errorf("PlaceArrival = %+v", p)
	}
	tm := Times(p, arrived, false)
	if tm.TS != nil || tm.Source != core.TimeSystem || !tm.CapturedAt.Equal(arrived) {
		t.Errorf("Times(arrival) = %+v", tm)
	}
	// A broadcast without rx_ts: the arrival time stands in for it.
	b := PlaceBroadcast(tenthsAt(arrived.Add(-time.Second)), 0, arrived, DefaultBroadcastPolicy())
	if b.Source != core.TimeBroadcast || !b.CapturedAt.Equal(arrived.Add(-time.Second)) {
		t.Errorf("broadcast at arrival = %+v", b)
	}
}

func TestTimes(t *testing.T) {
	pol := DefaultBroadcastPolicy()
	ok := PlaceBroadcast(20963, 0, rx, pol)
	tm := Times(ok, rx, true)
	if tm.TS == nil || !tm.TS.Equal(ok.TS) || !tm.RxTS.Equal(rx) || !tm.Backlog || tm.Source != core.TimeBroadcast {
		t.Errorf("Times(broadcast) = %+v", tm)
	}
	if lag := tm.LagS(); math.Abs(lag-0.4) > 1e-9 {
		t.Errorf("lag %v, want 0.4", lag)
	}
	for _, tenths := range []uint16{TimestampUnknown, 36000} {
		tm := Times(PlaceBroadcast(tenths, 0, rx, pol), rx, false)
		if tm.TS != nil {
			t.Errorf("tenths %d: ts %v, want nil (the record carried none)", tenths, *tm.TS)
		}
	}
	tm = Times(PlaceBroadcast(20915, 0, rx, pol), rx, false)
	if tm.TS == nil || tm.Source != core.TimeReceiver {
		t.Errorf("Times(too_old) = %+v, want the claimed ts kept", tm)
	}
}

func FuzzPlaceBroadcast(f *testing.F) {
	for _, s := range []struct {
		tenths uint16
		acc    uint8
	}{{20963, 0}, {35999, 0}, {5, 0}, {20912, 10}, {65535, 0}, {36000, 0}, {2367, 0}, {3567, 15}} {
		f.Add(s.tenths, s.acc, rx.UnixMicro())
	}
	pol := DefaultBroadcastPolicy()
	f.Fuzz(func(t *testing.T, tenths uint16, acc uint8, rxUnixMicro int64) {
		recv := time.UnixMicro(rxUnixMicro)
		p := PlaceBroadcast(tenths, acc, recv, pol)
		bound := recv.Add(seconds(pol.ToleranceS) + accuracy(acc))
		if p.CapturedAt.After(bound) {
			t.Fatalf("captured_at %s after %s", p.CapturedAt, bound)
		}
		switch p.Fallback {
		case FallbackNone:
			if p.Source != core.TimeBroadcast || !p.TS.Equal(p.CapturedAt) {
				t.Fatalf("believed placement %+v", p)
			}
		case FallbackClockAhead, FallbackTooOld, FallbackUnknown, FallbackInvalid:
			if p.Source != core.TimeReceiver || !p.CapturedAt.Equal(recv) {
				t.Fatalf("fallback placement %+v", p)
			}
		default:
			t.Fatalf("unknown fallback %q", p.Fallback)
		}
	})
}

func BenchmarkPlaceBroadcast(b *testing.B) {
	pol := DefaultBroadcastPolicy()
	for b.Loop() {
		_ = PlaceBroadcast(20963, 3, rx, pol)
	}
}

func BenchmarkPlaceNetwork(b *testing.B) {
	pol := DefaultNetworkPolicy()
	resp := rx.Add(5 * time.Minute)
	state := resp.Add(-2 * time.Second)
	for b.Loop() {
		_, _, _ = PlaceNetwork(state, &resp, rx, pol)
	}
}

// The accuracy field is 4 bits: code 15 widens the latency bound by
// 1.5 s, and a code above 15 is unknown and widens nothing.
func TestAccuracyCodeBeyondTheField(t *testing.T) {
	pol := DefaultBroadcastPolicy()
	at := rx.Add(-6 * time.Second)
	if p := PlaceBroadcast(tenthsAt(at), 15, rx, pol); p.Fallback != FallbackNone {
		t.Errorf("code 15, 6 s old: %+v, want believed (5 s + 1.5 s)", p)
	}
	if p := PlaceBroadcast(tenthsAt(at), 0, rx, pol); p.Fallback != FallbackTooOld {
		t.Errorf("code 0, 6 s old: %+v, want too_old", p)
	}
	old := rx.Add(-30 * time.Second)
	for _, code := range []uint8{16, 200, 255} {
		if p := PlaceBroadcast(tenthsAt(old), code, rx, pol); p.Fallback != FallbackTooOld {
			t.Errorf("code %d, 30 s old: %+v, want too_old", code, p)
		}
		if got := AccuracyS(code); got != 0 {
			t.Errorf("AccuracyS(%d) = %v, want 0", code, got)
		}
	}
}
