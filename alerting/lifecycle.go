package alerting

import (
	"maps"
	"slices"
	"strings"

	"github.com/rootxkit/uspace-core/core"
)

// alertState is an active alert and what the monitor needs to judge it.
type alertState struct {
	Alert
	// losStartS ranks conflicts in Active (cpa.Result.LoSStartS); 0 for
	// other kinds.
	losStartS float64
}

// snapshot is a copy of the alert the caller can keep and change.
func (s *alertState) snapshot() Alert {
	a := s.Alert
	a.Aircraft = slices.Clone(s.Aircraft)
	a.Detail = maps.Clone(s.Detail)
	return a
}

// resolved reports whether messages have shown the condition false for
// more than clearAfterS since it was last true (C-06). An alert never
// shown false is not resolved, whatever its times say.
func (s *alertState) resolved(clearAfterS float64) bool {
	return s.ShownFalse && s.LastFalseS-s.LastTrueS > clearAfterS
}

// keyEscaper escapes the separator and the escape character of a key
// part, so that two different id tuples never give one key.
var keyEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

// keyOf joins a kind and escaped parts with ':'.
func keyOf(kind string, parts ...string) string {
	n := len(kind)
	for _, p := range parts {
		n += 1 + len(p)
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteString(kind)
	for _, p := range parts {
		b.WriteByte(':')
		if strings.ContainsAny(p, "%:") {
			p = keyEscaper.Replace(p)
		}
		b.WriteString(p)
	}
	return b.String()
}

// raise is one finding of a check: the alert is true at atS with this
// severity and detail.
type raise struct {
	key       string
	kind      string
	severity  core.Severity
	aircraft  []string
	detail    map[string]any
	losStartS float64
}

// refresh holds the finding as true at atS. A new key is raised; a known
// key is refreshed silently with the new detail, and raised again when
// its severity changed (C-06, C-07).
func (m *Monitor) refresh(r raise, atS float64, ev *Events) {
	s, ok := m.active[r.key]
	if !ok {
		s = &alertState{Alert: Alert{
			Key:       r.key,
			Kind:      r.kind,
			Severity:  r.severity,
			Aircraft:  r.aircraft,
			Detail:    r.detail,
			RaisedAtS: atS,
			LastTrueS: atS,
		}, losStartS: r.losStartS}
		m.active[r.key] = s
		for _, id := range r.aircraft {
			m.aircraft[id].alerts[r.key] = struct{}{}
		}
		ev.Raised = append(ev.Raised, s.snapshot())
		return
	}
	s.Detail = r.detail
	s.LastTrueS = atS
	s.losStartS = r.losStartS
	if sev := r.severity; sev != s.Severity {
		s.Severity = sev
		ev.Raised = append(ev.Raised, s.snapshot())
	}
}

// showFalse records that a message judged the condition false at atS,
// and clears the alert as resolved once the hysteresis has passed. A
// check that could not judge never calls it (C-09, hysteresis_rule).
func (m *Monitor) showFalse(key string, atS float64, ev *Events) {
	s, ok := m.active[key]
	if !ok {
		return
	}
	s.LastFalseS = atS
	s.ShownFalse = true
	if s.resolved(m.cfg.ClearAfterS) {
		m.clear(s, ClearResolved, ev)
	}
}

// clear removes s and reports it cleared with reason.
func (m *Monitor) clear(s *alertState, reason ClearReason, ev *Events) {
	delete(m.active, s.Key)
	for _, id := range s.Aircraft {
		if ac, ok := m.aircraft[id]; ok {
			delete(ac.alerts, s.Key)
		}
	}
	ev.Cleared = append(ev.Cleared, Cleared{Alert: s.snapshot(), Reason: reason})
}

// clearAll clears, in key order, every alert of ac that match selects,
// with reason. An alert the evidence had resolved is never among them:
// showFalse clears it as resolved the moment the hysteresis passes, before
// any sweep, landing or switch can (evidence outranks silence, C-06).
func (m *Monitor) clearAll(ac *aircraft, reason ClearReason, match func(key string) bool, ev *Events) {
	if len(ac.alerts) == 0 {
		return
	}
	keys := make([]string, 0, len(ac.alerts))
	for key := range ac.alerts {
		if match(key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		m.clear(m.active[key], reason, ev)
	}
}
