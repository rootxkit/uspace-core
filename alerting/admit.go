package alerting

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// admit decides whether tr is judged at all (T-03, T-04, T-05, T-06,
// B-11, C-09), counts each refusal under its own name, and on admission returns
// the aircraft record with tr's source ordering and source noted. A
// refused sample judges nothing and therefore clears nothing.
func (m *Monitor) admit(tr *Track, wallS float64) (*aircraft, bool) {
	if tr.ID == "" || !core.IsFinite(tr.CapturedAtS) || !core.IsFinite(tr.RxAtS) ||
		(tr.SourceTS != nil && !core.IsFinite(*tr.SourceTS)) {
		m.counters.Inc(CounterRejectedInvalid)
		return nil, false
	}
	src := sourceKey{typ: tr.Source, station: tr.Station}
	if !m.follower.Query(src.typ, src.instance()).Enabled {
		// No aircraft is held from a disabled source: SwitchSource dropped
		// them when it took the state.
		m.counters.Inc(CounterRejectedSourceDisabled)
		return nil, false
	}
	if tr.Backlog {
		m.counters.Inc(CounterRejectedBacklog)
		return nil, false
	}
	// The lateness bound is on the leg from the ingest to here only, never
	// on the source's clock (T-05).
	if wallS-tr.RxAtS > m.cfg.LiveMaxAgeS {
		m.counters.Inc(CounterRejectedLate)
		return nil, false
	}
	// A clock ahead (T-06 and timeplace's clock-ahead rule): a sample
	// placed ahead of its receipt, or received ahead of our wall time, is
	// never admitted. Admitted, it would pin the track in the future (every
	// real sample after it older than held) and its placement would count
	// as hysteresis time it never earned.
	if tr.CapturedAtS-tr.RxAtS > m.cfg.AheadToleranceS || tr.RxAtS-wallS > m.cfg.AheadToleranceS {
		m.counters.Inc(CounterRejectedPlacedAhead)
		return nil, false
	}
	ac, known := m.aircraft[tr.ID]
	if known && tr.SourceTS != nil {
		if e := ac.orderOf(src); e != nil && *tr.SourceTS < e.sourceTS && tr.CapturedAtS <= e.capturedAtS {
			m.counters.Inc(CounterRejectedOutOfOrder)
			return nil, false
		}
	}
	// A newer state is never overwritten by an older one, whatever the
	// source (T-06): CapturedAtS is on the ingest's clock, so samples of
	// one aircraft from any source compare. The caller keeps it as history.
	if known && tr.CapturedAtS < ac.placedS {
		m.counters.Inc(CounterRejectedOlderPlacement)
		return nil, false
	}
	if ac = m.record(tr.ID); ac == nil {
		return nil, false
	}
	if tr.RxAtS-tr.CapturedAtS > m.cfg.StaleAfterS {
		m.counters.Inc(CounterPlacementBehindStale)
	}
	ac.src = src
	ac.placedS = tr.CapturedAtS
	ac.heardS = math.Max(ac.heardS, math.Min(tr.CapturedAtS, wallS))
	m.noteSeen(ac.heardS)
	if tr.SourceTS != nil {
		m.noteOrder(ac, src, *tr.SourceTS, tr.CapturedAtS)
	}
	return ac, true
}

// orderOf returns the ordering entry of src, or nil.
func (ac *aircraft) orderOf(src sourceKey) *orderEntry {
	for i := range ac.order {
		if ac.order[i].src == src {
			return &ac.order[i]
		}
	}
	return nil
}

// noteOrder records the latest sample of src for ac, evicting the least
// recently updated source past MaxSourcesPerAircraft (E-10).
func (m *Monitor) noteOrder(ac *aircraft, src sourceKey, sourceTS, capturedAtS float64) {
	m.serial++
	if e := ac.orderOf(src); e != nil {
		*e = orderEntry{src: src, sourceTS: sourceTS, capturedAtS: capturedAtS, updateSerial: m.serial}
		return
	}
	if len(ac.order) >= m.cfg.MaxSourcesPerAircraft {
		oldest := 0
		for i := range ac.order {
			if ac.order[i].updateSerial < ac.order[oldest].updateSerial {
				oldest = i
			}
		}
		ac.order = append(ac.order[:oldest], ac.order[oldest+1:]...)
		m.counters.Inc(CounterSourceOrderEvicted)
	}
	ac.order = append(ac.order, orderEntry{src: src, sourceTS: sourceTS, capturedAtS: capturedAtS, updateSerial: m.serial})
}
