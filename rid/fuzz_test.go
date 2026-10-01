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
