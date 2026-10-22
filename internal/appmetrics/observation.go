package appmetrics

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

const (
	maxObservationSnapshots     = 60
	maxObservationSeries        = 128
	maxObservationSnapshotBytes = 8 << 10
)

type CollectionFailure struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Observation is the schema-v1 phase-level application-metrics format. New
// runs use Timeline on report.Result; this type remains for offline replay.
type Observation struct {
	StartedAt string              `json:"started_at"`
	EndedAt   string              `json:"ended_at"`
	Interval  string              `json:"interval"`
	Attempts  int                 `json:"attempts"`
	Snapshots []Snapshot          `json:"snapshots"`
	Failures  []CollectionFailure `json:"failures,omitempty"`
	Coverage  string              `json:"coverage"`
	Reason    string              `json:"reason,omitempty"`
}

// State returns the saved coverage label for display. Auditors must call
// ReassessCoverage before using a persisted label as protocol evidence.
func (o Observation) State() string { return o.Coverage }

func ReassessCoverage(o Observation) (string, error) {
	start, err := time.Parse(time.RFC3339Nano, o.StartedAt)
	if err != nil {
		return "", fmt.Errorf("invalid application metrics start time")
	}
	end, err := time.Parse(time.RFC3339Nano, o.EndedAt)
	if err != nil || end.Before(start) {
		return "", fmt.Errorf("invalid application metrics end time")
	}
	interval, err := time.ParseDuration(o.Interval)
	if err != nil || interval < time.Second || interval > 30*time.Second {
		return "", fmt.Errorf("invalid application metrics interval")
	}
	if o.Attempts < 0 || o.Attempts > maxObservationSnapshots || len(o.Snapshots) > o.Attempts || len(o.Failures) > o.Attempts+1 {
		return "", fmt.Errorf("invalid application metrics collection counts")
	}
	previous := start
	gapExceeded := false
	for _, failure := range o.Failures {
		at, parseErr := time.Parse(time.RFC3339Nano, failure.At)
		if parseErr != nil || at.Before(start) || at.After(end) || failure.Reason == "" {
			return "", fmt.Errorf("invalid application metrics collection failure")
		}
	}
	for _, snapshot := range o.Snapshots {
		encoded, marshalErr := json.Marshal(snapshot)
		if marshalErr != nil || snapshot.SchemaVersion != 2 || len(snapshot.Samples) == 0 || len(snapshot.Samples) > maxObservationSeries || len(encoded) > maxObservationSnapshotBytes {
			return "", fmt.Errorf("invalid application metrics snapshot")
		}
		if err := validateLegacySource(snapshot.Source); err != nil {
			return "", err
		}
		at, parseErr := time.Parse(time.RFC3339Nano, snapshot.CollectedAt)
		if parseErr != nil || at.Before(previous) || at.After(end) {
			return "", fmt.Errorf("invalid application metrics snapshot timestamp")
		}
		if at.Sub(previous) > 2*interval {
			gapExceeded = true
		}
		previous = at
		seen := make(map[string]struct{}, len(snapshot.Samples))
		for _, sample := range snapshot.Samples {
			if !validName(sample.Name) || len(sample.Name) > maxMetricNameBytes || !finiteValue(sample.Value) {
				return "", fmt.Errorf("invalid application metric sample")
			}
			key := sample.Name + "\x00" + sample.SeriesID
			if _, exists := seen[key]; exists {
				return "", fmt.Errorf("duplicate application metric series")
			}
			seen[key] = struct{}{}
		}
	}
	reason := ""
	if len(o.Failures) > 0 {
		reason = "one or more metrics scrapes failed"
	} else if len(o.Snapshots) < 2 {
		reason = "fewer than two metrics snapshots were collected"
	} else if gapExceeded {
		reason = "metrics collection gap exceeds two intervals"
	} else if end.Sub(previous) > 2*interval {
		reason = "metrics collection ended more than two intervals before phase end"
	}
	if reason != o.Reason {
		return "", fmt.Errorf("stale application metrics coverage reason")
	}
	if reason == "" {
		if o.Coverage != "COMPLETE" {
			return "", fmt.Errorf("stale application metrics coverage")
		}
		return "COMPLETE", nil
	}
	if o.Coverage != "INCOMPLETE" {
		return "", fmt.Errorf("stale application metrics coverage")
	}
	return "INCOMPLETE", nil
}

func validateLegacySource(source string) error {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid application metrics source")
	}
	return nil
}

func finiteValue(value float64) bool {
	return value == value && value < 1.7976931348623157e308 && value > -1.7976931348623157e308
}

// TimelineFromObservation adapts validated legacy scrapes for shared report
// summaries without rewriting their stored campaign JSON.
func TimelineFromObservation(o Observation) *Timeline {
	timeline := &Timeline{SchemaVersion: TimelineSchemaVersion, Interval: o.Interval, StartedAt: o.StartedAt, FinishedAt: o.EndedAt, Samples: make([]TimedSample, 0, len(o.Snapshots)), FailedCollections: len(o.Failures), LegacyIncomplete: o.Coverage != "COMPLETE"}
	if len(o.Snapshots) > 0 {
		timeline.Source, _ = ValidateEndpoint(o.Snapshots[0].Source)
	}
	for _, snapshot := range o.Snapshots {
		timeline.Samples = append(timeline.Samples, TimedSample{StartedAt: snapshot.CollectedAt, FinishedAt: snapshot.CollectedAt, Samples: snapshot.Samples})
	}
	return timeline
}
