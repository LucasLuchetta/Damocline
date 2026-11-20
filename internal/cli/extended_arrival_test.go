package cli

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func TestExtendedArrivalGateChecksEveryRateAtFixedWorkers(t *testing.T) {
	old := assessExtendedCoverage
	t.Cleanup(func() { assessExtendedCoverage = old })
	var seen []int
	assessExtendedCoverage = func(s config.Scenario, record calibration.Record, _ calibration.Runtime, _ time.Time) calibration.ExtendedCoverage {
		if s.Users != 60 || record.SelfCalibrationFingerprint != "fixture" {
			t.Fatalf("unexpected planned workers or fingerprint: %d %q", s.Users, record.SelfCalibrationFingerprint)
		}
		seen = append(seen, s.ArrivalRate)
		if s.ArrivalRate == 150 {
			return calibration.ExtendedCoverage{Status: "BLOCKED", Reason: "fixture rate absent"}
		}
		return calibration.ExtendedCoverage{Status: "READY"}
	}
	v := config.Validated{
		Scenario: config.Scenario{Mode: "arrival", Users: 60, MaxRPS: 200},
		Timeout:  time.Second, WorkloadFingerprint: "fixture",
		Envelope: config.EnvelopeRecord{Mode: config.EnvelopeExtended, TargetClass: config.TargetClassLab},
	}
	record := &calibration.ScenarioRecord{
		Controls: calibration.ScenarioControls{Mode: "arrival", MaxRPS: 200, RequestTimeout: "1s"},
	}
	if err := gateExtendedArrivalRates(&v, record, []int{120, 150}); err == nil || v.Envelope.CalibrationCoverage != "" {
		t.Fatalf("uncovered rate opened the gate: %v", err)
	}
	if got := fmt.Sprint(seen); got != "[120 150]" {
		t.Fatalf("expected exact rates, got %s", got)
	}
	if err := gateExtendedArrivalRates(&v, record, []int{120}); err != nil || v.Envelope.CalibrationCoverage != "READY" {
		t.Fatalf("covered rate was refused: %v", err)
	}
}

func TestExtendedArrivalCommandsRequireAuthorizationAndCalibration(t *testing.T) {
	for _, command := range []string{"explore-arrival", "arrival-campaign"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run([]string{
				command, "--config", "synthetic.json", "--dry-run", "--extended-envelope",
				"--extended-load-authorized", "--target-class", "lab",
			}, &stdout, &stderr); code != 2 || !bytes.Contains(stderr.Bytes(), []byte("--require-calibration")) {
				t.Fatalf("expected extended gate refusal: %d %s", code, &stderr)
			}
		})
	}
}
