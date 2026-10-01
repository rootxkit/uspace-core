package f3411

import "testing"

// BenchmarkUnmarshalRIDFlight decodes one full flight (current state with
// position, height and accuracies, one recent position): what a Display
// Provider does per flight per poll (docs/bench-targets.txt).
func BenchmarkUnmarshalRIDFlight(b *testing.B) {
	raw := readExample(b, "rid_flight.json")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := UnmarshalRIDFlight(raw); err != nil {
			b.Fatal(err)
		}
	}
}
