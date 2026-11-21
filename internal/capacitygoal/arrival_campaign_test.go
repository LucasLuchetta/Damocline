package capacitygoal

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/arrivalcampaign"
)

func TestEvaluateJourneyCampaignReadsHistoricalRobustPass(t *testing.T) {
	saved := readHistoricalRobustPass(t)
	assessment, err := EvaluateJourneyCampaign(saved, JourneyStartGoal{PeakStartsPerSecond: saved.BaseStartsPerSecond})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("historical verdict gave assessment %q, want INSUFFICIENT_EVIDENCE", assessment.State)
	}
	if assessment.MatchedStartsPerSecond != nil || assessment.MatchedTrials != 0 {
		t.Fatalf("historical verdict must not claim a sustained target: %+v", assessment)
	}
	robustness := assessment.P95Robustness
	if robustness == nil || robustness.State != arrivalcampaign.P95Inconclusive {
		t.Fatalf("expected recomputed inconclusive robustness, got %+v", robustness)
	}
	if robustness.Independence.State != arrivalcampaign.TrendNotAssessed ||
		len(robustness.Reasons) != 1 || robustness.Reasons[0] != robustness.Independence.Reason {
		t.Fatalf("the unavailable trend screen must explain the downgrade: %+v", robustness)
	}
}

func TestEvaluateJourneyCampaignRejectsTamperedHistoricalRobustPass(t *testing.T) {
	saved := readHistoricalRobustPass(t)
	// Preserve the historical pass and trend labels. Alter only a stored
	// numerical claim that the auditor must reconcile with native evidence.
	saved.P95Robustness.Windows[0].UpperMS++
	_, err := EvaluateJourneyCampaign(saved, JourneyStartGoal{PeakStartsPerSecond: saved.BaseStartsPerSecond})
	if err == nil || !strings.Contains(err.Error(), "saved p95 robustness verdict disagrees with native report data") {
		t.Fatalf("expected the altered bound to fail robustness verification, got %v", err)
	}
}

// The fixture was recorded against a synthetic loopback HTTP server using
// the original p95 verdict implementation from 7df217b.
func readHistoricalRobustPass(t *testing.T) arrivalcampaign.Result {
	t.Helper()
	file, err := os.Open("testdata/arrival-campaign-v11-robust-pass.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := arrivalcampaign.ValidateArtifactJSON(data); err != nil {
		t.Fatal(err)
	}
	var saved arrivalcampaign.Result
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	robustness := saved.P95Robustness
	if saved.SchemaVersion != 11 || saved.Repeats != 2 || saved.Outcome != "PASS" ||
		robustness == nil || robustness.State != arrivalcampaign.P95RobustPass ||
		robustness.Independence.State != arrivalcampaign.TrendNotAssessed || len(robustness.Windows) == 0 {
		t.Fatal("fixture must be a completed schema-v11 campaign with the original robust pass")
	}
	return saved
}
