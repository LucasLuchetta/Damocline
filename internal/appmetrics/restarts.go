package appmetrics

import (
	"math"
	"sort"
	"time"
)

type ProcessRestartEvidence struct {
	Coverage         string `json:"coverage"`
	Samples          int    `json:"samples"`
	Series           int    `json:"series"`
	ObservedRestarts uint64 `json:"observed_restarts"`
}

// ObserveProcessRestarts compares process_start_time_seconds for stable
// label-derived series identities. Missing or partial timelines remain
// unavailable; the metric is observed evidence, not a container restart log.
func ObserveProcessRestarts(timelines []*Timeline) ProcessRestartEvidence {
	evidence := ProcessRestartEvidence{Coverage: "UNAVAILABLE"}
	type point struct {
		at    time.Time
		value float64
	}
	series := make(map[string][]point)
	complete := len(timelines) > 0
	for _, timeline := range timelines {
		if timeline == nil || timeline.State() != "COLLECTED" {
			complete = false
			continue
		}
		for _, snapshot := range timeline.Samples {
			at, err := time.Parse(time.RFC3339Nano, snapshot.FinishedAt)
			if err != nil {
				complete = false
				continue
			}
			for _, sample := range snapshot.Samples {
				if sample.Name != "process_start_time_seconds" {
					continue
				}
				if math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) || sample.Value < 0 {
					complete = false
					continue
				}
				identity := sample.SeriesID
				if identity == "" {
					identity = sample.Name
				}
				series[identity] = append(series[identity], point{at: at, value: sample.Value})
				evidence.Samples++
			}
		}
	}
	evidence.Series = len(series)
	if evidence.Samples == 0 {
		return evidence
	}
	for _, points := range series {
		sort.Slice(points, func(i, j int) bool { return points[i].at.Before(points[j].at) })
		for i := 1; i < len(points); i++ {
			if points[i].value > points[i-1].value {
				evidence.ObservedRestarts++
			}
		}
	}
	if complete {
		evidence.Coverage = "COMPLETE"
	} else {
		evidence.Coverage = "PARTIAL"
	}
	return evidence
}
