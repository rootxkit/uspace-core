package rid

import (
	"container/list"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/odid"
)

// Counter names of the Tracker (E-09). They are stable: the vectors
// compare the first four.
const (
	// CounterIdentityChanges counts a different Basic ID of one ID type
	// from a (receiver, address) that had one: everything known about it
	// is dropped.
	CounterIdentityChanges = "identity_changes"
	// CounterSilences counts (receiver, address) states forgotten after a
	// silence longer than MaxGapS.
	CounterSilences = "silences"
	// CounterUnidentified counts observations published without a fresh
	// identity.
	CounterUnidentified = "unidentified"
	// CounterAddressConflicts counts Basic IDs naming a second fresh
	// identity of one ID type for an address, from any receiver: "one
	// transmitter, two identities" (I-04).
	CounterAddressConflicts = "address_conflicts"
	// CounterEvicted counts addresses dropped because the table was full
	// (E-10).
	CounterEvicted = "evicted"
)

// Settings are the Tracker's thresholds (I-01, I-02, E-10).
type Settings struct {
	// IdentityTTLS is how long a Basic ID stays usable after it was
	// heard. Default 15 s: five of the standard's 3 s static periods.
	IdentityTTLS float64
	// MaxGapS is the longest silence after which an address is still the
	// same aircraft. Default 3 s: three 1 s Location periods.
	MaxGapS float64
	// IdentifyWithinS is how long a Location without a fresh identity is
	// held for one before it is published unidentified. Default 4 s.
	IdentifyWithinS float64
	// MaxTransmitters bounds the number of addresses held. When a new
	// address arrives at the bound, the address heard longest ago is
	// dropped and counted as evicted. 0 or less means the default, 50000.
	MaxTransmitters int
}

// DefaultSettings returns utm's thresholds: 15 s, 3 s, 4 s, and 50000
// addresses.
func DefaultSettings() Settings {
	return Settings{IdentityTTLS: 15, MaxGapS: 3, IdentifyWithinS: 4, MaxTransmitters: defaultMaxTransmitters}
}

const defaultMaxTransmitters = 50000

// Frame is one receiver report: the messages heard in one frame from one
// transmitter address.
type Frame struct {
	// Receiver is the id of the receiver that heard the frame.
	Receiver string
	// Transmitter is how the receiver told transmitters apart: a
	// Bluetooth or Wi-Fi address.
	Transmitter string
	// Messages are the decoded messages of the frame, as values or
	// pointers of the odid types. Other types are ignored.
	Messages []odid.Message
	// NowS is the tracker's monotonic clock, in seconds.
	NowS float64
	// RxTS is when the frame was received, on the ingest's clock. It is
	// carried to the Observation of a Location in this frame.
	RxTS time.Time
}

// Observation is a Location joined to the identity it belongs to, or
// published unidentified.
type Observation struct {
	// DroneID is AircraftID of the identity, or UnidentifiedID of the
	// transmitter when Identified is false.
	DroneID string
	// Label is the UAS ID, or the transmitter address when unidentified.
	Label string
	// Identified is false for a Location published without a fresh
	// identity (I-02).
	Identified bool
	// UAID is the UAS ID; empty when unidentified.
	UAID string
	// IDType is the ID type; odid.IDTypeNone when unidentified.
	IDType odid.IDType
	// OperatorID is the operator number as broadcast by this receiver's
	// view of the address; nil when none was heard since the last change.
	OperatorID *string
	// Location is the Location published.
	Location odid.Location
	// System is the last System message from this receiver's view of the
	// address; nil when none.
	System *odid.System
	// Receiver and Transmitter are those of the frame that carried the
	// Location.
	Receiver, Transmitter string
	// IdentityReceiver is the receiver whose Basic ID identified the
	// Location: Receiver itself, or another receiver that lent its fresh
	// identity for the same address (I-03, S-35). Empty when unidentified.
	IdentityReceiver string
	// RxTS is the receive time of the frame that carried the Location
	// (not of a later Basic ID that completed it).
	RxTS time.Time
}

// identity is one Basic ID and when it was last heard.
type identity struct {
	basic  odid.BasicID
	heardS float64
}

// stateKey is one receiver's view of one transmitter address.
type stateKey struct {
	receiver, transmitter string
}

// state is what one receiver heard from one address since the address
// was first heard, heard again after a silence, or taken by another
// identity.
type state struct {
	key         stateKey
	startedS    float64
	lastHeardS  float64
	identities  []identity // at most one per ID type
	location    *odid.Location
	locationRx  time.Time
	published   bool
	system      *odid.System
	operator    *odid.OperatorID
	recency     *list.Element // in Tracker.recency
	transmitter *address
}

// address is everything held for one transmitter address, across
// receivers.
type address struct {
	states []*state
}

// Tracker joins Basic ID and Location by transmitter address while the
// identity is fresh (LESSONS I-01 to I-04, R-13). State is per
// (receiver, address); freshness, borrowing and the two-identity anomaly
// are per address, across receivers.
//
// A Tracker is not safe for concurrent use: run one per goroutine, or
// guard it with a mutex (the race test does). NowS must not go backwards;
// a step back only delays the forgetting of silent addresses.
type Tracker struct {
	s         Settings
	states    map[stateKey]*state
	addresses map[string]*address
	// recency orders states by the time they were last heard, oldest
	// first, so that forgetting and eviction cost no scan.
	recency  *list.List
	counters core.Counters
}

// NewTracker returns an empty tracker with settings s. A MaxTransmitters
// of 0 or less is the default.
func NewTracker(s Settings) *Tracker {
	if s.MaxTransmitters <= 0 {
		s.MaxTransmitters = defaultMaxTransmitters
	}
	return &Tracker{
		s:         s,
		states:    make(map[stateKey]*state),
		addresses: make(map[string]*address),
		recency:   list.New(),
	}
}

// Counters returns the tracker's counters: identity_changes, silences,
// unidentified, address_conflicts and evicted.
func (t *Tracker) Counters() *core.Counters { return &t.counters }

// Transmitters returns the number of addresses held.
func (t *Tracker) Transmitters() int { return len(t.addresses) }

// Take applies one frame and returns the observation it completes, or nil
// when it publishes nothing (R-13: each Location is published once, when
// it arrives with a fresh identity or when the identity it was held for
// arrives; a Location without one is held for IdentifyWithinS, then
// published unidentified).
func (t *Tracker) Take(f Frame) *Observation {
	// Before this frame counts as hearing the address: a silence ends here.
	t.Forget(f.NowS)
	st := t.stateFor(f)
	st.lastHeardS = f.NowS
	t.recency.MoveToBack(st.recency)

	// Identity first: a Basic ID that changes it drops what the address
	// said before, and must not drop a Location in the same pack.
	for _, m := range f.Messages {
		if b, ok := basicID(m); ok && identified(b) {
			t.learn(st, b, f.NowS)
		}
	}
	for _, m := range f.Messages {
		switch v := m.(type) {
		case odid.Location:
			st.setLocation(&v, f.RxTS)
		case *odid.Location:
			if v != nil {
				loc := *v
				st.setLocation(&loc, f.RxTS)
			}
		case odid.System:
			st.system = &v
		case *odid.System:
			if v != nil {
				sys := *v
				st.system = &sys
			}
		case odid.OperatorID:
			st.operator = &v
		case *odid.OperatorID:
			if v != nil {
				op := *v
				st.operator = &op
			}
		}
	}
	if st.location == nil || st.published {
		return nil
	}
	basic, lender := t.identityFor(st, f.NowS)
	if basic == nil {
		if f.NowS-t.unidentifiedSince(st) < t.s.IdentifyWithinS {
			return nil
		}
		t.counters.Inc(CounterUnidentified)
	}
	st.published = true
	return st.observation(basic, lender)
}

func (st *state) setLocation(loc *odid.Location, rx time.Time) {
	st.location = loc
	st.locationRx = rx
	st.published = false
}

// basicID returns m as a Basic ID when it is one.
func basicID(m odid.Message) (odid.BasicID, bool) {
	switch v := m.(type) {
	case odid.BasicID:
		return v, true
	case *odid.BasicID:
		if v != nil {
			return *v, true
		}
	}
	return odid.BasicID{}, false
}

// identified reports whether a Basic ID names an identity: ID type 0
// with an empty UAS ID is "no identity", not a new one.
func identified(b odid.BasicID) bool {
	return b.IDType != odid.IDTypeNone && b.UAID != ""
}

// stateFor returns the frame's (receiver, address) state, creating it,
// and evicting the address heard longest ago when the table is full.
func (t *Tracker) stateFor(f Frame) *state {
	key := stateKey{f.Receiver, f.Transmitter}
	if st, ok := t.states[key]; ok {
		return st
	}
	addr, ok := t.addresses[f.Transmitter]
	if !ok {
		for len(t.addresses) >= t.s.MaxTransmitters {
			oldest := t.oldest()
			if oldest == nil {
				break
			}
			t.dropAddress(oldest.transmitter)
			t.counters.Inc(CounterEvicted)
		}
		addr = &address{}
		t.addresses[f.Transmitter] = addr
	}
	st := &state{key: key, startedS: f.NowS, lastHeardS: f.NowS, transmitter: addr}
	st.recency = t.recency.PushBack(st)
	t.states[key] = st
	addr.states = append(addr.states, st)
	return st
}

// oldest returns the state heard longest ago, nil when there is none.
func (t *Tracker) oldest() *state {
	e := t.recency.Front()
	if e == nil {
		return nil
	}
	st, _ := e.Value.(*state)
	return st
}

// dropAddress forgets every receiver's view of an address.
func (t *Tracker) dropAddress(addr *address) {
	for _, st := range addr.states {
		t.recency.Remove(st.recency)
		delete(t.states, st.key)
	}
	if len(addr.states) > 0 {
		delete(t.addresses, addr.states[0].key.transmitter)
	}
	addr.states = nil
}

// Forget drops every (receiver, address) state silent for longer than
// MaxGapS at nowS, counts each in silences and returns how many it
// dropped: whatever is heard from such an address next is not known to be
// the same aircraft. Take calls it; a caller with a periodic tick may too.
func (t *Tracker) Forget(nowS float64) int {
	n := 0
	for st := t.oldest(); st != nil; st = t.oldest() {
		// Written so that a NaN clock forgets nothing.
		if !(nowS-st.lastHeardS > t.s.MaxGapS) {
			break
		}
		t.removeState(st)
		t.counters.Inc(CounterSilences)
		n++
	}
	return n
}

// removeState forgets one receiver's view of an address.
func (t *Tracker) removeState(st *state) {
	t.recency.Remove(st.recency)
	delete(t.states, st.key)
	addr := st.transmitter
	for i, other := range addr.states {
		if other == st {
			addr.states = append(addr.states[:i], addr.states[i+1:]...)
			break
		}
	}
	if len(addr.states) == 0 {
		delete(t.addresses, st.key.transmitter)
	}
}

// fresh reports whether an identity is within the TTL at nowS.
func (t *Tracker) fresh(id identity, nowS float64) bool {
	return nowS-id.heardS <= t.s.IdentityTTLS
}

// identityFor is the fresh identity for a Location held by st and the
// receiver it came from: st's own receiver's first, else one another
// receiver holds for the same address under the same TTL (I-03). Nil when
// there is none.
func (t *Tracker) identityFor(st *state, nowS float64) (*odid.BasicID, string) {
	best, from := t.freshest(nil, "", st, nowS)
	if best == nil {
		for _, other := range st.transmitter.states {
			if other != st {
				best, from = t.freshest(best, from, other, nowS)
			}
		}
	}
	if best == nil {
		return nil, ""
	}
	b := best.basic
	return &b, from
}

// freshest returns the preferred of best and the fresh identities of st:
// a serial over any other ID type (a serial is fixed to the airframe, a
// registration can move, I-05), then the one heard last; from is the
// receiver that holds it.
func (t *Tracker) freshest(best *identity, from string, st *state, nowS float64) (*identity, string) {
	for i := range st.identities {
		id := &st.identities[i]
		if t.fresh(*id, nowS) && (best == nil || better(id, best)) {
			best, from = id, st.key.receiver
		}
	}
	return best, from
}

// better reports whether a is preferred over b.
func better(a, b *identity) bool {
	aSerial := a.basic.IDType == odid.IDTypeSerial
	bSerial := b.basic.IDType == odid.IDTypeSerial
	if aSerial != bSerial {
		return aSerial
	}
	return a.heardS > b.heardS
}

// learn records a Basic ID heard by st (I-01, I-04).
func (t *Tracker) learn(st *state, b odid.BasicID, nowS float64) {
	// One address with two fresh identities of one ID type, from this
	// receiver or another: two radios on one address, or a spoofer. Not a
	// reboot, which falls silent first.
	if t.clashes(st.transmitter, b, nowS) {
		t.counters.Inc(CounterAddressConflicts)
	}
	for i := range st.identities {
		known := &st.identities[i]
		if known.basic.IDType != b.IDType {
			continue
		}
		if known.basic.UAID == b.UAID {
			known.basic = b
			known.heardS = nowS
			return
		}
		// Another aircraft on this address: nothing it said before is
		// this one's, a held Location included.
		t.counters.Inc(CounterIdentityChanges)
		st.identities = st.identities[:0]
		st.location = nil
		st.published = false
		st.system = nil
		st.operator = nil
		st.startedS = nowS
		break
	}
	st.identities = append(st.identities, identity{basic: b, heardS: nowS})
}

// clashes reports whether an address holds a fresh identity of b's ID
// type with another UAS ID.
func (t *Tracker) clashes(addr *address, b odid.BasicID, nowS float64) bool {
	for _, st := range addr.states {
		for _, id := range st.identities {
			if id.basic.IDType == b.IDType && id.basic.UAID != b.UAID && t.fresh(id, nowS) {
				return true
			}
		}
	}
	return false
}

// unidentifiedSince is since when st has had no fresh identity of its
// own.
func (t *Tracker) unidentifiedSince(st *state) float64 {
	if len(st.identities) == 0 {
		return st.startedS
	}
	latest := st.identities[0].heardS
	for _, id := range st.identities[1:] {
		if id.heardS > latest {
			latest = id.heardS
		}
	}
	return max(st.startedS, latest+t.s.IdentityTTLS)
}

// observation builds the observation of st's Location under basic, lent
// by the receiver lender, or unidentified when basic is nil.
func (st *state) observation(basic *odid.BasicID, lender string) *Observation {
	o := &Observation{
		Location:    *st.location,
		Receiver:    st.key.receiver,
		Transmitter: st.key.transmitter,
		RxTS:        st.locationRx,
	}
	if basic == nil {
		o.DroneID = UnidentifiedID(st.key.transmitter)
		o.Label = st.key.transmitter
		o.IDType = odid.IDTypeNone
	} else {
		o.DroneID = AircraftID(basic.IDType, basic.UAID)
		o.Label = basic.UAID
		o.Identified = true
		o.UAID = basic.UAID
		o.IDType = basic.IDType
		o.IdentityReceiver = lender
	}
	if st.operator != nil {
		op := st.operator.OperatorID
		o.OperatorID = &op
	}
	if st.system != nil {
		sys := *st.system
		o.System = &sys
	}
	return o
}
