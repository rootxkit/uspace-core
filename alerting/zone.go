package alerting

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/zones"
)

// maxPlacedS bounds the placed times converted for zone applicability:
// beyond about year 5000 the time is taken as unknown, and an unknown
// time applies every zone (fail-safe, zones.Zone.AppliesAt).
const maxPlacedS = 1e11

// placedTime is the UTC instant of a placed time in seconds since the
// Unix epoch (T-09), or the zero time when it is out of range.
func placedTime(s float64) time.Time {
	if !(math.Abs(s) < maxPlacedS) {
		return time.Time{}
	}
	sec, frac := math.Modf(s)
	return time.Unix(int64(sec), int64(math.Round(frac*1e9))).UTC()
}

// zoneKey is how alert keys name a zone: its country and its identifier,
// which ED-269 makes unique within a country.
type zoneKey struct {
	country, identifier string
}

// zoneKeys names each zone in alert keys by country and identifier. A
// second zone with the same country and identifier (a true duplicate)
// gets "#" and its position in the list appended to its identifier, as
// often as needed to be unique, and each such fallback is counted as
// zone_key_duplicate: two zones never share a key.
func zoneKeys(zs []*zones.Zone, counters *core.Counters) map[*zones.Zone]zoneKey {
	keys := make(map[*zones.Zone]zoneKey, len(zs))
	used := make(map[zoneKey]bool, len(zs))
	for i, z := range zs {
		if z == nil {
			continue
		}
		k := zoneKey{country: z.Country, identifier: z.Identifier}
		if used[k] {
			counters.Inc(CounterZoneKeyDuplicate)
			for used[k] {
				k.identifier += "#" + strconv.Itoa(i)
			}
		}
		used[k] = true
		keys[z] = k
	}
	return keys
}

// isPlaceKind reports whether an alert kind is judged from the aircraft's
// position by judgeZones.
func isPlaceKind(kind string) bool {
	return kind == KindZone || kind == KindIdentification || kind == KindHeight
}

// judgeZones judges a flying sample against every candidate zone and the
// height limit (T-09, Z-09, Z-10, R-09, G-03):
//
//   - a zone that contains the position, applies at the placed time and
//     raises (zones.JudgeVertical) refreshes zone:<country>:<identifier>:<id>; inside a
//     PROHIBITED or REQ_AUTHORISATION zone an unidentified or
//     unknown_operator aircraft also refreshes
//     identification:<country>:<identifier>:<id>;
//   - a zone that cannot be judged (a containment error, NotEvaluated) or
//     an identification that is absent holds its alerts: neither
//     refreshed nor shown false (C-09);
//   - every other zone, identification and height alert of the aircraft
//     was judged false by this sample and is shown false.
func (m *Monitor) judgeZones(ac *aircraft, tr *Track, atS float64, ev *Events) {
	var kept []string
	ac2 := zones.Aircraft{AltAMSLM: tr.AltAMSLM, AltSource: tr.AltSource}
	at := placedTime(atS)
	cands := m.zoneIx.AppendCandidates(m.zoneBuf[:0], tr.Pos)
	m.zoneBuf = cands
	for _, z := range cands {
		zk := m.zoneKey[z]
		in, err := z.ContainsHorizontally(tr.Pos)
		if err == nil && (!in || !z.AppliesAt(at)) {
			continue
		}
		key := keyOf(KindZone, zk.country, zk.identifier, ac.id)
		idKey := keyOf(KindIdentification, zk.country, zk.identifier, ac.id)
		if err != nil {
			m.counters.Inc(CounterZoneNotEvaluated)
			kept = append(kept, key, idKey)
			continue
		}
		res := zones.JudgeVertical(z, ac2, tr.Env, m.cfg.ZonePolicy)
		res.Count(&m.counters)
		if res.NotEvaluated {
			kept = append(kept, key, idKey)
			continue
		}
		if res.Raise == nil {
			continue
		}
		m.refresh(zoneRaise(key, ac.id, res.Raise), atS, ev)
		kept = append(kept, key)
		if !z.Type.IncidentZone() {
			continue
		}
		switch ident := tr.Identification; {
		case ident == nil:
			kept = append(kept, idKey)
		case ident.Status.IncidentStatus():
			m.refresh(m.identificationRaise(idKey, ac.id, res.Raise, ident), atS, ev)
			kept = append(kept, idKey)
		}
	}
	if m.cfg.ZonePolicy.MaxHeightAGLM != nil {
		key := keyOf(KindHeight, ac.id)
		res := zones.JudgeHeightLimit(ac2, tr.Env, m.cfg.ZonePolicy)
		res.Count(&m.counters)
		switch {
		case res.NotEvaluated:
			kept = append(kept, key)
		case res.Raise != nil:
			m.refresh(zoneRaise(key, ac.id, res.Raise), atS, ev)
			kept = append(kept, key)
		}
	}
	var falseKeys []string
	for key := range ac.alerts {
		if isPlaceKind(m.active[key].Kind) && !slices.Contains(kept, key) {
			falseKeys = append(falseKeys, key)
		}
	}
	slices.Sort(falseKeys)
	for _, key := range falseKeys {
		m.showFalse(key, atS, nil, ev)
	}
}

// zoneRaise is a zone or height alert from a zones.Raise. The detail
// holds identifier and restriction (zone alerts) and each optional member
// zones.Detail set, under its JSON name.
func zoneRaise(key, id string, r *zones.Raise) raise {
	d := r.Detail
	detail := make(map[string]any, 4)
	if r.Kind == zones.KindZone {
		detail["identifier"] = d.Identifier
		detail["restriction"] = d.Restriction
	}
	putBool := func(name string, v *bool) {
		if v != nil {
			detail[name] = *v
		}
	}
	putFloat := func(name string, v *float64) {
		if v != nil {
			detail[name] = *v
		}
	}
	putBool("vertical_known", d.VerticalKnown)
	putBool("within_band", d.WithinBand)
	putBool("limit_not_judged", d.LimitNotJudged)
	if d.NotJudged != nil {
		detail["not_judged"] = slices.Clone(d.NotJudged)
	}
	putFloat("height_agl_m", d.HeightAGLM)
	putFloat("alt_hae_m", d.AltHAEM)
	putFloat("max_height_agl_m", d.MaxHeightAGLM)
	return raise{key: key, kind: r.Kind, severity: r.Severity, aircraft: []string{id}, detail: detail}
}

// identificationRaise is the G-03 alert beside a zone alert: an aircraft
// nobody can name where one must be named, the seam where the caller
// opens an incident.
func (m *Monitor) identificationRaise(key, id string, zr *zones.Raise, ident *core.Identification) raise {
	return raise{
		key:      key,
		kind:     KindIdentification,
		severity: m.cfg.IdentificationSeverity,
		aircraft: []string{id},
		detail: map[string]any{
			"identifier":            zr.Detail.Identifier,
			"restriction":           zr.Detail.Restriction,
			"status":                string(ident.Status),
			"identification_reason": string(ident.Reason),
		},
	}
}

// judgeMismatch judges identification_mismatch on every admitted sample,
// flying or not, with or without a position (G-02): raised while the
// identification says mismatch, shown false by one that says otherwise,
// and stale with the aircraft's identity, not its track. A sample with no
// identification judges nothing and does not keep the identity fresh.
func (m *Monitor) judgeMismatch(ac *aircraft, tr *Track, wallS float64, ev *Events) {
	ident := tr.Identification
	if ident == nil {
		return
	}
	atS := tr.CapturedAtS
	seenS := math.Min(atS, wallS)
	if ac.hasIdentity {
		seenS = math.Max(ac.identitySeenS, seenS)
	}
	ac.hasIdentity = true
	ac.identitySeenS = seenS
	m.noteSeen(ac.identitySeenS)
	key := keyOf(KindIdentificationMismatch, ac.id)
	if !ident.Mismatch {
		m.showFalse(key, atS, nil, ev)
		return
	}
	m.refresh(raise{
		key:      key,
		kind:     KindIdentificationMismatch,
		severity: m.cfg.MismatchSeverity,
		aircraft: []string{ac.id},
		detail: map[string]any{
			"status":                string(ident.Status),
			"identification_reason": string(ident.Reason),
		},
	}, atS, ev)
}
