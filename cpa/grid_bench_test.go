package cpa

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

var sinkIDs []string

func benchGrid() (*Grid, []string, func(i int) []string) {
	g := NewGrid(1000)
	rng := rand.New(rand.NewPCG(5, 6))
	half := math.Sqrt(10e6) / 2 // a square of 10 km^2
	for i, p := range scatter(rng, origin, half, 1000) {
		g.Upsert(fmt.Sprint(i), p)
	}
	queries := scatter(rng, origin, half, 64)
	buf := make([]string, 0, 1000)
	return g, buf, func(i int) []string {
		return g.AppendNear(buf[:0], queries[i%len(queries)], DefaultPolicy.NeighbourRadiusM)
	}
}

// BenchmarkGridNeighbours is one lookup at the 800 m radius among 1000
// aircraft in 10 km^2 with 1000 m cells, into a reused buffer (PLAN §8.6:
// target 20 µs).
func BenchmarkGridNeighbours(b *testing.B) {
	_, _, lookup := benchGrid()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		sinkIDs = lookup(i)
		i++
	}
}

// BenchmarkGridNeighboursNear is the same lookup through Near, which
// allocates its result.
func BenchmarkGridNeighboursNear(b *testing.B) {
	g, _, _ := benchGrid()
	rng := rand.New(rand.NewPCG(7, 8))
	queries := scatter(rng, origin, math.Sqrt(10e6)/2, 64)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		sinkIDs = g.Near(queries[i%len(queries)], DefaultPolicy.NeighbourRadiusM)
		i++
	}
}
