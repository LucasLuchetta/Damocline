package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ValidateMetricCountersInArtifactJSON rejects persisted run evidence where
// required counters were omitted and would otherwise decode as measured zero.
// It handles a run directly and run reports embedded in campaigns and
// exploration results. Optional diagnostic structures remain optional.
func ValidateMetricCountersInArtifactJSON(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return errors.New("evidence must be a JSON object")
	}
	if _, ok := root["metrics"]; ok {
		if err := validateRunMetricCounters(data, "run"); err != nil {
			return err
		}
	} else if _, campaign := root["trials"]; !campaign {
		if _, exploration := root["probes"]; !exploration {
			return errors.New("run report omits required metrics")
		}
	}
	if trials, ok := root["trials"]; ok {
		if err := validateTrialsMetricCounters(trials, "trials"); err != nil {
			return err
		}
	}
	if pending, ok := root["pending_trial"]; ok && !bytes.Equal(bytes.TrimSpace(pending), []byte("null")) {
		if err := validateTrialMetricCounters(pending, "pending_trial"); err != nil {
			return err
		}
	}
	if probes, ok := root["probes"]; ok {
		if err := validateProbesMetricCounters(probes); err != nil {
			return err
		}
	}
	return nil
}

func validateRunMetricCounters(raw json.RawMessage, path string) error {
	var run map[string]json.RawMessage
	if err := json.Unmarshal(raw, &run); err != nil || run == nil {
		return fmt.Errorf("%s must be an object", path)
	}
	schemaVersion := 0
	if value, ok := run["schema_version"]; ok {
		_ = json.Unmarshal(value, &schemaVersion)
	}
	metrics, ok := run["metrics"]
	if !ok {
		return fmt.Errorf("%s omits required request metrics", path)
	}
	if err := validateMetricSetCounters(metrics, path+".metrics"); err != nil {
		return err
	}
	for _, key := range []string{"journeys"} {
		if value, present := run[key]; present {
			if err := validateMetricSetCounters(value, path+"."+key); err != nil {
				return err
			}
		}
	}
	if err := validateMetricList(run["steps"], path+".steps", "metrics"); err != nil {
		return err
	}
	if err := validateMetricList(run["profiles"], path+".profiles", "journeys"); err != nil {
		return err
	}
	if windows, ok := run["windows"]; ok && !bytes.Equal(bytes.TrimSpace(windows), []byte("null")) {
		var values []json.RawMessage
		if err := json.Unmarshal(windows, &values); err != nil {
			return fmt.Errorf("%s.windows must be an array", path)
		}
		for i, value := range values {
			windowPath := fmt.Sprintf("%s.windows[%d]", path, i)
			var window map[string]json.RawMessage
			if err := json.Unmarshal(value, &window); err != nil || window == nil {
				return fmt.Errorf("%s must be an object", windowPath)
			}
			for _, key := range []string{"metrics", "journeys"} {
				metric, present := window[key]
				if key == "journeys" && schemaVersion < 22 {
					if present {
						if err := validateMetricSetCounters(metric, windowPath+"."+key); err != nil {
							return err
						}
					}
					continue
				}
				if !present {
					return fmt.Errorf("%s omits required %s metrics", windowPath, key)
				}
				if err := validateMetricSetCounters(metric, windowPath+"."+key); err != nil {
					return err
				}
			}
			for _, key := range []string{"steps", "profiles"} {
				childKey := "metrics"
				if key == "profiles" {
					childKey = "metrics"
				}
				if err := validateMetricList(window[key], windowPath+"."+key, childKey); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateMetricSetCounters(raw json.RawMessage, path string) error {
	var metric map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metric); err != nil || metric == nil {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, key := range []string{"total", "success", "failures"} {
		if _, ok := metric[key]; !ok {
			return fmt.Errorf("%s omits required %s counter; absence cannot be interpreted as zero", path, key)
		}
	}
	return nil
}

func validateMetricList(raw json.RawMessage, path, metricKey string) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s must be an array", path)
	}
	for i, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		var item map[string]json.RawMessage
		if err := json.Unmarshal(value, &item); err != nil || item == nil {
			return fmt.Errorf("%s must be an object", itemPath)
		}
		metric, ok := item[metricKey]
		if !ok {
			return fmt.Errorf("%s omits required %s metrics", itemPath, metricKey)
		}
		if err := validateMetricSetCounters(metric, itemPath+"."+metricKey); err != nil {
			return err
		}
	}
	return nil
}

func validateTrialsMetricCounters(raw json.RawMessage, path string) error {
	var trials []json.RawMessage
	if err := json.Unmarshal(raw, &trials); err != nil {
		return fmt.Errorf("%s must be an array", path)
	}
	for i, trial := range trials {
		if err := validateTrialMetricCounters(trial, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateTrialMetricCounters(raw json.RawMessage, path string) error {
	var trial map[string]json.RawMessage
	if err := json.Unmarshal(raw, &trial); err != nil || trial == nil {
		return fmt.Errorf("%s must be an object", path)
	}
	phases, ok := trial["phases"]
	if !ok || bytes.Equal(bytes.TrimSpace(phases), []byte("null")) {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(phases, &values); err != nil {
		return fmt.Errorf("%s.phases must be an array", path)
	}
	for i, value := range values {
		phasePath := fmt.Sprintf("%s.phases[%d]", path, i)
		var phase map[string]json.RawMessage
		if err := json.Unmarshal(value, &phase); err != nil || phase == nil {
			return fmt.Errorf("%s must be an object", phasePath)
		}
		run, ok := phase["run"]
		if !ok {
			return fmt.Errorf("%s omits its run report", phasePath)
		}
		if err := validateRunMetricCounters(run, phasePath+".run"); err != nil {
			return err
		}
	}
	return nil
}

func validateProbesMetricCounters(raw json.RawMessage) error {
	var probes []json.RawMessage
	if err := json.Unmarshal(raw, &probes); err != nil {
		return errors.New("probes must be an array")
	}
	for i, probeRaw := range probes {
		path := fmt.Sprintf("probes[%d]", i)
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(probeRaw, &probe); err != nil || probe == nil {
			return fmt.Errorf("%s must be an object", path)
		}
		runs, ok := probe["runs"]
		if !ok || bytes.Equal(bytes.TrimSpace(runs), []byte("null")) {
			continue
		}
		var values []json.RawMessage
		if err := json.Unmarshal(runs, &values); err != nil {
			return fmt.Errorf("%s.runs must be an array", path)
		}
		for j, run := range values {
			if err := validateRunMetricCounters(run, fmt.Sprintf("%s.runs[%d]", path, j)); err != nil {
				return err
			}
		}
	}
	return nil
}
