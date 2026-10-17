package statistics

import (
	"math"
	"testing"
)

func TestZeroFailureUpperBoundIsNotZero(t *testing.T) {
	bound, err := UpperFailureRate95(0, 50)
	if err != nil || math.Abs(bound-0.058155079116972) > 1e-10 {
		t.Fatalf("zero failures from 50 observations: bound=%v err=%v", bound, err)
	}
	if bound <= 0.01 {
		t.Fatal("50 error-free requests cannot independently substantiate a 1% error budget at 95% confidence")
	}
}

func TestIndependentZeroErrorPlanningExample(t *testing.T) {
	n, err := ZeroFailureSamples95(0.01)
	if err != nil || n != 299 {
		t.Fatalf("expected 299 independent samples: n=%d err=%v", n, err)
	}
	below, err := UpperFailureRate95(0, n)
	if err != nil || below > 0.01 {
		t.Fatalf("sample target not enough: %.7f %v", below, err)
	}
	above, _ := UpperFailureRate95(0, n-1)
	if above <= 0.01 {
		t.Fatalf("smaller sample must not satisfy bound: %.7f", above)
	}
}

func TestWilsonBoundsRemainWithinUnitInterval(t *testing.T) {
	for _, tc := range []struct{failed,total int}{{1,100},{3,50},{50,100},{99,100},{100,100}} {
		v, err := UpperFailureRate95(tc.failed, tc.total)
		if err != nil || v < float64(tc.failed)/float64(tc.total) || v > 1 {
			t.Fatalf("invalid failure-rate bound for %+v: %.8f %v", tc, v, err)
		}
	}
}

func TestInvalidCountsAndBudgetsRejected(t *testing.T) {
	for _, bad := range [][2]int{{0,0},{-1,5},{6,5}} {
		if _, err := UpperFailureRate95(bad[0], bad[1]); err == nil {
			t.Fatalf("accepted invalid counts %v", bad)
		}
	}
	for _, bad := range []float64{-0.01,0,1,math.NaN()} {
		if _, err := ZeroFailureSamples95(bad); err == nil {
			t.Fatalf("accepted invalid budget %v", bad)
		}
	}
}
