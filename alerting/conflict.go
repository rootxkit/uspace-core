package alerting

import (
	"math"
	"slices"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/cpa"
	"github.com/rootxkit/uspace-core/geodesy"
)

// Conflict counter names.
const (
	// CounterPairsNotJudged counts pairs cpa.Evaluate did not judge (a
	// stale neighbour, invalid input, an invalid policy, out of range);
	// each is also counted under this name plus "_" plus the cpa.Reason.
	// The pair's alert is neither raised, refreshed nor shown false.
	CounterPairsNotJudged = "conflict_pairs_not_judged"
	// CounterPairsSameTransmitter counts pairs not judged because they are
	// one Remote ID transmitter under two ids, one of them unidentified
	// (I-02).
	CounterPairsSameTransmitter = "pairs_same_transmitter"
)

// judge runs the checks on an admitted sample (C-05).
func (m *Monitor) judge(ac *aircraft, tr *Track, wallS float64, ev *Events) {
	switch {
	case tr.Flying == nil:
		// Unknown is not flying (C-05), and not a landing either: the
		// track is neither refreshed nor dropped, so its alerts are held
		// and go stale unless a flying sample follows.
		m.counters.Inc(CounterFlyingUnknown)
	case !*tr.Flying:
		// Disarmed, or declared on the ground: a landing (C-14).
		if ac.hasTrack {
			m.stopTrack(ac)
		}
		m.clearAll(ac, ClearLanded, m.notMismatch, ev)
	case !tr.Pos.Valid():
		m.counters.Inc(CounterInvalidPosition)
	default:
		m.judgeFlying(ac, tr, wallS, ev)
	}
}

// notMismatch selects every alert but identification_mismatch, which is
// about who, not where, and is judged on the ground too (G-02).
func (m *Monitor) notMismatch(key string) bool {
	return m.active[key].Kind != KindIdentificationMismatch
}

// judgeFlying holds a flying sample with a valid position as the
// aircraft's track and judges it.
func (m *Monitor) judgeFlying(ac *aircraft, tr *Track, wallS float64, ev *Events) {
	atS := tr.CapturedAtS
	ac.state = stateOf(tr)
	ac.transmitter, ac.unidentified = "", isFalse(tr.Identified)
	if tr.Transmitter != nil {
		ac.transmitter = *tr.Transmitter
	}
	ac.hasTrack = true
	ac.seenS = math.Min(atS, wallS)
	m.noteSeen(ac.seenS)
	m.grid.Upsert(ac.id, tr.Pos)
	m.judgeConflicts(ac, atS, ev)
}

// verticalKnown reports whether tr's altitude is a vertical position
// comparable with another aircraft's: a finite geodetic or network
// altitude. A pressure altitude (R-09), AltNone and an unknown source are
// judged on the horizontal alone.
func verticalKnown(tr *Track) bool {
	if tr.AltAMSLM == nil || !core.IsFinite(*tr.AltAMSLM) {
		return false
	}
	return tr.AltSource == core.AltGeodetic || tr.AltSource == core.AltNetwork
}

func stateOf(tr *Track) cpa.State {
	s := cpa.State{
		Pos:         tr.Pos,
		VNMS:        tr.VNMS,
		VEMS:        tr.VEMS,
		VDMS:        tr.VDMS,
		CapturedAtS: tr.CapturedAtS,
	}
	if verticalKnown(tr) {
		s.AltAMSLM = *tr.AltAMSLM
		s.VerticalKnown = true
	}
	return s
}

// sameRadio reports whether two tracks are one transmitter under two ids:
// the same address, one of them explicitly unidentified (I-02). Two
// identified tracks on one address are two claims and are judged (I-04).
func sameRadio(a, b *aircraft) bool {
	return a.transmitter != "" && a.transmitter == b.transmitter && (a.unidentified || b.unidentified)
}

func isFalse(b *bool) bool { return b != nil && !*b }

// conflictKey is conflict:<a>:<b> with the ids sorted.
func conflictKey(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return keyOf(KindConflict, a, b)
}

// partner is the other aircraft of a conflict.
func partner(s *alertState, id string) string {
	for _, o := range s.Aircraft {
		if o != id {
			return o
		}
	}
	return ""
}

// judgeConflicts pairs ac with every tracked aircraft within the
// neighbour radius (cpa.Grid.Near, then the tangent-plane distance), and
// with every aircraft it has an active conflict with wherever it is now,
// so that a conflict is shown false only by a judgement. Each pair is
// judged by cpa.Evaluate at the later of the two samples:
//
//   - a conflict raises once and refreshes the detail silently (C-06);
//   - a judged non-conflict shows an active alert false;
//   - a pair not judged (a stale neighbour, bad numbers) neither refreshes
//     nor shows false, and is counted (C-04, C-09);
//   - one transmitter under two ids is never paired (I-02).
func (m *Monitor) judgeConflicts(ac *aircraft, atS float64, ev *Events) {
	pol := m.cfg.Policy
	ids := m.grid.AppendNear(m.idBuf[:0], ac.state.Pos, pol.NeighbourRadiusM)
	for key := range ac.alerts {
		if s := m.active[key]; s.Kind == KindConflict {
			ids = append(ids, partner(s, ac.id))
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	m.idBuf = ids
	for _, id := range ids {
		o, ok := m.aircraft[id]
		if id == ac.id || !ok || !o.hasTrack {
			continue
		}
		key := conflictKey(ac.id, id)
		_, active := m.active[key]
		if !active && !(horizontalDistanceM(ac.state.Pos, o.state.Pos) <= pol.NeighbourRadiusM) {
			continue
		}
		if sameRadio(ac, o) {
			m.counters.Inc(CounterPairsSameTransmitter)
			continue
		}
		r := cpa.Evaluate(ac.state, o.state, pol)
		switch {
		case !r.Judged:
			m.counters.Inc(CounterPairsNotJudged)
			m.counters.Inc(CounterPairsNotJudged + "_" + string(r.NotJudged))
		case r.Conflict:
			m.refresh(conflictRaise(key, ac.id, id, r), atS, ev)
		case active:
			m.showFalse(key, atS, ev)
		}
	}
}

// horizontalDistanceM is the tangent-plane distance between two current
// positions, the neighbour-radius filter of the old index.
func horizontalDistanceM(a, b core.LatLon) float64 {
	northM, eastM := geodesy.LocalOffsetAboutMidLatM(a, b)
	return math.Hypot(northM, eastM)
}

// conflictRaise is the conflict alert of a judged pair (C-03). Detail
// names as alert_lifecycle.json; d_alt_at_cpa_m is nil when the vertical
// is unknown (R-09). los_start_s is cpa.Result.LoSStartS, the time to
// loss of separation (0 when inside the minima now), which the vectors
// predate.
func conflictRaise(key, a, b string, r cpa.Result) raise {
	if b < a {
		a, b = b, a
	}
	var dAltAtCPAM any
	if r.VerticalKnown {
		dAltAtCPAM = r.DAltAtCPAM
	}
	return raise{
		key:      key,
		kind:     KindConflict,
		severity: core.SeverityCritical,
		aircraft: []string{a, b},
		detail: map[string]any{
			"t_cpa_s":                   r.TCPAS,
			"d_cpa_horizontal_m":        r.DCPAHorizontalM,
			"d_alt_at_cpa_m":            dAltAtCPAM,
			"d_horizontal_now_m":        r.DHorizontalNowM,
			"vertical_separation_known": r.VerticalKnown,
			"los_start_s":               r.LoSStartS,
		},
		losStartS: r.LoSStartS,
	}
}
