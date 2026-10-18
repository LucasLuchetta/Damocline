// Package reportui renders local, read-only HTML views of Damocline reports.
package reportui

import (
	"fmt"
	"html/template"
	"net/http"
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
	if v.Result.WindowSeconds() == 0 {
		return 0
	}
	return float64(v.Result.Metrics.Total) / v.Result.WindowSeconds()
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
body{font:16px system-ui;margin:2rem;max-width:960px;color:#172033;background:#f8fafc}h1{margin-bottom:.2rem}.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:1rem}.card{background:#fff;border:1px solid #dbe3ec;border-radius:.5rem;padding:1rem}.value{font-size:1.6rem;font-weight:650}.pass{color:#087443}.fail,.bad{color:#b42318}.inconclusive{color:#a15c00}table{width:100%;border-collapse:collapse;background:#fff}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #e5e7eb}small{color:#52606d}</style></head><body>
<h1>{{.Result.Name}}</h1><p><small>{{.Result.Target}} · {{.Result.StartedAt}}</small></p>
<div class="grid"><div class="card"><small>Outcome</small><div class="value {{lower .Result.Outcome}}">{{.Result.Outcome}}</div></div><div class="card"><small>Requests</small><div class="value">{{.Result.Metrics.Total}}</div></div><div class="card"><small>Request p95</small><div class="value">{{printf "%.0f" .Result.Metrics.P95MS}} ms</div></div><div class="card"><small>Throughput</small><div class="value">{{printf "%.2f" .Throughput}} rps</div></div></div>
<h2>HTTP outcomes</h2><p>Status families: 1xx {{.Result.Metrics.HTTP.Status1xx}}, 2xx {{.Result.Metrics.HTTP.Status2xx}}, 3xx {{.Result.Metrics.HTTP.Status3xx}}, 4xx {{.Result.Metrics.HTTP.Status4xx}}, 5xx {{.Result.Metrics.HTTP.Status5xx}}. Redirects {{.Result.Metrics.HTTP.RedirectResponses}}, 429 responses {{.Result.Metrics.HTTP.RateLimitedResponses}}, assertion failures {{.Result.Metrics.HTTP.AssertionFailures}}, timeouts {{.Result.Metrics.HTTP.NetworkTimeouts}}, TLS failures {{.Result.Metrics.HTTP.TLSFailures}}, transport failures {{.Result.Metrics.HTTP.TransportFailures}}, oversized responses {{.Result.Metrics.HTTP.UnexpectedResponseSizes}}.</p>
<h2>Observation windows</h2><table><tr><th>Window</th><th>Requests</th><th>p95</th><th>Failures</th></tr>{{range .Result.Windows}}<tr class="{{$.WindowClass .}}"><td>{{printf "%.0f-%.0fs" .StartSeconds .EndSeconds}}</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{.Metrics.Failures}}</td></tr>{{end}}</table>
<h2>Journeys</h2><p>p95 {{printf "%.1f ms" .Result.Journeys.P95MS}}; {{printf "%.1f ms" .Result.ObservedThinkTimeMS}} observed think-time wait across {{.Result.ThinkTimeObservations}} intervals. Journey latency includes rate-token waits and configured think time; request latency does not.</p>
<h2>Steps</h2><table><tr><th>Step</th><th>Total</th><th>p95</th><th>Failures</th><th>Complete body sizes (min-max, total)</th><th>Incomplete / oversized bodies</th><th>Rate wait</th><th>DNS</th><th>TCP connect</th><th>TLS</th><th>Request write</th><th>First byte</th><th>Body receive</th><th>Full client time</th></tr>{{range .Result.Steps}}<tr><td>{{.Name}}</td><td>{{.Metrics.Total}}</td><td>{{printf "%.0f ms" .Metrics.P95MS}}</td><td>{{.Metrics.Failures}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.CompleteObservations}} ({{.Metrics.ResponseBody.MinBytes}}-{{.Metrics.ResponseBody.MaxBytes}} bytes, {{.Metrics.ResponseBody.TotalBytes}} bytes){{else}}not recorded in schema{{end}}</td><td>{{if ge $.Result.SchemaVersion 12}}{{.Metrics.ResponseBody.IncompleteObservations}} / {{.Metrics.ResponseBody.OversizedObservations}}{{else}}not recorded{{end}}</td><td>{{printf "%.1f ms" .HTTPTrace.RateWaitMS}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.DNSMS .HTTPTrace.DNSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.ConnectMS .HTTPTrace.ConnectCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.TLSMS .HTTPTrace.TLSCount}}</td><td>{{printf "%.1f ms (%d)" .HTTPTrace.WriteMS .HTTPTrace.WriteCount}}</td><td>{{printf "%.1f ms" .HTTPTrace.FirstByteMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.BodyMS}}</td><td>{{printf "%.1f ms" .HTTPTrace.CompleteMS}}</td></tr>{{end}}</table>
<p><small>This view presents observed HTTP results for the saved report. It does not establish a universal user limit, a bottleneck cause, or an infrastructure recommendation.</small></p></body></html>`
