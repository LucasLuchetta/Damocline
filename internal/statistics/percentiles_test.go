package statistics

import "testing"

func TestPercentileRankInterval95ForP95(t *testing.T) {
	low, high, err := PercentileRankInterval95(1_000, 0.95)
	if err != nil || low != 937 || high != 964 {
		t.Fatalf("unexpected rank interval: %d-%d, err=%v", low, high, err)
	}
}

func TestPercentileRankInterval95RejectsInadequateTailSamples(t *testing.T) {
	if _, _, err := PercentileRankInterval95(100, 0.95); err == nil {
		t.Fatal("p95 needs at least ten expected tail observations")
	}
	if _, _, err := PercentileRankInterval95(1_000, 1); err == nil {
		t.Fatal("invalid percentile accepted")
	}
}
