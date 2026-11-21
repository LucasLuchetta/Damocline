package harimport

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

const fixtureSecret = "fixture-secret-value-0123456789abcdef"

var fixtureHAR = `{"log":{"pages":[{"id":"page_1"},{"id":"page_2"}],"entries":[
 {"pageref":"page_1","request":{"method":"GET","url":"https://app.example.com/dashboard?tab=open&access_token=` + fixtureSecret + `",
   "headers":[{"name":"Accept","value":"text/html"},{"name":"Cookie","value":"sid=` + fixtureSecret + `"},{"name":"Accept-Encoding","value":"gzip"},{"name":":authority","value":"app.example.com"}],
   "cookies":[{"name":"sid","value":"` + fixtureSecret + `"}]},"response":{"status":200,"content":{"mimeType":"text/html"}}},
 {"pageref":"page_1","request":{"method":"GET","url":"https://app.example.com/static/app.js"},"response":{"status":200,"content":{"mimeType":"application/javascript"}}},
 {"pageref":"page_1","request":{"method":"GET","url":"https://app.example.com/logo"},"response":{"status":200,"content":{"mimeType":"image/png"}}},
 {"pageref":"page_1","request":{"method":"GET","url":"https://cdn.example.net/lib.js"},"response":{"status":200}},
 {"pageref":"page_2","request":{"method":"POST","url":"https://app.example.com/api/orders",
   "headers":[{"name":"Authorization","value":"Bearer ` + fixtureSecret + `"},{"name":"Content-Type","value":"application/json"},{"name":"X-CSRF-Token","value":"` + fixtureSecret + `"}],
   "postData":{"mimeType":"application/json","text":"{\"password\":\"` + fixtureSecret + `\"}"}},"response":{"status":201}},
 {"pageref":"page_2","request":{"method":"GET","url":"https://app.example.com/reset/` + fixtureSecret + `"},"response":{"status":200}},
 {"pageref":"page_2","request":{"method":"OPTIONS","url":"https://app.example.com/api/orders"},"response":{"status":204}},
 {"pageref":"page_2","request":{"method":"DELETE","url":"https://app.example.com/api/orders/42"},"response":{"status":204}}
]}}`

func base(t *testing.T) *url.URL {
	u, err := url.Parse("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func reasons(res Result) map[string]int {
	out := map[string]int{}
	for _, d := range res.Dropped {
		out[d.Reason]++
	}
	return out
}

func TestConvertReadOnlyByDefault(t *testing.T) {
	res, err := Convert([]byte(fixtureHAR), base(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.Scenario.Workflows[0].Steps
	if len(steps) != 1 || steps[0].Path != "/dashboard?tab=open" || steps[0].Name != "p01-01-get-dashboard" || steps[0].ExpectedStatus[0] != 200 {
		t.Fatalf("unexpected steps: %+v", steps)
	}
	if steps[0].Headers["Cookie"] != "${DAMOCLINE_COOKIE}" || steps[0].Headers["Accept"] != "text/html" || len(steps[0].Headers) != 2 {
		t.Fatalf("unexpected headers: %v", steps[0].Headers)
	}
	got := reasons(res)
	want := map[string]int{"static_asset": 2, "cross_origin": 1, "write_method_not_opted_in": 2, "sensitive_path": 1, "unsupported_method": 1}
	for reason, n := range want {
		if got[reason] != n {
			t.Fatalf("dropped %v, want %v", got, want)
		}
	}
	if len(res.RemovedQuery) != 1 || res.RemovedQuery[0] != "access_token" || len(res.WriteSteps) != 0 {
		t.Fatalf("unexpected review evidence: %+v", res)
	}
	assertNoSecret(t, res)
}

func TestConvertWritesRequireOptInAndUsePlaceholders(t *testing.T) {
	res, err := Convert([]byte(fixtureHAR), base(t), Options{IncludeWrites: true, Name: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.Scenario.Workflows[0].Steps
	if len(steps) != 3 || len(res.WriteSteps) != 2 {
		t.Fatalf("unexpected steps: %+v", steps)
	}
	post := steps[1]
	if post.Name != "p02-02-post-api-orders" || post.BodyEnv != "DAMOCLINE_BODY_02" || post.Headers["Authorization"] != "${DAMOCLINE_AUTHORIZATION}" ||
		post.Headers["X-CSRF-Token"] != "${DAMOCLINE_HEADER_X_CSRF_TOKEN}" || post.Headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected write step: %+v", post)
	}
	wantPlaceholders := "DAMOCLINE_AUTHORIZATION,DAMOCLINE_BODY_02,DAMOCLINE_COOKIE,DAMOCLINE_HEADER_X_CSRF_TOKEN"
	if strings.Join(res.Placeholders, ",") != wantPlaceholders {
		t.Fatalf("placeholders %v", res.Placeholders)
	}
	assertNoSecret(t, res)
	// The draft must be a valid scenario once placeholders are supplied and
	// writes are explicitly allowed.
	for _, name := range res.Placeholders {
		t.Setenv(name, "synthetic")
	}
	if _, err := config.Parse(res.Scenario, config.Options{Authorized: true}); err == nil || !strings.Contains(err.Error(), "--allow-writes") {
		t.Fatalf("write draft must require --allow-writes: %v", err)
	}
	if _, err := config.Parse(res.Scenario, config.Options{Authorized: true, AllowWrites: true}); err != nil {
		t.Fatalf("draft does not validate: %v", err)
	}
}

func TestConvertKeepStaticAndStepLimit(t *testing.T) {
	var entries []string
	for i := 0; i < MaxSteps+3; i++ {
		entries = append(entries, `{"request":{"method":"GET","url":"https://app.example.com/a.css"},"response":{"status":200}}`)
	}
	res, err := Convert([]byte(`{"log":{"entries":[`+strings.Join(entries, ",")+`]}}`), base(t), Options{KeepStatic: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Scenario.Workflows[0].Steps) != MaxSteps || reasons(res)["step_limit"] != 3 {
		t.Fatalf("unexpected limit handling: %d %v", len(res.Scenario.Workflows[0].Steps), reasons(res))
	}
}

func TestConvertRejectsEmptyAndInvalid(t *testing.T) {
	if _, err := Convert([]byte("not json"), base(t), Options{}); err == nil {
		t.Fatal("invalid HAR accepted")
	}
	if _, err := Convert([]byte(`{"log":{"entries":[{"request":{"method":"GET","url":"https://other.example.org/"}}]}}`), base(t), Options{}); err == nil {
		t.Fatal("HAR without same-origin requests accepted")
	}
}

func assertNoSecret(t *testing.T, res Result) {
	t.Helper()
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), fixtureSecret) || strings.Contains(string(encoded), "password") {
		t.Fatalf("draft leaked a secret: %s", encoded)
	}
}
