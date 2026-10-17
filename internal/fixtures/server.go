// Package fixtures provides loopback-only controlled responses to validate
// the load generator with known delay, concurrency, and failure characteristics.
package fixtures

import (
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "time"
)

type Config struct {
 Delay time.Duration
 MaxInFlight int
 FailEvery int64
}
type Counts struct {
 Total int64
 Success int64
 Rejected int64
 PeakInFlight int64
}
type Server struct {
 *httptest.Server
 total atomic.Int64
 success atomic.Int64
 rejected atomic.Int64
 active atomic.Int64
 peak atomic.Int64
}

// New starts a local httptest server, never a listening public metrics service.
func New(config Config) *Server {
 s:=&Server{}
 var gate chan struct{}
 if config.MaxInFlight>0{gate=make(chan struct{},config.MaxInFlight)}
 s.Server=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count:=s.total.Add(1)
  if gate!=nil {
   select {
   case gate<-struct{}{}:defer func(){<-gate}()
   default:
    s.rejected.Add(1)
    http.Error(w,"fixture concurrency limit",http.StatusServiceUnavailable)
    return
   }
  }
  active:=s.active.Add(1)
  for{
   old:=s.peak.Load()
   if old>=active||s.peak.CompareAndSwap(old,active){break}
  }
  defer s.active.Add(-1)
  if config.Delay>0{
   timer:=time.NewTimer(config.Delay)
   defer timer.Stop()
   select {case <-r.Context().Done():return;case <-timer.C:}
  }
  if config.FailEvery>0&&count%config.FailEvery==0{
   s.rejected.Add(1)
   http.Error(w,"fixture injected failure",http.StatusInternalServerError)
   return
  }
  s.success.Add(1)
  w.WriteHeader(http.StatusOK)
  _,_ = w.Write([]byte("fixture-ok"))
 }))
 return s
}
func (s *Server) Snapshot() Counts {
 return Counts{s.total.Load(),s.success.Load(),s.rejected.Load(),s.peak.Load()}
}
