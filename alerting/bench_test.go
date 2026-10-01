package alerting

import (
	"fmt"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/zones"
)

// splitmix is a small deterministic generator, so the benchmarks place
// the same aircraft on every run without math/rand.
type splitmix uint64

func (s *splitmix) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// unit is a uniform value in [0, 1).
func (s *splitmix) unit() float64 { return float64(s.next()>>11) / (1 << 53) }

// boxZone is a square zone of sideM metres centred northM, eastM from the
// origin.
func boxZone(id string, t core.ZoneType, northM, eastM, sideM float64) *zones.Zone {
	half := sideM / 2
	lat0 := originLatDeg + (northM-half)/metresPerDegLat
	lat1 := originLatDeg + (northM+half)/metresPerDegLat
	perLon := metresPerDegLat * math.Cos(originLatDeg*math.Pi/180)
	lon0 := originLonDeg + (eastM-half)/perLon
	lon1 := originLonDeg + (eastM+half)/perLon
	ring := geodesy.Ring{{LatDeg: lat0, LonDeg: lon0}, {LatDeg: lat0, LonDeg: lon1}, {LatDeg: lat1, LonDeg: lon1}, {LatDeg: lat1, LonDeg: lon0}, {LatDeg: lat0, LonDeg: lon0}}
	p := &geodesy.Polygon{Rings: []geodesy.Ring{ring}}
	return &zones.Zone{
		Identifier: id,
		Type:       t,
		Upper:      &zones.Limit{ValueM: 1000, Ref: core.RefAMSL},
		Polygon:    p,
		BBox:       p.BBox(),
	}
}

// fleet returns n hovering aircraft spread uniformly over a square of
// sideM metres, and five zones over it.
func fleet(n int, sideM float64) ([]Track, []*zones.Zone) {
	g := splitmix(1)
	perLon := metresPerDegLat * math.Cos(originLatDeg*math.Pi/180)
	tracks := make([]Track, n)
	for i := range tracks {
		northM, eastM := g.unit()*sideM, g.unit()*sideM
		alt := 500 + g.unit()*100
		tracks[i] = Track{
			ID:          fmt.Sprintf("U%04d", i),
			Pos:         core.LatLon{LatDeg: originLatDeg + northM/metresPerDegLat, LonDeg: originLonDeg + eastM/perLon},
			AltAMSLM:    &alt,
			AltSource:   core.AltGeodetic,
			VNMS:        g.unit()*10 - 5,
			VEMS:        g.unit()*10 - 5,
			Flying:      ptr(true),
			Source:      "relay",
			Station:     "gs-1",
			SourceTS:    new(float64),
			Transmitter: nil,
		}
	}
	types := []core.ZoneType{core.ZoneProhibited, core.ZoneReqAuthorization, core.ZoneConditional, core.ZoneUSpace, core.ZoneNoRestriction}
	zs := make([]*zones.Zone, len(types))
	for i, t := range types {
		zs[i] = boxZone(fmt.Sprintf("Z%d", i), t, g.unit()*sideM, g.unit()*sideM, sideM/4)
	}
	return tracks, zs
}

// benchObserve observes n aircraft at 1 Hz each, one sample per
// iteration, after a warm-up second that fills the grid.
func benchObserve(b *testing.B, n int, sideM float64) {
	tracks, zs := fleet(n, sideM)
	cfg := DefaultConfig()
	cfg.Zones = zs
	m := NewMonitor(cfg)
	stepS := 1.0 / float64(n)
	tS := 0.0
	observe := func(i int) {
		tr := &tracks[i%n]
		tr.CapturedAtS, tr.RxAtS = tS, tS
		*tr.SourceTS = tS
		m.Observe(*tr, tS)
		tS += stepS
	}
	for i := range n {
		observe(i)
	}
	neighbours := 0
	for i := range n {
		for _, id := range m.grid.Near(tracks[i].Pos, cfg.Policy.NeighbourRadiusM) {
			o := m.aircraft[id]
			if id != tracks[i].ID && horizontalDistanceM(tracks[i].Pos, o.state.Pos) <= cfg.Policy.NeighbourRadiusM {
				neighbours++
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		observe(i)
	}
	b.ReportMetric(float64(neighbours)/float64(n), "neighbours/op")
	b.ReportMetric(float64(len(m.active)), "active-alerts")
}

// BenchmarkMonitorObserve is plan §8.6's case: one observation with about
// ten neighbours within the 800 m radius and five zones (1000 aircraft
// over 14 km x 14 km; target 50 µs).
func BenchmarkMonitorObserve(b *testing.B) { benchObserve(b, 1000, 14_000) }

// BenchmarkDenseMonitorObserve is the brief's literal 1000 aircraft in
// 10 km^2: about 160 neighbours within the radius, some sixteen times the
// pair checks of the §8.6 case.
func BenchmarkDenseMonitorObserve(b *testing.B) { benchObserve(b, 1000, math.Sqrt(10e6)) }

// BenchmarkMonitorTick is a tick with 1000 live aircraft: nothing is
// stale, so it does not scan them.
func BenchmarkMonitorTick(b *testing.B) {
	tracks, _ := fleet(1000, 14_000)
	m := NewMonitor(DefaultConfig())
	for i := range tracks {
		tracks[i].CapturedAtS, tracks[i].RxAtS = 0, 0
		m.Observe(tracks[i], 0)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.Tick(1)
	}
}
