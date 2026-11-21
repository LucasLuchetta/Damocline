package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

// capacityMaxStepDowns bounds how many lower explore levels capacity may run
// a sustained campaign at after the selected level fails (ADR 0176). The
// levels are chosen by bisection over the repeatedly passing explore levels
// (ADR 0179).
const capacityMaxStepDowns = 3

const capacityLadderFile = "campaign-ladder.json"

// capacityLadderEntry records one sustained campaign attempted by capacity.
type capacityLadderEntry struct {
	Users    int                    `json:"users"`
	Campaign string                 `json:"campaign,omitempty"`
	Outcome  string                 `json:"outcome"`
	Reason   string                 `json:"reason,omitempty"`
	Recovery *capacityRecoveryCheck `json:"recovery,omitempty"`
}

// capacityLadder is the private ledger of the sustained campaigns run by one
// capacity protocol, in execution order (highest level first).
type capacityLadder struct {
	SchemaVersion    int                   `json:"schema_version"`
	ExploreStage     string                `json:"explore_stage"`
	MaxStepDowns     int                   `json:"max_step_downs"`
	Entries          []capacityLadderEntry `json:"entries"`
	SustainableUsers *int                  `json:"sustainable_users,omitempty"`
	AuditedCampaign  string                `json:"audited_campaign"`
	// StopReason is set when the ladder ended before bisection converged,
	// for example because the target did not recover between campaigns.
	StopReason string `json:"stop_reason,omitempty"`
}

// capacityStepDownLevels returns up to limit (all when limit < 1) repeatedly
// passing explore levels below selected, highest first. Explore under --require-calibration
// only probes calibrated levels, so each candidate is a calibrated level.
func capacityStepDownLevels(result explorer.Result, selected, limit int) []int {
	seen := map[int]bool{}
	var levels []int
	for _, probe := range result.Probes {
		if probe.Status != "PASS" || len(probe.Runs) != result.Repeats || probe.Users >= selected || probe.Users < 1 || seen[probe.Users] {
			continue
		}
		seen[probe.Users] = true
		levels = append(levels, probe.Users)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(levels)))
	if limit > 0 && len(levels) > limit {
		levels = levels[:limit]
	}
	return levels
}

// capacityStepDownAllowed reports whether a completed campaign failed on the
// declared latency or error objectives. INCONCLUSIVE (invalid delivery,
// insufficient evidence, drift) and ABORTED (including GENERATOR_LIMITED)
// are not target verdicts and never trigger a step-down.
func capacityStepDownAllowed(evidence campaign.Result) bool {
	return evidence.ExecutionState == "COMPLETED" && evidence.Outcome == "FAIL"
}

func capacityRequiredSamples(v config.Validated) (int, error) {
	required, err := statistics.ZeroFailureSamples95(v.Scenario.MaxErrorRate)
	if err != nil {
		return 0, err
	}
	p95Samples, err := statistics.MinimumSamplesForPercentileRankInterval95(.95)
	if err != nil {
		return 0, err
	}
	return max(required, p95Samples), nil
}

func capacityCampaignFiles(index, users int) (campaignFile, scenarioFile, calibrationFile string) {
	if index == 0 {
		return "campaign.json", "campaign-scenario.json", "selfcal-campaign.json"
	}
	n := strconv.Itoa(users)
	return "campaign-stepdown-" + n + ".json", "campaign-scenario-stepdown-" + n + ".json", "selfcal-campaign-stepdown-" + n + ".json"
}

// capacityBisection searches the ascending candidate levels for the highest
// level whose sustained campaign passes. Indices below lo are known or assumed
// lower; indices at or above hi are known to fail. lo == -1 means no
// candidate has passed yet; hi starts at len(levels) because the selected
// level above every candidate already failed.
type capacityBisection struct {
	levels []int
	lo, hi int
}

func newCapacityBisection(stepDowns []int) *capacityBisection {
	levels := append([]int(nil), stepDowns...)
	sort.Ints(levels)
	return &capacityBisection{levels: levels, lo: -1, hi: len(levels)}
}

// next returns the upper middle of the open interval, or false when the
// search has converged.
func (b *capacityBisection) next() (int, bool) {
	if b.hi-b.lo <= 1 {
		return 0, false
	}
	return b.levels[b.lo+(b.hi-b.lo+1)/2], true
}

func (b *capacityBisection) record(level int, passed bool) {
	for i, candidate := range b.levels {
		if candidate != level {
			continue
		}
		if passed {
			b.lo = i
		} else {
			b.hi = i
		}
		return
	}
}

// finishCapacityLadder selects the audited campaign: the highest level whose
// sustained campaign passed, otherwise the originally selected level.
func finishCapacityLadder(ladder *capacityLadder) {
	ladder.SustainableUsers, ladder.AuditedCampaign = nil, ""
	for _, entry := range ladder.Entries {
		if entry.Outcome == "PASS" && entry.Campaign != "" && (ladder.SustainableUsers == nil || entry.Users > *ladder.SustainableUsers) {
			users := entry.Users
			ladder.SustainableUsers, ladder.AuditedCampaign = &users, entry.Campaign
		}
	}
	if ladder.SustainableUsers != nil {
		return
	}
	if len(ladder.Entries) > 0 {
		ladder.AuditedCampaign = ladder.Entries[0].Campaign
	}
}

func readCapacityLadder(dir string) (*capacityLadder, error) {
	data, err := readCapacityFile(filepath.Join(dir, capacityLadderFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ladder capacityLadder
	if err := json.Unmarshal(data, &ladder); err != nil || ladder.SchemaVersion != 1 ||
		ladder.AuditedCampaign == "" || filepath.Base(ladder.AuditedCampaign) != ladder.AuditedCampaign {
		return nil, errors.New("saved campaign ladder is malformed")
	}
	return &ladder, nil
}

func capacityLadderSummary(ladder capacityLadder) []string {
	var lines []string
	var failed []string
	for _, entry := range ladder.Entries {
		if entry.Outcome == "FAIL" {
			failed = append(failed, strconv.Itoa(entry.Users))
		}
	}
	if ladder.SustainableUsers != nil {
		lines = append(lines, fmt.Sprintf("Sustainable level: %d VUs, the highest level whose sustained campaign passed. The audit in this report covers that campaign.", *ladder.SustainableUsers))
		if len(failed) > 0 {
			lines = append(lines, fmt.Sprintf("%s VUs passed the short explore stages but FAILED the sustained campaign.", strings.Join(failed, ", ")))
		}
	} else {
		var levels []string
		for _, entry := range ladder.Entries {
			levels = append(levels, strconv.Itoa(entry.Users))
		}
		lines = append(lines, fmt.Sprintf("No sustained campaign passed. This protocol reports no sustainable level; %s VUs passed short explore stages only. The audit in this report covers the originally selected campaign.", strings.Join(levels, ", ")))
	}
	if len(failed) > 0 {
		lines = append(lines, fmt.Sprintf("Time-dependent knee: %s VUs passed %s explore stages but failed when held longer. In plain language, the target keeps up for a short burst at that load but falls behind when the load continues, so waiting work (queues, backlogs, memory pressure) likely grows over time. Treat the short-stage pass as a burst capacity, not a sustainable one.", strings.Join(failed, ", "), ladder.ExploreStage))
	}
	for _, entry := range ladder.Entries {
		if entry.Recovery != nil && entry.Recovery.Status == capacityRecoveryUnverified {
			lines = append(lines, fmt.Sprintf("Recovery before the %d VU campaign was not verified (no network-path reference); carry-over from the previous campaign cannot be excluded.", entry.Users))
		}
	}
	if ladder.StopReason != "" {
		lines = append(lines, "The ladder stopped early: "+ladder.StopReason+". Lower levels were not run, so no FAIL is reported for them; repeat after the target has idled.")
	}
	lines = append(lines,
		fmt.Sprintf("Protocol note: explore stages lasted %s each, while a campaign holds its level through warm-up, a sample-sized observation and recovery, twice. Short stages can overestimate sustained capacity. The gap is a measured fact for this run; its cause (for example queue growth or pool exhaustion) is a hypothesis to investigate.", ladder.ExploreStage),
		fmt.Sprintf("Step-downs bisect over the repeatedly passing explore levels, are bounded to %d, are each preceded by a recovery gate, and continue only after a campaign PASS or FAIL on latency or errors; INCONCLUSIVE or ABORTED campaigns (including generator limits) stop the ladder.", ladder.MaxStepDowns))
	return lines
}

func renderCapacityLadderMarkdown(ladder capacityLadder) string {
	var b strings.Builder
	b.WriteString("\n## Sustained campaign step-down\n\n| VUs | Campaign | Outcome | Note |\n|---:|---|---|---|\n")
	for _, entry := range ladder.Entries {
		fmt.Fprintf(&b, "| %d | %s | %s | %s |\n", entry.Users, markdownText(orDash(entry.Campaign)), entry.Outcome, markdownText(orDash(capacityEntryNote(entry))))
	}
	b.WriteString("\n")
	for _, line := range capacityLadderSummary(ladder) {
		b.WriteString("- " + markdownText(line) + "\n")
	}
	return b.String()
}

func renderCapacityLadderHTML(ladder capacityLadder) string {
	var b strings.Builder
	b.WriteString("<section aria-labelledby=\"step-down\">\n<h2 id=\"step-down\">Sustained campaign step-down</h2>\n<table>\n<thead><tr><th>VUs</th><th>Campaign</th><th>Outcome</th><th>Note</th></tr></thead>\n<tbody>\n")
	for _, entry := range ladder.Entries {
		fmt.Fprintf(&b, "<tr><td>%d</td><td><code>%s</code></td><td>%s</td><td>%s</td></tr>\n", entry.Users, h(orDash(entry.Campaign)), h(entry.Outcome), h(orDash(capacityEntryNote(entry))))
	}
	b.WriteString("</tbody>\n</table>\n<ul>\n")
	for _, line := range capacityLadderSummary(ladder) {
		b.WriteString("<li>" + h(line) + "</li>\n")
	}
	b.WriteString("</ul>\n</section>\n")
	return b.String()
}

func capacityEntryNote(entry capacityLadderEntry) string {
	note := entry.Reason
	if r := entry.Recovery; r != nil && entry.Outcome != capacityOutcomeNotRecovered {
		gate := "recovery " + r.Status + " after " + r.Waited
		if len(r.ProbeP50MS) > 0 {
			gate += fmt.Sprintf(" (probe p50 %.1f ms, limit %.1f ms)", r.ProbeP50MS[len(r.ProbeP50MS)-1], r.LimitP50MS)
		}
		if note != "" {
			note += "; "
		}
		note += gate
	}
	return note
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// writeCapacityLadderReports composes the final capacity.md and capacity.html
// from the audit outputs plus the step-down section. Existing outputs are kept
// so a resumed protocol never overwrites a completed report.
func writeCapacityLadderReports(markdownSource, htmlSource, markdownPath, htmlPath string, ladder capacityLadder) error {
	if _, err := os.Stat(htmlPath); os.IsNotExist(err) {
		data, err := readCapacityFile(htmlSource)
		if err != nil {
			return err
		}
		page := string(data)
		section := renderCapacityLadderHTML(ladder)
		if i := strings.Index(page, "</header>\n"); i >= 0 {
			page = page[:i+len("</header>\n")] + section + page[i+len("</header>\n"):]
		} else if i := strings.LastIndex(page, "</main>"); i >= 0 {
			page = page[:i] + section + page[i:]
		} else {
			return errors.New("audit HTML report has no insertion point for the step-down section")
		}
		if err := writeCapacityMarkdown(htmlPath, page); err != nil {
			return err
		}
	}
	if _, err := os.Stat(markdownPath); os.IsNotExist(err) {
		data, err := readCapacityFile(markdownSource)
		if err != nil {
			return err
		}
		return writeCapacityMarkdown(markdownPath, string(data)+renderCapacityLadderMarkdown(ladder))
	}
	return nil
}

func capacityStepDownPlanLine(budget time.Duration) string {
	return fmt.Sprintf("     If that campaign FAILs on latency or errors (not INCONCLUSIVE or ABORTED, such as generator or measurement validity), bisect over the lower repeatedly passing calibrated explore levels with at most %d campaigns (%s cap each). Before each, a recovery gate waits %s and then probes the network path until its p50 is within %.0fx the pre-load reference + %.0f ms (at most %s); if the target does not recover, the ladder stops instead of reporting a contaminated FAIL. The sustainable level is the highest level whose campaign passed.",
		capacityMaxStepDowns, budget, capacityRecoveryCooldown, capacityRecoveryRatio, capacityRecoveryMarginMS, capacityRecoveryMaxWait)
}

// capacityFirstStepDown returns the first level the bisection would try.
func capacityFirstStepDown(stepDowns []int) int {
	level, _ := newCapacityBisection(stepDowns).next()
	return level
}
