package engine

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/reference"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

// TestNativeEngineReferencePayloadMatrix exercises the ACTUAL load scheduler
// against a local controlled fixture, unlike protocolcheck's Go-transport-only
// matrix. This only qualifies bounded HTTP loopback delivery, not HTTPS,
// browser timing, remote DNS, generator maxima or production capacity.
func TestNativeEngineReferencePayloadMatrix(t *testing.T) {
	cases:=[]struct{
		name string
		bytes int
		delay time.Duration
		extraEvery int64
		extraDelay time.Duration
		closeEvery int64
	}{
		{"1k",1024,10*time.Millisecond,0,0,0},
		{"4k",4096,10*time.Millisecond,0,0,0},
		{"64k",65536,10*time.Millisecond,0,0,0},
		{"latency-tail",4096,10*time.Millisecond,3,80*time.Millisecond,0},
		{"connection-close",4096,10*time.Millisecond,0,0,1},
	}
	for _,tc:=range cases {
		t.Run(tc.name,func(t *testing.T) {
			fixture,err:=reference.New(reference.Config{
				Delay:tc.delay,ExtraEvery:tc.extraEvery,
				ExtraDelay:tc.extraDelay,ResponseBytes:tc.bytes,
				CloseEvery:tc.closeEvery,MaxConcurrent:10,MaxRequests:100,
			})
			if err!=nil {t.Fatal(err)}
			server:=httptest.NewServer(fixture)
			defer server.Close()
			s:=config.Scenario{
				Name:"bounded local reference",BaseURL:server.URL,
				Users:10,Duration:"2s",Mode:"arrival",ArrivalRate:5,
				MaxRPS:20,Timeout:"1s",P95LimitMS:900,
				MaxErrorRate:0,MinSamplesPerStep:1,
				Steps:[]config.Step{{Name:"reference",Method:"GET",Path:"/work",ExpectedStatus:[]int{200}}},
			}
			v,err:=config.Parse(s,config.Options{Authorized:true})
			if err!=nil {t.Fatal(err)}
			out:=Run(context.Background(),v)
			ledger:=fixture.Snapshot()
			const expected=10
			if out.ArrivalsScheduled!=expected || out.IterationsStarted!=expected ||
				out.IterationsCompleted!=expected || out.ArrivalsDropped!=0 ||
				out.Metrics.Total!=expected || out.Metrics.Success!=expected ||
				out.Metrics.Failures!=0 || out.Outcome!="PASS" ||
				ledger.Received!=expected || ledger.Completed!=expected ||
				ledger.Success!=expected || ledger.Cancelled!=0 ||
				ledger.Failures!=0 || ledger.Rejected!=0 {
				t.Fatalf("%s: native engine and independent server ledger disagree: %+v %+v",
					tc.name,out,ledger)
			}
			if out.Quality==nil || out.Quality.State!=report.QualityLimited {
				t.Fatalf("%s: passing fixture cannot imply validated arbitrary capacity: %+v",
					tc.name,out.Quality)
			}
		})
	}
}
