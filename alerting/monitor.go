package alerting

import (
	"container/list"
	"math"
	"slices"
	"strings"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/cpa"
	"github.com/rootxkit/uspace-core/sources"
	"github.com/rootxkit/uspace-core/zones"
)

// sourceKey is a (source type, instance) pair as sources.State names it.
type sourceKey struct {
	typ, station string
}

// instance is the instance id to ask sources about: nil for a track with
// no station, which is judged by its type's row alone.
func (k sourceKey) instance() *string {
	if k.station == "" {
		return nil
	}
	s := k.station
	return &s
}

// orderEntry is the latest sample one source gave for one aircraft (T-03).
type orderEntry struct {
	src          sourceKey
	sourceTS     float64
	capturedAtS  float64
	updateSerial uint64
}

// aircraft is everything the monitor holds about one id.
type aircraft struct {
	id string
	// src is the source of the last admitted sample: a switch that
	// disables it drops the aircraft (B-11).
	src sourceKey
	// hasTrack: a flying sample with a valid position is held in the grid
	// and judged against; seenS is when it was placed (capped at the wall
	// time it was observed), the clock StaleAfterS runs on.
	hasTrack bool
	state    cpa.State
	seenS    float64
	// transmitter is the Remote ID address of the track ("" for none) and
	// unidentified whether it was explicitly unidentified (I-02).
	transmitter  string
	unidentified bool
	// hasIdentity: an identification was heard; identitySeenS when. The
	// mismatch alert goes stale with it, not with the track (G-02).
	hasIdentity   bool
	identitySeenS float64
	// heardS is when the last admitted sample was placed (capped at its
	// wall time): an aircraft with neither a track nor an identity is
	// forgotten StaleAfterS after it.
	heardS float64
	// placedS is the latest CapturedAtS admitted: placedS, heardS, seenS,
	// identitySeenS and state.CapturedAtS never go backwards (T-06).
	placedS float64
	order   []orderEntry
	// alerts holds the keys of the active alerts this aircraft is part of.
	alerts map[string]struct{}
	// identUnidentified: the last identification heard said unidentified.
	identUnidentified bool
	// pool is the eviction pool ac is in (poolNone while it holds an
	// alert: it is never evicted), elem its place there.
	pool int
	elem *list.Element
}

// Eviction pools, in the order victim takes from them (owner decision on
// PR #15): only aircraft without an active alert are ever evicted.
const (
	poolNone         = -1
	poolNotFlying    = 0 // no track: on the ground, flying unknown, or none yet
	poolUnidentified = 1 // flying, unidentified (I-02) or last said unidentified
	poolFlying       = 2 // flying and identified
	poolCount        = 3
)

// poolOf is the eviction pool ac belongs in now.
func poolOf(ac *aircraft) int {
	switch {
	case len(ac.alerts) > 0:
		return poolNone
	case !ac.hasTrack:
		return poolNotFlying
	case ac.unidentified || ac.identUnidentified:
		return poolUnidentified
	}
	return poolFlying
}

// Monitor is the alert state machine for one set of tracks (one cell set,
// spec 05 §3). It is not safe for concurrent use: one goroutine owns a
// Monitor; the caller partitions aircraft between monitors.
type Monitor struct {
	cfg      Config
	grid     *cpa.Grid
	zoneIx   *zones.Index
	zoneKey  map[*zones.Zone]zoneKey
	follower *sources.Follower
	aircraft map[string]*aircraft
	// pools hold the aircraft without an active alert, each least recently
	// heard first (E-10).
	pools    [poolCount]*list.List
	active   map[string]*alertState
	counters core.Counters
	// nextSweepS is the earliest wall time at which an aircraft or an
	// identity can have gone stale; sweep does nothing before it.
	nextSweepS float64
	serial     uint64

	idBuf   []string
	zoneBuf []*zones.Zone
}

// NewMonitor returns an empty monitor with c. Unusable times and
// severities are replaced by DefaultConfig's and counted as
// config_invalid; an invalid separation policy is kept (it is the
// caller's data) and counted, and every pair it refuses is counted.
func NewMonitor(c Config) *Monitor {
	m := &Monitor{
		follower:   sources.NewFollower(),
		aircraft:   make(map[string]*aircraft),
		active:     make(map[string]*alertState),
		nextSweepS: math.Inf(1),
	}
	for i := range m.pools {
		m.pools[i] = list.New()
	}
	m.cfg = sanitise(c, &m.counters)
	m.grid = cpa.NewGrid(m.cfg.GridCellM)
	m.cfg.Zones = slices.Clone(c.Zones)
	m.zoneIx = zones.NewIndex(m.cfg.Zones)
	m.zoneKey = zoneKeys(m.cfg.Zones, &m.counters)
	return m
}

// Counters returns the monitor's counters (see the Counter constants).
func (m *Monitor) Counters() *core.Counters { return &m.counters }

// Config returns the configuration in force, after NewMonitor replaced
// unusable values.
func (m *Monitor) Config() Config { return m.cfg }

// Tracked is the number of aircraft held.
func (m *Monitor) Tracked() int { return len(m.aircraft) }

// Observe takes one sample at the monitor's wall time wallS and returns
// what it raised and cleared. In order:
//
//  1. Admission: a sample with an empty id or a non-finite time is
//     rejected_invalid; from a disabled source rejected_source_disabled;
//     flagged backlog rejected_backlog; with wallS -
//     RxAtS > LiveMaxAgeS rejected_late; older on its source's clock than
//     the one held from that source and not received later
//     rejected_out_of_order; placed before the latest sample of the same
//     aircraft from any source rejected_older_than_held (T-06). A
//     rejected sample judges nothing.
//  2. Flying (C-05): a flying sample with a valid position is paired with
//     its neighbours and judged against the zones and the height limit.
//     A sample saying not flying clears the aircraft's alerts as landed
//     (except identification_mismatch); an unknown flying state holds
//     them, and they go stale if no flying sample follows.
//  3. Identity (G-02): identification_mismatch on every admitted sample.
//  4. Staleness: aircraft and identities not heard for StaleAfterS are
//     dropped and their alerts cleared as stale (evidence outranks
//     silence: an alert shown false for longer than the hysteresis clears
//     as resolved first).
func (m *Monitor) Observe(tr Track, wallS float64) Events {
	var ev Events
	if !core.IsFinite(wallS) {
		m.counters.Inc(CounterRejectedInvalid)
		return ev
	}
	if ac, ok := m.admit(&tr, wallS); ok {
		m.judge(ac, &tr, wallS, &ev)
	}
	m.sweep(wallS, &ev)
	return ev
}

// Tick drops stale aircraft and identities and clears their alerts as
// stale, with no message (T-10): the end of a condition can be silence.
// Call it periodically (once a second).
func (m *Monitor) Tick(wallS float64) Events {
	var ev Events
	if !core.IsFinite(wallS) {
		m.counters.Inc(CounterRejectedInvalid)
		return ev
	}
	m.sweep(wallS, &ev)
	return ev
}

// SwitchSource applies a published source-control state through a
// sources.Follower (B-09: within an epoch only a higher version is taken;
// a new epoch always is). When it is taken, every aircraft whose last
// admitted sample came from a source it disables is dropped at once and
// its alerts cleared as source_disabled (B-11); later samples from that
// source are rejected_source_disabled until it is enabled again, which
// replays nothing. A state not taken changes nothing and is counted.
func (m *Monitor) SwitchSource(st sources.State, wallS float64) Events {
	var ev Events
	if !m.follower.Apply(st) {
		m.counters.Inc(CounterSourceStateIgnored)
	} else {
		ids := make([]string, 0, len(m.aircraft))
		for id, ac := range m.aircraft {
			if !m.follower.Query(ac.src.typ, ac.src.instance()).Enabled {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			m.dropAircraft(m.aircraft[id], ClearSourceDisabled, &ev)
		}
	}
	if core.IsFinite(wallS) {
		m.sweep(wallS, &ev)
	}
	return ev
}

// Drop removes the aircraft id and clears every alert it is part of with
// reason, which the caller chooses: flight_ended when its flight ended,
// landed when it knows the aircraft landed (plan §11 gap 4). An empty
// reason is refused and counted; an unknown id changes nothing.
func (m *Monitor) Drop(id string, reason ClearReason, wallS float64) Events {
	var ev Events
	if reason == "" {
		m.counters.Inc(CounterDropWithoutReason)
		return ev
	}
	if ac, ok := m.aircraft[id]; ok {
		m.dropAircraft(ac, reason, &ev)
	}
	if core.IsFinite(wallS) {
		m.sweep(wallS, &ev)
	}
	return ev
}

// Active returns a copy of every active alert: the set the caller
// republishes every second and replays to a console that connects later
// (C-08). Ordered by severity (critical first), then, for conflicts, by
// the time to loss of separation (cpa.Result.LoSStartS, soonest first),
// then by key.
func (m *Monitor) Active() []Alert {
	states := make([]*alertState, 0, len(m.active))
	for _, s := range m.active {
		states = append(states, s)
	}
	slices.SortFunc(states, func(a, b *alertState) int {
		if d := severityRank(a.Severity) - severityRank(b.Severity); d != 0 {
			return d
		}
		if a.losStartS != b.losStartS {
			if a.losStartS < b.losStartS {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Key, b.Key)
	})
	out := make([]Alert, len(states))
	for i, s := range states {
		out[i] = s.snapshot()
	}
	return out
}

func severityRank(s core.Severity) int {
	switch s {
	case core.SeverityCritical:
		return 0
	case core.SeverityWarning:
		return 1
	case core.SeverityInfo:
		return 2
	}
	return 3
}

// CapacityExceeded reports whether the monitor is at Config.MaxAircraft
// with every aircraft holding an active alert, so that a new id is
// refused (rejected_capacity). A caller alerts on it: aircraft are going
// unjudged.
func (m *Monitor) CapacityExceeded() bool {
	if len(m.aircraft) < m.cfg.MaxAircraft {
		return false
	}
	for _, p := range m.pools {
		if p.Len() > 0 {
			return false
		}
	}
	return true
}

// record returns the aircraft id and marks it heard. A new id past
// MaxAircraft evicts one aircraft without an active alert (not flying
// first, then unidentified, then the least recently heard), counted as
// aircraft_evicted; when every aircraft holds an alert the new id is
// refused, counted as rejected_capacity, and record returns nil. An
// alert is never cleared to make room (owner decision on PR #15: a flood
// of spoofed ids must not clear a real conflict).
func (m *Monitor) record(id string) *aircraft {
	if ac, ok := m.aircraft[id]; ok {
		m.reclass(ac, true)
		return ac
	}
	if len(m.aircraft) >= m.cfg.MaxAircraft {
		victim := m.victim()
		if victim == nil {
			m.counters.Inc(CounterRejectedCapacity)
			return nil
		}
		m.counters.Inc(CounterAircraftEvicted)
		m.forget(victim)
	}
	ac := &aircraft{id: id, alerts: make(map[string]struct{}), placedS: math.Inf(-1), heardS: math.Inf(-1), pool: poolNone}
	m.aircraft[id] = ac
	m.reclass(ac, true)
	return ac
}

// victim is the aircraft to evict, or nil when every one holds an alert.
func (m *Monitor) victim() *aircraft {
	for _, p := range m.pools {
		if e := p.Front(); e != nil {
			return e.Value.(*aircraft)
		}
	}
	return nil
}

// reclass moves ac into the pool it belongs in now; touch also marks it
// the most recently heard of its pool.
func (m *Monitor) reclass(ac *aircraft, touch bool) {
	p := poolOf(ac)
	if p == ac.pool && !touch {
		return
	}
	if ac.elem != nil {
		m.pools[ac.pool].Remove(ac.elem)
		ac.elem = nil
	}
	ac.pool = p
	if p != poolNone {
		ac.elem = m.pools[p].PushBack(ac)
	}
}

// forget removes ac, which holds no alert, from every structure.
func (m *Monitor) forget(ac *aircraft) {
	m.grid.Remove(ac.id)
	if ac.elem != nil {
		m.pools[ac.pool].Remove(ac.elem)
		ac.elem = nil
	}
	delete(m.aircraft, ac.id)
}

// dropAircraft clears every alert ac is part of with reason and forgets
// it.
func (m *Monitor) dropAircraft(ac *aircraft, reason ClearReason, ev *Events) {
	m.clearAll(ac, reason, func(string) bool { return true }, ev)
	m.forget(ac)
}

// stopTrack takes ac out of the grid. Its alerts are cleared by the
// caller (landed) or by the sweep (stale).
func (m *Monitor) stopTrack(ac *aircraft) {
	ac.hasTrack = false
	m.grid.Remove(ac.id)
	m.reclass(ac, false)
}

// noteSeen lowers the next sweep time to when t + StaleAfterS passes.
func (m *Monitor) noteSeen(t float64) {
	m.nextSweepS = math.Min(m.nextSweepS, t+m.cfg.StaleAfterS)
}

// sweep drops tracks and identities not heard for StaleAfterS and clears
// their alerts as stale, and forgets aircraft with neither left
// once their last sample is as old. It does nothing before nextSweepS
// (the earliest time anything can be stale), so an Observe usually costs
// O(1) here.
func (m *Monitor) sweep(wallS float64, ev *Events) {
	if !(wallS > m.nextSweepS) {
		return
	}
	staleAfterS := m.cfg.StaleAfterS
	next := math.Inf(1)
	var lost, forget []*aircraft
	for _, ac := range m.aircraft {
		gone := false
		if ac.hasTrack {
			if wallS-ac.seenS > staleAfterS {
				m.stopTrack(ac)
				gone = true
			} else {
				next = math.Min(next, ac.seenS+staleAfterS)
			}
		}
		if ac.hasIdentity {
			if wallS-ac.identitySeenS > staleAfterS {
				ac.hasIdentity = false
				gone = true
			} else {
				next = math.Min(next, ac.identitySeenS+staleAfterS)
			}
		}
		if gone {
			lost = append(lost, ac)
		}
		if !ac.hasTrack && !ac.hasIdentity {
			if wallS-ac.heardS > staleAfterS {
				forget = append(forget, ac)
			} else {
				next = math.Min(next, ac.heardS+staleAfterS)
			}
		}
	}
	m.nextSweepS = next
	byID := func(a, b *aircraft) int { return strings.Compare(a.id, b.id) }
	slices.SortFunc(lost, byID)
	for _, ac := range lost {
		m.clearAll(ac, ClearStale, m.untracked, ev)
	}
	for _, ac := range forget {
		if len(ac.alerts) == 0 {
			m.forget(ac)
		}
	}
}

// untracked reports whether an aircraft of the alert key is no longer
// tracked for it: without a track for a conflict, zone, identification or
// height alert; without a fresh identity for identification_mismatch.
func (m *Monitor) untracked(key string) bool {
	s := m.active[key]
	for _, id := range s.Aircraft {
		o, ok := m.aircraft[id]
		switch {
		case !ok:
			return true
		case s.Kind == KindIdentificationMismatch:
			if !o.hasIdentity {
				return true
			}
		case !o.hasTrack:
			return true
		}
	}
	return false
}
