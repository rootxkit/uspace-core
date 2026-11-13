package rid

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/odid"
)

const testAddress = "AA:BB:CC:00:00:01"

// latOf is the latitude that tags Location i of a stream, so that a test
// can say which Location an observation publishes.
func latOf(i int) float64 { return 41 + float64(i)/1000 }

// tagOf is the index of the Location an observation publishes.
func tagOf(o *Observation) int { return int(math.Round((*o.Location.LatDeg - 41) * 1000)) }

// wantUnidentified checks everything an observation published without an
// identity carries, the same fields a hold that timed out sets, and that
// it is Location i of the stream, placed at its own receive time.
func wantUnidentified(t *testing.T, o *Observation, i int, nowS float64) {
	t.Helper()
	if o == nil {
		t.Fatalf("Location %d: no observation", i)
	}
	if o.Identified || o.UAID != "" || o.IDType != odid.IDTypeNone || o.IdentityReceiver != "" {
		t.Errorf("Location %d: %+v, want unidentified", i, o)
	}
	if o.DroneID != UnidentifiedID(testAddress) || o.Label != testAddress {
		t.Errorf("Location %d: DroneID %q, Label %q, want the unidentified id of the address", i, o.DroneID, o.Label)
	}
	if o.Receiver != "rx-1" || o.Transmitter != testAddress {
		t.Errorf("Location %d: receiver %q, transmitter %q", i, o.Receiver, o.Transmitter)
	}
	if o.Location.LatDeg == nil || *o.Location.LatDeg != latOf(i) {
		t.Errorf("Location %d: published latitude %v, want %v", i, o.Location.LatDeg, latOf(i))
	}
	if !o.RxTS.Equal(rxAt(nowS)) {
		t.Errorf("Location %d: RxTS %v, want its own frame's %v", i, o.RxTS, rxAt(nowS))
	}
}

// (1) A transmitter heard every 3.5 s that never sends a Basic ID: with
// PublishHeld every Location is published, unidentified, at its own
// receive time, by the call that forgets the state that held it. The
// MaxGapS of 3 s still forgets the address each time.
func TestPublishHeldPublishesARarelyHeardTransmitter(t *testing.T) {
	const n, spacingS = 20, 3.5
	tr := NewTracker(PublishHeldSettings())
	if got := tr.Settings().MaxGapS; got != 3 {
		t.Fatalf("MaxGapS %v, want the 3 s of the defaults", got)
	}
	for i := range n {
		got := tr.TakeAll(frame(float64(i)*spacingS, loc(latOf(i))))
		if i == 0 {
			if len(got) != 0 {
				t.Fatalf("first frame published %d observations, want it held", len(got))
			}
			continue
		}
		// The frame that ends the silence hands back the Location the
		// forgotten state held: the previous one, not its own.
		if len(got) != 1 {
			t.Fatalf("frame %d: %d observations, want the one Location flushed; counters %v", i, len(got), tr.Counters().Snapshot())
		}
		wantUnidentified(t, got[0], i-1, float64(i-1)*spacingS)
	}
	lastS := float64(n-1) * spacingS
	// Within MaxGapS of the last frame nothing is forgotten, so nothing
	// is flushed: the last Location is still held for an identity.
	if got := tr.Sweep(lastS + 3); len(got) != 0 {
		t.Fatalf("Sweep at the gap: %d observations, want 0", len(got))
	}
	got := tr.Sweep(lastS + 3.5)
	if len(got) != 1 {
		t.Fatalf("Sweep after the gap: %d observations, want the last Location", len(got))
	}
	wantUnidentified(t, got[0], n-1, lastS)
	if got := tr.Sweep(lastS + 100); len(got) != 0 {
		t.Fatalf("second Sweep: %d observations, want 0 (published once)", len(got))
	}
	c := tr.Counters()
	if c.Get(CounterUnidentified) != n || c.Get(CounterHeldFlushed) != n || c.Get(CounterSilences) != n || c.Get(CounterHeldForgotten) != 0 {
		t.Errorf("counters %v, want %d unidentified, %d flushed, %d silences, nothing forgotten", c.Snapshot(), n, n, n)
	}
	if tr.Transmitters() != 0 {
		t.Errorf("%d addresses still held", tr.Transmitters())
	}
}

// (1) again with a Basic ID that is never in time: one message per
// frame, 3.5 s apart, so the Basic ID is forgotten before the Location
// arrives. The Locations are published unidentified and the serial is
// never carried across the silence.
func TestPublishHeldNeverCarriesALateBasicID(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	var got []*Observation
	for i := range 20 {
		var m odid.Message = serial("SN-1")
		if i%2 == 1 {
			m = loc(latOf(i))
		}
		got = append(got, tr.TakeAll(frame(float64(i)*3.5, m))...)
	}
	got = append(got, tr.Sweep(1000)...)
	if len(got) != 10 {
		t.Fatalf("%d observations from 10 Locations, want 10; counters %v", len(got), tr.Counters().Snapshot())
	}
	for k, o := range got {
		i := 2*k + 1
		wantUnidentified(t, o, i, float64(i)*3.5)
	}
}

// (2) The same spacing with a Basic ID in time: identified exactly as
// without PublishHeld, and nothing is flushed.
func TestPublishHeldLeavesAnIdentifiedTransmitterAlone(t *testing.T) {
	for _, s := range []Settings{DefaultSettings(), PublishHeldSettings()} {
		// Basic ID and Location in one frame.
		tr := NewTracker(s)
		for i := range 20 {
			got := tr.TakeAll(frame(float64(i)*3.5, serial("SN-1"), loc(latOf(i))))
			if len(got) != 1 || !got[0].Identified || got[0].UAID != "SN-1" || !got[0].RxTS.Equal(rxAt(float64(i)*3.5)) {
				t.Fatalf("PublishHeld %v, frame %d: %+v, want one observation of SN-1", s.PublishHeld, i, got)
			}
		}
		if got := tr.Sweep(1000); len(got) != 0 {
			t.Errorf("PublishHeld %v: Sweep flushed %d published Locations", s.PublishHeld, len(got))
		}
		// Every 5 s a Location and, one second later inside MaxGapS, the
		// Basic ID (forgotten again before the next Location): it
		// completes the held Location, placed at the Location's time.
		tr = NewTracker(s)
		for i := range 20 {
			atS := float64(i) * 5
			if got := tr.TakeAll(frame(atS, loc(latOf(i)))); len(got) != 0 {
				t.Fatalf("PublishHeld %v, Location %d: %+v, want held", s.PublishHeld, i, got)
			}
			got := tr.TakeAll(frame(atS+1, serial("SN-1")))
			if len(got) != 1 || !got[0].Identified || got[0].UAID != "SN-1" || !got[0].RxTS.Equal(rxAt(atS)) {
				t.Fatalf("PublishHeld %v, Basic ID %d: %+v, want the held Location under SN-1 at its own time", s.PublishHeld, i, got)
			}
		}
		if got := tr.Sweep(1000); len(got) != 0 {
			t.Errorf("PublishHeld %v: Sweep flushed %d completed Locations", s.PublishHeld, len(got))
		}
		c := tr.Counters()
		if c.Get(CounterUnidentified) != 0 || c.Get(CounterHeldFlushed) != 0 || c.Get(CounterHeldForgotten) != 0 {
			t.Errorf("PublishHeld %v: counters %v, want nothing unidentified, flushed or forgotten", s.PublishHeld, c.Snapshot())
		}
	}
}

// restart feeds SC-10's shape: SN-OLD at 0, 1 and 2 s, four seconds of
// silence, then another transmitter on the same address whose first
// Location (6 s) comes before its Basic ID (6.5 s), then two more
// Locations. It returns what was published for the new transmitter's
// three Locations (tags 10, 11, 12), keyed by tag.
func restart(t *testing.T, tr *Tracker) map[int][]*Observation {
	t.Helper()
	for i := range 3 {
		got := tr.TakeAll(frame(float64(i), serial("SN-OLD"), loc(latOf(i))))
		if len(got) != 1 || got[0].UAID != "SN-OLD" {
			t.Fatalf("old transmitter, frame %d: %+v", i, got)
		}
	}
	var got []*Observation
	got = append(got, tr.TakeAll(frame(6, loc(latOf(10))))...)
	got = append(got, tr.TakeAll(frame(6.5, serial("SN-NEW")))...)
	got = append(got, tr.TakeAll(frame(7, loc(latOf(11))))...)
	got = append(got, tr.TakeAll(frame(8, loc(latOf(12))))...)
	got = append(got, tr.Sweep(1000)...)
	byTag := make(map[int][]*Observation)
	for _, o := range got {
		byTag[tagOf(o)] = append(byTag[tagOf(o)], o)
	}
	return byTag
}

// (3) The restart (I-07, the lab's SC-10): with PublishHeld the gap
// stays at 3 s, so no Location of the new transmitter is published
// under the old serial, and none is lost.
func TestPublishHeldRestartNeverUsesTheOldSerial(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	byTag := restart(t, tr)
	for _, tag := range []int{10, 11, 12} {
		obs := byTag[tag]
		if len(obs) != 1 {
			t.Fatalf("Location %d published %d times, want once; counters %v", tag, len(obs), tr.Counters().Snapshot())
		}
		if obs[0].UAID != "SN-NEW" || !obs[0].Identified {
			t.Errorf("Location %d published as %q (identified %v), want SN-NEW", tag, obs[0].UAID, obs[0].Identified)
		}
	}
	if !byTag[10][0].RxTS.Equal(rxAt(6)) {
		t.Errorf("the Location completed at 6.5 s has RxTS %v, want its own %v", byTag[10][0].RxTS, rxAt(6))
	}
	c := tr.Counters()
	if c.Get(CounterSilences) != 2 || c.Get(CounterHeldReplaced) != 0 || c.Get(CounterHeldFlushed) != 0 {
		t.Errorf("counters %v, want 2 silences, nothing replaced or flushed", c.Snapshot())
	}
}

// The restart again when the new transmitter's Basic ID is never heard
// and it is heard every 3.5 s: every Location unidentified, none under
// the old serial, none lost.
func TestPublishHeldRestartWithoutABasicID(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	if got := tr.TakeAll(frame(0, serial("SN-OLD"), loc(latOf(0)))); len(got) != 1 || got[0].UAID != "SN-OLD" {
		t.Fatalf("old transmitter: %+v", got)
	}
	var got []*Observation
	for i := 1; i <= 5; i++ {
		got = append(got, tr.TakeAll(frame(0.5+float64(i)*3.5, loc(latOf(i))))...)
	}
	got = append(got, tr.Sweep(1000)...)
	if len(got) != 5 {
		t.Fatalf("%d observations of the new transmitter's 5 Locations; counters %v", len(got), tr.Counters().Snapshot())
	}
	for k, o := range got {
		wantUnidentified(t, o, k+1, 0.5+float64(k+1)*3.5)
	}
}

// Why RecommendedSettings is deprecated, pinned as it shipped in 1.15.0:
// its 9 s gap does not forget the address across the 4 s restart, so
// the new transmitter's first Location is published under the old
// serial (I-07 widened from 3 s to 9 s).
func TestRecommendedSettingsCarryTheOldSerialAcrossTheRestart(t *testing.T) {
	tr := NewTracker(RecommendedSettings())
	byTag := restart(t, tr)
	if obs := byTag[10]; len(obs) != 1 || obs[0].UAID != "SN-OLD" {
		t.Fatalf("Location 10 under RecommendedSettings: %+v, pinned as published under SN-OLD", obs)
	}
}

// (4) Without PublishHeld nothing changes: TakeAll is Take as a slice,
// Sweep is Forget, and a held Location forgotten is dropped and counted
// in held_forgotten.
func TestWithoutPublishHeldNothingIsFlushed(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	for i := range 20 {
		if got := tr.TakeAll(frame(float64(i)*3.5, loc(latOf(i)))); len(got) != 0 {
			t.Fatalf("frame %d: %d observations under the defaults, want 0", i, len(got))
		}
	}
	if got := tr.Sweep(1000); len(got) != 0 {
		t.Fatalf("Sweep: %d observations under the defaults, want 0", len(got))
	}
	c := tr.Counters()
	if c.Get(CounterHeldForgotten) != 20 || c.Get(CounterSilences) != 20 || c.Get(CounterUnidentified) != 0 || c.Get(CounterHeldFlushed) != 0 {
		t.Errorf("counters %v, want 20 forgotten, 20 silences, nothing published", c.Snapshot())
	}
}

// Take and Forget cannot hand back a second observation, so with
// PublishHeld set they still drop, and still count it: a caller that
// set the flag and kept the old calls sees held_forgotten, not silence.
func TestPublishHeldThroughTakeAndForgetStillCounts(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	for i := range 20 {
		if o := tr.Take(frame(float64(i)*3.5, loc(latOf(i)))); o != nil {
			t.Fatalf("frame %d: Take returned %+v", i, o)
		}
	}
	if n := tr.Forget(1000); n != 1 {
		t.Fatalf("Forget dropped %d states, want 1", n)
	}
	c := tr.Counters()
	if c.Get(CounterHeldForgotten) != 20 || c.Get(CounterHeldFlushed) != 0 || c.Get(CounterUnidentified) != 0 {
		t.Errorf("counters %v, want 20 forgotten and nothing flushed", c.Snapshot())
	}
}

// stream is a reproducible mix of frames over a few receivers and
// addresses: Basic IDs of two ID types and several UAS IDs, Locations
// tagged by index, Systems and Operator IDs, at steps from 0 to 5 s.
func stream(r *rand.Rand, n int) []Frame {
	frames := make([]Frame, 0, n)
	nowS := 0.0
	for i := range n {
		nowS += []float64{0, 0.2, 1, 1, 2.9, 3.5, 5}[r.Intn(7)]
		f := Frame{
			Receiver:    "rx-" + strconv.Itoa(r.Intn(2)),
			Transmitter: "T" + strconv.Itoa(r.Intn(4)),
			NowS:        nowS,
			RxTS:        rxAt(nowS),
		}
		if r.Intn(4) == 0 {
			idType := odid.IDTypeSerial
			if r.Intn(3) == 0 {
				idType = odid.IDTypeCAARegistration
			}
			f.Messages = append(f.Messages, odid.BasicID{IDType: idType, UAID: "ID-" + strconv.Itoa(r.Intn(3))})
		}
		if r.Intn(3) != 0 {
			f.Messages = append(f.Messages, loc(latOf(i)))
		}
		if r.Intn(8) == 0 {
			f.Messages = append(f.Messages, odid.System{}, odid.OperatorID{OperatorID: "OP"})
		}
		frames = append(frames, f)
	}
	return frames
}

// streamSettings are small enough that TTL expiry, silences, the hold
// and eviction all happen in a short stream.
func streamSettings(publishHeld bool) Settings {
	return Settings{IdentityTTLS: 6, MaxGapS: 3, IdentifyWithinS: 4, MaxTransmitters: 3, PublishHeld: publishHeld}
}

// (4) Byte for byte: over random streams, Take on a tracker without
// PublishHeld, TakeAll on one without it, and Take on one with it give
// the same observations and the same counters.
func TestWithoutPublishHeldTakeAllIsTake(t *testing.T) {
	for seed := range int64(200) {
		frames := stream(rand.New(rand.NewSource(seed)), 80)
		plain := NewTracker(streamSettings(false))
		viaAll := NewTracker(streamSettings(false))
		flagOldCalls := NewTracker(streamSettings(true))
		for i, f := range frames {
			want := plain.Take(f)
			got := viaAll.TakeAll(f)
			if want == nil && len(got) != 0 || want != nil && (len(got) != 1 || !reflect.DeepEqual(got[0], want)) {
				t.Fatalf("seed %d frame %d: TakeAll %+v, Take %+v", seed, i, got, want)
			}
			if old := flagOldCalls.Take(f); !reflect.DeepEqual(old, want) {
				t.Fatalf("seed %d frame %d: Take with PublishHeld %+v, without %+v", seed, i, old, want)
			}
		}
		n := plain.Forget(1e6)
		if got := viaAll.Sweep(1e6); len(got) != 0 {
			t.Fatalf("seed %d: Sweep without PublishHeld returned %d observations", seed, len(got))
		}
		if m := flagOldCalls.Forget(1e6); m != n {
			t.Fatalf("seed %d: Forget dropped %d with PublishHeld, %d without", seed, m, n)
		}
		want := plain.Counters().Snapshot()
		if got := viaAll.Counters().Snapshot(); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: counters via TakeAll %v, via Take %v", seed, got, want)
		}
		if got := flagOldCalls.Counters().Snapshot(); !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: counters with PublishHeld through Take %v, without %v", seed, got, want)
		}
	}
}

// (5) A healthy transmitter, Basic ID and Location every second: one
// observation per frame, no duplicate, nothing flushed.
func TestPublishHeldHealthyTransmitterIsNotDuplicated(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	for i := range 30 {
		got := tr.TakeAll(frame(float64(i), serial("SN-1"), loc(latOf(i))))
		if len(got) != 1 || got[0].UAID != "SN-1" || *got[0].Location.LatDeg != latOf(i) {
			t.Fatalf("frame %d: %+v, want its own Location once", i, got)
		}
		if got := tr.Sweep(float64(i) + 0.5); len(got) != 0 {
			t.Fatalf("tick after frame %d flushed %d observations", i, len(got))
		}
	}
	if got := tr.Sweep(1000); len(got) != 0 {
		t.Fatalf("final Sweep flushed %d published Locations", len(got))
	}
	c := tr.Counters()
	if c.Get(CounterHeldFlushed) != 0 || c.Get(CounterUnidentified) != 0 || c.Get(CounterSilences) != 1 {
		t.Errorf("counters %v, want nothing flushed, one silence at the end", c.Snapshot())
	}
}

// (5) A transmitter heard every second without a Basic ID is published
// by the hold, as without the flag: the Locations replaced while held
// stay counted in held_replaced and are not flushed, because a newer
// Location of the same aircraft takes their place.
func TestPublishHeldDoesNotFlushAReplacedLocation(t *testing.T) {
	tr := NewTracker(PublishHeldSettings())
	var got []*Observation
	for i := range 20 {
		got = append(got, tr.TakeAll(frame(float64(i), loc(latOf(i))))...)
	}
	got = append(got, tr.Sweep(1000)...)
	if len(got) != 16 {
		t.Fatalf("%d observations from 20 Locations 1 s apart, want 16", len(got))
	}
	for k, o := range got {
		wantUnidentified(t, o, k+4, float64(k+4))
	}
	c := tr.Counters()
	if c.Get(CounterHeldReplaced) != 4 || c.Get(CounterHeldFlushed) != 0 || c.Get(CounterUnidentified) != 16 {
		t.Errorf("counters %v, want 4 replaced, nothing flushed, 16 unidentified", c.Snapshot())
	}
}

// (6) Eviction: a held Location of the address evicted at the bound is
// published unidentified by the frame that evicts it, before that
// frame's own observation; without the flag it is dropped and counted in
// held_evicted. A published Location evicted is neither.
func TestPublishHeldEviction(t *testing.T) {
	at := func(nowS float64, transmitter string, msgs ...odid.Message) Frame {
		f := frame(nowS, msgs...)
		f.Transmitter = transmitter
		return f
	}
	s := PublishHeldSettings()
	s.MaxTransmitters = 2
	tr := NewTracker(s)
	if got := tr.TakeAll(at(0, testAddress, loc(latOf(0)))); len(got) != 0 {
		t.Fatalf("held Location published: %+v", got)
	}
	if got := tr.TakeAll(at(0.1, "T1", serial("SN-1"), loc(latOf(1)))); len(got) != 1 {
		t.Fatalf("second address: %+v", got)
	}
	got := tr.TakeAll(at(0.2, "T2", serial("SN-2"), loc(latOf(2))))
	if len(got) != 2 {
		t.Fatalf("the evicting frame returned %d observations, want the evicted Location and its own; counters %v", len(got), tr.Counters().Snapshot())
	}
	wantUnidentified(t, got[0], 0, 0)
	if got[1].UAID != "SN-2" {
		t.Errorf("the frame's own observation is %+v, want SN-2 last", got[1])
	}
	// T1's Location was published: evicting it flushes nothing.
	if got := tr.TakeAll(at(0.3, "T3", serial("SN-3"), loc(latOf(3)))); len(got) != 1 || got[0].UAID != "SN-3" {
		t.Fatalf("evicting a published address: %+v, want only SN-3", got)
	}
	c := tr.Counters()
	if c.Get(CounterEvicted) != 2 || c.Get(CounterHeldFlushed) != 1 || c.Get(CounterHeldEvicted) != 0 || c.Get(CounterUnidentified) != 1 {
		t.Errorf("counters %v, want 2 evicted, 1 flushed", c.Snapshot())
	}

	// Without the flag: dropped, and counted.
	s.PublishHeld = false
	tr = NewTracker(s)
	tr.TakeAll(at(0, testAddress, loc(latOf(0))))
	tr.TakeAll(at(0.1, "T1", serial("SN-1"), loc(latOf(1))))
	if got := tr.TakeAll(at(0.2, "T2", serial("SN-2"), loc(latOf(2)))); len(got) != 1 || got[0].UAID != "SN-2" {
		t.Fatalf("without PublishHeld the evicting frame returned %+v", got)
	}
	tr.TakeAll(at(0.3, "T3", serial("SN-3"), loc(latOf(3))))
	c = tr.Counters()
	if c.Get(CounterEvicted) != 2 || c.Get(CounterHeldEvicted) != 1 || c.Get(CounterHeldFlushed) != 0 {
		t.Errorf("counters %v, want 2 evicted, 1 held Location evicted", c.Snapshot())
	}
}

// A held Location of an address whose identity changes (another serial
// after the first expired) is not the new aircraft's by any evidence:
// with the flag it is published unidentified before the frame's own
// observation, without it dropped and counted in held_identity_changed.
func TestPublishHeldIdentityChange(t *testing.T) {
	s := Settings{IdentityTTLS: 1, PublishHeld: true}
	tr := NewTracker(s)
	tr.TakeAll(frame(0, serial("SN-A")))
	if got := tr.TakeAll(frame(2, loc(latOf(0)))); len(got) != 0 {
		t.Fatalf("Location after the identity expired: %+v, want held", got)
	}
	got := tr.TakeAll(frame(2.5, serial("SN-B"), loc(latOf(1))))
	if len(got) != 2 {
		t.Fatalf("%d observations at the identity change, want the held Location and the new one; counters %v", len(got), tr.Counters().Snapshot())
	}
	wantUnidentified(t, got[0], 0, 2)
	if got[1].UAID != "SN-B" || *got[1].Location.LatDeg != latOf(1) {
		t.Errorf("the frame's own observation is %+v, want SN-B last", got[1])
	}
	c := tr.Counters()
	if c.Get(CounterIdentityChanges) != 1 || c.Get(CounterHeldFlushed) != 1 || c.Get(CounterHeldIdentityChanged) != 0 {
		t.Errorf("counters %v, want 1 identity change, 1 flushed", c.Snapshot())
	}

	s.PublishHeld = false
	tr = NewTracker(s)
	tr.Take(frame(0, serial("SN-A")))
	tr.Take(frame(2, loc(latOf(0))))
	if o := tr.Take(frame(2.5, serial("SN-B"), loc(latOf(1)))); o == nil || o.UAID != "SN-B" {
		t.Fatalf("without PublishHeld: %+v, want SN-B", o)
	}
	c = tr.Counters()
	if c.Get(CounterHeldIdentityChanged) != 1 || c.Get(CounterHeldFlushed) != 0 {
		t.Errorf("counters %v, want 1 held Location dropped by the identity change", c.Snapshot())
	}
	// The absence pair: an identity change with nothing held counts
	// neither.
	tr = NewTracker(s)
	tr.Take(frame(0, serial("SN-A"), loc(latOf(0))))
	tr.Take(frame(1, serial("SN-B"), loc(latOf(1))))
	if n := tr.Counters().Get(CounterHeldIdentityChanged); n != 0 {
		t.Errorf("held_identity_changed %d with nothing held, want 0", n)
	}
}

// accounted checks the invariant of (7) on one tracker: every Location
// given was published exactly once, or is counted in exactly one of the
// named counters, or is still held. given is the number of Locations
// taken and published the tags of the observations returned.
func accounted(t *testing.T, tr *Tracker, given int, published []int) {
	t.Helper()
	seen := make(map[int]bool, len(published))
	for _, tag := range published {
		if seen[tag] {
			t.Fatalf("Location %d published twice", tag)
		}
		seen[tag] = true
	}
	held := 0
	for _, st := range tr.states {
		if st.location != nil && !st.published {
			held++
		}
	}
	c := tr.Counters()
	lost := c.Get(CounterHeldForgotten) + c.Get(CounterHeldEvicted) + c.Get(CounterHeldIdentityChanged)
	dropped := c.Get(CounterHeldReplaced) + lost
	if got := uint64(len(published)) + dropped + uint64(held); got != uint64(given) {
		t.Fatalf("%d Locations given, %d published + %d counted as dropped + %d held = %d; counters %v",
			given, len(published), dropped, held, got, c.Snapshot())
	}
	if tr.s.PublishHeld && lost != 0 {
		t.Fatalf("PublishHeld through TakeAll and Sweep dropped %d held Locations; counters %v", lost, c.Snapshot())
	}
}

// (7) The property, over random streams with silences, TTL expiry,
// identity changes, two receivers and eviction, in both modes: every
// Location given is published exactly once or counted, never neither,
// never twice. With PublishHeld the only way not to be published is to
// be replaced by a newer Location of the same state, and a state's
// Locations are published in the order they were given.
func TestEveryLocationIsPublishedOnceOrCounted(t *testing.T) {
	for _, publishHeld := range []bool{false, true} {
		flushed := uint64(0)
		for seed := range int64(300) {
			r := rand.New(rand.NewSource(seed))
			frames := stream(r, 120)
			tr := NewTracker(streamSettings(publishHeld))
			given := 0
			var published []int
			last := make(map[stateKey]int)
			take := func(obs []*Observation) {
				for _, o := range obs {
					tag := tagOf(o)
					key := stateKey{o.Receiver, o.Transmitter}
					if prev, ok := last[key]; ok && prev >= tag {
						t.Fatalf("PublishHeld %v seed %d: Location %d of %v delivered after %d", publishHeld, seed, tag, key, prev)
					}
					last[key] = tag
					if !o.RxTS.Equal(frames[tag].RxTS) {
						t.Fatalf("PublishHeld %v seed %d: Location %d has RxTS %v, want its frame's %v", publishHeld, seed, tag, o.RxTS, frames[tag].RxTS)
					}
					if o.Receiver != frames[tag].Receiver || o.Transmitter != frames[tag].Transmitter {
						t.Fatalf("PublishHeld %v seed %d: Location %d published for %s/%s", publishHeld, seed, tag, o.Receiver, o.Transmitter)
					}
					published = append(published, tag)
				}
			}
			for _, f := range frames {
				for _, m := range f.Messages {
					if _, ok := m.(odid.Location); ok {
						given++
					}
				}
				take(tr.TakeAll(f))
				if r.Intn(5) == 0 {
					take(tr.Sweep(f.NowS + float64(r.Intn(4))))
				}
				accounted(t, tr, given, published)
			}
			take(tr.Sweep(1e6))
			accounted(t, tr, given, published)
			if len(tr.states) != 0 {
				t.Fatalf("%d states after the final Sweep", len(tr.states))
			}
			flushed += tr.Counters().Get(CounterHeldFlushed)
		}
		// The streams must exercise the flush, or the property proves
		// nothing about it (E-01).
		if publishHeld && flushed == 0 {
			t.Fatal("no stream flushed a held Location")
		}
		if !publishHeld && flushed != 0 {
			t.Fatalf("%d Locations flushed without PublishHeld", flushed)
		}
	}
}

// The flag makes a gap below the hold sound, so Validate passes it;
// without the flag the same values are refused as before.
func TestValidateWithPublishHeld(t *testing.T) {
	p := PublishHeldSettings()
	want := DefaultSettings()
	want.PublishHeld = true
	if p != want {
		t.Fatalf("PublishHeldSettings() = %+v, want the defaults with PublishHeld", p)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("PublishHeldSettings().Validate() = %v", err)
	}
	if err := (Settings{MaxGapS: 0.5, IdentifyWithinS: 4, PublishHeld: true}).Validate(); err != nil {
		t.Errorf("a gap below the hold with PublishHeld: %v, want nil", err)
	}
	var fe *core.FieldError
	if err := (Settings{MaxGapS: 0.5, IdentifyWithinS: 4}).Validate(); !errors.As(err, &fe) || fe.Field != "MaxGapS" {
		t.Errorf("the same gap without PublishHeld: %v, want a FieldError on MaxGapS", err)
	}
	if got := NewTracker(p).Settings(); got != p {
		t.Errorf("NewTracker(PublishHeldSettings()).Settings() = %+v", got)
	}
}

// The rarely heard transmitter through TakeAll: every frame forgets a
// state and flushes its Location.
func BenchmarkTrackerTakeAllFlush(b *testing.B) {
	tr := NewTracker(PublishHeldSettings())
	f := frame(0, loc(41.7151))
	i := 0
	for b.Loop() {
		f.NowS = float64(i) * 3.5
		i++
		if got := tr.TakeAll(f); i > 1 && len(got) != 1 {
			b.Fatalf("%d observations, want the flushed one", len(got))
		}
	}
}
