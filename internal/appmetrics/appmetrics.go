// Package appmetrics collects bounded, permissioned application metric snapshots.
package appmetrics

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxBodyBytes = 1 << 20

type Sample struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type Snapshot struct {
	SchemaVersion int      `json:"schema_version"`
	Source        string   `json:"source"`
	CollectedAt   string   `json:"collected_at"`
	Samples       []Sample `json:"samples"`
	Note          string   `json:"note"`
}

func Collect(ctx context.Context, endpoint string) (Snapshot, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return Snapshot{}, fmt.Errorf("metrics endpoint must be an HTTP(S) URL without credentials or fragment")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Snapshot{}, fmt.Errorf("metrics endpoint must use HTTP or HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build metrics request: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return Snapshot{}, fmt.Errorf("collect metrics: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("metrics endpoint returned HTTP %d", response.StatusCode)
	}
	samples, err := parse(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{SchemaVersion: 1, Source: u.Scheme + "://" + u.Host + u.EscapedPath(), CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Samples: samples, Note: "Direct application metric observations only; values are not proof of a bottleneck or capacity cause."}, nil
}

func parse(reader io.Reader) ([]Sample, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	values := map[string]float64{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid metric line")
		}
		name := fields[0]
		if index := strings.IndexByte(name, '{'); index >= 0 {
			name = name[:index]
		}
		if !validName(name) {
			return nil, fmt.Errorf("invalid metric name %q", name)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || value != value {
			return nil, fmt.Errorf("invalid metric value for %q", name)
		}
		values[name] += value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	samples := make([]Sample, 0, len(values))
	for name, value := range values {
		samples = append(samples, Sample{Name: name, Value: value})
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Name < samples[j].Name })
	return samples, nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if !(r == '_' || r == ':' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
