package config

import (
	"strings"
	"testing"
)

func TestStandardRunSafetyCeilings(t *testing.T) {
	for _, mode := range []string{"closed", "arrival"} {
		t.Run(mode, func(t *testing.T) {
			atLimit := good()
			atLimit.Mode = mode
			atLimit.Users = 50
			atLimit.MaxRPS = 100
			atLimit.Duration = "3m"
			if mode == "arrival" {
				atLimit.ArrivalRate = 100
			}
			if _, err := Parse(atLimit, Options{Authorized: true}); err != nil {
				t.Fatalf("configured ceilings should be accepted: %v", err)
			}
			for _, tc := range []struct {
				field  string
				exceed func(*Scenario)
			}{
				{"users", func(s *Scenario) { s.Users++ }},
				{"max_rps", func(s *Scenario) { s.MaxRPS++ }},
				{"duration", func(s *Scenario) { s.Duration = "3m1ns" }},
			} {
				t.Run(tc.field, func(t *testing.T) {
					s := atLimit
					tc.exceed(&s)
					if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), tc.field) {
						t.Fatalf("expected rejection at %s ceiling, got %v", tc.field, err)
					}
				})
			}
		})
	}
}
