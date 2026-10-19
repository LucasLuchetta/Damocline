// Package appmetrics collects bounded, permissioned application metric snapshots.
package appmetrics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
const MaxTimelineSamples = 1000
const MaxTimelineBytes = 4 << 20
const MinTimelineInterval = time.Second
const MaxTimelineInterval = time.Minute

const TimelineSchemaVersion = 1

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
	if t.FailedCollections > 0 || t.InterruptedScrapes > 0 || t.CollectionLimitHit {
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
			math.IsNaN(sample.DurationMS) || math.IsInf(sample.DurationMS, 0) || sample.DurationMS < 0 || len(sample.Samples) > maxMetricNames {
			return fmt.Errorf("invalid application-metrics sample bounds")
		}
		previousEnd = collectionEnd
		previous := ""
		for _, metric := range sample.Samples {
			if len(metric.Name) > maxMetricNameBytes || !validName(metric.Name) || metric.Name <= previous || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
				return fmt.Errorf("invalid application-metrics value")
			}
			previous = metric.Name
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
	source, err := ValidateEndpoint(endpoint)
	if err != nil {
		return Snapshot{}, err
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
	samples, err := parse(response.Body)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{SchemaVersion: 1, Source: source, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Samples: samples, Note: "Direct application metric observations only; values are not proof of a bottleneck or capacity cause."}, nil
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
		if len(name) > maxMetricNameBytes || !validName(name) {
			return nil, fmt.Errorf("invalid metric name %q", name)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid metric value for %q", name)
		}
		values[name] += value
		if math.IsNaN(values[name]) || math.IsInf(values[name], 0) {
			return nil, fmt.Errorf("aggregated metric value for %q is not finite", name)
		}
		if len(values) > maxMetricNames {
			return nil, fmt.Errorf("metrics response exceeds %d distinct metric names", maxMetricNames)
		}
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
