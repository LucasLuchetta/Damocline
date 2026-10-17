package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasLuchetta/Damocline/internal/generatorhealth"
)

func TestSignalsClassifyClientAndHTTPObservationsWithoutCausality(t *testing.T) {
	r:=validSyntheticQualityRun()
	r.ArrivalsDroppedBusy=2
	r.ArrivalsDroppedLate=1
	r.Config.MaxRPS=10
	r.Generator=&generatorhealth.Summary{
		Samples:10,GoCPUValid:true,ProcessCPUValid:true,
		GoCPUUtilizationRatio:0.95,PeakProcessCPUUtilizationRatio:0.92,
	}
	r.Metrics.Reasons=map[string]int{
		"http_503":1,
		"Bearer SECRET-SHOULD-NOT-LEAK":10,
	}
	signals:=DetectSignals(r)
	want:=map[string]bool{
		"GENERATOR_CPU_PRESSURE":false,
		"CLIENT_RATE_CEILING_NEAR":false,
		"CLIENT_WORKERS_BUSY":false,
		"CLIENT_SCHEDULER_LATE":false,
		"HTTP_ERROR_RESPONSE":false,
	}
	for _,s:=range signals {
		if _,exists:=want[s.Code]; exists {want[s.Code]=true}
		if s.Origin=="server_cause" {t.Fatal("an HTTP status cannot prove server bottleneck")}
	}
	for k,ok:=range want {
		if !ok {t.Fatalf("missing signal %s in %+v",k,signals)}
	}
	data,err:=json.Marshal(signals)
	if err!=nil {t.Fatal(err)}
	if strings.Contains(string(data),"SECRET-SHOULD-NOT-LEAK") {
		t.Fatal("untrusted reason leaked into diagnostics")
	}
}

func TestSignalsDoNotInferUnknownInfrastructure(t *testing.T) {
	r:=validSyntheticQualityRun()
	r.Config.MaxRPS=100
	r.Generator=&generatorhealth.Summary{Samples:10,ProcessCPUValid:true,GoCPUValid:true}
	sigs:=DetectSignals(r)
	if len(sigs)!=1 || sigs[0].Code!="BOTTLENECK_NOT_ESTABLISHED" {
		t.Fatalf("unknown server bottleneck should stay unknown: %+v",sigs)
	}
	r.Generator=nil
	sigs=DetectSignals(r)
	if len(sigs)!=1 || sigs[0].Code!="GENERATOR_TELEMETRY_UNAVAILABLE" {
		t.Fatalf("missing process metrics should be explicit: %+v",sigs)
	}
}
