package capacityplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSealProtocolRequiresAndBindsGeneratorControls(t *testing.T) {
	p := CapacityProtocol{
		Scenario: "synthetic", Target: "http://127.0.0.1", Environment: "local",
		WorkloadRevision: "workload-v1", ApplicationRevision: "app-v1",
		WorkloadFingerprint: "sha256:" + strings.Repeat("a", 64), MaxRPS: 100, RequestTimeout: "3s",
		CandidateUsers: 2, FirstFailingUsers: 8, RequiredConfirmationCampaigns: 2,
		Warmup: "0s", MinimumObservation: "10s", SpikeDuration: "0s", Recovery: "10s",
		MaxP95MS: 1000, MaxErrorRate: .01, ReservePercent: 20, PeakDemandUsers: 1,
		Preparation: "Use isolated synthetic target.", StopConditions: []string{"Stop on invalid generator scheduling."},
	}
	sealed, err := SealProtocol(p, time.Date(2026, 10, 22, 21, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if sealed.SchemaVersion != 2 || VerifyProtocol(sealed) != nil {
		t.Fatalf("new protocol did not seal and verify: %#v", sealed)
	}
	sealed.RequestTimeout = "4s"
	if err := VerifyProtocol(sealed); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("editing generator ceiling should invalidate protocol digest, got %v", err)
	}
	p.MaxRPS = 101
	if _, err := SealProtocol(p, time.Now()); err == nil {
		t.Fatal("protocol accepted max_rps above the generator safety limit")
	}
}

func TestVerifyHistoricalV1ProtocolDigest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "protocol-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var protocol CapacityProtocol
	if err := json.Unmarshal(data, &protocol); err != nil {
		t.Fatal(err)
	}
	if protocol.SchemaVersion != 1 {
		t.Fatalf("fixture schema = %d, want 1", protocol.SchemaVersion)
	}
	if err := VerifyProtocol(protocol); err != nil {
		t.Fatalf("historical v1 protocol digest failed verification: %v", err)
	}
}
