package hostsensor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 64 << 10

type RemoteSample struct {
	SentAt      string  `json:"sent_at"`
	ReceivedAt  string  `json:"received_at"`
	OffsetMinMS float64 `json:"clock_offset_min_ms"`
	OffsetMaxMS float64 `json:"clock_offset_max_ms"`
	RoundTripMS float64 `json:"round_trip_ms"`
	Reading     Reading `json:"reading"`
}

func ValidateEndpoint(endpoint, token string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Host == "" || u.Path != Path || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("host sensor endpoint must be an HTTP(S) URL ending in %s without credentials, query, or fragment", Path)
	}
	if token == "" {
		return fmt.Errorf("host sensor bearer token is required")
	}
	if u.Scheme != "https" && !loopbackHost(u.Hostname()) {
		return fmt.Errorf("remote host sensor requires HTTPS")
	}
	return nil
}

// CollectRemote ties a sensor reading to a declared experiment and bounds the
// possible clock offset using local send and receive timestamps.
func CollectRemote(ctx context.Context, endpoint, token, experimentID string) (RemoteSample, error) {
	if err := ValidateEndpoint(endpoint, token); err != nil {
		return RemoteSample{}, err
	}
	if !ValidExperimentID(experimentID) {
		return RemoteSample{}, fmt.Errorf("invalid experiment ID")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RemoteSample{}, fmt.Errorf("build sensor request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Damocline-Experiment-ID", experimentID)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sent := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return RemoteSample{}, fmt.Errorf("collect host sensor: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RemoteSample{}, fmt.Errorf("host sensor returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	received := time.Now()
	if err != nil {
		return RemoteSample{}, fmt.Errorf("read host sensor: %w", err)
	}
	if len(body) > maxResponseBytes {
		return RemoteSample{}, fmt.Errorf("host sensor response exceeds %d bytes", maxResponseBytes)
	}
	var reading Reading
	if err := json.Unmarshal(body, &reading); err != nil {
		return RemoteSample{}, fmt.Errorf("invalid host sensor response: %w", err)
	}
	if reading.SchemaVersion != 1 || reading.ExperimentID != experimentID || reading.Host.SchemaVersion != 1 || len(reading.Host.Samples) != 1 || !strings.HasPrefix(reading.Host.Source, "linux:") {
		return RemoteSample{}, fmt.Errorf("host sensor response has mismatched identity or schema")
	}
	observed, err := time.Parse(time.RFC3339Nano, reading.ObservedAt)
	if err != nil {
		return RemoteSample{}, fmt.Errorf("host sensor observation time is invalid")
	}
	sentAt := sent.UTC().Format(time.RFC3339Nano)
	receivedAt := received.UTC().Format(time.RFC3339Nano)
	persistedSent, _ := time.Parse(time.RFC3339Nano, sentAt)
	persistedReceived, _ := time.Parse(time.RFC3339Nano, receivedAt)
	roundTrip := persistedReceived.Sub(persistedSent)
	if roundTrip < 0 {
		return RemoteSample{}, fmt.Errorf("local wall clock moved backwards during host collection")
	}
	return RemoteSample{
		SentAt: sentAt, ReceivedAt: receivedAt,
		OffsetMinMS: float64(observed.Sub(persistedReceived)) / float64(time.Millisecond),
		OffsetMaxMS: float64(observed.Sub(persistedSent)) / float64(time.Millisecond),
		RoundTripMS: float64(roundTrip) / float64(time.Millisecond), Reading: reading,
	}, nil
}
