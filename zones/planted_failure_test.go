package zones

import "testing"

// BenchmarkPlantedFailure is planted to prove that a failing benchmark
// fails the bench job.
func BenchmarkPlantedFailure(b *testing.B) {
	b.Fatal("planted failure: this benchmark must fail the job")
}
