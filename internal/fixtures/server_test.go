package fixtures
import (
 "io"
 "net/http"
 "testing"
 "time"
)
func TestKnownResponseDistribution(t *testing.T){
 f:=New(Config{FailEvery:2});defer f.Close()
 c:=&http.Client{Timeout:time.Second}
 for n:=0;n<4;n++{
  r,err:=c.Get(f.URL)
  if err!=nil{t.Fatal(err)}
  _,_=io.Copy(io.Discard,r.Body);_ = r.Body.Close()
  want:=200;if n%2==1 {want=500}
  if r.StatusCode!=want{t.Fatalf("response %d got %d want %d",n,r.StatusCode,want)}
 }
 x:=f.Snapshot()
 if x.Total!=4||x.Success!=2||x.Rejected!=2{t.Fatalf("fixture mismatch: %+v",x)}
}
func TestBoundedConcurrency(t *testing.T){
 f:=New(Config{MaxInFlight:1,Delay:120*time.Millisecond});defer f.Close()
 ready:=make(chan struct{})
 finished:=make(chan struct{})
 go func(){
  close(ready)
  r,e:=http.Get(f.URL);if e==nil{_=r.Body.Close()}
  close(finished)
 }()
 <-ready
 // Allow the first handler to occupy its single-slot concurrency gate.
 limit:=time.After(250*time.Millisecond)
 for f.active.Load()==0 {
  select{case <-limit:t.Fatal("fixture did not admit first request");default:time.Sleep(time.Millisecond)}
 }
 r,e:=http.Get(f.URL)
 if e!=nil{t.Fatal(e)}
 _=r.Body.Close()
 if r.StatusCode!=http.StatusServiceUnavailable{t.Fatalf("expected controlled 503, got %d",r.StatusCode)}
 <-finished
 s:=f.Snapshot()
 if s.Rejected==0||s.PeakInFlight!=1{t.Fatalf("limit not enforced: %+v",s)}
}
