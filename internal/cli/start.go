package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

const (
	profileDir      = ".damocline"
	scenarioPath    = ".damocline/scenario.local.json"
	preferencesPath = ".damocline/start.json"
)

type startPreferences struct {
	Kind          string `json:"kind"`
	MinUsers      int    `json:"min_users"`
	MaxUsers      int    `json:"max_users"`
	StageDuration string `json:"stage_duration"`
	Repeats       int    `json:"repeats"`
	Budget        string `json:"budget"`
}
type wizard struct {
	input *bufio.Reader
	out   io.Writer
}

func (w *wizard) ask(question, fallback string) (string, error) {
	if fallback == "" {
		fmt.Fprintf(w.out, "%s: ", question)
	} else {
		fmt.Fprintf(w.out, "%s [%s]: ", question, fallback)
	}
	line, err := w.input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return fallback, nil
	}
	return value, nil
}
func (w *wizard) number(question string, fallback, low, high int) (int, error) {
	for {
		value, err := w.ask(question, strconv.Itoa(fallback))
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(value)
		if err == nil && n >= low && n <= high {
			return n, nil
		}
		fmt.Fprintf(w.out, "Enter an integer from %d to %d.\n", low, high)
	}
}
func (w *wizard) duration(question, def string, low, high time.Duration) (string, error) {
	for {
		value, err := w.ask(question, def)
		if err != nil {
			return "", err
		}
		n, err := time.ParseDuration(value)
		if err == nil && n >= low && n <= high {
			return value, nil
		}
		fmt.Fprintf(w.out, "Enter a duration from %s to %s (e.g., 20s or 1m).\n", low, high)
	}
}
func (w *wizard) choose(question, def string, answers ...string) (string, error) {
	for {
		value, err := w.ask(question, def)
		if err != nil {
			return "", err
		}
		for _, answer := range answers {
			if value == answer {
				return value, nil
			}
		}
		fmt.Fprintf(w.out, "Choose %s.\n", strings.Join(answers, ", "))
	}
}
func (w *wizard) configure() (config.Scenario, startPreferences, error) {
	var s config.Scenario
	var p startPreferences
	fmt.Fprintln(w.out, "\n1 / Application")
	base, err := w.ask("HTTPS origin (e.g., https://app.example.com)", "")
	if err != nil {
		return s, p, err
	}
	u, err := url.Parse(base)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return s, p, errors.New("expected an HTTPS origin without any path or credentials")
	}
	hostname := u.Hostname()
	ip := net.ParseIP(hostname)
	if u.Scheme == "http" && hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return s, p, errors.New("guided mode requires HTTPS except for localhost/loopback")
	}
	s.BaseURL = base
	s.Name, err = w.ask("Scenario name", "Read-only web app")
	if err != nil {
		return s, p, err
	}
	if s.Name == "" {
		return s, p, errors.New("scenario name cannot be empty")
	}
	fmt.Fprintln(w.out, "\n2 / Pages (GET only)")
	raw, err := w.ask("Paths separated by commas (e.g. /dashboard,/employees)", "/")
	if err != nil {
		return s, p, err
	}
	paths := strings.Split(raw, ",")
	if len(paths) > 20 {
		return s, p, errors.New("at most 20 paths")
	}
	for i, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?\r\n") {
			return s, p, fmt.Errorf("invalid path %q; provide paths such as /dashboard without query strings", path)
		}
		s.Steps = append(s.Steps, config.Step{Name: fmt.Sprintf("Page %d", i+1), Method: "GET", Path: path, ExpectedStatus: []int{200}})
	}
	fmt.Fprintln(w.out, "Optional expected text helps catch login pages that return HTTP 200.")
	for i := range s.Steps {
		marker, err := w.ask("Expected text on "+s.Steps[i].Path+" (optional)", "")
		if err != nil {
			return s, p, err
		}
		s.Steps[i].MustContain = marker
	}
	fmt.Fprintln(w.out, "\n3 / Authentication")
	fmt.Fprintln(w.out, "  1 = public; 2 = shared session cookie; 3 = separate test-account cookies")
	auth, err := w.choose("Authentication mode", "1", "1", "2", "3")
	if err != nil {
		return s, p, err
	}
	switch auth {
	case "2":
		for i := range s.Steps {
			s.Steps[i].Headers = map[string]string{"Cookie": "${DAMOCLINE_SESSION_COOKIE}"}
		}
		fmt.Fprintln(w.out, "Set DAMOCLINE_SESSION_COOKIE in your shell. A shared session is not multiple distinct users.")
	case "3":
		s.PerVUCookieEnvPrefix = "DAMOCLINE_VU_COOKIE_"
		fmt.Fprintln(w.out, "Set a different DAMOCLINE_VU_COOKIE_1...N environment value per test account.")
	}
	fmt.Fprintln(w.out, "\n4 / Test workload")
	fmt.Fprintln(w.out, "  1 = quick fixed-level test; 2 = repeated capacity exploration")
	kind, err := w.choose("Test type", "2", "1", "2")
	if err != nil {
		return s, p, err
	}
	p.Kind = "run"
	if kind == "2" {
		p.Kind = "explore"
	}
	p.MinUsers, err = w.number("Starting virtual users", 2, 1, 50)
	if err != nil {
		return s, p, err
	}
	s.Users = p.MinUsers
	p.MaxUsers = p.MinUsers
	if p.Kind == "explore" {
		maximum := 10
		if p.MinUsers > maximum {
			maximum = p.MinUsers
		}
		p.MaxUsers, err = w.number("Maximum virtual users", maximum, p.MinUsers, 50)
		if err != nil {
			return s, p, err
		}
		p.StageDuration, err = w.duration("Duration per trial", "20s", 10*time.Second, 3*time.Minute)
		if err != nil {
			return s, p, err
		}
		p.Repeats = 2
		p.Budget = "8m"
		s.Duration = p.StageDuration
	} else {
		s.Duration, err = w.duration("Test duration", "30s", time.Second, 3*time.Minute)
		if err != nil {
			return s, p, err
		}
	}
	s.Mode = "closed"
	ramp := 2 * time.Second
	if d, err := time.ParseDuration(s.Duration); err == nil && d < ramp {
		ramp = d / 4
	}
	s.RampUp = ramp.String()
	s.ThinkTime = "1s"
	s.Timeout = "8s"
	s.MaxRPS, err = w.number("Maximum requests/second (global)", 20, 1, 100)
	if err != nil {
		return s, p, err
	}
	s.P95LimitMS = 1500
	s.MaxErrorRate = 0.01
	s.MinSamplesPerStep = 5
	fmt.Fprintln(w.out, "Pass criteria: p95 <= 1500ms, errors <= 1%, >=5 samples per step.")
	return s, p, nil
}
func writePrivateJSON(path string, object any) error {
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
func loadStartProfile() (config.Scenario, startPreferences, error) {
	var s config.Scenario
	var p startPreferences
	data, err := os.ReadFile(preferencesPath)
	if err != nil {
		return s, p, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return s, p, err
	}
	s, err = config.Load(scenarioPath)
	if err != nil {
		return s, p, err
	}
	if p.Kind != "run" && p.Kind != "explore" {
		return s, p, errors.New("invalid stored test type")
	}
	return s, p, nil
}

// start is an interactive interface over Damocline's own native HTTP engine.
// Every actual test needs explicit confirmation; setup never sends traffic.
func start(input io.Reader, output, errOutput io.Writer) int {
	w := wizard{input: bufio.NewReader(input), out: output}
	fmt.Fprintln(output, "Damocline | Guided capacity testing")
	fmt.Fprintln(output, "Built-in Go load engine. No k6 or hosted account required.")
	var s config.Scenario
	var p startPreferences
	var err error
	useSaved := false
	if _, statErr := os.Stat(scenarioPath); statErr == nil {
		selected, askErr := w.choose("Saved profile found. Use it? (y/n)", "y", "y", "n")
		if askErr != nil {
			fmt.Fprintf(errOutput, "Input ended: %v\n", askErr)
			return 2
		}
		useSaved = selected == "y"
	}
	if useSaved {
		s, p, err = loadStartProfile()
		if err != nil {
			fmt.Fprintf(errOutput, "Could not open saved profile: %v\n", err)
			return 2
		}
	} else {
		s, p, err = w.configure()
		if err != nil {
			fmt.Fprintf(errOutput, "Setup stopped: %v\n", err)
			return 2
		}
		err = writePrivateJSON(scenarioPath, s)
		if err == nil {
			err = writePrivateJSON(preferencesPath, p)
		}
		if err != nil {
			fmt.Fprintf(errOutput, "Could not save configuration: %v\n", err)
			return 2
		}
		fmt.Fprintf(output, "Scenario saved in %s (no credentials stored).\n", scenarioPath)
	}
	if p.Kind == "explore" && (p.MinUsers < 1 || p.MaxUsers < p.MinUsers || p.MaxUsers > 50) {
		fmt.Fprintln(errOutput, "Invalid capacity exploration bounds")
		return 2
	}
	if p.Kind == "explore" {
		s.Users = p.MinUsers
	}
	v, err := config.Parse(s, config.Options{Authorized: true})
	if err != nil {
		fmt.Fprintf(errOutput, "Preflight failed: %v\nCheck required environment variables, then run 'damocline start' again.\n", err)
		return 2
	}
	fmt.Fprintf(output, "\nReview: %s | %s://%s | %s | %d VUs | maximum %d RPS\n", s.Name, v.Base.Scheme, v.Base.Host, p.Kind, s.Users, s.MaxRPS)
	for _, step := range s.Steps {
		fmt.Fprintf(output, "  GET %s\n", step.Path)
	}
	if p.Kind == "explore" {
		fmt.Fprintf(output, "Capacity search: %d-%d VUs, %s/trial, %d repetitions.\n", p.MinUsers, p.MaxUsers, p.StageDuration, p.Repeats)
	}
	fmt.Fprintln(output, "Traffic originates on this computer. Test only systems you own or are explicitly authorized to assess.")
	approved, err := w.ask("Type AUTHORIZE to confirm permission", "")
	if err != nil || approved != "AUTHORIZE" {
		fmt.Fprintln(output, "Cancelled. No traffic sent.")
		return 0
	}
	proceed, err := w.choose("Start test now? (y/n)", "n", "y", "n")
	if err != nil || proceed != "y" {
		fmt.Fprintln(output, "Profile saved. No traffic sent.")
		return 0
	}
	outfile := filepath.Join(profileDir, "reports", time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+p.Kind+".json")
	if p.Kind == "run" {
		return run([]string{"--config", scenarioPath, "--authorized", "--out", outfile}, output, errOutput)
	}
	return explore([]string{"--config", scenarioPath, "--min-users", strconv.Itoa(p.MinUsers), "--max-users", strconv.Itoa(p.MaxUsers), "--repeats", strconv.Itoa(p.Repeats), "--stage-duration", p.StageDuration, "--budget", p.Budget, "--authorized", "--out", outfile}, output, errOutput)
}
