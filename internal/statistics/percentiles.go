package statistics

import (
	"errors"
	"math"
)

const maxExactRankIntervalSamples = 1_000_000

// PercentileRankInterval95 returns a conservative two-sided 95% interval for
// the one-indexed order-statistic ranks that bound a population percentile.
// Coverage is calculated from the exact binomial order-statistic distribution.
// It is available only when both expected binomial groups have at least ten
// observations and the sample count is within the analysis resource bound.
// Callers must state the IID assumption; HTTP observations from one VU or time
// period often violate it.
func PercentileRankInterval95(samples int, percentile float64) (low, high int, err error) {
	if samples < 1 || math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile <= 0 || percentile >= 1 {
		return 0, 0, errors.New("samples must be positive and percentile must be between 0 and 1")
	}
	if samples > maxExactRankIntervalSamples {
		return 0, 0, errors.New("sample count exceeds the exact percentile interval resource limit")
	}
	left, right := float64(samples)*percentile, float64(samples)*(1-percentile)
	if left < 10 || right < 10 {
		return 0, 0, errors.New("too few samples for the percentile rank interval screen")
	}

	// Start at the binomial mode and recurse in both directions. Scaling the
	// mode probability to one avoids underflow in the probability mass itself;
	// normalization below recovers the CDF and remains stable for bounded n.
	mode := int(math.Floor(float64(samples+1) * percentile))
	probabilities := make([]float64, samples+1)
	probabilities[mode] = 1
	for k := mode; k > 0; k-- {
		probabilities[k-1] = probabilities[k] * (float64(k) / float64(samples-k+1)) * ((1 - percentile) / percentile)
	}
	for k := mode; k < samples; k++ {
		probabilities[k+1] = probabilities[k] * (float64(samples-k) / float64(k+1)) * (percentile / (1 - percentile))
	}
	total := 0.0
	for _, probability := range probabilities {
		total += probability
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, 0, errors.New("could not normalize exact binomial order-statistic probabilities")
	}

	const tailProbability = 0.025
	cumulative := 0.0
	low, high = 0, 0
	for k, probability := range probabilities {
		cumulative += probability / total
		if cumulative <= tailProbability {
			low = k + 1
		}
		if high == 0 && cumulative >= 1-tailProbability {
			high = k + 1
		}
	}
	if low < 1 || high < low || high > samples {
		return 0, 0, errors.New("exact percentile interval is outside the observed order statistics")
	}
	return low, high, nil
}
