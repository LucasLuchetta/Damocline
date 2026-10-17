package correlate

import (
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"testing"
)

func TestTimeWindowJoin(t *testing.T) {
	load := explorer.Result{Scenario: "fixture", Probes: []explorer.Probe{{Users: 2, Runs: []report.Result{{StartedAt: "2026-10-16T20:00:00Z", ElapsedSeconds: 10, Outcome: "PASS"}}}}}
	host := hostmon.Result{Source: "fixture", Samples: []hostmon.Sample{
		{At: "2026-10-16T19:59:59Z", CPUPercent: 99},
		{At: "2026-10-16T20:00:03Z", CPUPercent: 60, MemoryUsedBytes: 5000},
		{At: "2026-10-16T20:00:08Z", CPUPercent: 80, MemoryUsedBytes: 6000},
		{At: "2026-10-16T20:00:20Z", CPUPercent: 100},
	}}
	r, err := Analyze(load, host)
	if err != nil {
		t.Fatal(err)
	}
	w := r.Windows[0]
	if w.Samples != 2 || w.CPUAvgPercent != 70 || w.CPUPeakPercent != 80 || w.MemoryPeakBytes != 6000 {
		t.Fatalf("wrong correlation %+v", w)
	}
}
