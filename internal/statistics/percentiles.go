package statistics

import (
	"errors"
	"math"
)

const twoSided95Z = 1.959963984540054

// PercentileRankInterval95 returns an approximate two-sided 95% interval for
// the one-indexed order-statistic ranks that bound a population percentile.
// It uses a normal approximation to the binomial order-statistic distribution.
// It is available only when both expected binomial groups have at least ten
// observations. Callers must state the IID assumption; HTTP observations from
// one VU or time period often violate it.
func PercentileRankInterval95(samples int, percentile float64) (low, high int, err error) {
	if samples < 1 || math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile <= 0 || percentile >= 1 {
		return 0, 0, errors.New("samples must be positive and percentile must be between 0 and 1")
	}
	left, right := float64(samples)*percentile, float64(samples)*(1-percentile)
	if left < 10 || right < 10 {
		return 0, 0, errors.New("too few samples for the normal rank approximation")
	}
	halfWidth := twoSided95Z * math.Sqrt(left*(1-percentile))
	low = int(math.Ceil(left - halfWidth))
	high = int(math.Ceil(left + halfWidth))
	if low < 1 {
		low = 1
	}
	if high > samples {
		high = samples
	}
	return low, high, nil
}
