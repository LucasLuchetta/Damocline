package scheduler
import (
 "context"
 "testing"
 "time"
)
func TestDueCountReconcilesCoalescedWakeups(t *testing.T){
 period,duration:=50*time.Millisecond,time.Second
 cases:=[]struct{time time.Duration;want int64}{
  {0,0},{24*time.Millisecond,0},{25*time.Millisecond,1},
  {74*time.Millisecond,1},{75*time.Millisecond,2},
  {510*time.Millisecond,10},{975*time.Millisecond,20},
  {time.Second,20},{2*time.Second,20},
 }
 for _,x:=range cases{
  if got:=DueCount(x.time,period,duration);got!=x.want{
   t.Fatalf("at %v planned %d; want %d",x.time,got,x.want)
  }
 }
}
func TestDriveEveryArrivalHasAnOutcome(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),320*time.Millisecond)
 defer cancel()
 s:=Drive(ctx,250*time.Millisecond,20,func()bool{return false})
 if !s.Consistent()||s.Planned!=5||s.Dropped()!=5||s.Admitted!=0{
  t.Fatalf("scheduled events vanished: %+v",s)
 }
}
func TestDriveAdmitsAllOnAvailableGenerator(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),320*time.Millisecond)
 defer cancel()
 s:=Drive(ctx,250*time.Millisecond,20,func()bool{return true})
 if !s.Consistent()||s.Planned!=5||s.Admitted!=5||s.Dropped()!=0{
  t.Fatalf("unexpected generator underdelivery: %+v",s)
 }
}
func TestDriveAbortBeforeStart(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 s:=Drive(ctx,time.Second,20,func()bool{return true})
 if s.Planned!=0||!s.Consistent(){t.Fatalf("unexpected traffic: %+v",s)}
}
