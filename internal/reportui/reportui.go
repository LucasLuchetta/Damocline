// Package reportui renders local, read-only HTML views of Damocline reports.
package reportui

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func Handler(result report.Result) http.Handler {
	page := template.Must(template.New("report").Funcs(template.FuncMap{"lower": strings.ToLower}).Parse(document))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := page.Execute(w, view{Result: result, Metrics: PhaseMetricCharts(result)}); err != nil {
			http.Error(w, "render report", http.StatusInternalServerError)
		}
	})
}

type TrialView struct {
	Number  int
	Outcome string
	Reason  string
	Pending bool
	Phases  []PhaseView
}

type PhaseView struct {
	Name          string
	EvidenceState string
	Reason        string
	Load          string
	ErrorRate     string
	Run           report.Result
	Metrics       []MetricChart
}

// CampaignView is a sanitized presentation model built from a validated
// campaign artifact. It preserves the distinct offered-load unit.
type CampaignView struct {
	Kind      string
	Unit      string
	Scenario  string
	Target    string
	StartedAt string
	Outcome   string
	Pending   bool
	Trials    []TrialView
}

type MetricChart struct {
	Name          string
	Min           string
	Max           string
	Points        []MetricPoint
	RequestMin    string
	RequestMax    string
	RequestPoints []MetricPoint
	HasRequests   bool
	StartLabel    string
	EndLabel      string
}

type MetricPoint struct {
	X     string
	Y     string
	Value string
	At    string
}

type view struct {
	Result  report.Result
	Metrics []MetricChart
}

// CampaignHandler serves a local, read-only view of validated campaign phases.
func CampaignHandler(c CampaignView) http.Handler {
	page := template.Must(template.New("campaign").Funcs(template.FuncMap{"lower": strings.ToLower}).Parse(campaignDocument))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := page.Execute(w, c); err != nil {
			http.Error(w, "render campaign", http.StatusInternalServerError)
		}
	})
}

// MetricCharts prepares application-metric observations for SVG rendering.
func MetricCharts(timeline *appmetrics.Timeline) []MetricChart {
	return buildMetricCharts(timeline, nil)
}

// PhaseMetricCharts aligns external scrape observations with request p95
// window observations using the client's recorded UTC phase start.
func PhaseMetricCharts(run report.Result) []MetricChart {
	return buildMetricCharts(run.ApplicationMetrics, &run)
}

func buildMetricCharts(timeline *appmetrics.Timeline, run *report.Result) []MetricChart {
	if timeline == nil || len(timeline.Samples) == 0 {
		return nil
	}
	start, err := time.Parse(time.RFC3339Nano, timeline.StartedAt)
	if err != nil {
		return nil
	}
	end, err := time.Parse(time.RFC3339Nano, timeline.FinishedAt)
	if err != nil || !end.After(start) {
		end = start.Add(time.Second)
	}
	type timedValue struct {
		at    time.Time
		value float64
	}
	requestPoints := make([]timedValue, 0)
	requestMinimum, requestMaximum := 0.0, 0.0
	if run != nil {
		if runStart, parseErr := time.Parse(time.RFC3339Nano, run.StartedAt); parseErr == nil {
			for _, window := range run.Windows {
				if window.Metrics.Total == 0 || !isFinite(window.Metrics.P95MS) || window.Metrics.P95MS < 0 || !isFinite(window.StartSeconds) || !isFinite(window.EndSeconds) || window.EndSeconds < window.StartSeconds {
					continue
				}
				centerSeconds := (window.StartSeconds + window.EndSeconds) / 2
				at := runStart.Add(time.Duration(centerSeconds * float64(time.Second)))
				requestPoints = append(requestPoints, timedValue{at: at, value: window.Metrics.P95MS})
				if len(requestPoints) == 1 || window.Metrics.P95MS < requestMinimum {
					requestMinimum = window.Metrics.P95MS
				}
				if len(requestPoints) == 1 || window.Metrics.P95MS > requestMaximum {
					requestMaximum = window.Metrics.P95MS
				}
				if at.Before(start) {
					start = at
				}
				if at.After(end) {
					end = at
				}
			}
		}
	}
	type observation struct {
		at    time.Time
		value float64
	}
	series := map[string][]observation{}
	for _, sample := range timeline.Samples {
		at, parseErr := time.Parse(time.RFC3339Nano, sample.FinishedAt)
		if parseErr != nil {
			continue
		}
		for _, value := range sample.Samples {
			series[value.Name] = append(series[value.Name], observation{at: at, value: value.Value})
		}
	}
	names := make([]string, 0, len(series))
	for name := range series {
		names = append(names, name)
	}
	sort.Strings(names)
	charts := make([]MetricChart, 0, len(names))
	span := end.Sub(start).Seconds()
	if span <= 0 {
		span = 1
	}
	requestRange := requestMaximum - requestMinimum
	if requestRange == 0 || !isFinite(requestRange) {
		requestRange = 1
	}
	for _, name := range names {
		observations := series[name]
		if len(observations) == 0 {
			continue
		}
		minimum, maximum := observations[0].value, observations[0].value
		for _, point := range observations[1:] {
			minimum = min(minimum, point.value)
			maximum = max(maximum, point.value)
		}
		rangeValue := maximum - minimum
		scaledMinimum, scaledMaximum, scale := minimum, maximum, 1.0
		if !isFinite(rangeValue) {
			scale = max(abs(minimum), abs(maximum))
			scaledMinimum, scaledMaximum = minimum/scale, maximum/scale
			rangeValue = scaledMaximum - scaledMinimum
		}
		if rangeValue == 0 {
			rangeValue = 1
		}
		points := make([]MetricPoint, 0, len(observations))
		for _, observation := range observations {
			x := 45 + 550*observation.at.Sub(start).Seconds()/span
			valueForY := observation.value - minimum
			if scale != 1 {
				valueForY = observation.value/scale - scaledMinimum
			}
			y := 140 - 110*valueForY/rangeValue
			points = append(points, MetricPoint{
				X: strconv.FormatFloat(x, 'f', 1, 64), Y: strconv.FormatFloat(y, 'f', 1, 64),
				Value: strconv.FormatFloat(observation.value, 'g', 6, 64), At: observation.at.UTC().Format(time.RFC3339Nano),
			})
		}
		httpPoints := make([]MetricPoint, 0, len(requestPoints))
		for _, request := range requestPoints {
			y := 85.0
			if requestMaximum != requestMinimum {
				y = 140 - 110*(request.value-requestMinimum)/requestRange
			}
			x := 45 + 550*request.at.Sub(start).Seconds()/span
			httpPoints = append(httpPoints, MetricPoint{X: strconv.FormatFloat(x, 'f', 1, 64), Y: strconv.FormatFloat(y, 'f', 1, 64),
				Value: strconv.FormatFloat(request.value, 'g', 6, 64), At: request.at.UTC().Format(time.RFC3339Nano)})
		}
		charts = append(charts, MetricChart{Name: name, Min: strconv.FormatFloat(minimum, 'g', 6, 64), Max: strconv.FormatFloat(maximum, 'g', 6, 64),
			Points: points, RequestMin: strconv.FormatFloat(requestMinimum, 'g', 6, 64), RequestMax: strconv.FormatFloat(requestMaximum, 'g', 6, 64), RequestPoints: httpPoints, HasRequests: len(httpPoints) > 0,
			StartLabel: start.UTC().Format("15:04:05.000Z"), EndLabel: end.UTC().Format("15:04:05.000Z")})
	}
	return charts
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func (v view) Throughput() float64 {
	if v.Result.WindowSeconds() <= 0 {
		return 0
	}
	return float64(v.Result.Metrics.Total) / v.Result.WindowSeconds()
}

func (v view) SuccessfulJourneyThroughput() float64 {
	return report.SuccessfulJourneysPerSecond(v.Result)
}

func (v view) HasWorkloadWindow() bool    { return v.Result.WindowSeconds() > 0 }
func (v view) HasObservationWindow() bool { return v.Result.ObservationSeconds > 0 }
func (v view) HasTimeline() bool          { return len(v.Result.Windows) > 0 }
func (v view) HasLatencyTimeline() bool {
	for _, window := range v.Result.Windows {
		if window.Metrics.Total > 0 {
			return true
		}
	}
	return false
}

func (v view) LatencyP95Points() string { return v.latencyPoints(false) }
func (v view) LatencyP99Points() string { return v.latencyPoints(true) }
func (v view) LatencyMaxLabel() string  { return fmt.Sprintf("%.0f ms", v.latencyMaximum()) }

type timelineLabel struct {
	X     string
	Label string
}

func (v view) TimelineLabels() []timelineLabel {
	windows := v.Result.Windows
	if len(windows) == 0 {
		return nil
	}
	start, end := windows[0].StartSeconds, windows[len(windows)-1].EndSeconds
	if end <= start {
		end = start + 1
	}
	labels := make([]timelineLabel, 0, len(windows))
	for _, window := range windows {
		x := 45 + 550*(window.StartSeconds+window.EndSeconds-2*start)/(2*(end-start))
		labels = append(labels, timelineLabel{X: strconv.FormatFloat(x, 'f', 1, 64), Label: fmt.Sprintf("%.0f-%.0fs", window.StartSeconds, window.EndSeconds)})
	}
	return labels
}

func (v view) latencyMaximum() float64 {
	maximum := 1.0
	for _, window := range v.Result.Windows {
		if window.Metrics.Total > 0 {
			maximum = max(maximum, window.Metrics.P95MS, window.Metrics.P99MS)
		}
	}
	return maximum
}

func (v view) latencyPoints(p99 bool) string {
	windows := v.Result.Windows
	if len(windows) == 0 {
		return ""
	}
	maxLatency := v.latencyMaximum()
	start, end := windows[0].StartSeconds, windows[len(windows)-1].EndSeconds
	if end <= start {
		end = start + 1
	}
	points := make([]string, 0, len(windows))
	for _, window := range windows {
		if window.Metrics.Total == 0 {
			continue
		}
		latency := window.Metrics.P95MS
		if p99 {
			latency = window.Metrics.P99MS
		}
		x := 45 + 550*(window.StartSeconds+window.EndSeconds-2*start)/(2*(end-start))
		y := 145 - 115*latency/maxLatency
		points = append(points, strconv.FormatFloat(x, 'f', 1, 64)+","+strconv.FormatFloat(y, 'f', 1, 64))
	}
	return strings.Join(points, " ")
}

func (v view) WindowClass(window report.WindowResult) string {
	if window.Metrics.Total == 0 || window.Metrics.P95MS > v.Result.Config.P95LimitMS {
		return "bad"
	}
	if float64(window.Metrics.Failures)/float64(window.Metrics.Total) > v.Result.Config.MaxErrorRate {
		return "bad"
	}
	return "ok"
}

func ListenAddress(address string) error {
	if !strings.HasPrefix(address, "127.0.0.1:") && !strings.HasPrefix(address, "[::1]:") && !strings.HasPrefix(address, "localhost:") {
		return fmt.Errorf("dashboard must bind to a loopback address")
	}
	return nil
}

const document = `<!doctype html><html><head><meta charset="utf-8"><title>Damocline report</title><style>
body{font:16px system-ui;margin:2rem;max-width:960px;color:#172033;background:#f8fafc}h1{margin-bottom:.2rem}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:1rem}.card{background:#fff;border:1px solid #dbe3ec;border-radius:.5rem;padding:1rem}.value{font-size:1.6rem;font-weight:650}.pass{color:#087443}.fail,.bad{color:#b42318}.inconclusive{color:#a15c00}table{width:100%;border-collapse:collapse;background:#fff}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #e5e7eb}small{color:#52606d}</style></head><body>
<h1>{{.Result.Name}}</h1><p><small>{{.Result.Target}} · {{.Result.StartedAt}}</small></p>
<div class="grid"><div class="card"><small>Outcome</small><div class="value {{lower .Result.Outcome}}">{{.Result.Outcome}}</div></div><div class="card"><small>Requests</small><div class="value">{{.Result.Metrics.Total}}</div></div><div class="card"><small>Request p95</small><div class="value">{{printf "%.0f" .Result.Metrics.P95MS}} ms</div></div><div class="card"><small>HTTP requests/s</small><div class="value">{{if .HasWorkloadWindow}}{{printf "%.2f" .Throughput}}{{else}}not recorded{{end}}</div><small>max_rps ceiling: {{.Result.Config.MaxRPS}}</small></div><div class="card"><small>Successful configured journeys/s</small><div class="value">{{if .HasObservationWindow}}{{printf "%.2f" .SuccessfulJourneyThroughput}}{{else}}not recorded{{end}}</div></div><div class="card"><small>Completed journeys</small><div class="value">{{.Result.Journeys.Total}}</div><small>{{.Result.Journeys.Success}} successful; {{.Result.Journeys.Failures}} failed</small></div><div class="card"><small>Cancelled / incomplete journeys</small><div class="value">{{.Result.Journeys.Cancelled}}</div></div></div>
<h2>HTTP outcomes</h2><p>Status families: 1xx {{.Result.Metrics.HTTP.Status1xx}}, 2xx {{.Result.Metrics.HTTP.Status2xx}}, 3xx {{.Result.Metrics.HTTP.Status3xx}}, 4xx {{.Result.Metrics.HTTP.Status4xx}}, 5xx {{.Result.Metrics.HTTP.Status5xx}}. Redirects {{.Result.Metrics.HTTP.RedirectResponses}}, 429 responses {{.Result.Metrics.HTTP.RateLimitedResponses}}, assertion failures {{.Result.Metrics.HTTP.AssertionFailures}}, timeouts {{.Result.Metrics.HTTP.NetworkTimeouts}}, DNS failures {{.Result.Metrics.HTTP.DNSFailures}}, connection failures {{.Result.Metrics.HTTP.ConnectionFailures}}, TLS failures {{.Result.Metrics.HTTP.TLSFailures}}, transport failures {{.Result.Metrics.HTTP.TransportFailures}}, oversized responses {{.Result.Metrics.HTTP.UnexpectedResponseSizes}}.</p>
{{if .Result.Calibration}}<h2>Calibration coverage: {{.Result.Calibration.Coverage.Status}}</h2><table><tr><th>Dimension</th><th>Status</th><th>Reason</th></tr>{{range .Result.Calibration.Coverage.Dimensions}}<tr><td>{{.Name}}</td><td>{{.Status}}</td><td>{{.Reason}}</td></tr>{{end}}</table>{{end}}
{{if .Result.Quality}}<h2>Measurement quality: {{.Result.Quality.State}}</h2><ul>{{range .Result.Quality.Findings}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .HasTimeline}}<h2>Observation-window timeline</h2><p>Aggregate HTTP request latency by observation window. Values are descriptive for completed requests and are not browser page timings.</p>{{if .HasLatencyTimeline}}<svg role="img" aria-label="Request p95 and p99 by observation window" viewBox="0 0 640 185" width="100%" height="185"><line x1="45" y1="145" x2="595" y2="145" stroke="#52606d"/><line x1="45" y1="25" x2="45" y2="145" stroke="#52606d"/><text x="2" y="30">{{.LatencyMaxLabel}}</text><text x="8" y="147">0 ms</text><polyline fill="none" stroke="#175cd3" stroke-width="3" points="{{.LatencyP95Points}}"/><polyline fill="none" stroke="#d92d20" stroke-width="3" stroke-dasharray="6 4" points="{{.LatencyP99Points}}"/>{{range .TimelineLabels}}<text x="{{.X}}" y="165" text-anchor="middle">{{.Label}}</text>{{end}}</svg><p><span style="color:#175cd3">● p95</span> &nbsp; <span style="color:#d92d20">● p99</span></p>{{else}}<p>No completed request samples are available in these windows.</p>{{end}}{{end}}
{{if .Metrics}}<h2>Application-metric timeline</h2><p>Timestamped samples collected during this run. Each chart shows observations only; connecting lines and causal interpretation are intentionally omitted. Scrape traffic is outside workload request counts.</p>{{range .Metrics}}<h3>{{.Name}}</h3><small>Metric scale {{.Min}}–{{.Max}}. Blue circles: metric. {{if .HasRequests}}Request p95 scale {{.RequestMin}}–{{.RequestMax}} ms; red squares: p95 window centers.{{else}}No request-window p95 values to align.{{end}} UTC {{.StartLabel}}–{{.EndLabel}}</small><svg role="img" aria-label="Application metric {{.Name}} observed values" viewBox="0 0 640 180" width="100%" height="180"><line x1="45" y1="140" x2="595" y2="140" stroke="#52606d"/><line x1="45" y1="25" x2="45" y2="140" stroke="#52606d"/><text x="45" y="165">{{.StartLabel}}</text><text x="595" y="165" text-anchor="end">{{.EndLabel}}</text>{{range .Points}}<circle cx="{{.X}}" cy="{{.Y}}" r="3" fill="#175cd3"><title>{{.At}} — {{.Value}}</title></circle>{{end}}{{range .RequestPoints}}<rect x="{{.X}}" y="{{.Y}}" width="6" height="6" fill="#d92d20"><title>Request p95 window center {{.At}} — {{.Value}} ms</title></rect>{{end}}</svg>{{end}}<p><small>Samples retain metric names and values without Prometheus labels. Values are not mapped to Damocline capacity or SLO verdicts.</small></p>{{else if .Result.ApplicationMetrics}}<h2>Application-metric timeline</h2><p>Collection state: {{.Result.ApplicationMetrics.State}}; no successful samples are available to plot.</p>{{end}}
<h2>Observation windows</h2><table><tr><th>Window</th><th>Work</th><th>Unit</th><th>Samples</th><th>p95</th><th>p99</th><th>Failures</th><th>Success</th></tr>{{range .Result.Windows}}<tr class="{{$.WindowClass .}}"><td>{{printf "%.0f-%.0fs" .StartSeconds .EndSeconds}}</td><td>aggregate</td><td>requests</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{range .Steps}}<tr><td></td><td>step {{.Name}}</td><td>requests</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{end}}{{range .Profiles}}<tr><td></td><td>profile {{.Name}}</td><td>completed journeys</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{end}}<tr><td></td><td>aggregate journeys</td><td>completed journeys</td><td>{{.Journeys.Total}}</td><td>{{printf "%.0f ms" .Journeys.P95MS}}</td><td>{{printf "%.0f ms" .Journeys.P99MS}}</td><td>{{.Journeys.Failures}}</td><td>{{.Journeys.Success}}</td></tr>{{end}}</table>
{{if or .Result.Generator (gt .Result.ArrivalsScheduled 0)}}<h2>Run-wide context (not aligned to windows)</h2>{{if gt .Result.ArrivalsScheduled 0}}<p>Scheduled arrivals: {{.Result.ArrivalsScheduled}}; dropped starts: {{.Result.ArrivalsDropped}} (late {{.Result.ArrivalsDroppedLate}}, busy {{.Result.ArrivalsDroppedBusy}}).</p>{{end}}{{if .Result.Generator}}<p>Generator samples: {{.Result.Generator.Samples}}; peak Go heap {{.Result.Generator.PeakHeapAllocBytes}} bytes; peak goroutines {{.Result.Generator.PeakGoroutines}}; peak process RSS {{.Result.Generator.PeakProcessRSSBytes}} bytes. These are run-wide samples, not a time-aligned series.</p>{{end}}{{end}}
<h2>Journeys</h2><p>p95 {{printf "%.1f ms" .Result.Journeys.P95MS}}; {{printf "%.1f ms" .Result.ObservedThinkTimeMS}} observed think-time wait across {{.Result.ThinkTimeObservations}} intervals. Journey latency includes rate-token waits and configured think time; request latency does not.</p>
<h2>Steps</h2><table><tr><th>Step</th><th>Total</th><th>p95</th><th>Failures</th><th>Complete body sizes (min-max, total)</th><th>Incomplete / oversized bodies</th><th>Rate wait</th><th>DNS</th><th>TCP connect</th><th>TLS</th><th>Request write</th><th>First byte</th><th>Body receive</th><th>Full client time</th></tr>{{range .Result.Steps}}<tr><td>{{.Name}}</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{.Metrics.Failures}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.CompleteObservations}} ({{.Metrics.ResponseBody.MinBytes}}-{{.Metrics.ResponseBody.MaxBytes}} bytes, {{.Metrics.ResponseBody.TotalBytes}} bytes){{else}}not recorded in schema{{end}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.IncompleteObservations}} / {{.Metrics.ResponseBody.OversizedObservations}}{{else}}not recorded{{end}}</td><td>{{printf "%.1f ms" .HTTPTrace.RateWaitMS}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.DNSMS .HTTPTrace.DNSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.ConnectMS .HTTPTrace.ConnectCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.TLSMS .HTTPTrace.TLSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.WriteMS .HTTPTrace.WriteCount}}</td><td>{{printf "%.1f ms" .HTTPTrace.FirstByteMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.BodyMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.CompleteMS}}</td></tr>{{end}}</table>
<p><small>This view presents observed HTTP requests and configured journey outcomes for the saved report. It does not establish a universal user limit, a bottleneck cause, or an infrastructure recommendation.</small></p></body></html>`

const campaignDocument = `<!doctype html><html><head><meta charset="utf-8"><title>Damocline campaign report</title><style>
body{font:16px system-ui;margin:2rem;max-width:1000px;color:#172033;background:#f8fafc}h1{margin-bottom:.2rem}.card{background:#fff;border:1px solid #dbe3ec;border-radius:.5rem;padding:1rem;margin:1rem 0}.pass{color:#087443}.fail,.bad{color:#b42318}.inconclusive{color:#a15c00}table{width:100%;border-collapse:collapse;background:#fff}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #e5e7eb}small{color:#52606d}</style></head><body>
<h1>{{.Scenario}} campaign</h1><p><small>{{.Kind}} · {{.Target}} · {{.StartedAt}}</small></p><div class="card"><strong>Recorded outcome:</strong> <span class="{{lower .Outcome}}">{{.Outcome}}</span>{{if .Pending}}<p>This artifact contains an in-progress checkpoint. The campaign outcome is not final.</p>{{end}}<p>Load unit: {{.Unit}}. Trials and phases below are saved observations; this dashboard does not reassess campaign capacity.</p></div>
{{range .Trials}}<section><h2>Trial {{.Number}} — {{.Outcome}}{{if .Pending}} (checkpoint in progress){{end}}</h2>{{if .Reason}}<p>{{.Reason}}</p>{{end}}{{if .Phases}}{{range .Phases}}<article class="card"><h3>{{.Name}} phase — {{.Run.Outcome}}</h3><p>{{.EvidenceState}} · {{.Load}} · {{printf "%.1f" .Run.ObservationSeconds}} s observation · {{.Run.Metrics.Total}} HTTP requests · p95 {{printf "%.1f ms" .Run.Metrics.P95MS}} · {{.ErrorRate}} error rate</p>{{if .Reason}}<p>{{.Reason}}</p>{{end}}{{if .Run.ApplicationMetrics}}<p>Application metrics: {{.Run.ApplicationMetrics.State}}; {{len .Run.ApplicationMetrics.Samples}} snapshots, source {{.Run.ApplicationMetrics.Source}}.</p>{{end}}{{if .Run.Windows}}<h4>HTTP latency by observation window</h4><table><tr><th>Window (s)</th><th>Requests</th><th>p95</th><th>p99</th><th>Failures</th><th>Journeys</th><th>Journey p95</th></tr>{{range .Run.Windows}}<tr><td>{{printf "%.0f–%.0f" .StartSeconds .EndSeconds}}</td><td>{{.Metrics.Total}}</td><td>{{printf "%.1f ms" .Metrics.P95MS}}</td><td>{{printf "%.1f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Journeys.Total}}</td><td>{{printf "%.1f ms" .Journeys.P95MS}}</td></tr>{{end}}</table>{{end}}{{range .Metrics}}<h4>Application metric: {{.Name}}</h4><small>Metric scale {{.Min}}–{{.Max}}. Blue circles: metric. {{if .HasRequests}}Request p95 scale {{.RequestMin}}–{{.RequestMax}} ms; red squares: p95 window centers.{{else}}No request-window p95 values to align.{{end}} UTC {{.StartLabel}}–{{.EndLabel}}</small><svg role="img" aria-label="Application metric {{.Name}} observations" viewBox="0 0 640 180" width="100%" height="180"><line x1="45" y1="140" x2="595" y2="140" stroke="#52606d"/><line x1="45" y1="25" x2="45" y2="140" stroke="#52606d"/><text x="45" y="165">{{.StartLabel}}</text><text x="595" y="165" text-anchor="end">{{.EndLabel}}</text>{{range .Points}}<circle cx="{{.X}}" cy="{{.Y}}" r="3" fill="#175cd3"><title>{{.At}} — {{.Value}}</title></circle>{{end}}{{range .RequestPoints}}<rect x="{{.X}}" y="{{.Y}}" width="6" height="6" fill="#d92d20"><title>Request p95 window center {{.At}} — {{.Value}} ms</title></rect>{{end}}</svg>{{end}}<p><small>Application values are shown as timestamped observations, without labels. No interpolation, SLO mapping, or cause inference is performed.</small></p></article>{{end}}{{else}}<p>No phase reports were saved for this trial.</p>{{end}}</section>{{end}}
<p><small>This is a local, read-only visualization of the saved campaign. Scheduled journey starts/s, virtual users, completed journeys, and HTTP requests/s are distinct workload units. The recorded PASS/FAIL does not independently prove sustainable capacity.</small></p></body></html>`
