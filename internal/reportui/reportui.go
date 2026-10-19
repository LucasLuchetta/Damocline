// Package reportui renders local, read-only HTML views of Damocline reports.
package reportui

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

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
		if err := page.Execute(w, view{Result: result}); err != nil {
			http.Error(w, "render report", http.StatusInternalServerError)
		}
	})
}

type view struct{ Result report.Result }

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
<h2>Observation windows</h2><table><tr><th>Window</th><th>Work</th><th>Unit</th><th>Samples</th><th>p95</th><th>p99</th><th>Failures</th><th>Success</th></tr>{{range .Result.Windows}}<tr class="{{$.WindowClass .}}"><td>{{printf "%.0f-%.0fs" .StartSeconds .EndSeconds}}</td><td>aggregate</td><td>requests</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{range .Steps}}<tr><td></td><td>step {{.Name}}</td><td>requests</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{end}}{{range .Profiles}}<tr><td></td><td>profile {{.Name}}</td><td>completed journeys</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{printf "%.0f ms" .Metrics.P99MS}}</td><td>{{.Metrics.Failures}}</td><td>{{.Metrics.Success}}</td></tr>{{end}}<tr><td></td><td>aggregate journeys</td><td>completed journeys</td><td>{{.Journeys.Total}}</td><td>{{printf "%.0f ms" .Journeys.P95MS}}</td><td>{{printf "%.0f ms" .Journeys.P99MS}}</td><td>{{.Journeys.Failures}}</td><td>{{.Journeys.Success}}</td></tr>{{end}}</table>
{{if or .Result.Generator (gt .Result.ArrivalsScheduled 0)}}<h2>Run-wide context (not aligned to windows)</h2>{{if gt .Result.ArrivalsScheduled 0}}<p>Scheduled arrivals: {{.Result.ArrivalsScheduled}}; dropped starts: {{.Result.ArrivalsDropped}} (late {{.Result.ArrivalsDroppedLate}}, busy {{.Result.ArrivalsDroppedBusy}}).</p>{{end}}{{if .Result.Generator}}<p>Generator samples: {{.Result.Generator.Samples}}; peak Go heap {{.Result.Generator.PeakHeapAllocBytes}} bytes; peak goroutines {{.Result.Generator.PeakGoroutines}}; peak process RSS {{.Result.Generator.PeakProcessRSSBytes}} bytes. These are run-wide samples, not a time-aligned series.</p>{{end}}{{end}}
<h2>Journeys</h2><p>p95 {{printf "%.1f ms" .Result.Journeys.P95MS}}; {{printf "%.1f ms" .Result.ObservedThinkTimeMS}} observed think-time wait across {{.Result.ThinkTimeObservations}} intervals. Journey latency includes rate-token waits and configured think time; request latency does not.</p>
<h2>Steps</h2><table><tr><th>Step</th><th>Total</th><th>p95</th><th>Failures</th><th>Complete body sizes (min-max, total)</th><th>Incomplete / oversized bodies</th><th>Rate wait</th><th>DNS</th><th>TCP connect</th><th>TLS</th><th>Request write</th><th>First byte</th><th>Body receive</th><th>Full client time</th></tr>{{range .Result.Steps}}<tr><td>{{.Name}}</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{.Metrics.Failures}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.CompleteObservations}} ({{.Metrics.ResponseBody.MinBytes}}-{{.Metrics.ResponseBody.MaxBytes}} bytes, {{.Metrics.ResponseBody.TotalBytes}} bytes){{else}}not recorded in schema{{end}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.IncompleteObservations}} / {{.Metrics.ResponseBody.OversizedObservations}}{{else}}not recorded{{end}}</td><td>{{printf "%.1f ms" .HTTPTrace.RateWaitMS}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.DNSMS .HTTPTrace.DNSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.ConnectMS .HTTPTrace.ConnectCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.TLSMS .HTTPTrace.TLSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.WriteMS .HTTPTrace.WriteCount}}</td><td>{{printf "%.1f ms" .HTTPTrace.FirstByteMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.BodyMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.CompleteMS}}</td></tr>{{end}}</table>
<p><small>This view presents observed HTTP requests and configured journey outcomes for the saved report. It does not establish a universal user limit, a bottleneck cause, or an infrastructure recommendation.</small></p></body></html>`
