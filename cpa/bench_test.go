package cpa

import "testing"

var sinkResult Result

// BenchmarkEvaluate is one pair check with fresh samples (PLAN §8.6:
// target 2 µs, no allocation).
func BenchmarkEvaluate(b *testing.B) {
	p := at(-300, 0, 550).moving(10, 0, 0)
	q := at(0, 500, 545).moving(0, -10, 0.5)
	b.ReportAllocs()
	for b.Loop() {
		sinkResult = Evaluate(p, q, DefaultPolicy)
	}
}

// BenchmarkEvaluateAdvanced is the same pair with the older sample
// advanced first, the slower path.
func BenchmarkEvaluateAdvanced(b *testing.B) {
	p := at(-300, 0, 550).moving(10, 0, 0)
	q := at(0, 500, 545).moving(0, -10, 0.5).capturedAt(3)
	b.ReportAllocs()
	for b.Loop() {
		sinkResult = Evaluate(p, q, DefaultPolicy)
	}
}
