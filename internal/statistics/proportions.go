// Package statistics implements bounded descriptive uncertainty summaries.
// These calculations assume independent Bernoulli outcomes and are not a
// substitute for time-separated trials or correlated-load analysis.
package statistics

import (
	"errors"
	"math"
)

const (
	oneSided95Z = 1.6448536269514722
)

// UpperFailureRate95 returns a one-sided 95% upper confidence bound for a
// binomial failure proportion. For zero observed failures the bound is exact
// Clopper-Pearson; otherwise it uses the Wilson score approximation.
// The caller must disclose independence and stationarity assumptions.
func UpperFailureRate95(failures, total int) (float64, error) {
	if total <= 0 || failures < 0 || failures > total {
		return 0, errors.New("counts require 0 <= failures <= total and total > 0")
	}
	if failures == total {
		return 1, nil
	}
	if failures == 0 {
		return -math.Expm1(math.Log(0.05) / float64(total)), nil
	}
	n := float64(total)
	p := float64(failures) / n
	z2 := oneSided95Z * oneSided95Z
	denom := 1 + z2/n
	center := (p + z2/(2*n)) / denom
	margin := (oneSided95Z / denom) * math.Sqrt(p*(1-p)/n + z2/(4*n*n))
	return math.Min(1, center+margin), nil
}

// ZeroFailureSamples95 is a planning illustration: the minimum independent,
// zero-failure observations needed before the exact one-sided 95% binomial
// upper bound can be <= maxFailureRate. Real HTTP samples often correlate.
func ZeroFailureSamples95(maxFailureRate float64) (int, error) {
	if maxFailureRate <= 0 || maxFailureRate >= 1 || math.IsNaN(maxFailureRate) {
		return 0, errors.New("error budget must be greater than 0 and less than 1")
	}
	required := math.Ceil(math.Log(0.05) / math.Log1p(-maxFailureRate))
	if math.IsInf(required, 0) || required > float64(math.MaxInt) {
		return 0, errors.New("error budget requires too many independent samples")
	}
	return int(required), nil
}
