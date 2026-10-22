package statistics

import (
	"errors"
	"math"
)

const maxExactRankIntervalSamples = 1_000_000
const MaxExactPercentileRankIntervalSamples = maxExactRankIntervalSamples
const maxExactBinomialTrials = 10_000
const minimumExpectedTailObservations = 10

// MinimumSamplesForPercentileRankInterval95 returns the smallest sample size
// allowed by the exact rank-interval screen. Both expected binomial groups
// (below and above the requested percentile) must contain at least ten samples.
func MinimumSamplesForPercentileRankInterval95(percentile float64) (int, error) {
	if math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile <= 0 || percentile >= 1 {
		return 0, errors.New("percentile must be between 0 and 1")
	}
	below := int(math.Ceil(minimumExpectedTailObservations / percentile))
	above := int(math.Ceil(minimumExpectedTailObservations / (1 - percentile)))
	minimum := max(below, above)
	if minimum > maxExactRankIntervalSamples {
		return 0, errors.New("percentile rank interval sample requirement exceeds the exact-analysis resource limit")
	}
	return minimum, nil
}

// BinomialProportionInterval95 returns a two-sided exact Clopper-Pearson
// interval for a binomial success probability. Trials must be independent
// Bernoulli observations; callers must retain and state that assumption.
func BinomialProportionInterval95(successes, trials int) (lower, upper float64, err error) {
	if trials < 1 || trials > maxExactBinomialTrials || successes < 0 || successes > trials {
		return 0, 0, errors.New("successes must be between zero and the bounded positive trial count")
	}
	const tail = 0.025
	if successes == 0 {
		lower = 0
	} else {
		left, right := 0.0, 1.0
		for range 64 {
			mid := (left + right) / 2
			if binomialUpperTail(trials, successes, mid) >= tail {
				right = mid
			} else {
				left = mid
			}
		}
		lower = (left + right) / 2
	}
	if successes == trials {
		upper = 1
	} else {
		left, right := 0.0, 1.0
		for range 64 {
			mid := (left + right) / 2
			if binomialCDF(trials, successes, mid) >= tail {
				left = mid
			} else {
				right = mid
			}
		}
		upper = (left + right) / 2
	}
	return lower, upper, nil
}

func binomialUpperTail(trials, successes int, p float64) float64 {
	return binomialRangeProbability(trials, successes, trials, p)
}

func binomialCDF(trials, successes int, p float64) float64 {
	return binomialRangeProbability(trials, 0, successes, p)
}

func binomialRangeProbability(trials, first, last int, p float64) float64 {
	if p <= 0 {
		if first == 0 {
			return 1
		}
		return 0
	}
	if p >= 1 {
		if last == trials {
			return 1
		}
		return 0
	}
	logs := make([]float64, 0, last-first+1)
	maxLog := math.Inf(-1)
	for k := first; k <= last; k++ {
		logP, _ := math.Lgamma(float64(trials + 1))
		logK, _ := math.Lgamma(float64(k + 1))
		logOther, _ := math.Lgamma(float64(trials - k + 1))
		value := logP - logK - logOther + float64(k)*math.Log(p) + float64(trials-k)*math.Log1p(-p)
		logs = append(logs, value)
		if value > maxLog {
			maxLog = value
		}
	}
	sum := 0.0
	for _, value := range logs {
		sum += math.Exp(value - maxLog)
	}
	return math.Exp(maxLog) * sum
}

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
	minimum, err := MinimumSamplesForPercentileRankInterval95(percentile)
	if err != nil || samples < minimum {
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
