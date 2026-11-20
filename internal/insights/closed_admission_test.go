package insights

import (
	"strings"
	"testing"
)

func TestEvaluateDoesNotFlagClosedLoopZeroAdmission(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 17
	r.ArrivalsScheduled = 20
	r.IterationsStarted = 20
	zero := int64(0)
	r.ArrivalsAdmitted = &zero
	for _, f := range Evaluate(r, nil).Findings {
		if strings.Contains(f.Text, "Scheduler") && strings.Contains(f.Text, "admitted") {
			t.Fatalf("closed-loop run got admission finding %q", f.Text)
		}
	}
}

func TestEvaluateFlagsOpenArrivalAdmissionMismatch(t *testing.T) {
	r := synthetic()
	r.SchemaVersion = 17
	r.Config.Mode = "arrival"
	r.ArrivalsScheduled = 20
	r.IterationsStarted = 20
	admitted := int64(19)
	r.ArrivalsAdmitted = &admitted
	for _, f := range Evaluate(r, nil).Findings {
		if f.Level == "INVALID" && strings.Contains(f.Text, "do not reconcile") {
			return
		}
	}
	t.Fatal("open-arrival admission mismatch was not flagged INVALID")
}
