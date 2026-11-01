package timeplace

import (
	"math"
	"testing"
	"time"
)

// t0 is the start of the anchor tests on our clock.
var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// skew is the source's clock error: its clock reads ours minus 7 s.
const skew = -7 * time.Second

// src is the source's clock reading at our time t.
func src(t time.Time) time.Time { return t.Add(skew) }

// sec is an offset from t0 in seconds.
func sec(s float64) time.Time { return t0.Add(time.Duration(math.Round(s*1000)) * time.Millisecond) }

// heard is a delivery whose rows were heard at our times at (seconds
// from t0), as the source's clock stamps them.
func heard(at ...float64) []time.Time {
	out := make([]time.Time, len(at))
	for i, s := range at {
		out[i] = src(sec(s))
	}
	return out
}

// feed runs one delivery a second from start to end (exclusive), each
// row heard 0.2 s before its read, and returns the anchor.
func feed(t *testing.T, a *Anchor, start, end float64, pol AnchorPolicy) *Anchor {
	t.Helper()
	for s := start; s < end; s++ {
		r := PlaceAnchored(sec(s+0.2), heard(s-0.8, s), false, DefaultMaxBatchSpacing, a, pol)
		for i, b := range r.Backlog {
			if b {
				t.Fatalf("timely delivery at %vs: row %d flagged backlog", s, i)
			}
		}
		a = r.Next
	}
	return a
}

func TestDefaultAnchorPolicy(t *testing.T) {
	if p := DefaultAnchorPolicy(); p.BacklogAfterS != 10 || p.AnchorMaxAgeS != 60 || p.ToleranceS != 1 {
		t.Errorf("DefaultAnchorPolicy = %+v", p)
	}
}

// E-01, the absence half: a timely delivery is placed by the batch rule
// and is not backlog, and it teaches the anchor.
func TestPlaceAnchoredTimelyIsNotBacklog(t *testing.T) {
	pol := DefaultAnchorPolicy()
	r := PlaceAnchored(sec(1.2), heard(0.2, 1), false, DefaultMaxBatchSpacing, nil, pol)
	want := []time.Time{sec(0.4), sec(1.2)}
	for i := range want {
		if !r.CapturedAt[i].Equal(want[i]) || r.Backlog[i] {
			t.Errorf("row %d at %v backlog %v, want %v live", i, r.CapturedAt[i], r.Backlog[i], want[i])
		}
	}
	if r.Next == nil || r.ByAnchor != 0 || r.Note != AnchorNoteNone || r.Clamped != 0 {
		t.Errorf("anchored %+v", r)
	}
	// And later timely deliveries stay live and unmoved beyond the jitter.
	a := feed(t, r.Next, 2, 30, pol)
	r = PlaceAnchored(sec(30.2), heard(29.2, 30), false, DefaultMaxBatchSpacing, a, pol)
	if r.Backlog[0] || r.Backlog[1] || !r.CapturedAt[1].Equal(sec(30.2)) {
		t.Errorf("timely delivery at 30 s: %+v", r)
	}
}

// E-01, the presence half (SC-15): the reader stalls from 30 s to 70 s;
// what the source sent meanwhile is read at 70 s. Each row is placed by
// the source's own time through the anchor, not at the read, and a row
// placed more than the backlog bound behind the read is backlog.
func TestPlaceAnchoredLateDeliveryPlacedBySourceTimeAndBacklog(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 30, pol)
	read := sec(70)
	for s := 30.0; s < 70; s++ {
		r := PlaceAnchored(read, heard(s-0.8, s), false, DefaultMaxBatchSpacing, a, pol)
		for i, at := range []float64{s - 0.8, s} {
			// Placed at its hearing plus the anchor's 0.2 s delay.
			want := sec(at + 0.2)
			if !r.CapturedAt[i].Equal(want) {
				t.Fatalf("delivery of %vs row %d at %v, want %v (not the read %v)", s, i, r.CapturedAt[i], want, read)
			}
			wantBacklog := read.Sub(want) > 10*time.Second
			if r.Backlog[i] != wantBacklog {
				t.Fatalf("delivery of %vs row %d backlog %v, want %v", s, i, r.Backlog[i], wantBacklog)
			}
		}
		if r.ByAnchor != 2 {
			t.Fatalf("delivery of %vs: by_anchor %d, want 2", s, r.ByAnchor)
		}
		a = r.Next
	}
	// The source's next timely delivery after the drain is live again.
	r := PlaceAnchored(sec(70.2), heard(69.2, 70), false, DefaultMaxBatchSpacing, a, pol)
	if r.Backlog[0] || r.Backlog[1] {
		t.Errorf("timely delivery after the drain flagged backlog: %+v", r)
	}
}

// The backlog bound is policy data, inside at the bound.
func TestPlaceAnchoredBacklogBoundary(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 5, pol)
	// Heard at 5 s, placed at 5.2 s; read exactly 10 s later, then 1 ms more.
	if r := PlaceAnchored(sec(15.2), heard(5), false, DefaultMaxBatchSpacing, a, pol); r.Backlog[0] {
		t.Errorf("exactly the bound behind the read: backlog")
	}
	if r := PlaceAnchored(sec(15.201), heard(5), false, DefaultMaxBatchSpacing, a, pol); !r.Backlog[0] {
		t.Errorf("past the bound behind the read: not backlog")
	}
	pol.BacklogAfterS = 20
	if r := PlaceAnchored(sec(15.201), heard(5), false, DefaultMaxBatchSpacing, a, pol); r.Backlog[0] {
		t.Errorf("BacklogAfterS 20: backlog at 10 s")
	}
}

// Without an anchor a delivery read late cannot be told from a timely
// one by the read alone; a row of it older than the bound by its spacing
// is still backlog.
func TestPlaceAnchoredNoAnchorSpacing(t *testing.T) {
	r := PlaceAnchored(sec(20), heard(5, 19), false, DefaultMaxBatchSpacing, nil, DefaultAnchorPolicy())
	if !r.CapturedAt[0].Equal(sec(6)) || !r.Backlog[0] || r.Backlog[1] {
		t.Errorf("no anchor: %+v", r)
	}
}

// A flagged delivery is backlog throughout and never teaches the anchor,
// but the anchor still places it.
func TestPlaceAnchoredFlagged(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 10, pol)
	r := PlaceAnchored(sec(10.2), heard(9.2, 10), true, DefaultMaxBatchSpacing, a, pol)
	if !r.Backlog[0] || !r.Backlog[1] || r.Next != a {
		t.Errorf("flagged timely: %+v", r)
	}
	r = PlaceAnchored(sec(40), heard(20), true, DefaultMaxBatchSpacing, a, pol)
	if !r.CapturedAt[0].Equal(sec(20.2)) || !r.Backlog[0] || r.Next != a {
		t.Errorf("flagged late: %+v", r)
	}
	// Without an anchor a flagged delivery learns none.
	if r := PlaceAnchored(sec(1), heard(1), true, DefaultMaxBatchSpacing, nil, pol); r.Next != nil || !r.Backlog[0] {
		t.Errorf("flagged first delivery: %+v", r)
	}
}

// The anchor carries the smallest delay: a slower delivery does not move
// later rows later.
func TestPlaceAnchoredSmallestDelay(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 5, pol)
	r := PlaceAnchored(sec(8), heard(5), false, DefaultMaxBatchSpacing, a, pol) // 3 s slow
	if !r.CapturedAt[0].Equal(sec(5.2)) || r.ByAnchor != 1 {
		t.Errorf("slow delivery: %+v", r)
	}
	r = PlaceAnchored(sec(9.05), heard(9), false, DefaultMaxBatchSpacing, r.Next, pol) // faster than the anchor
	if !r.CapturedAt[0].Equal(sec(9.05)) || r.ByAnchor != 0 {
		t.Errorf("fast delivery: %+v", r)
	}
	r = PlaceAnchored(sec(12), heard(10), false, DefaultMaxBatchSpacing, r.Next, pol)
	if !r.CapturedAt[0].Equal(sec(10.05)) {
		t.Errorf("after the fast delivery: %+v, want the 0.05 s delay", r)
	}
}

// PlaceAnchored never changes the caller's anchor.
func TestPlaceAnchoredDoesNotMutate(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 5, pol)
	before := *a
	_ = PlaceAnchored(sec(40), heard(20), false, DefaultMaxBatchSpacing, a, pol)
	if *a != before {
		t.Errorf("anchor changed in place: %+v -> %+v", before, *a)
	}
}

// AnchorMaxAgeS after it was learnt, an anchor is relearnt by a timely
// delivery; a delivery it calls late keeps it (anchor_kept), so a stall
// longer than the age is still placed by the source's time.
func TestPlaceAnchoredMaxAgeKeptAndRelearnt(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 2, pol)
	// Stalled from 2 s to 100 s: the first read is 98 s after the anchor
	// was learnt, and the deliveries are late.
	r := PlaceAnchored(sec(100), heard(10), false, DefaultMaxBatchSpacing, a, pol)
	if r.Note != AnchorNoteKept || !r.CapturedAt[0].Equal(sec(10.2)) || !r.Backlog[0] {
		t.Errorf("late delivery past the anchor age: %+v", r)
	}
	// A timely delivery past the age relearns: the source's clock has
	// drifted 0.5 s behind, which the old relation would place 0.5 s
	// early; it is dropped and the delivery placed by the batch rule.
	old := feed(t, nil, 0, 2, pol)
	drift := 500 * time.Millisecond
	ts := []time.Time{src(sec(80)).Add(-drift)}
	r = PlaceAnchored(sec(80.2), ts, false, DefaultMaxBatchSpacing, old, pol)
	if r.Note != AnchorNoteNone || !r.CapturedAt[0].Equal(sec(80.2)) || r.ByAnchor != 0 || r.Backlog[0] {
		t.Errorf("timely delivery past the anchor age: %+v", r)
	}
	if r.Next == nil || !r.Next.learnt.Equal(sec(80.2)) {
		t.Errorf("anchor not relearnt: %+v", r.Next)
	}
}

// A source clock that steps back makes the anchor call every delivery
// late; one read offset kept for AnchorMaxAgeS of reads is a clock
// change and replaces the anchor (anchor_relearnt). A stall, many
// deliveries at one read time, never does.
func TestPlaceAnchoredClockStepRelearnt(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 10, pol)
	step := 30 * time.Second
	var r Anchored
	relearnt := -1.0
	for s := 10.0; s < 100; s++ {
		r = PlaceAnchored(sec(s+0.2), []time.Time{src(sec(s)).Add(-step)}, false, DefaultMaxBatchSpacing, a, pol)
		a = r.Next
		if r.Note == AnchorNoteRelearnt {
			relearnt = s
			break
		}
		if !r.Backlog[0] {
			t.Fatalf("stepped clock at %vs before relearning: not backlog", s)
		}
	}
	if relearnt < 0 || relearnt-10 < 60 {
		t.Fatalf("relearnt at %vs, want after 60 s of reads", relearnt)
	}
	if r.Backlog[0] || !r.CapturedAt[0].Equal(sec(relearnt+0.2)) {
		t.Errorf("relearning delivery: %+v", r)
	}

	// A stall: 90 deliveries read at one instant never relearn.
	a = feed(t, nil, 0, 10, pol)
	for s := 10.0; s < 100; s++ {
		r = PlaceAnchored(sec(100), heard(s), false, DefaultMaxBatchSpacing, a, pol)
		if r.Note == AnchorNoteRelearnt {
			t.Fatalf("a stall relearnt the anchor at %vs", s)
		}
		a = r.Next
	}
}

func TestPlaceAnchoredEmptyAndClamped(t *testing.T) {
	pol := DefaultAnchorPolicy()
	a := feed(t, nil, 0, 2, pol)
	if r := PlaceAnchored(sec(3), nil, false, DefaultMaxBatchSpacing, a, pol); r.Next != a || r.CapturedAt != nil || r.Backlog != nil {
		t.Errorf("empty delivery: %+v", r)
	}
	r := PlaceAnchored(sec(200.2), heard(10, 200), false, 120*time.Second, nil, pol)
	if r.Clamped != 1 || !r.CapturedAt[0].Equal(sec(80.2)) || !r.Backlog[0] {
		t.Errorf("clamped delivery: %+v", r)
	}
}

func BenchmarkPlaceAnchored(b *testing.B) {
	pol := DefaultAnchorPolicy()
	r := PlaceAnchored(sec(0.2), heard(0), false, DefaultMaxBatchSpacing, nil, pol)
	ts := heard(9, 9.5, 10)
	for b.Loop() {
		_ = PlaceAnchored(sec(10.2), ts, false, DefaultMaxBatchSpacing, r.Next, pol)
	}
}
