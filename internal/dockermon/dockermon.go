// Package dockermon collects one bounded Docker container resource snapshot.
package dockermon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxOutputBytes = 1 << 20

type Container struct {
	Name        string  `json:"name"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	MemoryLimit uint64  `json:"memory_limit_bytes"`
}

type Snapshot struct {
	SchemaVersion int         `json:"schema_version"`
	CollectedAt   string      `json:"collected_at"`
	Containers    []Container `json:"containers"`
	Note          string      `json:"note"`
}

func Collect(ctx context.Context) (Snapshot, error) {
	command := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}")
	output, err := command.Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("docker stats: %w", err)
	}
	if len(output) > maxOutputBytes {
		return Snapshot{}, fmt.Errorf("docker stats output exceeds 1 MiB")
	}
	containers, err := parse(string(output))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{SchemaVersion: 1, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Containers: containers, Note: "Point-in-time Docker resource observations; CPU or memory pressure does not prove a bottleneck cause."}, nil
}

func parse(text string) ([]Container, error) {
	containers := []Container{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var raw struct {
			Name   string `json:"Name"`
			CPU    string `json:"CPUPerc"`
			Memory string `json:"MemUsage"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("decode docker stats: %w", err)
		}
		cpu, err := percent(raw.CPU)
		if err != nil {
			return nil, fmt.Errorf("container %q CPU: %w", raw.Name, err)
		}
		parts := strings.Split(raw.Memory, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("container %q memory format", raw.Name)
		}
		used, err := bytes(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, err
		}
		limit, err := bytes(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, err
		}
		containers = append(containers, Container{Name: raw.Name, CPUPercent: cpu, MemoryBytes: used, MemoryLimit: limit})
	}
	return containers, nil
}

func percent(value string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
}
func bytes(value string) (uint64, error) {
	value = strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(value)), " ", "")
	for _, unit := range []struct {
		name       string
		multiplier float64
	}{{"GIB", 1024 * 1024 * 1024}, {"MIB", 1024 * 1024}, {"KIB", 1024}, {"B", 1}} {
		if !strings.HasSuffix(value, unit.name) {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.name), 64)
		if err != nil {
			return 0, err
		}
		return uint64(n * unit.multiplier), nil
	}
	return 0, fmt.Errorf("unsupported memory value %q", value)
}
