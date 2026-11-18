package main

import (
	"strings"
	"testing"
)

func TestStageCPUAvailabilityUsesReportSchema(t *testing.T) {
	for _, tc := range []struct {
		name         string
		version      int
		processValid bool
		wantIssue    bool
	}{
		{"current process evidence", 33, true, false},
		{"legacy requires both", 32, true, true},
		{"process evidence remains mandatory", 33, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := passingStageFixture()
			run.SchemaVersion = tc.version
			run.Generator.GoCPUValid = false
			run.Generator.GoCPUUtilizationRatio = 0
			run.Generator.ProcessCPUValid = tc.processValid
			issues := checkStage(2, 10, run)
			found := strings.Contains(strings.Join(issues, " "), "generator envelope")
			if found != tc.wantIssue {
				t.Fatalf("generator issue=%t want=%t: %v", found, tc.wantIssue, issues)
			}
		})
	}
}
