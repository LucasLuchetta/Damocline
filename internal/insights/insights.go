// Package insights translates measured results into conservative, actionable
// explanations. It never infers infrastructure capacity without evidence.
package insights

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/hostmon"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

const maxReportFile = 16 << 20

type Finding struct {
	Level string
	Text  string
}

type Assessment struct {
	Verdict        string
	Performance    string
	Capacity       string
	Infrastructure string
	Findings       []Finding
	Next           []string
}

func ReadRun(path string) (report.Result, error) {
	var r report.Result
	if err := readJSON(path, &r); err != nil {
		return r, err
	}
	if r.SchemaVersion < 1 || r.SchemaVersion > 2 || r.Name == "" || r.Metrics.Total < 0 || r.Metrics.Success < 0 || r.Metrics.Failures < 0 || r.Metrics.Success+r.Metrics.Failures != r.Metrics.Total || r.Config.Users < 1 || r.ElapsedSeconds <= 0 {
		return report.Result{}, errors.New("not a valid Damocline HTTP run report")
	}
	return r, nil
}
func ReadHost(path string) (hostmon.Result, error) {
	var r hostmon.Result
	if err := readJSON(path, &r); err != nil {
		return r, err
	}
	if r.SchemaVersion != 1 || r.Source == "" {
		return hostmon.Result{}, errors.New("not a valid Damocline host-monitor report")
	}
	return r, nil
}
func readJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open report: %w", err)
	}
	defer f.Close()
	var data []byte
	data, err = io.ReadAll(io.LimitReader(f, maxReportFile+1))
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	if len(data) > maxReportFile {
		return errors.New("report exceeds 16 MiB maximum")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode report: %w", err)
	}
	return nil
}

// Evaluate distinguishes meeting configured objectives from proving capacity.
// A successful single run never proves that the server has spare resources.
func Evaluate(r report.Result, host *hostmon.Result) Assessment {
	a := Assessment{Verdict: r.Outcome, Capacity: "NOT DETERMINED", Infrastructure: "NOT MEASURED"}
	if a.Verdict == "" {
		a.Verdict = "UNKNOWN"
	}
	if r.Outcome == "PASS" {
		a.Performance = "OBJECTIVES MET IN THIS RUN"
	} else if r.Outcome == "FAIL" {
		a.Performance = "OBJECTIVES NOT MET"
	} else {
		a.Performance = "INCONCLUSIVE"
	}
	if r.Config.P95LimitMS > 0 {
		a.Findings = append(a.Findings, Finding{"INFO", fmt.Sprintf("HTTP request p95: %.0f ms (configured limit: %.0f ms).", r.Metrics.P95MS, r.Config.P95LimitMS)})
	} else {
		a.Findings = append(a.Findings, Finding{"CAUTION", "This report does not declare an HTTP p95 objective."})
	}
	if r.Metrics.Total > 0 {
		a.Findings = append(a.Findings, Finding{"INFO", fmt.Sprintf("HTTP requests: %d successful / %d total; %d failures, %.2f%% error rate.", r.Metrics.Success, r.Metrics.Total, r.Metrics.Failures, 100*float64(r.Metrics.Failures)/float64(r.Metrics.Total))})
	} else {
		a.Findings = append(a.Findings, Finding{"CAUTION", "No completed HTTP requests were measured."})
	}
	if r.Metrics.Total < 100 {
		a.Findings = append(a.Findings, Finding{"CAUTION", fmt.Sprintf("Only %d completed requests: percentile estimates, especially p99, are sensitive to individual samples. Repeat with more observations.", r.Metrics.Total)})
	}
	if r.Metrics.Total >= 1 && r.Metrics.P95MS > 0 && r.Metrics.P99MS > 3*r.Metrics.P95MS && r.Metrics.P99MS-r.Metrics.P95MS > 1000 {
		a.Findings = append(a.Findings, Finding{"CAUTION", fmt.Sprintf("Long-tail latency: request p99 reached %.0f ms versus p95 of %.0f ms. Investigate intermittent slow requests.", r.Metrics.P99MS, r.Metrics.P95MS)})
	}
	if r.Journeys.Total > 0 {
		a.Findings = append(a.Findings, Finding{"INFO", fmt.Sprintf("Complete journey p95: %.0f ms, including configured think time; this is different from HTTP request p95.", r.Journeys.P95MS)})
	}
	if r.Config.Users == 1 {
		a.Findings = append(a.Findings, Finding{"LIMIT", "Only one virtual user was configured: this is a baseline, not a concurrent-user capacity test."})
	} else if !r.Config.SeparateSessions {
		a.Findings = append(a.Findings, Finding{"LIMIT", fmt.Sprintf("%d virtual users were configured, but independent authenticated sessions were not verified; they are not necessarily %d distinct people.", r.Config.Users, r.Config.Users)})
	}
	if r.ElapsedSeconds > 0 {
		actual := float64(r.Metrics.Total) / r.ElapsedSeconds
		a.Findings = append(a.Findings, Finding{"INFO", fmt.Sprintf("Observed HTTP request throughput: %.2f requests/s. The max_rps setting (%d) is only a ceiling, not achieved traffic.", actual, r.Config.MaxRPS)})
		if r.Config.MaxRPS > 0 && actual >= 0.90*float64(r.Config.MaxRPS) {
			a.Findings = append(a.Findings, Finding{"LIMIT", "Traffic approached the generator's max_rps setting. This run cannot separate a client-side rate cap from application capacity."})
		}
	}
	if r.ArrivalsDropped > 0 {
		a.Findings = append(a.Findings, Finding{"LIMIT", fmt.Sprintf("%d scheduled journey starts were dropped: offered load was not delivered.", r.ArrivalsDropped)})
	}
	if r.Metrics.Cancelled > 0 {
		a.Findings = append(a.Findings, Finding{"CAUTION", fmt.Sprintf("%d requests were cancelled at the run boundary and excluded from completed-request percentiles.", r.Metrics.Cancelled)})
	}
	for _, st := range r.Steps {
		if st.Metrics.Total == 0 {
			a.Findings = append(a.Findings, Finding{"CAUTION", fmt.Sprintf("Step %q received no complete samples.", st.Name)})
			continue
		}
		if (r.Config.P95LimitMS > 0 && st.Metrics.P95MS > r.Config.P95LimitMS) || st.Metrics.Failures > 0 {
			a.Findings = append(a.Findings, Finding{"CAUTION", fmt.Sprintf("Step %q: p95 %.0f ms; %d failures out of %d requests.", st.Name, st.Metrics.P95MS, st.Metrics.Failures, st.Metrics.Total)})
		}
	}
	for _, w := range r.Warnings {
		if strings.TrimSpace(w) != "" {
			a.Findings = append(a.Findings, Finding{"CAUTION", w})
		}
	}
	if host != nil {
		samples, avg, peakCPU, peakRAM, ok := alignedHost(*host, r)
		if ok {
			a.Infrastructure = "HOST OBSERVED (CPU/RAM ONLY)"
			a.Findings = append(a.Findings, Finding{"INFO", fmt.Sprintf("Linux host: %d aligned samples; average CPU %.0f%%, peak CPU %.0f%%, peak memory used %.0f%%.", samples, avg, peakCPU, peakRAM)})
			if samples < 3 {
				a.Findings = append(a.Findings, Finding{"CAUTION", "Very few aligned host samples: do not infer sustained resource pressure."})
			}
			if samples >= 3 && (avg >= 85 || peakRAM >= 90) {
				a.Findings = append(a.Findings, Finding{"CAUTION", "Possible host resource pressure observed; confirm sustained utilization and inspect app, database and container metrics before upgrading."})
			}
		} else {
			a.Infrastructure = "HOST DATA NOT ALIGNED"
			a.Findings = append(a.Findings, Finding{"CAUTION", "No Linux host samples overlap the run timestamps. Check host/generator clocks and monitoring window."})
		}
	}
	a.Next = []string{
		"Run a controlled multi-user capacity exploration with several load levels and repeated trials.",
		"Collect synchronized Linux host, Docker, and database metrics during the load test to investigate bottlenecks.",
		"Repeat representative authenticated workflows with independently provisioned test accounts, when possible.",
	}
	return a
}

func alignedHost(h hostmon.Result, r report.Result) (count int, avg, peakCPU, peakRAM float64, ok bool) {
	start, err := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil {
		return
	}
	end := start.Add(time.Duration(r.ElapsedSeconds * float64(time.Second)))
	for _, s := range h.Samples {
		at, err := time.Parse(time.RFC3339Nano, s.At)
		if err != nil || at.Before(start) || at.After(end) {
			continue
		}
		count++
		avg += s.CPUPercent
		peakCPU = math.Max(peakCPU, s.CPUPercent)
		if s.MemoryTotalBytes > 0 {
			peakRAM = math.Max(peakRAM, 100*float64(s.MemoryUsedBytes)/float64(s.MemoryTotalBytes))
		}
	}
	if count > 0 {
		avg /= float64(count)
		ok = true
	}
	return
}

func Write(w io.Writer, r report.Result, host *hostmon.Result, previous *report.Result) {
	a := Evaluate(r, host)
	fmt.Fprintln(w, "\n--- DAMOCLINE ASSESSMENT ---")
	fmt.Fprintf(w, "PERFORMANCE:    %s (%s)\n", a.Performance, a.Verdict)
	fmt.Fprintf(w, "USER CAPACITY:  %s\n", a.Capacity)
	fmt.Fprintf(w, "SERVER UPGRADE: NOT JUSTIFIED BY THIS RUN ALONE\n")
	fmt.Fprintf(w, "HOST METRICS:   %s\n", a.Infrastructure)
	fmt.Fprintln(w, "\nWhat the data says:")
	for _, f := range a.Findings {
		fmt.Fprintf(w, "  [%s] %s\n", f.Level, f.Text)
	}
	if previous != nil {
		writeComparison(w, *previous, r)
	}
	fmt.Fprintln(w, "\nRecommended next steps:")
	for i, next := range a.Next {
		fmt.Fprintf(w, "  %d. %s\n", i+1, next)
	}
	fmt.Fprintln(w, "Reminder: objective compliance at one load level is not proof of spare CPU/RAM, a user-capacity limit, or a need for a larger server.")
}

func writeComparison(w io.Writer, old, current report.Result) {
	fmt.Fprintln(w, "\nComparison with previous run:")
	compatible := old.Target == current.Target && old.Config.Users == current.Config.Users && old.Config.Mode == current.Config.Mode && old.Config.SeparateSessions == current.Config.SeparateSessions && old.Config.ThinkTime == current.Config.ThinkTime && sameSteps(old.Steps, current.Steps)
	if !compatible {
		fmt.Fprintln(w, "  [LIMIT] Workload settings, authentication mode, target, or routes differ. This is NOT a controlled comparison.")
	}
	if old.Config.MaxRPS != current.Config.MaxRPS {
		fmt.Fprintf(w, "  [LIMIT] max_rps ceilings differ (%d vs %d); a configured ceiling is not the actual traffic rate.\n", old.Config.MaxRPS, current.Config.MaxRPS)
	}
	if old.Config.Duration != current.Config.Duration {
		fmt.Fprintf(w, "  [LIMIT] Test durations differ (%s vs %s).\n", old.Config.Duration, current.Config.Duration)
	}
	if old.ElapsedSeconds > 0 && current.ElapsedSeconds > 0 {
		a := float64(old.Metrics.Total) / old.ElapsedSeconds
		b := float64(current.Metrics.Total) / current.ElapsedSeconds
		fmt.Fprintf(w, "  Achieved throughput: %.2f -> %.2f requests/s.\n", a, b)
		if a > 0 && math.Abs(b-a)/a > 0.15 {
			fmt.Fprintln(w, "  [LIMIT] Achieved throughput differs by more than 15%, so latency changes cannot be attributed to an application improvement.")
		}
	}
	fmt.Fprintf(w, "  HTTP p95: %.0f -> %.0f ms; failures: %d/%d -> %d/%d.\n", old.Metrics.P95MS, current.Metrics.P95MS, old.Metrics.Failures, old.Metrics.Total, current.Metrics.Failures, current.Metrics.Total)
	fmt.Fprintln(w, "  Comparisons describe observations only; they do not identify the cause of a change or prove increased infrastructure headroom.")
}
func sameSteps(a, b []report.StepResult) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Method != b[i].Method {
			return false
		}
	}
	return true
}
