package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolSealWritesLocalPretrafficProtocol(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("..", "..", "examples", "capacity-protocol-draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	draft := filepath.Join(dir, "draft.json")
	sealed := filepath.Join(dir, "sealed.json")
	if err := os.WriteFile(draft, input, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := protocolCmd([]string{"seal", "--file", draft, "--out", sealed}, &stdout, &stderr); code != 0 {
		t.Fatalf("protocol sealing failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(sealed)
	if err != nil || !bytes.Contains(data, []byte(`"protocol_id": "sha256:`)) || !strings.Contains(stdout.String(), "Saved before campaign traffic: yes") {
		t.Fatalf("sealed protocol output is incomplete: data=%s err=%v stdout=%s", data, err, stdout.String())
	}
}

func TestCampaignDryRunValidatesOptionalHostSensorWithoutLeakingToken(t *testing.T) {
	t.Setenv("DAMOCLINE_TEST_SENSOR_TOKEN", "synthetic-secret-token")
	args := []string{"--config", filepath.Join("..", "..", "examples", "local-smoke.json"), "--dry-run"}
	var stdout, stderr bytes.Buffer
	if code := campaignCmd(append(args, "--host-sensor-endpoint", "http://127.0.0.1:9467/v1/host", "--host-sensor-token-env", "DAMOCLINE_TEST_SENSOR_TOKEN"), &stdout, &stderr); code != 0 {
		t.Fatalf("host sensor dry run failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "synthetic-secret-token") {
		t.Fatal("sensor token appeared in CLI output")
	}
	stdout.Reset()
	stderr.Reset()
	if code := campaignCmd(args, &stdout, &stderr); code != 0 {
		t.Fatalf("campaign without host sensor was rejected by the optional sensor default: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
