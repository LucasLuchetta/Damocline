package generatorhealth

import "testing"

// Run this benchmark alone to limit unrelated process CPU activity.
// The CPU counter includes runtime activity attributable to the batch.
// Use a fixed batch large enough to exceed the OS counter's resolution:
//
//	go test ./internal/generatorhealth -run '^$' -bench '^BenchmarkSampleRuntime$' -benchtime=30000x -count=3 -cpu=1 -benchmem
//
// A missing CPU metric means the counter was unavailable or did not advance.
func BenchmarkSampleRuntime(b *testing.B) {
	before, _, beforeOK := readProcessHealth()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampleRuntime()
	}
	b.StopTimer()
	after, _, afterOK := readProcessHealth()
	if beforeOK && afterOK && after > before {
		b.ReportMetric((after-before)*1e9/float64(b.N), "process-cpu-ns/op")
	}
}
