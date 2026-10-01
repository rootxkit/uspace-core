package geodesy

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

var (
	sinkF float64
	sinkB bool
)

func BenchmarkVincentyInverse(b *testing.B) {
	p, q := ll(41.7151, 44.8271), ll(41.7239, 44.8401)
	for b.Loop() {
		d, _, _, err := Inverse(p, q)
		if err != nil {
			b.Fatal(err)
		}
		sinkF = d
	}
}

func BenchmarkHaversine(b *testing.B) {
	p, q := ll(41.7151, 44.8271), ll(41.7239, 44.8401)
	for b.Loop() {
		sinkF = HaversineM(p, q)
	}
}

func BenchmarkLocalOffset(b *testing.B) {
	p, q := ll(41.7151, 44.8271), ll(41.7239, 44.8401)
	for b.Loop() {
		n, e := LocalOffsetM(p, q)
		sinkF = n + e
	}
}

// circleRing is an n-vertex closed ring approximating a circle.
func circleRing(center core.LatLon, radiusDeg float64, n int) Ring {
	r := make(Ring, 0, n+1)
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		r = append(r, ll(center.LatDeg+radiusDeg*math.Sin(a), center.LonDeg+radiusDeg*math.Cos(a)))
	}
	return append(r, r[0])
}

func BenchmarkInPolygon100(b *testing.B) {
	c := ll(41.71, 44.81)
	p := Polygon{Rings: []Ring{circleRing(c, 0.05, 99), circleRing(c, 0.01, 4)}}
	pt := ll(41.73, 44.82)
	if !p.Contains(pt) {
		b.Fatal("benchmark point not inside")
	}
	for b.Loop() {
		sinkB = p.Contains(pt)
	}
}

func BenchmarkInCircle(b *testing.B) {
	c := Circle{Center: ll(41.71, 44.81), RadiusM: 500}
	pt := ll(41.71448761185305, 44.81)
	for b.Loop() {
		in, _, err := c.Contains(pt)
		if err != nil {
			b.Fatal(err)
		}
		sinkB = in
	}
}
