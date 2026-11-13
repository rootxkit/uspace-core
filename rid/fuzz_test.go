package rid

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/odid"
)

// FuzzTrackerTake drives a small tracker with arbitrary clocks (NaN and
// infinities included), strings, ID types and nil messages. Property: no
// panic, the table never exceeds its bound, and the internal indexes
// agree.
func FuzzTrackerTake(f *testing.F) {
	f.Add(0.0, 0.5, "rx-1", "AA:BB:CC:00:00:01", uint8(1), "SN-1", uint8(0xff), 41.7)
	f.Add(math.NaN(), 1.0, "", "", uint8(0), "", uint8(0x0f), 0.0)
	f.Add(math.Inf(1), math.Inf(-1), "rx\x00", "\xff\xfe", uint8(9), "\x00", uint8(0xaa), math.NaN())
	f.Add(-5.0, 3.5, "rx-a", "T", uint8(2), "GEO-OP-77", uint8(0x55), 1e308)
	f.Fuzz(func(t *testing.T, nowS, stepS float64, receiver, transmitter string, idType uint8, uaID string, kinds uint8, latDeg float64) {
		const bound = 3
		tr := NewTracker(Settings{MaxTransmitters: bound})
		var nilBasic *odid.BasicID
		var nilLoc *odid.Location
		var nilSys *odid.System
		var nilOp *odid.OperatorID
		for i := range 6 {
			var msgs []odid.Message
			bit := func(k uint) bool { return kinds>>((k+uint(i))%8)&1 == 1 }
			if bit(0) {
				msgs = append(msgs, odid.BasicID{IDType: odid.IDType(idType), UAID: uaID})
			}
			if bit(1) {
				msgs = append(msgs, odid.Location{LatDeg: &latDeg})
			}
			if bit(2) {
				msgs = append(msgs, nilBasic, nilLoc, nilSys, nilOp, nil)
			}
			if bit(3) {
				msgs = append(msgs, &odid.OperatorID{OperatorID: uaID}, &odid.System{})
			}
			fr := Frame{
				Receiver:    receiver + string(rune('a'+i%2)),
				Transmitter: transmitter + string(rune('0'+i)),
				Messages:    msgs,
				NowS:        nowS + float64(i)*stepS,
			}
			if o := tr.Take(fr); o != nil && o.Identified && o.UAID == "" {
				t.Fatalf("identified without a UAS ID: %+v", o)
			}
			if n := tr.Transmitters(); n > bound {
				t.Fatalf("%d addresses held, bound %d", n, bound)
			}
			if len(tr.states) != tr.recency.Len() {
				t.Fatalf("%d states, %d in recency", len(tr.states), tr.recency.Len())
			}
		}
		tr.Forget(nowS + 100*stepS)
		if len(tr.states) != tr.recency.Len() {
			t.Fatalf("after Forget: %d states, %d in recency", len(tr.states), tr.recency.Len())
		}
	})
}

// FuzzTrackerTakeAll drives a small tracker through TakeAll and Sweep,
// with and without PublishHeld, over arbitrary clocks, strings and ID
// types. Property: no panic, the table stays within its bound, and
// every Location taken is accounted for after every call: published
// (once: the sum is exact), counted in held_replaced, held_forgotten,
// held_evicted or held_identity_changed, or still held. With
// PublishHeld only held_replaced may count one, and whatever is
// published without an identity is counted in unidentified.
func FuzzTrackerTakeAll(f *testing.F) {
	f.Add(0.0, 3.5, "rx-1", "AA:BB:CC:00:00:01", uint8(1), "SN-1", uint16(0x2222), true)
	f.Add(0.0, 1.0, "rx-1", "T", uint8(1), "SN-1", uint16(0x3333), true)
	f.Add(0.0, 3.5, "rx-1", "T", uint8(1), "SN-1", uint16(0x2222), false)
	f.Add(math.NaN(), 1.0, "", "", uint8(0), "", uint16(0xffff), true)
	f.Add(math.Inf(1), math.Inf(-1), "rx\x00", "\xff\xfe", uint8(9), "\x00", uint16(0xaaaa), true)
	f.Add(-5.0, 0.4, "rx-a", "T", uint8(2), "GEO-OP-77", uint16(0x5a5a), true)
	f.Fuzz(func(t *testing.T, nowS, stepS float64, receiver, transmitter string, idType uint8, uaID string, kinds uint16, publishHeld bool) {
		const bound = 2
		tr := NewTracker(Settings{IdentityTTLS: 2, MaxTransmitters: bound, PublishHeld: publishHeld})
		given, published, unidentified := 0, 0, uint64(0)
		check := func(when string, obs []*Observation) {
			for _, o := range obs {
				if o == nil {
					t.Fatalf("%s: nil observation in the slice", when)
				}
				if o.Identified && o.UAID == "" {
					t.Fatalf("%s: identified without a UAS ID: %+v", when, o)
				}
				if !o.Identified {
					unidentified++
				}
				published++
			}
			if n := tr.Transmitters(); n > bound {
				t.Fatalf("%s: %d addresses held, bound %d", when, n, bound)
			}
			if len(tr.states) != tr.recency.Len() {
				t.Fatalf("%s: %d states, %d in recency", when, len(tr.states), tr.recency.Len())
			}
			held := 0
			for _, st := range tr.states {
				if st.location != nil && !st.published {
					held++
				}
			}
			c := tr.Counters()
			lost := c.Get(CounterHeldForgotten) + c.Get(CounterHeldEvicted) + c.Get(CounterHeldIdentityChanged)
			if got := uint64(published+held) + c.Get(CounterHeldReplaced) + lost; got != uint64(given) {
				t.Fatalf("%s: %d Locations given, %d published, %d held, counters %v", when, given, published, held, c.Snapshot())
			}
			if publishHeld && lost != 0 {
				t.Fatalf("%s: PublishHeld dropped %d held Locations: %v", when, lost, c.Snapshot())
			}
			if !publishHeld && c.Get(CounterHeldFlushed) != 0 {
				t.Fatalf("%s: flushed without PublishHeld: %v", when, c.Snapshot())
			}
			if got := c.Get(CounterUnidentified); got != unidentified {
				t.Fatalf("%s: unidentified counts %d, %d published unidentified", when, got, unidentified)
			}
		}
		for i := range 8 {
			var msgs []odid.Message
			bit := func(k uint) bool { return kinds>>((k*4+uint(i))%16)&1 == 1 }
			if bit(0) {
				msgs = append(msgs, odid.BasicID{IDType: odid.IDType(idType), UAID: uaID + string(rune('0'+i%2))})
			}
			if bit(1) {
				msgs = append(msgs, odid.Location{})
				given++
			}
			if bit(2) {
				msgs = append(msgs, &odid.Location{}, &odid.OperatorID{OperatorID: uaID}, &odid.System{})
				given++
			}
			fr := Frame{
				Receiver:    receiver + string(rune('a'+i%2)),
				Transmitter: transmitter + string(rune('0'+i%3)),
				Messages:    msgs,
				NowS:        nowS + float64(i)*stepS,
			}
			check("TakeAll", tr.TakeAll(fr))
			if bit(3) {
				check("Sweep", tr.Sweep(fr.NowS+stepS/2))
			}
		}
		check("final Sweep", tr.Sweep(nowS+100*stepS))
	})
}
