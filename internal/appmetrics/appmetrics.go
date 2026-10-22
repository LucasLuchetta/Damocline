// Package appmetrics collects bounded, permissioned application metric snapshots.
package appmetrics

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBodyBytes = 1 << 20
const maxMetricNames = 64
const maxMetricNameBytes = 128
const maxMetricSeries = 4096
const MaxTimelineSamples = 1000
const MaxTimelineBytes = 4 << 20
const MinTimelineInterval = time.Second
const MaxTimelineInterval = time.Minute

const TimelineSchemaVersion = 1

type Sample struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Type     string  `json:"type,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	SeriesID string  `json:"series_id,omitempty"`
}

type Snapshot struct {
	SchemaVersion int      `json:"schema_version"`
	Source        string   `json:"source"`
	CollectedAt   string   `json:"collected_at"`
	Samples       []Sample `json:"samples"`
	Note          string   `json:"note"`
}

// TimedSample retains one bounded scrape and its collection interval so
// offline tools can align observations with the corresponding load phase.
type TimedSample struct {
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
	DurationMS float64  `json:"duration_ms"`
	Samples    []Sample `json:"samples"`
}

// Timeline records optional application-metric collection during one run.
// It contains aggregate metric names and values only; Prometheus labels are
// intentionally discarded by the parser.
type Timeline struct {
	SchemaVersion      int           `json:"schema_version"`
	Source             string        `json:"source"`
	Interval           string        `json:"interval"`
	StartedAt          string        `json:"started_at"`
	FinishedAt         string        `json:"finished_at"`
	Samples            []TimedSample `json:"samples"`
	FailedCollections  int           `json:"failed_collections"`
	InterruptedScrapes int           `json:"interrupted_scrapes,omitempty"`
	CollectionLimitHit bool          `json:"collection_limit_hit,omitempty"`
	Note               string        `json:"note"`
	LegacyIncomplete   bool          `json:"-"`
}

// Budget caps scrape attempts across all phases in one campaign artifact.
type Budget struct {
	mu             sync.Mutex
	remaining      int
	remainingBytes int
}

func NewBudget(maxAttempts int) *Budget {
	if maxAttempts < 1 || maxAttempts > MaxTimelineSamples {
		maxAttempts = MaxTimelineSamples
	}
	return &Budget{remaining: maxAttempts, remainingBytes: MaxTimelineBytes}
}

func (b *Budget) take() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining == 0 || b.remainingBytes == 0 {
		return false
	}
	b.remaining--
	return true
}

func (b *Budget) storeSample(sample TimedSample) bool {
	data, err := json.Marshal(sample)
	if err != nil {
		return false
	}
	if b == nil {
		return len(data) <= MaxTimelineBytes
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Reserve one delimiter byte per sample. This conservatively ensures the
	// combined JSON arrays across campaign phases stay within the shared cap.
	reservedBytes := len(data) + 1
	if reservedBytes > b.remainingBytes {
		return false
	}
	b.remainingBytes -= reservedBytes
	return true
}

func (t Timeline) State() string {
	if t.LegacyIncomplete || t.FailedCollections > 0 || t.InterruptedScrapes > 0 || t.CollectionLimitHit {
		if len(t.Samples) == 0 {
			return "UNAVAILABLE"
		}
		return "PARTIAL"
	}
	if len(t.Samples) == 0 {
		return "NO_SAMPLES"
	}
	return "COLLECTED"
}

func SerializedSamplesBytes(t Timeline) (int, error) {
	serialized, err := json.Marshal(t.Samples)
	if err != nil {
		return 0, err
	}
	return len(serialized), nil
}

func ValidateTimeline(t Timeline) error {
	if t.SchemaVersion != TimelineSchemaVersion || t.Note == "" {
		return fmt.Errorf("unsupported or incomplete application-metrics timeline")
	}
	source, err := ValidateEndpoint(t.Source)
	if err != nil || source != t.Source {
		return fmt.Errorf("invalid application-metrics source")
	}
	interval, err := time.ParseDuration(t.Interval)
	if err != nil || ValidateInterval(interval) != nil {
		return fmt.Errorf("invalid application-metrics interval")
	}
	start, startErr := time.Parse(time.RFC3339Nano, t.StartedAt)
	end, endErr := time.Parse(time.RFC3339Nano, t.FinishedAt)
	if startErr != nil || endErr != nil || end.Before(start) ||
		t.FailedCollections < 0 || t.InterruptedScrapes < 0 || len(t.Samples) > MaxTimelineSamples ||
		t.InterruptedScrapes > 1 || len(t.Samples)+t.FailedCollections+t.InterruptedScrapes > MaxTimelineSamples ||
		(t.State() == "COLLECTED" && (t.FailedCollections != 0 || t.InterruptedScrapes != 0 || t.CollectionLimitHit)) {
		return fmt.Errorf("inconsistent application-metrics timeline bounds")
	}
	serializedSize, err := SerializedSamplesBytes(t)
	if err != nil || serializedSize > MaxTimelineBytes {
		return fmt.Errorf("application-metrics timeline exceeds %d bytes", MaxTimelineBytes)
	}
	var previousEnd time.Time
	for _, sample := range t.Samples {
		collectionStart, collectionStartErr := time.Parse(time.RFC3339Nano, sample.StartedAt)
		collectionEnd, collectionEndErr := time.Parse(time.RFC3339Nano, sample.FinishedAt)
		if collectionStartErr != nil || collectionEndErr != nil || collectionStart.Before(start) || collectionEnd.After(end) || collectionEnd.Before(collectionStart) || (!previousEnd.IsZero() && collectionStart.Before(previousEnd)) ||
			math.IsNaN(sample.DurationMS) || math.IsInf(sample.DurationMS, 0) || sample.DurationMS < 0 || len(sample.Samples) > maxMetricSeries {
			return fmt.Errorf("invalid application-metrics sample bounds")
		}
		previousEnd = collectionEnd
		previous := ""
		previousSeries := ""
		for _, metric := range sample.Samples {
			if len(metric.Name) > maxMetricNameBytes || !validName(metric.Name) || metric.Name < previous || (metric.Name == previous && metric.SeriesID <= previousSeries) || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
				return fmt.Errorf("invalid application-metrics value")
			}
			if metric.SeriesID != "" && (len(metric.SeriesID) != 64 || strings.Trim(metric.SeriesID, "0123456789abcdef") != "") {
				return fmt.Errorf("invalid application-metrics series identity")
			}
			if metric.Type != "" && metric.Type != "counter" && metric.Type != "gauge" && metric.Type != "histogram" && metric.Type != "gaugehistogram" && metric.Type != "summary" && metric.Type != "info" && metric.Type != "stateset" && metric.Type != "unknown" {
				return fmt.Errorf("invalid application-metrics type")
			}
			previous = metric.Name
			previousSeries = metric.SeriesID
		}
	}
	return nil
}

// ValidateEndpoint rejects URL fields that commonly carry credentials and
// returns the source identifier safe to persist in a local report.
func ValidateEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return "", fmt.Errorf("metrics endpoint must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("metrics endpoint must use HTTP or HTTPS")
	}
	return u.Scheme + "://" + u.Host, nil
}

func ValidateInterval(interval time.Duration) error {
	if interval < MinTimelineInterval || interval > MaxTimelineInterval {
		return fmt.Errorf("application metrics interval must be between %s and %s", MinTimelineInterval, MaxTimelineInterval)
	}
	return nil
}

func Collect(ctx context.Context, endpoint string) (Snapshot, error) {
	return CollectWithBearer(ctx, endpoint, "")
}

// ValidateEndpointWithBearer checks endpoint syntax and ensures authentication
// credentials cannot leave the machine over plaintext HTTP.
func ValidateEndpointWithBearer(endpoint, token string) error {
	if _, err := ValidateEndpoint(endpoint); err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		return fmt.Errorf("bearer authentication requires HTTPS or a loopback endpoint")
	}
	return nil
}

// CollectWithBearer performs one bounded scrape. The bearer token is never
// included in errors or persisted in the returned snapshot.
func CollectWithBearer(ctx context.Context, endpoint, token string) (Snapshot, error) {
	source, err := ValidateEndpoint(endpoint)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ValidateEndpointWithBearer(endpoint, token); err != nil {
		return Snapshot{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build metrics request: %w", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
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
	samples, err := parse(response.Body)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{SchemaVersion: 2, Source: source, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Samples: samples, Note: "Direct application metric observations only; values are not proof of a bottleneck or capacity cause."}, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CaptureDuring samples a configured endpoint while run executes. Scrapes
// stop when the phase ends, overlap is prevented, and budget limits apply to
// all callers that share the same Budget.
func CaptureDuring[T any](ctx context.Context, endpoint string, interval time.Duration, budget *Budget, run func() T) (T, Timeline) {
	started := time.Now()
	source, endpointErr := ValidateEndpoint(endpoint)
	timeline := Timeline{SchemaVersion: TimelineSchemaVersion, Source: source, Interval: interval.String(),
		StartedAt: started.UTC().Format(time.RFC3339Nano), Samples: []TimedSample{},
		Note: "Auxiliary Prometheus-text scrapes are outside the load request counters and max_rps governor; timestamps are observations, not causal attribution."}
	if endpointErr != nil || ValidateInterval(interval) != nil {
		result := run()
		finished := time.Now()
		timeline.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
		timeline.FailedCollections = 1
		return result, timeline
	}
	if budget == nil {
		budget = NewBudget(MaxTimelineSamples)
	}
	sampleCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if !budget.take() {
				timeline.CollectionLimitHit = true
				return
			}
			collectionStarted := time.Now()
			requestCtx, requestCancel := context.WithTimeout(sampleCtx, 10*time.Second)
			snapshot, err := Collect(requestCtx, endpoint)
			requestCancel()
			finished := time.Now()
			if err != nil {
				if sampleCtx.Err() != nil {
					timeline.InterruptedScrapes++
					return
				}
				timeline.FailedCollections++
			} else {
				timed := TimedSample{
					StartedAt:  collectionStarted.UTC().Format(time.RFC3339Nano),
					FinishedAt: finished.UTC().Format(time.RFC3339Nano),
					DurationMS: float64(finished.Sub(collectionStarted)) / float64(time.Millisecond),
					Samples:    snapshot.Samples,
				}
				if !budget.storeSample(timed) {
					timeline.CollectionLimitHit = true
					return
				}
				timeline.Samples = append(timeline.Samples, timed)
			}
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	result := run()
	cancel()
	<-done
	finished := time.Now()
	timeline.FinishedAt = finished.UTC().Format(time.RFC3339Nano)
	return result, timeline
}

func parse(reader io.Reader) ([]Sample, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("metrics response exceeds %d bytes", maxBodyBytes)
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	samples := make([]Sample, 0)
	metricNames := map[string]struct{}{}
	types := map[string]string{}
	units := map[string]string{}
	seenSeries := map[string]struct{}{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# TYPE ") || strings.HasPrefix(line, "# UNIT ") {
			fields := strings.Fields(line)
			if len(fields) != 4 || !validName(fields[2]) {
				return nil, fmt.Errorf("invalid metric metadata")
			}
			if fields[1] == "TYPE" {
				types[fields[2]] = fields[3]
			} else {
				units[fields[2]] = fields[3]
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, ok := splitSample(line)
		if !ok {
			return nil, fmt.Errorf("invalid metric line")
		}
		name := key
		var labels map[string]string
		if index := strings.IndexByte(key, '{'); index >= 0 {
			name = key[:index]
			if !strings.HasSuffix(key, "}") {
				return nil, fmt.Errorf("invalid labels for %q", name)
			}
			var err error
			labels, err = parseLabels(key[index+1 : len(key)-1])
			if err != nil {
				return nil, fmt.Errorf("invalid labels for %q: %w", name, err)
			}
		}
		if len(name) > maxMetricNameBytes || !validName(name) {
			return nil, fmt.Errorf("invalid metric name %q", name)
		}
		fields := strings.Fields(rest)
		if len(fields) != 1 {
			return nil, fmt.Errorf("unsupported metric sample for %q", name)
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid metric value for %q", name)
		}
		metricNames[name] = struct{}{}
		if len(metricNames) > maxMetricNames {
			return nil, fmt.Errorf("metrics response exceeds %d distinct metric names", maxMetricNames)
		}
		identity := seriesIdentity(name, labels)
		seriesKey := name + "\x00" + identity
		if _, duplicate := seenSeries[seriesKey]; duplicate {
			return nil, fmt.Errorf("duplicate metric series %q", name)
		}
		seenSeries[seriesKey] = struct{}{}
		samples = append(samples, Sample{Name: name, Value: value, Type: types[name], Unit: units[name], SeriesID: identity})
		if len(samples) > maxMetricSeries {
			return nil, fmt.Errorf("metrics response exceeds %d metric series", maxMetricSeries)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read metrics: %w", err)
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].Name != samples[j].Name {
			return samples[i].Name < samples[j].Name
		}
		return samples[i].SeriesID < samples[j].SeriesID
	})
	return samples, nil
}

func seriesIdentity(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, key := range keys {
		b.WriteByte(0)
		b.WriteString(key)
		b.WriteByte(0)
		b.WriteString(strconv.Quote(labels[key]))
	}
	digest := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(digest[:])
}

func splitSample(line string) (string, string, bool) {
	inQuote, escaped := false, false
	for i := 0; i < len(line); i++ {
		switch {
		case escaped:
			escaped = false
		case inQuote && line[i] == '\\':
			escaped = true
		case line[i] == '"':
			inQuote = !inQuote
		case !inQuote && (line[i] == ' ' || line[i] == '\t'):
			return line[:i], line[i+1:], true
		}
	}
	return "", "", false
}

func parseLabels(raw string) (map[string]string, error) {
	labels := map[string]string{}
	for raw != "" {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 || !validName(raw[:eq]) || len(raw) <= eq+1 || raw[eq+1] != '"' {
			return nil, fmt.Errorf("malformed label")
		}
		name := raw[:eq]
		if _, exists := labels[name]; exists {
			return nil, fmt.Errorf("duplicate label")
		}
		end := eq + 2
		for end < len(raw) {
			if raw[end] == '\\' {
				end += 2
				continue
			}
			if raw[end] == '"' {
				break
			}
			end++
		}
		if end >= len(raw) {
			return nil, fmt.Errorf("unterminated label")
		}
		value, err := strconv.Unquote(raw[eq+1 : end+1])
		if err != nil {
			return nil, err
		}
		labels[name] = value
		raw = raw[end+1:]
		if raw != "" {
			if raw[0] != ',' {
				return nil, fmt.Errorf("malformed label separator")
			}
			raw = raw[1:]
		}
	}
	return labels, nil
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
