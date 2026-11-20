package config

import (
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

func extendedRequest() *ExtendedEnvelopeRequest {
	return &ExtendedEnvelopeRequest{Acknowledged: true, TargetClass: TargetClassLab}
}

func TestExtendedEnvelopeRefusedWithoutOptIn(t *testing.T) {
	s := good()
	s.Users = 51
	if _, err := Parse(s, Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "users") {
		t.Fatalf("standard envelope accepted 51 users: %v", err)
	}
	for name, req := range map[string]*ExtendedEnvelopeRequest{
		"no acknowledgement": {TargetClass: TargetClassLab},
		"no target class":    {Acknowledged: true},
		"production class":   {Acknowledged: true, TargetClass: "production"},
	} {
		if _, err := Parse(s, Options{Authorized: true, ExtendedEnvelope: req}); err == nil || !strings.Contains(err.Error(), "extended envelope") {
			t.Fatalf("%s: expected refusal, got %v", name, err)
		}
	}
	prod := s
	prod.Environment = "production-eu"
	if _, err := Parse(prod, Options{Authorized: true, ExtendedEnvelope: extendedRequest()}); err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("production environment label accepted: %v", err)
	}
}

func TestExtendedEnvelopeHardMaximum(t *testing.T) {
	for _, mode := range []string{"closed", "arrival"} {
		t.Run(mode, func(t *testing.T) {
			atLimit := good()
			atLimit.Mode = mode
			atLimit.Users = 500
			atLimit.MaxRPS = 1000
			atLimit.Duration = "15m"
			if mode == "arrival" {
				atLimit.ArrivalRate = 1000
			}
			v, err := Parse(atLimit, Options{Authorized: true, ExtendedEnvelope: extendedRequest()})
			if err != nil {
				t.Fatalf("hard maximum should be accepted: %v", err)
			}
			if v.Envelope.Mode != EnvelopeExtended || !v.ExceedsStandard() || !v.GeneratorGuardrails.Enabled() ||
				v.GeneratorGuardrails.MaxProcessCPUPercent != ExtendedDefaultMaxCPUPercent || v.GeneratorGuardrails.MaxProcessRSSBytes != ExtendedDefaultMaxRSSBytes {
				t.Fatalf("extended plan lacks envelope or default guardrails: %+v %+v", v.Envelope, v.GeneratorGuardrails)
			}
			for _, tc := range []struct {
				field  string
				exceed func(*Scenario)
			}{
				{"users", func(s *Scenario) { s.Users++ }},
				{"max_rps", func(s *Scenario) { s.MaxRPS++ }},
				{"duration", func(s *Scenario) { s.Duration = "15m1ns" }},
			} {
				s := atLimit
				tc.exceed(&s)
				if _, err := Parse(s, Options{Authorized: true, ExtendedEnvelope: extendedRequest()}); err == nil || !strings.Contains(err.Error(), tc.field) {
					t.Fatalf("expected rejection above hard %s maximum, got %v", tc.field, err)
				}
			}
		})
	}
}

func TestStandardPlanRecordsStandardEnvelope(t *testing.T) {
	v, err := Parse(good(), Options{Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if v.Envelope.Mode != EnvelopeStandard || v.Envelope.Limits != StandardEnvelopeLimits || v.GeneratorGuardrails.Enabled() {
		t.Fatalf("standard plan changed: %+v %+v", v.Envelope, v.GeneratorGuardrails)
	}
}

func TestExtendedGuardrailsMayOnlyTighten(t *testing.T) {
	g, err := ExtendedGuardrails(generatorhealth.Guardrails{MaxProcessCPUPercent: 50, ConsecutiveSamples: 2})
	if err != nil || g.MaxProcessCPUPercent != 50 || g.MaxProcessRSSBytes != ExtendedDefaultMaxRSSBytes || g.ConsecutiveSamples != 2 {
		t.Fatalf("tightened guardrails = %+v, %v", g, err)
	}
	for _, loose := range []generatorhealth.Guardrails{{MaxProcessCPUPercent: 95}, {MaxProcessRSSBytes: ExtendedDefaultMaxRSSBytes + 1}, {ConsecutiveSamples: 21}} {
		if _, err := ExtendedGuardrails(loose); err == nil {
			t.Fatalf("loosened guardrails accepted: %+v", loose)
		}
	}
}
