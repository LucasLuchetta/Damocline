package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/calibration"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/insights"
	"github.com/LucasLuchetta/Damocline/internal/selfcalibration"
)

func writeCapacityScenario(t *testing.T, dir string, maxRPS int) string {
	t.Helper()
	path := filepath.Join(dir, "scenario.json")
	data, err := json.Marshal(config.Scenario{
		Name: "fixture", BaseURL: "http://127.0.0.1:18089", Users: 2, Duration: "20s",
		P95LimitMS: 750, MaxErrorRate: .01, MaxRPS: maxRPS,
		BaselinePath: "/baseline", Steps: []config.Step{{Name: "fast", Path: "/fast"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCapacityDryRunPrintsFullPlanAndWallTime(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var out, errOut bytes.Buffer
	code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "8"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("dry run failed: %d %s", code, &errOut)
	}
	text := out.String()
	for _, want := range []string{"1. Network baseline", "2. damocline calibrate for levels [2 4 8]", "3. Explore levels [2 4 8] with --calibration and --require-calibration", "4. Campaign", "5. Offline audit", "Total planned wall time at most", "Zero-error sample floor: 299"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry run output missing %q:\n%s", want, text)
		}
	}
}

func TestCapacityWarnsWhenMaxRPSCapsRequestedLevel(t *testing.T) {
	reference := filepath.Join("..", "insights", "testdata", "reports", "schema-v39.json")
	var out bytes.Buffer
	if err := capacityRateWarning(&out, reference, 50, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "WARNING: max_rps 1 would cap throughput below the 50 VU level") {
		t.Fatalf("missing rate-cap warning: %s", &out)
	}
	out.Reset()
	if err := capacityRateWarning(&out, reference, 1, 1000); err != nil || strings.Contains(out.String(), "WARNING") {
		t.Fatalf("unexpected warning under a generous cap: %s %v", &out, err)
	}
}

func TestCapacityRateWarningDoesNotExtrapolateCappedReference(t *testing.T) {
	reference := filepath.Join("..", "insights", "testdata", "reports", "schema-v39.json")
	run, err := insights.ReadRun(reference)
	if err != nil {
		t.Fatal(err)
	}
	run.Config.MaxRPS = 1
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	reference = filepath.Join(t.TempDir(), "reference.json")
	if err := os.WriteFile(reference, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := capacityRateWarning(&out, reference, 1, 10); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "its own max_rps 1") ||
		!strings.Contains(out.String(), "planned max_rps 10") ||
		strings.Contains(out.String(), "VUs imply") {
		t.Fatalf("expected a capped-reference warning without a forecast: %s", out.String())
	}
}

func TestCapacityRateWarningNearCeiling(t *testing.T) {
	reference := filepath.Join("..", "insights", "testdata", "reports", "schema-v39.json")
	run, err := insights.ReadRun(reference)
	if err != nil {
		t.Fatal(err)
	}
	const users = 100
	expected := float64(run.Metrics.Total) / run.ElapsedSeconds / float64(run.Config.Users) * users
	ceiling := int(expected) + 1
	var out bytes.Buffer
	if err := capacityRateWarning(&out, reference, users, ceiling); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "the exploration may be inconclusive") {
		t.Fatalf("missing near-ceiling warning: %s", out.String())
	}
}

func TestCapacityRequiresMirrorSourceAndExtendedGateFlags(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	cases := map[string][]string{
		"no mirror":     {"--config", scenario, "--authorized", "--output-dir", filepath.Join(dir, "run")},
		"extended bare": {"--config", scenario, "--dry-run", "--extended-envelope"},
		"stray class":   {"--config", scenario, "--dry-run", "--target-class", "lab"},
		"both mirrors":  {"--config", scenario, "--dry-run", "--reference-report", "r.json", "--response-bytes", "10"},
	}
	for name, args := range cases {
		var errOut bytes.Buffer
		if code := capacityCmd(args, &bytes.Buffer{}, &errOut); code != 2 {
			t.Fatalf("%s: expected refusal, got %d", name, code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "run")); !os.IsNotExist(err) {
		t.Fatal("refused execution created an output directory")
	}
	var out bytes.Buffer
	if code := capacityCmd([]string{"--config", scenario, "--dry-run", "--max-users", "32", "--extended-envelope", "--extended-load-authorized", "--target-class", "lab"}, &out, &bytes.Buffer{}); code != 0 ||
		!strings.Contains(out.String(), "One calibration for ladder, refinement and campaign candidates") ||
		!strings.Contains(out.String(), "Explore and campaign use the extended envelope (target class lab)") {
		t.Fatalf("extended dry run: %d %s", code, &out)
	}
}

func TestCapacityCalibrationSizingProposesAdjustments(t *testing.T) {
	if _, err := capacityCalibrationWall(calibration.ScenarioCalibrationMaxLevels+1, time.Second); err == nil || !strings.Contains(err.Error(), "--max-users") {
		t.Fatalf("too many levels: %v", err)
	}
	if _, err := capacityCalibrationWall(10, 2*time.Minute); err == nil || !strings.Contains(err.Error(), "--calibration-level-duration") {
		t.Fatalf("over wall ceiling: %v", err)
	}
	if wall, err := capacityCalibrationWall(3, 20*time.Second); err != nil || wall != selfcalibration.PlannedWallTime(3, 20*time.Second) {
		t.Fatalf("fitting plan: %s %v", wall, err)
	}
}

func TestCapacityCalibrationFailureStopsBeforeTargetTraffic(t *testing.T) {
	dir := t.TempDir()
	scenario := writeCapacityScenario(t, dir, 100)
	var calls int
	var gotLevels []int
	previous := selfCalibrationRun
	selfCalibrationRun = func(_ context.Context, _ config.Scenario, opt selfcalibration.Options) (calibration.ScenarioRecord, error) {
		calls++
		gotLevels = opt.Levels
		return calibration.ScenarioRecord{}, errors.New("fixture failure")
	}
	defer func() { selfCalibrationRun = previous }()
	v := capacityScenario(t, "http://127.0.0.1:18089")
	mirror := capacityMirror{ResponseBytes: 64, ServerDelay: time.Millisecond, LevelDuration: 5 * time.Second}
	code := ensureCapacityCalibration(filepath.Join(dir, "selfcal.json"), scenario, v, []int{2, 4}, mirror, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 || calls != 1 || len(gotLevels) != 2 || gotLevels[1] != 4 {
		t.Fatalf("code=%d calls=%d levels=%v", code, calls, gotLevels)
	}
	args := strings.Join(capacityMirror{FromReport: "ref.json", LevelDuration: time.Second, Extended: true}.args("s.json", "o.json", []int{2, 4}), " ")
	if !strings.Contains(args, "--users 2,4") || !strings.Contains(args, "--from-report ref.json") || !strings.Contains(args, "--extended-envelope") {
		t.Fatalf("calibrate args: %s", args)
	}
}
