package protocolcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBoundedTransportMatrixReconcilesAllFixtures(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	r:=Run(ctx)
	if r.Status!="TRANSPORT_FIXTURES_PASSED" {
		t.Fatalf("%s; results: %+v", Summary(r),r.Cases)
	}
	if err:=Validate(r);err!=nil {t.Fatal(err)}
	if len(r.Cases)!=10 || r.RequestsTotal!=30 {t.Fatalf("unexpected matrix size: %+v",r)}
	var gotTLS,gotTail,gotStream,gotReuse,gotClosed bool
	for _,c:=range r.Cases {
		if c.Name=="tls-private-fixture" && c.Protocol=="https" && c.ClientCompleted==3 {gotTLS=true}
		if c.Name=="http-tail-delay" && c.ElapsedMilliseconds>=100 {gotTail=true}
		if c.Name=="http-streaming" && c.FirstByteObserved {gotStream=true}
		if c.Name=="http-keepalive" && c.ConnectionsReused>=1 {gotReuse=true}
		if c.Name=="http-connection-close" && c.ConnectionsReused==0 {gotClosed=true}
	}
	if !(gotTLS&&gotTail&&gotStream&&gotReuse&&gotClosed) {
		t.Fatalf("matrix failed to exercise distinct transport cases: %+v",r.Cases)
	}
	var output bytes.Buffer
	if err:=Encode(&output,r);err!=nil {t.Fatal(err)}
	for _,forbidden:=range []string{"127.0.0.1:","https://127.","private-token","-----BEGIN CERTIFICATE-----","/work"}{
		if strings.Contains(output.String(),forbidden){t.Fatalf("fixture output leaks endpoint or secret: %s",forbidden)}
	}
	var decoded Result
	if err:=json.Unmarshal(output.Bytes(),&decoded);err!=nil {t.Fatal(err)}
	if err:=Validate(decoded);err!=nil {t.Fatal(err)}
}

func TestMatrixNeverAcceptsInconsistentOrIncompletePass(t *testing.T) {
	r:=Run(context.Background())
	if r.Status!="TRANSPORT_FIXTURES_PASSED" {t.Fatalf("reference fixture failed: %s",Summary(r))}
	r.Cases[0].ServerCompleted--
	if err:=Validate(r);err==nil {t.Fatal("PASS with incomplete server ledger accepted")}
	r.Cases[0].ServerCompleted++
	r.RequestsTotal++
	if err:=Validate(r);err==nil {t.Fatal("aggregate mismatch accepted")}
	r.RequestsTotal--
	r.Cases=r.Cases[:len(r.Cases)-1]
	if err:=Validate(r);err==nil {t.Fatal("truncated case list accepted")}
}

func TestMatrixCancellationDoesNotPass(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background())
	cancel()
	r:=Run(ctx)
	if r.Status=="TRANSPORT_FIXTURES_PASSED" {t.Fatal("cancelled transport reference passed")}
	if err:=Validate(r);err==nil {t.Fatal("partial matrix accepted as valid report")}
}

func TestFixtureRejectsUnsafeSizeAndRequestCount(t *testing.T) {
	for _,f:=range []fixtureCase{
		{name:"overflow",size:maxResponseBytes+1,requests:1,code:200},
		{name:"too-many",size:1024,requests:100,code:200},
		{name:"unexpected-code",size:1024,requests:1,code:201},
	} {
		r:=executeCase(context.Background(),f)
		if r.Status=="PASS" || len(r.Findings)==0 {t.Fatalf("unsafe fixture accepted: %+v",r)}
	}
}
