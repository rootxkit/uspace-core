package ed269

import (
	"bytes"
	"runtime"
	"testing"
)

// hostileOnes is the costliest input per byte for the parsed tree: a list
// of one-digit numbers.
func hostileOnes(n int) []byte {
	b := append([]byte("["), bytes.Repeat([]byte("1,"), n)...)
	b[len(b)-1] = ']'
	return b
}

// Z-06, E-10: the memory one input byte can cost is pinned roughly, so
// that the MaxBytes default keeps its documented bound. Measured with Go
// 1.27 on a 1 MiB list: about 80 bytes allocated per input byte; the
// test fails past 160 (twice that), well inside the 250 per byte the
// MaxBytes comment budgets for the whole process.
func TestMemoryPerInputByte(t *testing.T) {
	data := hostileOnes(512 << 10)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, probs := Parse(data, DefaultLimits)
	runtime.ReadMemStats(&after)
	if !hasProblem(probs, "$", "not a JSON object") {
		t.Fatalf("got %v", probs)
	}
	perByte := float64(after.TotalAlloc-before.TotalAlloc) / float64(len(data))
	t.Logf("%.0f bytes allocated per input byte", perByte)
	if perByte > 160 {
		t.Errorf("%.0f bytes allocated per input byte; measured about 80, bound 160", perByte)
	}
}
