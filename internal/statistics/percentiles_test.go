package statistics

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

func TestBinomialProportionInterval95(t *testing.T) {
	for _, test := range []struct {
		name              string
		successes, trials int
		lower, upper      float64
	}{
		{name: "all pass with five repeats", successes: 5, trials: 5, lower: 0.4781762498950185, upper: 1},
		{name: "none pass with five repeats", successes: 0, trials: 5, lower: 0, upper: 0.5218237501049815},
		{name: "one of five", successes: 1, trials: 5, lower: 0.005050763379468057, upper: 0.7164179361180896},
	} {
		t.Run(test.name, func(t *testing.T) {
			lower, upper, err := BinomialProportionInterval95(test.successes, test.trials)
			if err != nil || math.Abs(lower-test.lower) > 1e-10 || math.Abs(upper-test.upper) > 1e-10 {
				t.Fatalf("interval=(%.15f, %.15f), err=%v; want (%.15f, %.15f)", lower, upper, err, test.lower, test.upper)
			}
		})
	}
	for _, counts := range [][2]int{{-1, 5}, {6, 5}, {0, 0}, {0, maxExactBinomialTrials + 1}} {
		if _, _, err := BinomialProportionInterval95(counts[0], counts[1]); err == nil {
			t.Fatalf("invalid counts (%d, %d) were accepted", counts[0], counts[1])
		}
	}
}

func TestPercentileRankInterval95ForP95(t *testing.T) {
	for _, tc := range []struct {
		samples int
		low     int
		high    int
	}{
		{samples: 200, low: 184, high: 197},
		{samples: 230, low: 212, high: 225},
		{samples: 1_000, low: 936, high: 964},
	} {
		low, high, err := PercentileRankInterval95(tc.samples, 0.95)
		if err != nil || low != tc.low || high != tc.high {
			t.Errorf("n=%d: got rank interval %d-%d, err=%v; want %d-%d", tc.samples, low, high, err, tc.low, tc.high)
		}
	}
}

func TestPercentileRankInterval95RejectsInadequateTailSamples(t *testing.T) {
	if _, _, err := PercentileRankInterval95(100, 0.95); err == nil {
		t.Fatal("p95 needs at least ten expected tail observations")
	}
	if _, _, err := PercentileRankInterval95(1_000, 1); err == nil {
		t.Fatal("invalid percentile accepted")
	}
	if _, _, err := PercentileRankInterval95(maxExactRankIntervalSamples+1, 0.95); err == nil {
		t.Fatal("sample count above the exact-analysis resource bound was accepted")
	}
}

func TestPercentileRankInterval95IIDCoverageAndClusteredUndercoverage(t *testing.T) {
	const (
		samples      = 1_000
		repetitions  = 2_000
		quantile     = 0.95
		clusterCount = 40
		clusterSize  = samples / clusterCount
	)
	low, high, err := PercentileRankInterval95(samples, quantile)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(0x5eed))
	iidCovered, clusteredCovered := 0, 0
	iid := make([]float64, samples)
	clustered := make([]float64, 0, samples)
	for repetition := 0; repetition < repetitions; repetition++ {
		for i := range iid {
			iid[i] = rng.Float64()
		}
		sort.Float64s(iid)
		if iid[low-1] <= quantile && iid[high-1] >= quantile {
			iidCovered++
		}

		clustered = clustered[:0]
		for block := 0; block < clusterCount; block++ {
			value := rng.Float64()
			for copy := 0; copy < clusterSize; copy++ {
				clustered = append(clustered, value)
			}
		}
		sort.Float64s(clustered)
		if clustered[low-1] <= quantile && clustered[high-1] >= quantile {
			clusteredCovered++
		}
	}
	iidCoverage := float64(iidCovered) / repetitions
	clusteredCoverage := float64(clusteredCovered) / repetitions
	if iidCoverage < 0.92 || iidCoverage > 0.98 {
		t.Fatalf("IID simulation coverage %.3f is outside the expected 95%% Monte Carlo band", iidCoverage)
	}
	if clusteredCoverage >= 0.5 {
		t.Fatalf("clustered simulation coverage %.3f unexpectedly resembles the IID interval guarantee", clusteredCoverage)
	}
}
