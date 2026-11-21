package cli

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// htmlReportInput is everything the offline executive HTML report renders.
// It is derived only from saved evidence; rendering never contacts a target.
type htmlReportInput struct {
	Assessment capacityplan.Assessment
	Source     string
	Evidence   campaign.Result
	// Explore is optional exploration evidence for the same scenario and
	// target; ExploreSource and ExploreFileSHA256 identify its exact bytes.
	Explore           *explorer.Result
	ExploreSource     string
	ExploreFileSHA256 string
	// ActionsPerUserMinute is a declared usage assumption; zero means none
	// was declared and no people estimate is shown.
	ActionsPerUserMinute float64
}

// Executive outcomes. Only outcomeMet carries an approved level.
const (
	outcomeMet          = "MET"
	outcomeNotMet       = "NOT_MET"
	outcomeInsufficient = "INSUFFICIENT_EVIDENCE"
	outcomeInvalid      = "INVALID_EVIDENCE"
	outcomeNoDemand     = "NO_DEMAND_DECLARED"
)

func executiveOutcome(a capacityplan.Assessment) string {
	switch {
	case a.State == "INVALID_EVIDENCE" || a.Audit.State == "INVALID_EVIDENCE":
		return outcomeInvalid
	case a.Demand == nil:
		return outcomeNoDemand
	case a.State == "GOAL_MET_UNDER_TESTED_CONDITIONS":
		return outcomeMet
	case a.Audit.State == "OBSERVED_FAIL" && a.Demand.TestedUsers <= a.Demand.TargetUsers:
		// A measured breach at or below the requested demand refutes it.
		return outcomeNotMet
	default:
		return outcomeInsufficient
	}
}

type chartPoint struct {
	Source string // "explore" or "campaign"
	Label  string
	Users  int
	P95MS  float64
	RPS    float64
	Errors float64
	Passed bool
	Failed bool // the run recorded an explicit FAIL outcome
}

func runThroughput(run report.Result) float64 {
	seconds := run.ObservationSeconds
	if seconds <= 0 {
		seconds = run.ElapsedSeconds
	}
	if seconds <= 0 || !finiteFloat(seconds) {
		return 0
	}
	return float64(run.Metrics.Success) / seconds
}

func runErrorRate(run report.Result) float64 {
	if run.Metrics.Total <= 0 {
		return 0
	}
	return float64(run.Metrics.Failures) / float64(run.Metrics.Total)
}

func finiteFloat(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func collectChartPoints(in htmlReportInput) []chartPoint {
	var points []chartPoint
	if in.Explore != nil {
		for _, probe := range in.Explore.Probes {
			for index, run := range probe.Runs {
				points = append(points, chartPoint{Source: "explore", Label: fmt.Sprintf("explore %d users, repeat %d", probe.Users, index+1), Users: probe.Users, P95MS: run.Metrics.P95MS, RPS: runThroughput(run), Errors: runErrorRate(run), Passed: run.Outcome == "PASS", Failed: run.Outcome == "FAIL"})
			}
		}
	}
	for _, trial := range in.Evidence.Trials {
		for _, phase := range trial.Phases {
			run := phase.Run
			points = append(points, chartPoint{Source: "campaign", Label: fmt.Sprintf("campaign trial %d %s", trial.Number, phase.Name), Users: run.Config.Users, P95MS: run.Metrics.P95MS, RPS: runThroughput(run), Errors: runErrorRate(run), Passed: run.Outcome == "PASS", Failed: run.Outcome == "FAIL"})
		}
	}
	for i := range points {
		if !finiteFloat(points[i].P95MS) || points[i].P95MS < 0 {
			points[i].P95MS = 0
		}
	}
	return points
}

func objectiveLimits(in htmlReportInput) (p95, errorRate float64) {
	for _, trial := range in.Evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Run.Config.P95LimitMS > 0 {
				return phase.Run.Config.P95LimitMS, phase.Run.Config.MaxErrorRate
			}
		}
	}
	if in.Explore != nil {
		for _, probe := range in.Explore.Probes {
			for _, run := range probe.Runs {
				if run.Config.P95LimitMS > 0 {
					return run.Config.P95LimitMS, run.Config.MaxErrorRate
				}
			}
		}
	}
	return 0, 0
}

// observedPeopleRate returns the mean successful request rate across recorded
// observation phases, used only for the declared usage assumption.
func observedPeopleRate(evidence campaign.Result) (float64, bool) {
	total, count := 0.0, 0
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Name != "observation" || phase.Run.ElapsedSeconds <= 0 {
				continue
			}
			total += float64(phase.Run.Metrics.Success) / phase.Run.ElapsedSeconds
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func h(value string) string { return html.EscapeString(value) }

func fmtNum(v float64, decimals int) string {
	if !finiteFloat(v) {
		return "not recorded"
	}
	return strconv.FormatFloat(v, 'f', decimals, 64)
}

const htmlReportStyle = `:root{color-scheme:light dark;--bg:#ffffff;--fg:#1b1f24;--muted:#57606a;--line:#d0d7de;--panel:#f6f8fa;--ok:#1a7f37;--ok-bg:#dafbe1;--bad:#cf222e;--bad-bg:#ffebe9;--warn:#9a6700;--warn-bg:#fff8c5;--fact:#0969da;--hyp:#8250df;--unk:#9a6700;--s1:#0969da;--s2:#bc4c00}
@media (prefers-color-scheme:dark){:root{--bg:#0d1117;--fg:#e6edf3;--muted:#9da7b3;--line:#30363d;--panel:#161b22;--ok:#3fb950;--ok-bg:#0f2a18;--bad:#ff7b72;--bad-bg:#3a1215;--warn:#d29922;--warn-bg:#2e2410;--fact:#58a6ff;--hyp:#bc8cff;--unk:#d29922;--s1:#58a6ff;--s2:#ffa657}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:960px;margin:0 auto;padding:24px 16px 48px}
h1{font-size:1.7rem;margin:0 0 4px}h2{font-size:1.3rem;margin:32px 0 8px;border-bottom:1px solid var(--line);padding-bottom:4px}h3{font-size:1.05rem;margin:20px 0 6px}
p,li{max-width:75ch}.muted{color:var(--muted)}
.verdict{border:3px solid var(--line);border-radius:8px;padding:16px;margin:16px 0}
.verdict strong.state{display:block;font-size:1.4rem;letter-spacing:.02em}
.v-ok{border-color:var(--ok);background:var(--ok-bg)}.v-bad{border-color:var(--bad);background:var(--bad-bg)}.v-warn{border-color:var(--warn);background:var(--warn-bg)}
.figures{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin:16px 0}
.figure{border:1px solid var(--line);border-radius:8px;padding:12px;background:var(--panel)}
.figure .value{font-size:1.6rem;font-weight:700;display:block}
.kind{border-left:6px solid var(--line);padding:4px 12px;margin:12px 0}
.kind>h3{margin-top:4px}.k-fact{border-color:var(--fact)}.k-hyp{border-color:var(--hyp)}.k-unk{border-color:var(--unk)}
.tag{display:inline-block;font-size:.75rem;font-weight:700;text-transform:uppercase;letter-spacing:.06em;padding:1px 6px;border-radius:4px;border:1px solid currentColor}
.t-fact{color:var(--fact)}.t-hyp{color:var(--hyp)}.t-unk{color:var(--unk)}
table{border-collapse:collapse;width:100%;margin:8px 0;font-size:.9rem}th,td{border:1px solid var(--line);padding:4px 8px;text-align:left;vertical-align:top}th{background:var(--panel)}
.table-wrap{overflow-x:auto}
figure{margin:16px 0}figcaption{color:var(--muted);font-size:.9rem}
svg{width:100%;height:auto;max-width:720px;display:block}
svg text{fill:var(--fg);font-size:12px}svg .axis{stroke:var(--muted);stroke-width:1}svg .grid{stroke:var(--line);stroke-width:1}
svg .limit{stroke:var(--bad);stroke-width:2;stroke-dasharray:6 4}
svg .s-explore{stroke:var(--s1);stroke-width:2}svg .s-campaign{stroke:var(--s2);stroke-width:2}
svg .line-p95{fill:none;stroke:var(--s1);stroke-width:2}svg .line-rps{fill:none;stroke:var(--s2);stroke-width:2}
.exec{border:2px solid var(--line);border-radius:8px;padding:16px;margin:16px 0}.exec>h2{border:0;margin-top:0}.exec .headline{font-size:1.25rem;font-weight:700}
.notmean{border:2px dashed var(--warn);background:var(--warn-bg);border-radius:8px;padding:8px 16px;margin:12px 0}.notmean>h3{margin-top:4px}
svg .fill-explore{fill:var(--s1)}svg .fill-campaign{fill:var(--s2)}svg .hollow{fill:var(--bg)}
pre{background:var(--panel);border:1px solid var(--line);border-radius:6px;padding:12px;overflow-x:auto;white-space:pre-wrap;word-break:break-all}
code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:.88em}
@media print{:root{--bg:#fff;--fg:#000;--muted:#333;--line:#888;--panel:#fff}body{font-size:11pt}main{max-width:none;padding:0}h2,h3,figure,.exec,.notmean,.verdict,.figure,table{break-inside:avoid}details{display:block}details>summary{display:none}a{color:inherit}}`

// renderCampaignCapacityHTML renders a self-contained executive HTML report.
// The output is deterministic for identical inputs and references no
// external resources.
func renderCampaignCapacityHTML(in htmlReportInput) string {
	a := in.Assessment
	audit := a.Audit
	outcome := executiveOutcome(a)
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; img-src data:\">\n")
	b.WriteString("<title>Capacity assessment</title>\n<style>\n" + htmlReportStyle + "\n</style>\n</head>\n<body>\n<main>\n")
	fmt.Fprintf(&b, "<header>\n<h1>Capacity assessment</h1>\n<p class=\"muted\">Scenario %s · environment %s · target %s · campaign started %s. Generated offline from saved evidence; no traffic was sent to produce this report.</p>\n</header>\n",
		h(orNotRecorded(audit.Scenario)), h(orNotRecorded(audit.DeclaredEnvironment)), h(orNotRecorded(audit.Target)), h(orNotRecorded(audit.StartedAt)))

	writeHTMLExecutiveSummary(&b, in)
	writeHTMLVerdict(&b, in, outcome)
	writeHTMLFigures(&b, in, outcome)
	writeHTMLConclusion(&b, in, outcome)
	writeHTMLNextDecision(&b, in, outcome)
	writeHTMLCharts(&b, in)
	writeHTMLFactsHypothesesUnknowns(&b, in)
	writeHTMLEvidence(&b, in)
	writeHTMLBlockers(&b, a)
	writeHTMLLimitations(&b, a)
	writeHTMLReplay(&b, in)
	b.WriteString("</main>\n</body>\n</html>\n")
	return b.String()
}

func orNotRecorded(value string) string {
	if strings.TrimSpace(value) == "" {
		return "not recorded"
	}
	return value
}

func writeHTMLVerdict(b *strings.Builder, in htmlReportInput, outcome string) {
	class, label := "v-warn", ""
	switch outcome {
	case outcomeMet:
		class, label = "v-ok", "DEMAND MET UNDER TESTED CONDITIONS"
	case outcomeNotMet:
		class, label = "v-bad", "DEMAND NOT MET"
	case outcomeInvalid:
		class, label = "v-bad", "INVALID EVIDENCE: NO CAPACITY DECISION"
	case outcomeNoDemand:
		label = "NO DEMAND DECLARED: NO CAPACITY DECISION"
	default:
		label = "INSUFFICIENT EVIDENCE: NO CAPACITY DECISION"
	}
	fmt.Fprintf(b, "<section class=\"verdict %s\" aria-labelledby=\"verdict-heading\">\n<h2 id=\"verdict-heading\" style=\"border:0;margin:0\">Decision</h2>\n<strong class=\"state\">%s</strong>\n", class, h(label))
	fmt.Fprintf(b, "<p>Formal assessment state: <code>%s</code>; observed campaign state: <code>%s</code>; measurement quality: <code>%s</code>; decision confidence: <code>%s</code>.</p>\n",
		h(in.Assessment.State), h(in.Assessment.Audit.State), h(in.Assessment.Audit.MeasurementQuality), h(in.Assessment.Confidence.Decision))
	if outcome != outcomeMet {
		b.WriteString("<p><strong>No number in this report is an approved capacity.</strong> Resolve the decision blockers below before using these results for sizing.</p>\n")
	}
	b.WriteString("</section>\n")
}

func writeHTMLFigures(b *strings.Builder, in htmlReportInput, outcome string) {
	a := in.Assessment
	requested := "not declared"
	if a.Demand != nil {
		requested = fmt.Sprintf("%d modeled active users", a.Demand.TargetUsers)
	}
	approved := "none approved"
	if outcome == outcomeMet {
		approved = fmt.Sprintf("%d modeled active users", a.Demand.TestedUsers)
	}
	b.WriteString("<section aria-label=\"Key figures\" class=\"figures\">\n")
	fmt.Fprintf(b, "<div class=\"figure\"><span class=\"muted\">Requested demand</span><span class=\"value\">%s</span>", h(requested))
	if goal := a.DemandInputs; goal != nil {
		fmt.Fprintf(b, "<span class=\"muted\">peak %d, growth %s%%, reserve %s%%</span>", goal.PeakUsers, h(markdownFloat(goal.GrowthPercent)), h(markdownFloat(goal.ReservePercent)))
	}
	b.WriteString("</div>\n")
	fmt.Fprintf(b, "<div class=\"figure\"><span class=\"muted\">Highest approved level</span><span class=\"value\">%s</span><span class=\"muted\">only a passing, decision-ready campaign approves a level</span></div>\n", h(approved))
	fmt.Fprintf(b, "<div class=\"figure\"><span class=\"muted\">Tested campaign level</span><span class=\"value\">%d modeled active users</span><span class=\"muted\">required phase duration %s</span></div>\n", a.Audit.TestedBaseUsers, h(orNotRecorded(a.Audit.TestedDuration)))
	b.WriteString("</section>\n")
	b.WriteString("<p class=\"muted\">Modeled active users are closed-loop virtual users of the recorded workload. They are not people, connected sessions, requests in flight or requests per second.")
	if people, ok := peopleAssumption(in); ok {
		fmt.Fprintf(b, " <span class=\"tag t-hyp\">assumption</span> %s", h(people))
	}
	b.WriteString("</p>\n")
}

func peopleAssumption(in htmlReportInput) (string, bool) {
	if in.ActionsPerUserMinute <= 0 || !finiteFloat(in.ActionsPerUserMinute) {
		return "", false
	}
	rate, ok := observedPeopleRate(in.Evidence)
	if !ok {
		return "", false
	}
	people := rate * 60 / in.ActionsPerUserMinute
	if !finiteFloat(people) || people < 0 {
		return "", false
	}
	return fmt.Sprintf("Declared usage assumption: at %s actions per person per minute, %d modeled users would correspond to about %s people, assuming one successful measured request per real action and the same workflow mix. This is not a measured human concurrency and does not change the decision.",
		fmtNum(in.ActionsPerUserMinute, 2), in.Evidence.BaseUsers, fmtNum(people, 1)), true
}

func writeHTMLConclusion(b *strings.Builder, in htmlReportInput, outcome string) {
	a := in.Assessment
	conditions := fmt.Sprintf("for scenario %q in the declared %s environment over a required phase duration of %s", a.Audit.Scenario, orNotRecorded(a.Audit.DeclaredEnvironment), orNotRecorded(a.Audit.TestedDuration))
	var text string
	switch outcome {
	case outcomeMet:
		text = fmt.Sprintf("The requested demand of %d modeled active users was met under the tested conditions. The highest approved level is %d modeled active users %s. This is not a prediction for other workloads, environments, durations or real people.", a.Demand.TargetUsers, a.Demand.TestedUsers, conditions)
	case outcomeNotMet:
		text = fmt.Sprintf("The requested demand of %d modeled active users was not met. The campaign at %d modeled active users breached its predeclared objectives %s. No level is approved under these conditions.", a.Demand.TargetUsers, a.Demand.TestedUsers, conditions)
	case outcomeInvalid:
		text = "The saved evidence failed validation, so no capacity conclusion can be drawn. No level is approved."
	case outcomeNoDemand:
		text = fmt.Sprintf("No demand target was declared, so nothing was approved. The campaign observed state %s at %d modeled active users %s.", a.Audit.State, a.Audit.TestedBaseUsers, conditions)
	default:
		target := "the requested demand"
		if a.Demand != nil {
			target = fmt.Sprintf("the requested demand of %d modeled active users", a.Demand.TargetUsers)
		}
		text = fmt.Sprintf("The evidence is insufficient to decide whether %s can be met; it is neither confirmed nor refuted. No level is approved. The campaign ran at %d modeled active users %s, and the decision blockers below explain what is missing.", target, a.Audit.TestedBaseUsers, conditions)
	}
	fmt.Fprintf(b, "<section aria-labelledby=\"conclusion\">\n<h2 id=\"conclusion\">Conclusion</h2>\n<p>%s</p>\n</section>\n", h(text))
}

func writeHTMLNextDecision(b *strings.Builder, in htmlReportInput, outcome string) {
	var text string
	switch outcome {
	case outcomeMet:
		text = "Decide whether the tested scenario, environment and duration represent the situation you need to plan for. If not, run a new campaign under those conditions before relying on this result."
	case outcomeNotMet:
		text = "Choose between investigating the breached workflows listed below (root causes are not identified by this report) and accepting a lower demand target that must then be tested exactly. Do not size for the requested demand from this evidence."
	case outcomeNoDemand:
		text = "Declare the demand to evaluate (peak users, growth and reserve) and replay the audit offline."
	default:
		text = "Resolve the decision blockers below, then repeat the campaign at the exact demand level. Do not use any number in this report for sizing."
	}
	fmt.Fprintf(b, "<section aria-labelledby=\"next\">\n<h2 id=\"next\">Next decision</h2>\n<p>%s</p>\n", h(text))
	blockers := allBlockers(in.Assessment)
	if len(blockers) > 0 && outcome != outcomeMet {
		fmt.Fprintf(b, "<p>First remediation: %s</p>\n", h(blockers[0].Action))
	}
	b.WriteString("</section>\n")
}

func allBlockers(a capacityplan.Assessment) []capacityplan.DecisionBlocker {
	if a.Demand != nil {
		return a.Demand.Blockers
	}
	return a.DecisionBlockers
}

func writeHTMLCharts(b *strings.Builder, in htmlReportInput) {
	points := collectChartPoints(in)
	p95Limit, errorLimit := objectiveLimits(in)
	b.WriteString("<section aria-labelledby=\"charts\">\n<h2 id=\"charts\">Measured behavior under load</h2>\n")
	if len(points) == 0 {
		b.WriteString("<p><span class=\"tag t-unk\">unknown</span> No phase or probe measurements were recorded, so no charts can be drawn.</p>\n</section>\n")
		return
	}
	b.WriteString("<p class=\"muted\">Blue circles: exploration probes. Orange squares: campaign phases. Filled marks passed their run objectives; hollow marks did not. Exploration probes are short and are not capacity approvals.</p>\n")
	writeSVGScatter(b, "chart-p95", "Request p95 latency versus load", "Modeled active users", "Request p95 (ms)", points, func(p chartPoint) float64 { return p.P95MS }, p95Limit, "objective "+fmtNum(p95Limit, 0)+" ms", 0)
	writeSVGScatter(b, "chart-rps", "Successful requests per second versus load", "Modeled active users", "Successful requests/s", points, func(p chartPoint) float64 { return p.RPS }, 0, "", 1)
	b.WriteString("<p class=\"muted\">If throughput stops rising while load increases, the system is saturating under this workload. The chart shows where, not why.</p>\n")
	writeSVGScatter(b, "chart-errors", "Request error rate versus load", "Modeled active users", "Error rate (%)", points, func(p chartPoint) float64 { return p.Errors * 100 }, errorLimit*100, "maximum "+fmtNum(errorLimit*100, 2)+"%", 2)
	b.WriteString("<details>\n<summary>Chart data</summary>\n<div class=\"table-wrap\"><table>\n<caption>Every plotted measurement</caption>\n<thead><tr><th scope=\"col\">Source</th><th scope=\"col\">Modeled users</th><th scope=\"col\">Request p95 (ms)</th><th scope=\"col\">Successful requests/s</th><th scope=\"col\">Error rate</th><th scope=\"col\">Run outcome</th></tr></thead>\n<tbody>\n")
	for _, p := range points {
		outcome := "did not pass"
		if p.Passed {
			outcome = "passed"
		}
		fmt.Fprintf(b, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td><td>%s%%</td><td>%s</td></tr>\n", h(p.Label), p.Users, fmtNum(p.P95MS, 1), fmtNum(p.RPS, 2), fmtNum(p.Errors*100, 3), outcome)
	}
	b.WriteString("</tbody>\n</table></div>\n</details>\n</section>\n")
}

func niceCeil(v float64) float64 {
	if v <= 0 || !finiteFloat(v) {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*exp >= v {
			return m * exp
		}
	}
	return 10 * exp
}

func writeSVGScatter(b *strings.Builder, id, title, xLabel, yLabel string, points []chartPoint, value func(chartPoint) float64, limit float64, limitLabel string, decimals int) {
	const width, height = 640.0, 300.0
	const left, right, top, bottom = 64.0, 16.0, 16.0, 44.0
	plotW, plotH := width-left-right, height-top-bottom
	maxX, maxY := 0, limit
	for _, p := range points {
		maxX = max(maxX, p.Users)
		if v := value(p); finiteFloat(v) {
			maxY = math.Max(maxY, v)
		}
	}
	xTop := niceCeil(float64(max(maxX, 1)))
	yTop := niceCeil(maxY * 1.1)
	sx := func(x float64) float64 { return left + x/xTop*plotW }
	sy := func(y float64) float64 { return top + plotH - y/yTop*plotH }
	fmt.Fprintf(b, "<figure>\n<svg viewBox=\"0 0 640 300\" role=\"img\" aria-labelledby=\"%s-t %s-d\">\n<title id=\"%s-t\">%s</title>\n<desc id=\"%s-d\">Scatter plot of %d measurements of %s against %s. The data is listed in the table below the charts.</desc>\n", id, id, id, h(title), id, len(points), h(yLabel), h(xLabel))
	for i := 0; i <= 4; i++ {
		y := yTop * float64(i) / 4
		fmt.Fprintf(b, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\">%s</text>\n", left, sy(y), width-right, sy(y), left-6, sy(y)+4, fmtNum(y, decimals))
		x := xTop * float64(i) / 4
		fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%s</text>\n", sx(x), top+plotH+16, fmtNum(x, 0))
	}
	fmt.Fprintf(b, "<line class=\"axis\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><line class=\"axis\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, top, left, top+plotH, left, top+plotH, width-right, top+plotH)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%s</text>\n", left+plotW/2, height-6, h(xLabel))
	fmt.Fprintf(b, "<text x=\"14\" y=\"%.1f\" text-anchor=\"middle\" transform=\"rotate(-90 14 %.1f)\">%s</text>\n", top+plotH/2, top+plotH/2, h(yLabel))
	if limit > 0 {
		fmt.Fprintf(b, "<line class=\"limit\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\">%s</text>\n", left, sy(limit), width-right, sy(limit), width-right-4, sy(limit)-6, h(limitLabel))
	}
	ordered := append([]chartPoint(nil), points...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Source == "explore" && ordered[j].Source != "explore" })
	for _, p := range ordered {
		v := value(p)
		if !finiteFloat(v) {
			continue
		}
		fill := "fill-" + p.Source
		if !p.Passed {
			fill = "hollow"
		}
		x, y := sx(float64(p.Users)), sy(v)
		if p.Source == "campaign" {
			fmt.Fprintf(b, "<rect class=\"s-campaign %s\" x=\"%.1f\" y=\"%.1f\" width=\"9\" height=\"9\"><title>%s: %s</title></rect>\n", fill, x-4.5, y-4.5, h(p.Label), fmtNum(v, decimals+1))
		} else {
			fmt.Fprintf(b, "<circle class=\"s-explore %s\" cx=\"%.1f\" cy=\"%.1f\" r=\"4.5\"><title>%s: %s</title></circle>\n", fill, x, y, h(p.Label), fmtNum(v, decimals+1))
		}
	}
	fmt.Fprintf(b, "</svg>\n<figcaption>%s.", h(title))
	if limit > 0 {
		fmt.Fprintf(b, " Dashed line: predeclared %s.", h(limitLabel))
	}
	b.WriteString("</figcaption>\n</figure>\n")
}

func writeHTMLList(b *strings.Builder, items []string, empty string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "<p class=\"muted\">%s</p>\n", h(empty))
		return
	}
	b.WriteString("<ul>\n")
	for _, item := range items {
		fmt.Fprintf(b, "<li>%s</li>\n", h(item))
	}
	b.WriteString("</ul>\n")
}

func writeHTMLFactsHypothesesUnknowns(b *strings.Builder, in htmlReportInput) {
	a := in.Assessment
	audit := a.Audit
	b.WriteString("<section aria-labelledby=\"fhu\">\n<h2 id=\"fhu\">Facts, hypotheses and unknowns</h2>\n")
	b.WriteString("<div class=\"kind k-fact\">\n<h3><span class=\"tag t-fact\">measured fact</span> What was observed</h3>\n")
	facts := []string{
		fmt.Sprintf("Campaign at %d modeled active users; observed passing levels %v, failing levels %v.", audit.TestedBaseUsers, audit.PassingLevels, audit.FailingLevels),
		fmt.Sprintf("Repeat trials: %d of %d evaluable trials passed (%d excluded).", audit.Repeatability.PassingTrials, audit.Repeatability.EvaluatedTrials, audit.Repeatability.ExcludedTrials),
	}
	for _, trial := range audit.ObservationTrials {
		facts = append(facts, fmt.Sprintf("Observation trial %d: %d requests, request p95 %s ms, journey p95 %s ms, %s successful journeys/s, request errors %s.", trial.Trial, trial.Requests, fmtNum(trial.RequestP95MS, 1), fmtNum(trial.JourneyP95MS, 1), fmtNum(trial.SuccessfulJourneysPerSec, 2), markdownOptionalPercent(trial.RequestErrorRate)))
	}
	facts = append(facts, breachDescriptions(audit.WorkflowBreaches)...)
	if in.Explore != nil && in.Explore.HighestPassingUsers != nil {
		facts = append(facts, fmt.Sprintf("Exploration passed short probes up to %d modeled users. This is exploratory and is not an approved level.", *in.Explore.HighestPassingUsers))
	}
	writeHTMLList(b, facts, "")
	b.WriteString("</div>\n<div class=\"kind k-hyp\">\n<h3><span class=\"tag t-hyp\">hypothesis</span> Possible explanations and assumptions (not validated)</h3>\n")
	var hypotheses []string
	if len(audit.LimitingWorkflows) > 0 {
		hypotheses = append(hypotheses, fmt.Sprintf("The workflows %s breached objectives first and are candidates for investigation. A breach location is not a root cause.", strings.Join(audit.LimitingWorkflows, ", ")))
	}
	if people, ok := peopleAssumption(in); ok {
		hypotheses = append(hypotheses, people)
	}
	writeHTMLList(b, hypotheses, "No hypotheses are recorded. This report does not identify root causes or recommend infrastructure changes.")
	b.WriteString("</div>\n<div class=\"kind k-unk\">\n<h3><span class=\"tag t-unk\">unknown</span> What this evidence cannot establish</h3>\n")
	writeHTMLList(b, audit.Unknowns, "No additional unknowns were recorded.")
	b.WriteString("</div>\n</section>\n")
}

func writeHTMLEvidence(b *strings.Builder, in htmlReportInput) {
	audit := in.Assessment.Audit
	b.WriteString("<section aria-labelledby=\"evidence\">\n<h2 id=\"evidence\">Evidence quality</h2>\n")
	fmt.Fprintf(b, "<p>Campaign calibration requirement: %s. Generator runtime identity: <code>%s</code>; CPU capacity consistency: <code>%s</code>; host type: <code>%s</code>.</p>\n",
		h(calibrationSummary(in.Evidence)), h(audit.GeneratorRuntimeStatus), h(audit.GeneratorCapacityStatus), h(audit.HostType))
	b.WriteString("<div class=\"table-wrap\"><table>\n<caption>Per-phase calibration, network baseline, generator health and drift</caption>\n<thead><tr><th scope=\"col\">Trial</th><th scope=\"col\">Phase</th><th scope=\"col\">Delivery verdict</th><th scope=\"col\">Measurement quality</th><th scope=\"col\">Calibration</th><th scope=\"col\">Network baseline</th><th scope=\"col\">Generator CPU peak</th><th scope=\"col\">Drift screen</th></tr></thead>\n<tbody>\n")
	runs := map[string]report.Result{}
	for _, trial := range in.Evidence.Trials {
		for _, phase := range trial.Phases {
			runs[strconv.Itoa(trial.Number)+"/"+phase.Name] = phase.Run
		}
	}
	for _, phase := range audit.Phases {
		run := runs[strconv.Itoa(phase.Trial)+"/"+phase.Name]
		calibrationState := "not recorded"
		if run.ScenarioCalibration != nil {
			calibrationState = run.ScenarioCalibration.Status
		} else if run.Calibration != nil {
			calibrationState = run.Calibration.Coverage.Status
		}
		network := "not recorded"
		if nb := run.NetworkBaseline; nb != nil {
			network = nb.Status
			if nb.Completed > 0 {
				network += fmt.Sprintf(" (p50 %s ms, p95 %s ms, %d/%d)", fmtNum(nb.P50MS, 1), fmtNum(nb.P95MS, 1), nb.Completed, nb.Planned)
			}
		}
		generator := "not recorded"
		if g := run.Generator; g != nil {
			if g.ProcessCPUValid {
				generator = fmtNum(g.PeakProcessCPUUtilizationRatio*100, 1) + "%"
			} else {
				generator = "process CPU not valid"
			}
			if g.GuardrailTrigger != nil {
				generator += "; guardrail stop"
			}
		}
		fmt.Fprintf(b, "<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n", phase.Trial, h(phase.Name), h(phase.EvidenceState), h(phase.Measurement), h(calibrationState), h(network), h(generator), h(orNotRecorded(phase.Stability)))
	}
	b.WriteString("</tbody>\n</table></div>\n")
	r := audit.Repeatability
	fmt.Fprintf(b, "<p>Repeats: %d/%d evaluable trials passed; method <code>%s</code> under assumption <code>%s</code>", r.PassingTrials, r.EvaluatedTrials, h(r.Method), h(r.Assumption))
	if r.EvaluatedTrials > 0 && r.Status == "CONDITIONAL_INTERVAL" {
		fmt.Fprintf(b, "; conditional pass-probability interval %s–%s, which is not capacity confidence", fmtNum(r.Lower95, 3), fmtNum(r.Upper95, 3))
	}
	b.WriteString(".</p>\n")
	if s := audit.ObservationSpread; s != nil && s.RequestP95RobustMethod != "" {
		fmt.Fprintf(b, "<p>Repeat-level request p95: median %s ms, MAD %s ms (descriptive; not a confidence interval).</p>\n", fmtNum(s.RequestP95MedianMS, 2), fmtNum(s.RequestP95MADMS, 2))
	}
	fmt.Fprintf(b, "<p>Confidence dimensions: measurement <code>%s</code>, workload representativeness <code>%s</code>, decision <code>%s</code>.</p>\n</section>\n",
		h(in.Assessment.Confidence.Measurement), h(in.Assessment.Confidence.WorkloadRepresentativeness), h(in.Assessment.Confidence.Decision))
}

func writeHTMLBlockers(b *strings.Builder, a capacityplan.Assessment) {
	b.WriteString("<section aria-labelledby=\"blockers\">\n<h2 id=\"blockers\">Decision blockers and remediation</h2>\n")
	blockers := allBlockers(a)
	if len(blockers) == 0 {
		b.WriteString("<p>No decision blockers were recorded.</p>\n</section>\n")
		return
	}
	b.WriteString("<div class=\"table-wrap\"><table>\n<thead><tr><th scope=\"col\">Category</th><th scope=\"col\">Finding</th><th scope=\"col\">Remediation</th></tr></thead>\n<tbody>\n")
	labels := map[string]string{capacityplan.BlockerConfiguration: "Configuration and scenario", capacityplan.BlockerEvidence: "Insufficient evidence", capacityplan.BlockerDamocline: "Damocline limitation"}
	for _, group := range []string{capacityplan.BlockerConfiguration, capacityplan.BlockerEvidence, capacityplan.BlockerDamocline} {
		for _, item := range blockers {
			if item.Category == group {
				fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>\n", h(labels[group]), h(item.Finding), h(item.Action))
			}
		}
	}
	b.WriteString("</tbody>\n</table></div>\n</section>\n")
}

func writeHTMLLimitations(b *strings.Builder, a capacityplan.Assessment) {
	b.WriteString("<section aria-labelledby=\"limits\">\n<h2 id=\"limits\">Limitations and uncertainty</h2>\n")
	items := []string{
		"This assessment describes the saved evidence under its declared conditions. It does not certify universal or human-user capacity, establish dependence-aware confidence, or recommend infrastructure changes.",
		"Percentiles are nearest-rank descriptive estimates; statistical intervals assume independent, stationary samples, which this assessment does not establish.",
	}
	items = append(items, a.Confidence.Findings...)
	items = append(items, a.Audit.Findings...)
	items = append(items, demandFindings(a.Demand)...)
	writeHTMLList(b, items, "")
	b.WriteString("</section>\n")
}

func writeHTMLReplay(b *strings.Builder, in htmlReportInput) {
	a := in.Assessment
	command := markdownReplayCommand([]string{in.Source}, []string{a.Audit.EvidenceID}, []string{a.Audit.SourceFileSHA256}, a.Auditor.ExecutableSHA256, a.DemandInputs)
	if in.Explore != nil && in.ExploreSource != "" {
		command += " --explore-file " + powershellQuote(in.ExploreSource)
		if in.ExploreFileSHA256 != "" {
			command += " --expect-explore-file-sha256 " + powershellQuote(in.ExploreFileSHA256)
		}
	}
	if in.ActionsPerUserMinute > 0 {
		command += " --actions-per-user-minute " + markdownFloat(in.ActionsPerUserMinute)
	}
	command += " --html-out 'replayed-capacity-report.html'"
	b.WriteString("<section aria-labelledby=\"replay\">\n<h2 id=\"replay\">Offline replay and evidence digests</h2>\n<ul>\n")
	fmt.Fprintf(b, "<li>Campaign source: <code>%s</code></li>\n<li>Campaign evidence ID (canonical decoded fields): <code>%s</code></li>\n<li>Campaign source file SHA-256 (exact bytes): <code>%s</code></li>\n", h(in.Source), h(a.Audit.EvidenceID), h(a.Audit.SourceFileSHA256))
	if in.Explore != nil && in.ExploreFileSHA256 != "" {
		fmt.Fprintf(b, "<li>Exploration source file SHA-256: <code>%s</code></li>\n", h(in.ExploreFileSHA256))
	}
	if a.Auditor.ExecutableSHA256 != "" {
		fmt.Fprintf(b, "<li>Auditor %s executable SHA-256: <code>%s</code></li>\n", h(a.Auditor.Version), h(a.Auditor.ExecutableSHA256))
	}
	fmt.Fprintf(b, "<li>Workload fingerprint: <code>%s</code></li>\n</ul>\n", h(orNotRecorded(a.Audit.ScenarioFingerprint)))
	b.WriteString("<p class=\"muted\">Digests identify evidence; they are not signatures or host, producer or target attestation.</p>\n")
	fmt.Fprintf(b, "<pre><code>%s</code></pre>\n</section>\n", h(command))
}
