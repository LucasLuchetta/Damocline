package dockermon

import "testing"

func TestParseDockerStats(t *testing.T) {
	items, err := parse(`{"Name":"api","CPUPerc":"12.50%","MemUsage":"64MiB / 512MiB"}`)
	if err != nil || len(items) != 1 || items[0].CPUPercent != 12.5 || items[0].MemoryBytes != 64*1024*1024 {
		t.Fatalf("unexpected: %+v %v", items, err)
	}
}
