package rid

import (
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/odid"
)

func f64(v float64) *float64 { return &v }

// Location defaults of the generator's location_with() (rid_identity.json
// header: only identity matters).
const (
	defaultLatDeg = 41.7151
	defaultLonDeg = 44.8271
)

// identityEpoch is the instant now_s 0 maps onto (PLAN §11 gap 8).
var identityEpoch = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// rxAt maps the tracker clock onto the vectors' receive time.
func rxAt(nowS float64) time.Time {
	return identityEpoch.Add(time.Duration(math.Round(nowS*1e6)) * time.Microsecond)
}

func loc(lat float64) odid.Location {
	return odid.Location{Status: odid.StatusAirborne, LatDeg: f64(lat), LonDeg: f64(defaultLonDeg)}
}

func serial(ua string) odid.BasicID { return odid.BasicID{IDType: odid.IDTypeSerial, UAID: ua} }

func frame(nowS float64, msgs ...odid.Message) Frame {
	return Frame{Receiver: "rx-1", Transmitter: "AA:BB:CC:00:00:01", Messages: msgs, NowS: nowS, RxTS: rxAt(nowS)}
}

func TestIDs(t *testing.T) {
	// utm's ids, pinned by rid_identity.json.
	for _, c := range []struct{ got, want string }{
		{AircraftID(odid.IDTypeSerial, "SN-OLD-0001"), "47505d6b-9788-58d9-9201-6551ff4396b9"},
		{AircraftID(odid.IDTypeSerial, "SN-NEW-0002"), "8d0666de-18da-5914-b218-3fd588febc82"},
		{UnidentifiedID("AA:BB:CC:00:00:01"), "924c95fb-b97c-549d-bc63-8711f666d129"},
	} {
		if c.got != c.want {
			t.Errorf("got %s, want %s", c.got, c.want)
		}
	}
	// The serial and the registration with one text are two aircraft.
	if AircraftID(odid.IDTypeSerial, "X") == AircraftID(odid.IDTypeCAARegistration, "X") {
		t.Error("ID type not in the id")
	}
	if NamespaceUUID != "6f1c7d52-4a0b-5c1e-9d3a-2b8e41f07a65" {
		t.Error("namespace changed")
	}
}

func TestVelocityNED(t *testing.T) {
	vn, ve, vd := VelocityNED(f64(10), f64(90), f64(2))
	if vn == nil || ve == nil || vd == nil || math.Abs(*vn) > 1e-9 || math.Abs(*ve-10) > 1e-9 || *vd != -2 {
		t.Errorf("east at 10 m/s climbing 2: %v %v %v", vn, ve, vd)
	}
	vn, ve, vd = VelocityNED(f64(10), f64(180), nil)
	if vn == nil || ve == nil || vd != nil || math.Abs(*vn+10) > 1e-9 || math.Abs(*ve) > 1e-9 {
		t.Errorf("south, no climb: %v %v %v", vn, ve, vd)
	}
	// R-10: a speed without a direction is not a velocity.
	for _, c := range [][3]*float64{{f64(10), nil, f64(1)}, {nil, f64(90), f64(1)}, {nil, nil, nil}} {
		if vn, ve, vd := VelocityNED(c[0], c[1], c[2]); vn != nil || ve != nil || vd != nil {
			t.Errorf("VelocityNED(%v) = %v %v %v, want all nil", c, vn, ve, vd)
		}
	}
}

func TestAirborne(t *testing.T) {
	for st, want := range map[odid.Status]bool{
		odid.StatusUndeclared: true, odid.StatusGround: false, odid.StatusAirborne: true,
		odid.StatusEmergency: true, odid.StatusRemoteIDSystemFailure: true,
	} {
		if Airborne(st) != want {
			t.Errorf("Airborne(%d) = %v", st, !want)
		}
	}
}

func TestDefaults(t *testing.T) {
	s := DefaultSettings()
	if s.IdentityTTLS != 15 || s.MaxGapS != 3 || s.IdentifyWithinS != 4 || s.MaxTransmitters != 50000 {
		t.Errorf("DefaultSettings = %+v", s)
	}
	if tr := NewTracker(Settings{}); tr.s.MaxTransmitters != 50000 {
		t.Errorf("zero MaxTransmitters = %d, want the default", tr.s.MaxTransmitters)
	}
}

// Pointer messages are taken like values; a System message and an
// Operator ID are carried, and are copies the caller cannot change.
func TestTakePointersAndStatic(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	b := serial("SN-1")
	l := loc(41.7)
	sys := odid.System{OperatorLatDeg: f64(41.6)}
	op := odid.OperatorID{OperatorID: "GEO-OP-1"}
	var nilLoc *odid.Location
	o := tr.Take(frame(0, &b, &sys, &op, &l, nilLoc, odid.SelfID{}))
	if o == nil || !o.Identified || o.System == nil || o.OperatorID == nil || *o.OperatorID != "GEO-OP-1" {
		t.Fatalf("observation %+v", o)
	}
	if *o.System.OperatorLatDeg != 41.6 || o.Receiver != "rx-1" || o.Transmitter != "AA:BB:CC:00:00:01" {
		t.Errorf("observation %+v", o)
	}
	sys.ClassEU = 9
	if o.System.ClassEU == 9 {
		t.Error("System aliased the caller's message")
	}
	// Value System and Operator ID are taken too.
	o = tr.Take(frame(1, odid.System{ClassEU: 3}, odid.OperatorID{OperatorID: "GEO-OP-2"}, loc(41.8)))
	if o == nil || o.System.ClassEU != 3 || *o.OperatorID != "GEO-OP-2" {
		t.Errorf("observation %+v", o)
	}
	// A frame without a Location after one was published publishes nothing.
	if o := tr.Take(frame(1.5, &op)); o != nil {
		t.Errorf("static message republished %+v", o)
	}
	var nilBasic *odid.BasicID
	var nilSys *odid.System
	var nilOp *odid.OperatorID
	if o := tr.Take(frame(2, nilBasic, nilSys, nilOp)); o != nil {
		t.Errorf("nil messages published %+v", o)
	}
}

// E-01 pair of the R-13 vector: the repeated Basic ID with a Location
// does publish.
func TestRepeatedIdentityWithLocationPublishes(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	if tr.Take(frame(0, serial("SN-1"), loc(41.7))) == nil {
		t.Fatal("first Location not published")
	}
	if tr.Take(frame(0.5, serial("SN-1"))) != nil {
		t.Fatal("Basic ID alone published")
	}
	if tr.Take(frame(1, serial("SN-1"), loc(41.71))) == nil {
		t.Fatal("new Location with the repeated identity not published")
	}
}

// Forget on a tick drops silent addresses without a frame and counts
// them; an address within the gap stays (E-01).
func TestForget(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	tr.Take(frame(0, serial("SN-1"), loc(41.7)))
	f2 := frame(2, serial("SN-2"), loc(41.7))
	f2.Transmitter = "AA:BB:CC:00:00:02"
	tr.Take(f2)
	if n := tr.Forget(3); n != 0 || tr.Transmitters() != 2 {
		t.Fatalf("Forget(3) = %d, %d held", n, tr.Transmitters())
	}
	if n := tr.Forget(3.5); n != 1 || tr.Transmitters() != 1 {
		t.Fatalf("Forget(3.5) = %d, %d held", n, tr.Transmitters())
	}
	if n := tr.Forget(math.NaN()); n != 0 || tr.Transmitters() != 1 {
		t.Fatalf("Forget(NaN) = %d, %d held", n, tr.Transmitters())
	}
	if n := tr.Forget(10); n != 1 || tr.Transmitters() != 0 {
		t.Fatalf("Forget(10) = %d, %d held", n, tr.Transmitters())
	}
	if got := tr.Counters().Get(CounterSilences); got != 2 {
		t.Errorf("silences %d, want 2", got)
	}
}

// One receiver going silent leaves the address held through the other.
func TestForgetOneReceiver(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	tr.Take(frame(0, serial("SN-1"), loc(41.7)))
	fb := frame(2, loc(41.7))
	fb.Receiver = "rx-2"
	if o := tr.Take(fb); o == nil || o.UAID != "SN-1" || o.Receiver != "rx-2" {
		t.Fatalf("borrowed identity: %+v", o)
	}
	fb.NowS = 4
	fb.RxTS = rxAt(4)
	// rx-1 is now silent for 4 s: forgotten, so rx-2 has nothing to borrow.
	o := tr.Take(fb)
	if tr.Transmitters() != 1 || tr.Counters().Get(CounterSilences) != 1 {
		t.Fatalf("%d held, silences %d", tr.Transmitters(), tr.Counters().Get(CounterSilences))
	}
	if o != nil {
		t.Errorf("after the lender went silent: %+v, want the Location held (rx-2 started at 2 s)", o)
	}
	fb.NowS = 6
	fb.RxTS = rxAt(6)
	if o := tr.Take(fb); o == nil || o.Identified || o.DroneID != UnidentifiedID(fb.Transmitter) {
		t.Errorf("4 s after rx-2 started: %+v, want unidentified", o)
	}
}

// E-10: one address past the bound evicts the one heard longest ago and
// counts it.
func TestTrackerBound(t *testing.T) {
	const bound = 50000
	tr := NewTracker(DefaultSettings())
	for i := range bound + 1 {
		f := frame(1, loc(41.7))
		f.Transmitter = "T" + strconv.Itoa(i)
		tr.Take(f)
	}
	if n := tr.Transmitters(); n != bound {
		t.Fatalf("%d held, want %d", n, bound)
	}
	if got := tr.Counters().Get(CounterEvicted); got != 1 {
		t.Errorf("evicted %d, want 1", got)
	}
	if _, ok := tr.addresses["T0"]; ok {
		t.Error("the oldest address was kept")
	}
	if _, ok := tr.addresses["T50000"]; !ok {
		t.Error("the new address was not taken")
	}
	// At the bound, a known address is not a new one: nothing evicted.
	f := frame(1.5, loc(41.7))
	f.Transmitter = "T1"
	f.Receiver = "rx-2"
	tr.Take(f)
	if got := tr.Counters().Get(CounterEvicted); got != 1 || tr.Transmitters() != bound {
		t.Errorf("evicted %d, %d held after a second receiver", got, tr.Transmitters())
	}
}

func TestTrackerSmallBoundEvictsEveryReceiver(t *testing.T) {
	tr := NewTracker(Settings{IdentityTTLS: 15, MaxGapS: 3, IdentifyWithinS: 4, MaxTransmitters: 1})
	a := frame(0, serial("SN-1"), loc(41.7))
	tr.Take(a)
	a.Receiver = "rx-2"
	tr.Take(a)
	b := frame(0.5, serial("SN-2"), loc(41.7))
	b.Transmitter = "BB"
	if o := tr.Take(b); o == nil || o.UAID != "SN-2" {
		t.Fatalf("new address: %+v", o)
	}
	if tr.Transmitters() != 1 || len(tr.states) != 1 || tr.recency.Len() != 1 {
		t.Errorf("held %d addresses, %d states, %d in recency", tr.Transmitters(), len(tr.states), tr.recency.Len())
	}
	if got := tr.Counters().Get(CounterEvicted); got != 1 {
		t.Errorf("evicted %d, want 1", got)
	}
}

// A serial and a registration from two receivers: the serial wins, and
// among two fresh serials the latest heard.
func TestPreferred(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	a := frame(0, odid.BasicID{IDType: odid.IDTypeCAARegistration, UAID: "REG"})
	tr.Take(a)
	b := frame(0.2, serial("SN-A"))
	b.Receiver = "rx-2"
	tr.Take(b)
	c := frame(0.4, serial("SN-B"))
	c.Receiver = "rx-3"
	tr.Take(c)
	d := frame(0.6, loc(41.7))
	d.Receiver = "rx-4"
	o := tr.Take(d)
	if o == nil || o.UAID != "SN-B" {
		t.Fatalf("borrowed %+v, want the latest serial SN-B", o)
	}
	if got := tr.Counters().Get(CounterAddressConflicts); got != 1 {
		t.Errorf("address_conflicts %d, want 1", got)
	}
	// Own identity first, even a registration.
	e := frame(0.8, loc(41.7))
	o = tr.Take(e)
	if o == nil || o.UAID != "REG" {
		t.Errorf("own identity %+v, want REG", o)
	}
}

// The tracker is not safe for concurrent use; guarded by a mutex it is
// (§8.3). Run with -race.
func TestTrackerConcurrentWithMutex(t *testing.T) {
	tr := NewTracker(DefaultSettings())
	var mu sync.Mutex
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				f := frame(float64(i)*0.01, serial("SN-"+strconv.Itoa(g)), loc(41.7))
				f.Receiver = "rx-" + strconv.Itoa(g)
				mu.Lock()
				tr.Take(f)
				_ = tr.Transmitters()
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if tr.Transmitters() != 1 {
		t.Errorf("%d held, want 1", tr.Transmitters())
	}
	_ = tr.Counters().Snapshot() // counters are safe on their own
}

func BenchmarkTrackerTake(b *testing.B) {
	tr := NewTracker(DefaultSettings())
	id := serial("SN-BENCH-0001")
	l := loc(41.7151)
	rx := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	f := Frame{Receiver: "rx-1", Transmitter: "AA:BB:CC:00:00:01", Messages: []odid.Message{id, l}, RxTS: rx}
	i := 0
	for b.Loop() {
		f.NowS = float64(i) * 0.001
		i++
		if tr.Take(f) == nil {
			b.Fatal("nothing published")
		}
	}
}
