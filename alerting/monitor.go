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
	order  []orderEntry
	// alerts holds the keys of the active alerts this aircraft is part of.
	alerts map[string]struct{}
	elem   *list.Element
}

// Monitor is the alert state machine for one set of tracks (one cell set,
// spec 05 §3). It is not safe for concurrent use: one goroutine owns a
// Monitor; the caller partitions aircraft between monitors.
type Monitor struct {
	cfg      Config
	grid     *cpa.Grid
	zoneIx   *zones.Index
	zoneKey  map[*zones.Zone]string
	follower *sources.Follower
	aircraft map[string]*aircraft
	lru      *list.List
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
		lru:        list.New(),
		active:     make(map[string]*alertState),
		nextSweepS: math.Inf(1),
	}
	m.cfg = sanitise(c, &m.counters)
	m.grid = cpa.NewGrid(m.cfg.GridCellM)
	m.cfg.Zones = slices.Clone(c.Zones)
	m.zoneIx = zones.NewIndex(m.cfg.Zones)
	m.zoneKey = zoneKeys(m.cfg.Zones)
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
//     rejected_out_of_order. A rejected sample judges nothing.
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
	if ac, ok := m.admit(&tr, wallS, &ev); ok {
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

// record returns the aircraft id, creating it (and evicting the least
// recently heard past MaxAircraft) when new, and marks it heard.
func (m *Monitor) record(id string, ev *Events) *aircraft {
	if ac, ok := m.aircraft[id]; ok {
		m.lru.MoveToBack(ac.elem)
		return ac
	}
	for len(m.aircraft) >= m.cfg.MaxAircraft {
		oldest := m.lru.Front().Value.(*aircraft)
		m.counters.Inc(CounterAircraftEvicted)
		m.dropAircraft(oldest, ClearEvicted, ev)
	}
	ac := &aircraft{id: id, alerts: make(map[string]struct{})}
	ac.elem = m.lru.PushBack(ac)
	m.aircraft[id] = ac
	return ac
}

// dropAircraft forgets ac and clears every alert it is part of with
// reason.
func (m *Monitor) dropAircraft(ac *aircraft, reason ClearReason, ev *Events) {
	m.clearAll(ac, reason, func(string) bool { return true }, ev)
	m.grid.Remove(ac.id)
	m.lru.Remove(ac.elem)
	delete(m.aircraft, ac.id)
}

// stopTrack takes ac out of the grid. Its alerts are cleared by the
// caller (landed) or by the sweep (stale).
func (m *Monitor) stopTrack(ac *aircraft) {
	ac.hasTrack = false
	m.grid.Remove(ac.id)
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
			m.lru.Remove(ac.elem)
			delete(m.aircraft, ac.id)
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
