package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func TestRotateCampaignCookies(t *testing.T) {
	t.Setenv("TEST_SESSION_1_1", "session=synthetic-one; marker=fixture")
	t.Setenv("TEST_SESSION_2_1", "session=synthetic-two")
	cookies, err := config.ResolveTrialSessions("TEST_SESSION_", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Observed", cookie.Value)
	}))
	defer server.Close()

	var observed string
	runner, selectTrial := rotateCampaignCookies(func(ctx context.Context, v config.Validated) report.Result {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", v.Cookies[0])
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		observed = response.Header.Get("X-Observed")
		return report.Result{}
	}, cookies)
	for _, step := range []struct {
		number int
		want   string
	}{{1, "synthetic-one"}, {1, "synthetic-one"}, {2, "synthetic-two"}} {
		selectTrial(step.number)
		runner(context.Background(), config.Validated{Scenario: config.Scenario{Users: 1}})
		if observed != step.want {
			t.Fatalf("trial %d: got %q, want %q", step.number, observed, step.want)
		}
	}
	t.Setenv("TEST_SESSION_2_1", "marker=fixture; session=synthetic-one")
	if _, err := config.ResolveTrialSessions("TEST_SESSION_", 1, 2); err == nil {
		t.Fatal("cross-trial duplicate was accepted")
	}
	t.Setenv("TEST_SESSION_2_1", "")
	if _, err := config.ResolveTrialSessions("TEST_SESSION_", 1, 2); err == nil {
		t.Fatal("missing later trial cookie was accepted")
	}
}
