// Package protocolcheck validates bounded, deterministic HTTP transport
// fixtures. It exercises Go's client transport, not the Damocline scheduler:
// observations MUST NOT be called generator or application capacity evidence.
package protocolcheck

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sort"
	"sync/atomic"
	"time"
)

const (
	SchemaVersion = 1
	maxResponseBytes = 64 << 10
	maxTests = 12
	requestTimeout = 2 * time.Second
)

type Case struct {
	Name string `json:"name"`
	Protocol string `json:"protocol"`
	ExpectedStatus int `json:"expected_status"`
	ExpectedBytes int `json:"expected_bytes"`
	Requests int `json:"requests"`
	ServerReceived int64 `json:"server_received"`
	ServerCompleted int64 `json:"server_completed"`
	ClientCompleted int `json:"client_completed"`
	ConnectionsReused int `json:"connections_reused"`
	FirstByteObserved bool `json:"first_byte_observed"`
	ElapsedMilliseconds float64 `json:"elapsed_milliseconds"`
	Status string `json:"status"`
	Findings []string `json:"findings,omitempty"`
}

type Result struct {
	SchemaVersion int `json:"schema_version"`
	Status string `json:"status"`
	Kind string `json:"kind"`
	BoundedToLoopback bool `json:"bounded_to_loopback"`
	RequestsTotal int `json:"requests_total"`
	Cases []Case `json:"cases"`
	Limitations []string `json:"limitations"`
}

// This matrix is independent of test application addresses and entirely
// defined in code. Streaming, latency tails, TLS and connection reuse are
// exercised over httptest-only private listeners with no outbound DNS.
type fixtureCase struct {
	name string
	tls bool
	size int
	requests int
	code int
	delay time.Duration
	stream bool
	closeConnections bool
	wantReuse bool
}

func fixtureDefinitions() []fixtureCase {
	return []fixtureCase{
		{name:"http-empty", size:0, requests:3, code:200},
		{name:"http-1k", size:1024, requests:3, code:200},
		{name:"http-4k", size:4096, requests:3, code:200},
		{name:"http-64k", size:65536, requests:3, code:200},
		{name:"http-keepalive", size:1024, requests:3, code:200, wantReuse:true},
		{name:"http-connection-close", size:1024, requests:3, code:200, closeConnections:true},
		{name:"http-tail-delay", size:1024, requests:3, code:200, delay:40*time.Millisecond},
		{name:"http-streaming", size:4096, requests:3, code:200, stream:true},
		{name:"http-503", size:128, requests:3, code:503},
		{name:"tls-private-fixture", tls:true, size:4096, requests:3, code:200},
	}
}

func executeCase(ctx context.Context, spec fixtureCase) Case {
	protocol := "http"
	if spec.tls { protocol = "https" }
	result := Case{Name:spec.name, Protocol:protocol, ExpectedStatus:spec.code,
		ExpectedBytes:spec.size, Requests:spec.requests, Status:"FAIL"}
	if spec.requests < 1 || spec.requests > 3 || spec.size < 0 ||
		spec.size > maxResponseBytes || (spec.code != 200 && spec.code != 503) {
		result.Findings = append(result.Findings, "invalid bounded fixture definition")
		return result
	}
	var received, completed atomic.Int64
	body := make([]byte,spec.size)
	for i := range body { body[i] = 'x' }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		defer completed.Add(1)
		if r.Method != "GET" || r.URL.Path != "/work" {
			w.WriteHeader(http.StatusNotFound); return
		}
		if spec.closeConnections { w.Header().Set("Connection","close") }
		if spec.delay > 0 {
			t := time.NewTimer(spec.delay)
			defer t.Stop()
			select { case <-ctx.Done(): return; case <-r.Context().Done(): return; case <-t.C: }
		}
		w.Header().Set("Content-Type","application/octet-stream")
		w.WriteHeader(spec.code)
		if spec.stream {
			half := len(body)/2
			_,_ = w.Write(body[:half])
			if f, ok := w.(http.Flusher); ok { f.Flush() }
			t := time.NewTimer(15*time.Millisecond)
			defer t.Stop()
			select { case <-ctx.Done(): return; case <-r.Context().Done(): return; case <-t.C: }
			_,_ = w.Write(body[half:])
			return
		}
		_,_ = w.Write(body)
	})
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = false
	if spec.tls { server.StartTLS() } else { server.Start() }
	defer server.Close()
	var tlsConfig *tls.Config
	if spec.tls {
		// Trust precisely the ephemeral certificate issued by this local
		// httptest fixture; never disable certificate verification globally.
		if tr, ok := server.Client().Transport.(*http.Transport); ok && tr.TLSClientConfig != nil {
			tlsConfig = tr.TLSClientConfig.Clone()
		} else {
			result.Findings = append(result.Findings, "local TLS fixture trust unavailable")
			return result
		}
	}
	transport := &http.Transport{
		Proxy:nil,
		TLSClientConfig:tlsConfig,
		DisableKeepAlives:spec.closeConnections,
		MaxIdleConns:4,
		MaxIdleConnsPerHost:4,
		IdleConnTimeout:time.Second,
		ForceAttemptHTTP2:false,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport:transport, Timeout:requestTimeout,
		CheckRedirect:func(*http.Request,[]*http.Request) error {return http.ErrUseLastResponse}}
	started := time.Now()
	for i:=0;i<spec.requests;i++ {
		requestCtx, cancel := context.WithTimeout(ctx,requestTimeout)
		var reused, firstByte bool
		trace := &httptrace.ClientTrace{
			GotConn:func(info httptrace.GotConnInfo) { reused = info.Reused },
			GotFirstResponseByte:func() { firstByte = true },
		}
		req,err:=http.NewRequestWithContext(httptrace.WithClientTrace(requestCtx, trace),
			"GET",server.URL+"/work",nil)
		if err != nil {cancel();result.Findings=append(result.Findings,"fixture request setup failed");break}
		res,err:=client.Do(req)
		if err != nil {
			cancel()
			result.Findings=append(result.Findings,"local transport request failed");break
		}
		content,readErr:=io.ReadAll(io.LimitReader(res.Body,maxResponseBytes+1))
		_ = res.Body.Close()
		cancel()
		if readErr != nil || len(content)!=spec.size || len(content)>maxResponseBytes ||
			res.StatusCode != spec.code {
			result.Findings=append(result.Findings,"fixture response status, size or body read mismatch")
			break
		}
		result.ClientCompleted++
		if reused {result.ConnectionsReused++}
		if firstByte {result.FirstByteObserved=true}
	}
	result.ElapsedMilliseconds=float64(time.Since(started).Microseconds())/1000
	result.ServerReceived=received.Load()
	result.ServerCompleted=completed.Load()
	if result.ClientCompleted!=spec.requests || result.ServerReceived!=int64(spec.requests) ||
		result.ServerCompleted!=int64(spec.requests) {
		result.Findings=append(result.Findings,"client/server request ledger mismatch")
	}
	if spec.wantReuse && result.ConnectionsReused<1 {
		result.Findings=append(result.Findings,"expected reused persistent connection was not observed")
	}
	if spec.closeConnections && result.ConnectionsReused!=0 {
		result.Findings=append(result.Findings,"disabled keep-alive unexpectedly reused a connection")
	}
	if !result.FirstByteObserved {
		result.Findings=append(result.Findings,"time to first response byte was not observed")
	}
	if len(result.Findings)==0 {result.Status="PASS"}
	return result
}

// Run performs only bounded same-host fixture operations. The caller may
// specify a shorter context but cannot expand request count or duration.
func Run(ctx context.Context) Result {
	result:=Result{SchemaVersion:SchemaVersion,Kind:"http_transport_reference",
		BoundedToLoopback:true, Status:"CHECKS_FAILED",
		Limitations:[]string{
			"These checks validate Go HTTP client transport and local fixture ledgers; they do not use the native Damocline load scheduler.",
			"Ephemeral loopback TLS certificate trust is limited to the test fixture; remote TLS, DNS and real network effects are untested.",
			"No generator maximum, server capacity, payload distribution or sustained workload threshold is inferred.",
		}}
	defs:=fixtureDefinitions()
	if len(defs)>maxTests {return result}
	budgetCtx,cancel:=context.WithTimeout(ctx,20*time.Second)
	defer cancel()
	allPassed:=true
	for _,spec:=range defs {
		if budgetCtx.Err()!=nil {
			allPassed=false
			break
		}
		test:=executeCase(budgetCtx,spec)
		result.RequestsTotal+=test.Requests
		result.Cases=append(result.Cases,test)
		if test.Status!="PASS" { allPassed=false }
	}
	if allPassed && len(result.Cases)==len(defs) {result.Status="TRANSPORT_FIXTURES_PASSED"}
	return result
}

// Validate ensures a reader cannot mistake a truncated matrix for a PASS.
// This remains structural validation, NOT evidence attestation or signing.
func Validate(r Result) error {
	if r.SchemaVersion!=SchemaVersion || r.Kind!="http_transport_reference" ||
		!r.BoundedToLoopback || len(r.Cases)!=len(fixtureDefinitions()) ||
		(r.Status!="TRANSPORT_FIXTURES_PASSED" && r.Status!="CHECKS_FAILED") {
		return errors.New("unsupported or incomplete transport fixture report")
	}
	expected:=fixtureDefinitions()
	sum:=0
	allPassed:=true
	for i,v:=range r.Cases {
		want:=expected[i]
		scheme:="http";if want.tls {scheme="https"}
		if v.Name!=want.name || v.Protocol!=scheme ||
			v.ExpectedStatus!=want.code || v.ExpectedBytes!=want.size ||
			v.Requests!=want.requests || v.ServerReceived<0 ||
			v.ServerCompleted<0 || v.ClientCompleted<0 ||
			v.ClientCompleted>v.Requests || v.ServerReceived>int64(v.Requests) ||
			v.ServerCompleted>v.ServerReceived ||
			(v.Status!="PASS" && v.Status!="FAIL") {
			return fmt.Errorf("invalid fixture case %d",i)
		}
		sum+=v.Requests
		if v.Status!="PASS" || len(v.Findings)!=0 {allPassed=false}
		if v.Status=="PASS" && (v.ClientCompleted!=v.Requests ||
			v.ServerCompleted!=int64(v.Requests) ||
			v.ServerReceived!=int64(v.Requests) ||
			(want.wantReuse && v.ConnectionsReused<1) ||
			(want.closeConnections && v.ConnectionsReused>0) || !v.FirstByteObserved) {
			return fmt.Errorf("incomplete evidence in fixture case %d",i)
		}
	}
	if r.RequestsTotal!=sum || (r.Status=="TRANSPORT_FIXTURES_PASSED")!=allPassed {
		return errors.New("fixture aggregate counters or status inconsistent")
	}
	if len(r.Limitations)<3 {return errors.New("transport limitations not documented")}
	return nil
}

// JSON keeps all network endpoints, hostnames, dynamic body data and TLS certs
// out of exported evidence. The only variable data are numeric observations.
func Encode(w io.Writer,r Result) error {
	if err:=Validate(r);err!=nil {return err}
	encoder:=json.NewEncoder(w)
	encoder.SetIndent("","  ")
	return encoder.Encode(r)
}

// Summary is stable for consumers and intentionally avoids a claim of capacity.
func Summary(r Result) string {
	count:=0
	for _,c:=range r.Cases {if c.Status=="PASS" {count++}}
	names:=make([]string,0)
	for _,c:=range r.Cases {if c.Status!="PASS" {names=append(names,c.Name)}}
	sort.Strings(names)
	return fmt.Sprintf("%s: %d/%d transport fixtures passed; remaining: %v (transport-only evidence)",
		r.Status,count,len(r.Cases),names)
}
