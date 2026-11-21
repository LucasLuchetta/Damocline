package cli

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// levelPoint aggregates every plotted measurement taken at one load level.
type levelPoint struct {
	Users   int
	P95MS   float64 // worst request p95 observed at this level
	RPS     float64 // mean successful requests per second
	Errors  float64 // worst request error rate observed at this level
	AllPass bool
	AnyFail bool
}

func aggregateLevels(points []chartPoint) []levelPoint {
	byUsers := map[int]*levelPoint{}
	counts := map[int]int{}
	for _, p := range points {
		if p.Users <= 0 {
			continue
		}
		level, ok := byUsers[p.Users]
		if !ok {
			level = &levelPoint{Users: p.Users, AllPass: true}
			byUsers[p.Users] = level
		}
		level.P95MS = math.Max(level.P95MS, p.P95MS)
		level.Errors = math.Max(level.Errors, p.Errors)
		level.RPS += p.RPS
		level.AllPass = level.AllPass && p.Passed
		level.AnyFail = level.AnyFail || p.Failed
		counts[p.Users]++
	}
	levels := make([]levelPoint, 0, len(byUsers))
	for users, level := range byUsers {
		level.RPS /= float64(counts[users])
		levels = append(levels, *level)
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i].Users < levels[j].Users })
	return levels
}

// executiveSummary is the plain-language summary shown first in reports.
// Every sentence is derived from saved evidence; hypotheses are labeled.
type executiveSummary struct {
	Headline   string
	Confidence string
	Limit      string
	Saturation string // always a labeled hypothesis
	NotMean    []string
}

func journeyName(in htmlReportInput) string {
	name := strings.TrimSpace(in.Assessment.Audit.Scenario)
	if name == "" {
		name = strings.TrimSpace(in.Evidence.Scenario)
	}
	if name == "" {
		return "the recorded journey"
	}
	return fmt.Sprintf("the %q journey", name)
}

func objectivePhrase(in htmlReportInput) string {
	p95, errRate := objectiveLimits(in)
	if p95 <= 0 {
		return "within its predeclared objectives"
	}
	return fmt.Sprintf("with request p95 under %s ms and request errors under %s%%", fmtNum(p95, 0), fmtNum(errRate*100, 2))
}

func buildExecutiveSummary(in htmlReportInput) executiveSummary {
	a := in.Assessment
	audit := a.Audit
	outcome := executiveOutcome(a)
	journey := journeyName(in)
	var s executiveSummary
	switch outcome {
	case outcomeMet:
		s.Headline = fmt.Sprintf("Under the tested conditions, the application supports %d concurrent active virtual users doing %s %s.", a.Demand.TestedUsers, journey, objectivePhrase(in))
	case outcomeNotMet:
		s.Headline = fmt.Sprintf("The application did not sustain %d concurrent active virtual users doing %s %s. No sustainable level is approved.", a.Demand.TestedUsers, journey, objectivePhrase(in))
	case outcomeInvalid:
		s.Headline = "The saved evidence failed validation, so this report states no sustainable level."
	default:
		s.Headline = fmt.Sprintf("No sustainable level is approved yet: the evidence does not establish how many concurrent active virtual users doing %s the application supports %s.", journey, objectivePhrase(in))
	}

	confidence := fmt.Sprintf("Decision confidence: %s; measurement quality: %s.", orNotRecorded(a.Confidence.Decision), orNotRecorded(string(audit.MeasurementQuality)))
	if r := audit.Repeatability; r.EvaluatedTrials > 0 {
		confidence += fmt.Sprintf(" %d of %d repeat trials passed", r.PassingTrials, r.EvaluatedTrials)
		if r.Upper95 > 0 {
			confidence += fmt.Sprintf("; the exact 95%% interval for the repeat pass rate is %s%% to %s%%, assuming independent, stationary trials", fmtNum(r.Lower95*100, 1), fmtNum(r.Upper95*100, 1))
		}
		confidence += "."
	}
	if outcome == outcomeMet {
		confidence += " The result holds only for the tested scenario, environment and duration."
	}
	s.Confidence = confidence

	levels := aggregateLevels(collectChartPoints(in))
	highestPass, firstFail := -1, -1
	for i, level := range levels {
		if firstFail >= 0 {
			break
		}
		switch {
		case level.AnyFail:
			firstFail = i
		case level.AllPass:
			highestPass = i
		}
	}
	switch {
	case len(levels) == 0:
		s.Limit = "No load levels were measured, so no limit was observed."
	case firstFail < 0 && highestPass < 0:
		s.Limit = "No measured level recorded a pass or fail outcome, so no limit was observed."
	case firstFail < 0:
		s.Limit = fmt.Sprintf("No failing level was observed up to %d virtual users; the limit is above the tested range and is unknown.", levels[len(levels)-1].Users)
	case highestPass < 0:
		f := levels[firstFail]
		s.Limit = fmt.Sprintf("Even the lowest measured level (%d virtual users) breached its objectives (worst request p95 %s ms, errors %s%%); the limit is below the tested range.", f.Users, fmtNum(f.P95MS, 0), fmtNum(f.Errors*100, 2))
	default:
		p, f := levels[highestPass], levels[firstFail]
		s.Limit = fmt.Sprintf("Observed limit (knee): every measurement passed at %d virtual users, and objectives were breached at %d virtual users (worst request p95 %s ms, errors %s%%). The limit lies between these levels; short exploration probes are not approvals.", p.Users, f.Users, fmtNum(f.P95MS, 0), fmtNum(f.Errors*100, 2))
	}

	var saturation []string
	for i := 1; i < len(levels); i++ {
		prev, cur := levels[i-1], levels[i]
		if prev.RPS <= 0 {
			continue
		}
		loadGain := float64(cur.Users)/float64(prev.Users) - 1
		throughputGain := cur.RPS/prev.RPS - 1
		if loadGain > 0 && throughputGain < loadGain/2 {
			saturation = append(saturation, fmt.Sprintf("throughput rose only from %s to %s successful requests/s while load rose from %d to %d virtual users, which suggests the system was saturating between these levels", fmtNum(prev.RPS, 1), fmtNum(cur.RPS, 1), prev.Users, cur.Users))
			break
		}
	}
	if len(audit.LimitingWorkflows) > 0 {
		saturation = append(saturation, fmt.Sprintf("the workflows %s breached objectives first", strings.Join(audit.LimitingWorkflows, ", ")))
	}
	if len(saturation) == 0 {
		s.Saturation = "No saturation pattern was observed in the measured range. Which resource would saturate first is unknown."
	} else {
		s.Saturation = "Correlational, not a measured root cause: " + strings.Join(saturation, "; and ") + ". No diagnostic evidence in this report proves which resource saturated."
	}

	s.NotMean = []string{
		"Virtual users are simulated clients repeating the recorded journey with only the pauses the scenario declares. They are not registered users, logged-in accounts, connected sessions, requests in flight or requests per second.",
		"The result applies only to the tested journey mix, environment and duration. It is not a prediction for other workloads, peak events or future releases.",
	}
	if people, ok := peopleAssumption(in); ok {
		s.NotMean = append(s.NotMean, "Estimate, not a measurement: "+people)
	} else {
		s.NotMean = append(s.NotMean, "No usage assumption (think time or actions per person) was declared, so no estimate of real people is shown. Real people usually outnumber active virtual users because they pause between actions.")
	}
	return s
}

func writeHTMLExecutiveSummary(b *strings.Builder, in htmlReportInput) {
	s := buildExecutiveSummary(in)
	b.WriteString("<section class=\"exec\" aria-labelledby=\"exec-heading\">\n<h2 id=\"exec-heading\">Executive summary</h2>\n")
	fmt.Fprintf(b, "<p class=\"headline\">%s</p>\n", h(s.Headline))
	fmt.Fprintf(b, "<p><span class=\"tag t-fact\">measured fact</span> %s</p>\n", h(s.Confidence))
	fmt.Fprintf(b, "<p><span class=\"tag t-fact\">measured fact</span> %s</p>\n", h(s.Limit))
	fmt.Fprintf(b, "<p><span class=\"tag t-hyp\">hypothesis</span> %s</p>\n", h(s.Saturation))
	writeSVGLevelChart(b, aggregateLevels(collectChartPoints(in)), in)
	b.WriteString("<aside class=\"notmean\" aria-labelledby=\"notmean-heading\">\n<h3 id=\"notmean-heading\">What this does NOT mean</h3>\n")
	writeHTMLList(b, s.NotMean, "")
	b.WriteString("</aside>\n<p class=\"muted\">Technical details, evidence quality and the replay command follow below.</p>\n</section>\n")
}

func writeMarkdownExecutiveSummary(b *strings.Builder, in htmlReportInput) {
	s := buildExecutiveSummary(in)
	fmt.Fprintf(b, "**%s**\n\n", markdownText(s.Headline))
	fmt.Fprintf(b, "- **Confidence (measured):** %s\n", markdownText(s.Confidence))
	fmt.Fprintf(b, "- **Limit (measured):** %s\n", markdownText(s.Limit))
	fmt.Fprintf(b, "- **What saturated (hypothesis):** %s\n\n", markdownText(s.Saturation))
	fmt.Fprintln(b, "> **What this does NOT mean**")
	fmt.Fprintln(b, ">")
	for _, item := range s.NotMean {
		fmt.Fprintf(b, "> - %s\n", markdownText(item))
	}
	fmt.Fprintln(b)
}

// writeSVGLevelChart draws worst request p95 (left axis) and mean successful
// throughput (right axis) per load level as two lines in one inline SVG.
func writeSVGLevelChart(b *strings.Builder, levels []levelPoint, in htmlReportInput) {
	if len(levels) == 0 {
		return
	}
	const width, height = 640.0, 300.0
	const left, right, top, bottom = 64.0, 64.0, 16.0, 44.0
	plotW, plotH := width-left-right, height-top-bottom
	p95Limit, _ := objectiveLimits(in)
	maxX, maxP95, maxRPS := 1, p95Limit, 0.0
	for _, l := range levels {
		maxX = max(maxX, l.Users)
		maxP95 = math.Max(maxP95, l.P95MS)
		maxRPS = math.Max(maxRPS, l.RPS)
	}
	xTop, p95Top, rpsTop := niceCeil(float64(maxX)), niceCeil(maxP95*1.1), niceCeil(maxRPS*1.1)
	sx := func(x int) float64 { return left + float64(x)/xTop*plotW }
	sp := func(v float64) float64 { return top + plotH - v/p95Top*plotH }
	sr := func(v float64) float64 { return top + plotH - v/rpsTop*plotH }
	b.WriteString("<figure>\n<svg viewBox=\"0 0 640 300\" role=\"img\" aria-labelledby=\"chart-level-t chart-level-d\">\n<title id=\"chart-level-t\">Request p95 and throughput versus load level</title>\n")
	fmt.Fprintf(b, "<desc id=\"chart-level-d\">Line chart over %d load levels: worst request p95 in milliseconds on the left axis and mean successful requests per second on the right axis.</desc>\n", len(levels))
	for i := 0; i <= 4; i++ {
		f := float64(i) / 4
		y := top + plotH - f*plotH
		fmt.Fprintf(b, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\">%s</text><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"start\">%s</text>\n", left, y, width-right, y, left-6, y+4, fmtNum(p95Top*f, 0), width-right+6, y+4, fmtNum(rpsTop*f, 1))
		fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%s</text>\n", left+f*plotW, top+plotH+16, fmtNum(xTop*f, 0))
	}
	fmt.Fprintf(b, "<line class=\"axis\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, top+plotH, width-right, top+plotH)
	fmt.Fprintf(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">Concurrent active virtual users</text>\n", left+plotW/2, height-6)
	fmt.Fprintf(b, "<text x=\"14\" y=\"%.1f\" text-anchor=\"middle\" transform=\"rotate(-90 14 %.1f)\">Request p95 (ms)</text>\n", top+plotH/2, top+plotH/2)
	fmt.Fprintf(b, "<text x=\"630\" y=\"%.1f\" text-anchor=\"middle\" transform=\"rotate(90 630 %.1f)\">Successful requests/s</text>\n", top+plotH/2, top+plotH/2)
	if p95Limit > 0 {
		fmt.Fprintf(b, "<line class=\"limit\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/>\n", left, sp(p95Limit), width-right, sp(p95Limit))
	}
	var p95Path, rpsPath []string
	for _, l := range levels {
		p95Path = append(p95Path, fmt.Sprintf("%.1f,%.1f", sx(l.Users), sp(l.P95MS)))
		rpsPath = append(rpsPath, fmt.Sprintf("%.1f,%.1f", sx(l.Users), sr(l.RPS)))
	}
	fmt.Fprintf(b, "<polyline class=\"line-p95\" points=\"%s\"/>\n<polyline class=\"line-rps\" points=\"%s\"/>\n", strings.Join(p95Path, " "), strings.Join(rpsPath, " "))
	for _, l := range levels {
		fill := "fill-explore"
		if !l.AllPass {
			fill = "hollow"
		}
		fmt.Fprintf(b, "<circle class=\"s-explore %s\" cx=\"%.1f\" cy=\"%.1f\" r=\"4.5\"><title>%d users: p95 %s ms</title></circle>\n", fill, sx(l.Users), sp(l.P95MS), l.Users, fmtNum(l.P95MS, 1))
		fmt.Fprintf(b, "<rect class=\"s-campaign fill-campaign\" x=\"%.1f\" y=\"%.1f\" width=\"9\" height=\"9\"><title>%d users: %s successful requests/s</title></rect>\n", sx(l.Users)-4.5, sr(l.RPS)-4.5, l.Users, fmtNum(l.RPS, 2))
	}
	b.WriteString("</svg>\n<figcaption>Blue circles and line: worst request p95 per level (left axis; hollow means a measurement at that level failed). Orange squares and line: mean successful requests/s (right axis).")
	if p95Limit > 0 {
		fmt.Fprintf(b, " Dashed line: p95 objective %s ms.", fmtNum(p95Limit, 0))
	}
	b.WriteString(" When throughput flattens while p95 climbs, the system is saturating.</figcaption>\n</figure>\n")
}
