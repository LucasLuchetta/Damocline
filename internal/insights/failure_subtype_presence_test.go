package insights

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readGoldenRoot(t *testing.T, version string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "reports", "schema-v"+version+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeRoot(t *testing.T, root map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func httpDiagnostics(root map[string]any) []map[string]any {
	out := []map[string]any{root["metrics"].(map[string]any)["http_diagnostics"].(map[string]any)}
	for _, step := range root["steps"].([]any) {
		out = append(out, step.(map[string]any)["metrics"].(map[string]any)["http_diagnostics"].(map[string]any))
	}
	return out
}

// setDNSFailure turns the golden's single local construction failure into one
// classified DNS failure so the subtype partition must be non-empty.
func setDNSFailure(root map[string]any) {
	for _, metrics := range []map[string]any{root["metrics"].(map[string]any), root["steps"].([]any)[0].(map[string]any)["metrics"].(map[string]any)} {
		metrics["request_construction_failures"] = 0
		metrics["failure_reasons"] = map[string]any{"dns_failure": 1}
		d := metrics["http_diagnostics"].(map[string]any)
		d["dns_failures"] = 1
		d["transport_failures"] = 1
	}
}

func TestReadRunRequiresReconciledFailureSubtypesFromSchema34(t *testing.T) {
	t.Run("classified_dns_failure_with_subtype_is_readable", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		setDNSFailure(root)
		for _, d := range httpDiagnostics(root) {
			d["failure_subtypes"].(map[string]any)["dns_not_found"] = 1
		}
		got, err := ReadRun(writeRoot(t, root))
		if err != nil {
			t.Fatalf("reconciled schema-v34 DNS subtype was rejected: %v", err)
		}
		if got.Metrics.HTTP.FailureSubtypes.DNSNotFound != 1 || got.Steps[0].Metrics.HTTP.FailureSubtypes.DNSNotFound != 1 {
			t.Fatalf("DNS subtype was not preserved: %+v", got.Metrics.HTTP.FailureSubtypes)
		}
	})
	t.Run("missing_subtypes_with_dns_failure_is_rejected", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		setDNSFailure(root)
		for _, d := range httpDiagnostics(root) {
			delete(d, "failure_subtypes")
		}
		if _, err := ReadRun(writeRoot(t, root)); err == nil || !strings.Contains(err.Error(), "failure-classification") {
			t.Fatalf("schema-v34 DNS failure without subtypes was accepted: %v", err)
		}
	})
	t.Run("subtype_partition_mismatch_is_rejected", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		setDNSFailure(root)
		for _, d := range httpDiagnostics(root) {
			d["failure_subtypes"].(map[string]any)["tls_other"] = 1
		}
		if _, err := ReadRun(writeRoot(t, root)); err == nil {
			t.Fatal("schema-v34 subtype counts that do not partition DNS/TLS totals were accepted")
		}
	})
	t.Run("negative_subtype_is_rejected", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		for _, d := range httpDiagnostics(root) {
			subtypes := d["failure_subtypes"].(map[string]any)
			subtypes["dns_other"] = -1
			subtypes["dns_temporary"] = 1
		}
		if _, err := ReadRun(writeRoot(t, root)); err == nil {
			t.Fatal("negative schema-v34 subtype counter was accepted")
		}
	})
	t.Run("zero_totals_without_subtypes_remain_readable", func(t *testing.T) {
		root := readGoldenRoot(t, "34")
		for _, d := range httpDiagnostics(root) {
			delete(d, "failure_subtypes")
		}
		if _, err := ReadRun(writeRoot(t, root)); err != nil {
			t.Fatalf("schema-v34 zero DNS/TLS totals fully determine subtypes but were rejected: %v", err)
		}
	})
	t.Run("pre_v34_reports_must_not_claim_subtypes", func(t *testing.T) {
		root := readGoldenRoot(t, "33")
		for _, d := range httpDiagnostics(root) {
			d["failure_subtypes"] = map[string]any{"dns_not_found": 0}
		}
		if _, err := ReadRun(writeRoot(t, root)); err == nil {
			t.Fatal("schema-v33 report carrying v34-only failure subtypes was accepted")
		}
		legacy, err := ReadRun(filepath.Join("testdata", "reports", "schema-v33.json"))
		if err != nil || legacy.Metrics.HTTP.FailureSubtypes != nil {
			t.Fatalf("schema-v33 subtypes must remain unknown (absent), not zero: %v %+v", err, legacy.Metrics.HTTP.FailureSubtypes)
		}
	})
}
