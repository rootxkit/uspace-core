package rid

import (
	"container/list"
	"math"
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
	// CounterHeldReplaced counts held Locations replaced by a newer one
	// before they were published: never shown (E-09).
	CounterHeldReplaced = "held_replaced"
	// CounterHeldForgotten counts held Locations dropped unpublished
	// because their (receiver, address) fell silent for longer than
	// MaxGapS: an aircraft that was heard and never shown (E-09). The
	// state is also counted in silences, which counts every departure.
	// With Settings.PublishHeld, TakeAll and Sweep publish such a
	// Location instead (held_flushed) and this stays at zero.
	CounterHeldForgotten = "held_forgotten"
	// CounterHeldEvicted counts held Locations dropped unpublished
	// because their address was evicted from a full table (E-09). The
	// address is also counted in evicted.
	CounterHeldEvicted = "held_evicted"
	// CounterHeldIdentityChanged counts held Locations dropped
	// unpublished because another Basic ID of the same ID type arrived
	// from their (receiver, address) (E-09). The change is also counted
	// in identity_changes.
	CounterHeldIdentityChanged = "held_identity_changed"
	// CounterHeldFlushed counts held Locations that TakeAll or Sweep
	// published unidentified, under Settings.PublishHeld, because their
	// state was forgotten, evicted or taken by another identity. Each is
	// also counted in unidentified.
	CounterHeldFlushed = "held_flushed"
)

// Settings are the Tracker's thresholds (I-01, I-02, E-10).
type Settings struct {
	// IdentityTTLS is how long a Basic ID stays usable after it was
	// heard. Default 15 s: five of the standard's 3 s static periods.
	IdentityTTLS float64
	// MaxGapS is the longest silence after which an address is still the
	// same aircraft; after a longer one everything held for it is
	// dropped, a held Location included. Default 3 s: three 1 s Location
	// periods. That default is below IdentifyWithinS, so a transmitter
	// heard less often than every 3 s is never published (Validate
	// refuses it) unless PublishHeld is set, which is how a system runs
	// it: PublishHeldSettings.
	MaxGapS float64
	// IdentifyWithinS is how long a Location without a fresh identity is
	// held for one before it is published unidentified. Default 4 s. A
	// negative value (IdentifyAtOnceS) publishes it at once.
	IdentifyWithinS float64
	// MaxTransmitters bounds the number of addresses held. When a new
	// address arrives at the bound, the address heard longest ago is
	// dropped and counted as evicted. 0 or less means the default, 50000.
	MaxTransmitters int
	// PublishHeld makes TakeAll and Sweep publish, unidentified, a held
	// Location that would otherwise be dropped unpublished with its
	// state: when the state is forgotten after a silence longer than
	// MaxGapS (held_forgotten without the flag), when its address is
	// evicted from a full table (held_evicted), and when another Basic
	// ID of the same ID type takes the address (held_identity_changed).
	// So every aircraft that was heard is shown, while MaxGapS stays
	// short and no identity is carried across a longer silence. A held
	// Location replaced by a newer one of the same state is not flushed
	// (held_replaced): the newer one is published in its place.
	//
	// Default false: exactly the behaviour without the field. Take and
	// Forget return at most one observation and none, so they cannot
	// hand a flushed Location back and keep dropping and counting it
	// whatever the flag says: a caller that sets it calls TakeAll and
	// Sweep instead. It becomes the default in the next major.
	PublishHeld bool
}

// IdentifyAtOnceS is the IdentifyWithinS that publishes a Location
// without a fresh identity at once, unidentified. Zero cannot say this:
// a zero field is the default (see NewTracker).
const IdentifyAtOnceS = -1.0

// DefaultSettings returns utm's thresholds: 15 s, 3 s, 4 s, and 50000
// addresses.
//
// They fail Validate, by design until the next major: the 3 s MaxGapS is
// below the 4 s IdentifyWithinS hold, so a transmitter heard less often
// than every 3 s is never published (review 2026-11-12). Changing a
// default changes what the tracker returns for existing callers, which
// is a major (docs/RELEASING.md section 1), so 1.x keeps the values
// rid_identity.json was written under and a system opts in to
// PublishHeldSettings.
func DefaultSettings() Settings {
	return Settings{IdentityTTLS: 15, MaxGapS: 3, IdentifyWithinS: 4, MaxTransmitters: defaultMaxTransmitters}
}

// RecommendedSettings returns the thresholds a system should run: 15 s,
// 9 s, 4 s, and 50000 addresses. They are DefaultSettings with MaxGapS
// at 9 s: three of the standard's 3 s static periods, the slowest
// message a receiver must hear to identify a transmitter that sends one
// message per advertisement. That is at or above IdentifyWithinS, so a
// transmitter heard at least once every 9 s is published (Validate
// passes), and below IdentityTTLS, so a silence still forgets an
// identity before its TTL would. A Location completed by a later Basic
// ID can then be up to 9 s old; Observation.RxTS says so.
//
// Deprecated: use PublishHeldSettings. The 9 s gap also keeps an
// identity for 9 s of silence, so a transmitter that restarts on the
// same address with another serial within 9 s has its Locations
// published under the old serial until its own Basic ID is heard (I-07
// widened from 3 s to 9 s; the lab's SC-10 fails), and a transmitter
// heard less often than every 9 s is still never published. The values
// returned are unchanged and stay so in 1.x.
func RecommendedSettings() Settings {
	s := DefaultSettings()
	s.MaxGapS = 9
	return s
}

// PublishHeldSettings returns the settings a system should run: the
// defaults (15 s, 3 s, 4 s, 50000 addresses) with PublishHeld set. The
// 3 s MaxGapS still forgets an identity after three missed Location
// periods, so I-07 is as narrow as under DefaultSettings, and a
// Location still held when its state is forgotten is published
// unidentified by TakeAll or Sweep, so a transmitter is published
// however rarely it is heard (Validate passes). The caller must call
// TakeAll where it called Take and Sweep where it called Forget; with
// Take and Forget these settings behave as DefaultSettings. They become
// DefaultSettings in the next major.
func PublishHeldSettings() Settings {
	s := DefaultSettings()
	s.PublishHeld = true
	return s
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

// NewTracker returns an empty tracker with settings s. A field that is
// zero, NaN or infinite takes its default, and so does a negative one
// except IdentifyWithinS (negative is "no wait", IdentifyAtOnceS). So
// Settings{} is DefaultSettings(): a zero TTL or gap would forget every
// identity at once, which is never what a caller who left the field out
// meant.
func NewTracker(s Settings) *Tracker {
	s = s.withDefaults()
	return &Tracker{
		s:         s,
		states:    make(map[stateKey]*state),
		addresses: make(map[string]*address),
		recency:   list.New(),
	}
}

// withDefaults returns s as a Tracker runs it: every field NewTracker
// would replace holds its default.
func (s Settings) withDefaults() Settings {
	d := DefaultSettings()
	if !positive(s.IdentityTTLS) {
		s.IdentityTTLS = d.IdentityTTLS
	}
	if !positive(s.MaxGapS) {
		s.MaxGapS = d.MaxGapS
	}
	if s.IdentifyWithinS == 0 || math.IsNaN(s.IdentifyWithinS) || math.IsInf(s.IdentifyWithinS, 0) {
		s.IdentifyWithinS = d.IdentifyWithinS
	}
	if s.MaxTransmitters <= 0 {
		s.MaxTransmitters = d.MaxTransmitters
	}
	return s
}

// Validate reports whether the settings, as NewTracker would run them
// (after defaulting), can publish a transmitter that is heard rarely:
// without PublishHeld, MaxGapS must not be below IdentifyWithinS. A
// Location without a fresh
// identity is held for IdentifyWithinS, and the hold restarts when the
// address is forgotten after a silence longer than MaxGapS; with the gap
// below the hold, a transmitter heard less often than every MaxGapS is
// forgotten before each hold ends and is never published, identified or
// not (only held_forgotten shows it). The error is a *core.FieldError
// naming MaxGapS. IdentifyAtOnceS holds nothing and passes with any gap.
//
// With PublishHeld any gap passes: the Location held when the address
// is forgotten is published unidentified, so a gap below the hold loses
// nothing. That is true for a caller of TakeAll and Sweep only, and
// Validate cannot see which methods are called: with Take and Forget
// the flag publishes nothing more, and held_forgotten above zero is the
// sign of it.
//
// NewTracker does not call it and keeps the settings it is given: a
// caller that reads Settings from its configuration validates them and
// refuses to start on an error. DefaultSettings fails it in 1.x (its
// MaxGapS is 3 s); PublishHeldSettings passes.
func (s Settings) Validate() error {
	s = s.withDefaults()
	if !s.PublishHeld && s.IdentifyWithinS > 0 && s.MaxGapS < s.IdentifyWithinS {
		return &core.FieldError{Field: "MaxGapS", Reason: "below IdentifyWithinS: a transmitter heard less often than every MaxGapS would never be published"}
	}
	return nil
}

// positive reports whether v is a finite number above zero.
func positive(v float64) bool {
	return v > 0 && !math.IsInf(v, 1)
}

// Settings returns the settings in use, after defaulting.
func (t *Tracker) Settings() Settings { return t.s }

// Counters returns the tracker's counters: identity_changes, silences,
// unidentified, address_conflicts, evicted, held_replaced,
// held_forgotten, held_evicted, held_identity_changed and held_flushed.
func (t *Tracker) Counters() *core.Counters { return &t.counters }

// Transmitters returns the number of addresses held.
func (t *Tracker) Transmitters() int { return len(t.addresses) }

// Take applies one frame and returns the observation it completes, or nil
// when it publishes nothing (R-13: each Location is published once, when
// it arrives with a fresh identity or when the identity it was held for
// arrives; a Location without one is held for IdentifyWithinS, then
// published unidentified).
//
// A held Location whose state this frame discards is dropped and
// counted, also with Settings.PublishHeld: Take has one observation to
// return. TakeAll returns those too.
func (t *Tracker) Take(f Frame) *Observation {
	return t.take(f, nil)
}

// TakeAll applies one frame as Take does and returns every observation
// it publishes, oldest first; nil when it publishes nothing. Without
// Settings.PublishHeld that is Take's observation, if any, and nothing
// else.
//
// With PublishHeld it returns first each held Location that the frame
// made the tracker discard unpublished, as an unidentified observation:
// the Locations of the states silent for longer than MaxGapS at
// f.NowS, whatever their address or receiver (the frame is the clock
// that ends their silence), of an address evicted to make room for the
// frame's, and of the frame's own state when its Basic ID names another
// identity. The frame's own observation, if any, is last.
//
// A flushed observation is what a hold that timed out publishes:
// DroneID is UnidentifiedID(address), Label the address, Identified
// false, UAID and IdentityReceiver empty, IDType 0, with the Operator
// ID and System its state had heard, and counted in unidentified (and
// held_flushed). It is unidentified even if an identity for the address
// is known by then: the state it belonged to is gone, and an identity
// is never attached across that. Receiver, Transmitter and RxTS are
// those of the frame that carried the Location, never of the frame
// that flushed it, which is later by more than MaxGapS for a silence:
// the caller places it by RxTS.
//
// Order: the observations of one (receiver, address) are returned in
// the order their Locations were taken, within a call and across calls
// of TakeAll and Sweep, because a Location is flushed by the call that
// discards its state and nothing is queued. A caller that publishes
// each slice in order before its next call therefore never puts an
// older Location of a receiver's view of an address after a newer one.
// Across receivers there is no such order, with or without the flag,
// so a picture keyed by DroneID keeps the observation with the latest
// RxTS.
//
// Each Location is returned at most once across Take, TakeAll and
// Sweep. A flushed Location is returned by the first call whose clock
// is past its state's silence: without a periodic Sweep that is the
// next frame of any transmitter, which may be much later.
func (t *Tracker) TakeAll(f Frame) []*Observation {
	var out []*Observation
	if o := t.take(f, t.sink(&out)); o != nil {
		out = append(out, o)
	}
	return out
}

// Sweep is Forget for a caller that set Settings.PublishHeld: it drops
// every (receiver, address) state silent for longer than MaxGapS at
// nowS, counts each in silences, and returns the Locations they still
// held, unpublished, as unidentified observations (see TakeAll),
// longest silent first; nil when there is none. Without PublishHeld it
// returns nil and counts them in held_forgotten, as Forget does. A
// caller runs it on a periodic tick, so that the last Location of a
// transmitter that fell silent is published about MaxGapS plus one
// tick after it was heard, not when some other transmitter is next
// heard.
func (t *Tracker) Sweep(nowS float64) []*Observation {
	var out []*Observation
	t.forget(nowS, t.sink(&out))
	return out
}

// sink returns where a call that can return several observations has
// the flushed ones appended: out under PublishHeld, nil (drop and
// count) without it.
func (t *Tracker) sink(out *[]*Observation) *[]*Observation {
	if !t.s.PublishHeld {
		return nil
	}
	return out
}

// discard accounts for the Location st holds when st, or what st knew,
// is dropped: nothing when it holds none or published it; else it is
// appended to sink as an unidentified observation, or, with no sink,
// counted in dropped.
func (t *Tracker) discard(st *state, dropped string, sink *[]*Observation) {
	if st.location == nil || st.published {
		return
	}
	if sink == nil {
		t.counters.Inc(dropped)
		return
	}
	t.counters.Inc(CounterUnidentified)
	t.counters.Inc(CounterHeldFlushed)
	*sink = append(*sink, st.observation(nil, ""))
	st.published = true
}

// take is Take; held Locations it discards go to sink (see discard).
func (t *Tracker) take(f Frame, sink *[]*Observation) *Observation {
	// Before this frame counts as hearing the address: a silence ends here.
	t.forget(f.NowS, sink)
	st := t.stateFor(f, sink)
	st.lastHeardS = f.NowS
	t.recency.MoveToBack(st.recency)

	// Identity first: a Basic ID that changes it drops what the address
	// said before, and must not drop a Location in the same pack.
	for _, m := range f.Messages {
		if b, ok := basicID(m); ok && identified(b) {
			t.learn(st, b, f.NowS, sink)
		}
	}
	for _, m := range f.Messages {
		switch v := m.(type) {
		case odid.Location:
			t.hold(st, &v, f.RxTS)
		case *odid.Location:
			if v != nil {
				loc := *v
				t.hold(st, &loc, f.RxTS)
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

// hold makes loc the Location st waits to publish. A held one not yet
// published is replaced, and counted: it is never shown.
func (t *Tracker) hold(st *state, loc *odid.Location, rx time.Time) {
	if st.location != nil && !st.published {
		t.counters.Inc(CounterHeldReplaced)
	}
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
func (t *Tracker) stateFor(f Frame, sink *[]*Observation) *state {
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
			t.dropAddress(oldest.transmitter, sink)
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

// dropAddress forgets every receiver's view of an evicted address.
func (t *Tracker) dropAddress(addr *address, sink *[]*Observation) {
	for _, st := range addr.states {
		t.discard(st, CounterHeldEvicted, sink)
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
// the same aircraft. A state dropped while it holds a Location that was
// never published is also counted in held_forgotten, also with
// Settings.PublishHeld: Forget has no observation to return, Sweep
// does. Take calls it; a caller with a periodic tick may too.
func (t *Tracker) Forget(nowS float64) int {
	return t.forget(nowS, nil)
}

// forget is Forget; held Locations it discards go to sink (see discard).
func (t *Tracker) forget(nowS float64, sink *[]*Observation) int {
	n := 0
	for st := t.oldest(); st != nil; st = t.oldest() {
		// Written so that a NaN clock forgets nothing.
		if !(nowS-st.lastHeardS > t.s.MaxGapS) {
			break
		}
		t.discard(st, CounterHeldForgotten, sink)
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
func (t *Tracker) learn(st *state, b odid.BasicID, nowS float64, sink *[]*Observation) {
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
		t.discard(st, CounterHeldIdentityChanged, sink)
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
