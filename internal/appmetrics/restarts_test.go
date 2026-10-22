package appmetrics

import "testing"

func TestObserveProcessRestartsUsesStableSeriesAndReportsCoverage(t *testing.T) {
	first := &Timeline{Samples: []TimedSample{
		{FinishedAt: "2026-01-01T00:00:00Z", Samples: []Sample{{Name: "process_start_time_seconds", Value: 100, SeriesID: "web-a"}}},
		{FinishedAt: "2026-01-01T00:00:01Z", Samples: []Sample{{Name: "process_start_time_seconds", Value: 100, SeriesID: "web-a"}}},
	}}
	second := &Timeline{Samples: []TimedSample{
		{FinishedAt: "2026-01-01T00:00:02Z", Samples: []Sample{{Name: "process_start_time_seconds", Value: 200, SeriesID: "web-a"}}},
		{FinishedAt: "2026-01-01T00:00:03Z", Samples: []Sample{{Name: "process_start_time_seconds", Value: 300, SeriesID: "web-b"}}},
	}}
	evidence := ObserveProcessRestarts([]*Timeline{first, second})
	if evidence.Coverage != "COMPLETE" || evidence.Samples != 4 || evidence.Series != 2 || evidence.ObservedRestarts != 1 {
		t.Fatalf("unexpected restart evidence: %+v", evidence)
	}
	second.FailedCollections = 1
	if partial := ObserveProcessRestarts([]*Timeline{first, second}); partial.Coverage != "PARTIAL" {
		t.Fatalf("partial metrics coverage was reported complete: %+v", partial)
	}
}
