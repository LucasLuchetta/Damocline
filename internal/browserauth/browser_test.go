package browserauth

import (
 "net/url"
 "strings"
 "testing"
 "time"
)
func TestCookieScope(t *testing.T) {
 u,_:=url.Parse("https://app.example.com")
 cookies:=[]browserCookie{
  {Name:"session",Value:"abc",Domain:".example.com",Path:"/",Secure:true},
  {Name:"thirdparty",Value:"secret",Domain:"idp.other.com",Path:"/"},
  {Name:"admin",Value:"secret",Domain:"app.example.com",Path:"/admin"},
  {Name:"expired",Value:"dead",Domain:"app.example.com",Path:"/",Expires:float64(time.Now().Add(-time.Hour).Unix())},
 }
 value,err:=scopeCookies(cookies,u)
 if err!=nil{t.Fatal(err)}
 if value!="session=abc"{t.Fatalf("unscoped session cookies: %q",value)}
}
func TestRejectCookieHeaderInjection(t *testing.T) {
 u,_:=url.Parse("https://app.example.com")
 _,err:=scopeCookies([]browserCookie{{Name:"session",Value:"a\r\nInjected: bad",Domain:"app.example.com",Path:"/"}},u)
 if err==nil{t.Fatal("accepted injected newline")}
}
func TestNoCrossOriginCookies(t *testing.T) {
 u,_:=url.Parse("https://app.example.com")
 value,err:=scopeCookies([]browserCookie{{Name:"s",Value:"x",Domain:"other.com",Path:"/"}},u)
 if err!=nil||strings.TrimSpace(value)!=""{t.Fatalf("unexpected cookie: %q / %v",value,err)}
}
