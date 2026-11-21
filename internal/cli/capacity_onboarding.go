package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func provisionCapacityCookies(program string, args []string, prefix string, count int) (func(), error) {
	if !regexp.MustCompile(`^[A-Z_][A-Z0-9_]*_$`).MatchString(prefix) {
		return nil, errors.New("session hook requires a valid per-VU cookie environment prefix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), fmt.Sprintf("DAMOCLINE_SESSION_COUNT=%d", count))
	var output hookOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil, errors.New("session hook failed, timed out, or exceeded its output limit")
	}
	var cookies []string
	if err := json.Unmarshal(output.buf.Bytes(), &cookies); err != nil || len(cookies) != count {
		return nil, errors.New("session hook must return exactly the requested number of cookies")
	}
	keys := make([]string, 0, count)
	previous := make(map[string]string)
	restore := func() {
		for _, key := range keys {
			if value, exists := previous[key]; exists {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
	for i, cookie := range cookies {
		key := fmt.Sprintf("%s%d", prefix, i+1)
		if value, exists := os.LookupEnv(key); exists {
			previous[key] = value
		}
		keys = append(keys, key)
		if err := os.Setenv(key, cookie); err != nil {
			restore()
			return nil, errors.New("could not prepare session cookie")
		}
	}
	if _, err := config.ResolveSessions(prefix, count); err != nil {
		restore()
		return nil, errors.New("session hook returned malformed or duplicate cookies")
	}
	return restore, nil
}

type hookOutput struct{ buf bytes.Buffer }

func (b *hookOutput) Write(data []byte) (int, error) {
	const maxOutput = 2 << 20
	if len(data) > maxOutput-b.buf.Len() {
		return 0, errors.New("session hook output too large")
	}
	return b.buf.Write(data)
}

func appendIntensityHypothesis(source, destination string, evidence campaign.Result, intensity float64) error {
	data, err := readCapacityFile(source)
	if err != nil {
		return err
	}
	var rates []float64
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "observation" || phase.Run.ElapsedSeconds <= 0 {
				continue
			}
			rates = append(rates, float64(phase.Run.Metrics.Success)/phase.Run.ElapsedSeconds)
		}
	}
	if len(rates) == 0 {
		return writeCapacityMarkdown(destination, string(data))
	}
	total := 0.0
	for _, rate := range rates {
		total += rate
	}
	rate := total / float64(len(rates))
	people := rate * 60 / intensity
	if math.IsNaN(people) || math.IsInf(people, 0) || people < 0 {
		return errors.New("human usage hypothesis cannot be computed from the selected intensity")
	}
	section := fmt.Sprintf(
		"\n## Human usage hypothesis\n\n%d modeled VUs correspond to approximately %.1f people at %.2f actions per person per minute. "+
			"This assumes one successful measured HTTP request per real action and the same workflow mix. The estimate uses %.2f successful requests/s averaged across recorded observations. "+
			"It does not establish supported human concurrency or change the audit verdict.\n",
		evidence.BaseUsers, people, intensity, rate,
	)
	return writeCapacityMarkdown(destination, string(data)+section)
}

func capacityLogIntensity(path string) (float64, error) {
	data, err := readCapacityFile(path)
	if err != nil {
		return 0, err
	}
	users := make(map[string]bool)
	var first, last time.Time
	count := 0
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		var row struct {
			User string    `json:"user"`
			Time time.Time `json:"timestamp"`
		}
		if json.Unmarshal(lines.Bytes(), &row) != nil || row.User == "" || row.Time.IsZero() {
			return 0, errors.New("access log requires JSON lines with pseudonymous user and RFC3339 timestamp")
		}
		users[row.User] = true
		count++
		if first.IsZero() || row.Time.Before(first) {
			first = row.Time
		}
		if row.Time.After(last) {
			last = row.Time
		}
	}
	if lines.Err() != nil || !last.After(first) {
		return 0, errors.New("access log requires a readable, nonzero observation span")
	}
	return float64(count) / float64(len(users)) / last.Sub(first).Minutes(), nil
}

func writeNetworkPlacement(w io.Writer, result report.Result) {
	if result.NetworkBaseline == nil {
		return
	}
	if result.NetworkBaseline.Status == report.NetworkBaselineSkipped {
		fmt.Fprintln(w, "Generator placement: baseline skipped; repeat from an authorized path with baseline measurement enabled.")
		return
	}
	if report.NetworkBaselineIssue(result) != "" {
		fmt.Fprintln(w, "Generator placement: repeat the baseline from a stable path in the same network or region as the application.")
		return
	}
	fmt.Fprintln(w, "Generator placement: the baseline provides no signal to relocate this generator; verify headroom under the intended workload.")
}
