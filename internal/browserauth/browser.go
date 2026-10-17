// Package browserauth borrows a temporary, manually authenticated Chrome/Edge
// session to bootstrap read-only HTTP load tests. No password is captured.
package browserauth

import (
 "bufio"
 "context"
 "crypto/rand"
 "crypto/sha1"
 "encoding/base64"
 "encoding/binary"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strconv"
 "strings"
 "time"
)

type Result struct {
 CookieHeader string
 Paths []string
}
type browserCookie struct {
 Name string `json:"name"`
 Value string `json:"value"`
 Domain string `json:"domain"`
 Path string `json:"path"`
 Secure bool `json:"secure"`
 Expires float64 `json:"expires"`
}
func findBrowser()(string,error) {
 if custom:=strings.TrimSpace(os.Getenv("DAMOCLINE_BROWSER"));custom!="" {
  if _,err:=os.Stat(custom);err!=nil{return "",err}
  return custom,nil
 }
 var paths []string
 switch runtime.GOOS {
 case "windows":
  for _,name:=range []string{"PROGRAMFILES","PROGRAMFILES(X86)","LOCALAPPDATA"} {
   root:=os.Getenv(name);if root==""{continue}
   paths=append(paths,filepath.Join(root,"Microsoft","Edge","Application","msedge.exe"),filepath.Join(root,"Google","Chrome","Application","chrome.exe"))
  }
 case "darwin":
  paths=[]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome","/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
 default:
  for _,name:=range []string{"microsoft-edge","microsoft-edge-stable","google-chrome","google-chrome-stable","chromium","chromium-browser"}{
   if path,err:=exec.LookPath(name);err==nil{return path,nil}
  }
 }
 for _,path:=range paths {if info,err:=os.Stat(path);err==nil&&!info.IsDir(){return path,nil}}
 return "",errors.New("Chrome/Edge not found; install one or set DAMOCLINE_BROWSER")
}

// Capture creates a NEW isolated browser profile, waits for the human to sign
// in, and reads only same-site root-path cookies into process memory.
func Capture(origin,loginPath string,waitForUser func()error)(Result,error){
 browser,err:=findBrowser();if err!=nil{return Result{},err}
 base,err:=url.Parse(origin)
 if err!=nil||base.Host==""||base.User!=nil||base.Path!=""||(base.Scheme!="https"&&base.Scheme!="http"){return Result{},errors.New("invalid target origin")}
 if loginPath==""{loginPath="/login"}
 target,err:=url.Parse(loginPath)
 if err!=nil||!strings.HasPrefix(loginPath,"/")||strings.HasPrefix(loginPath,"//")||target.IsAbs()||target.Host!=""||target.RawQuery!=""||target.Fragment!=""||strings.Contains(loginPath,"\\"){return Result{},errors.New("invalid login page path")}
 dir,err:=os.MkdirTemp("","damocline-browser-");if err!=nil{return Result{},err}
 defer os.RemoveAll(dir)
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 cmd:=exec.CommandContext(ctx,browser,"--remote-debugging-port=0","--remote-debugging-address=127.0.0.1","--user-data-dir="+dir,"--no-first-run","--no-default-browser-check","--disable-sync","--disable-background-mode",base.ResolveReference(target).String())
 if err=cmd.Start();err!=nil{return Result{},err}
 defer func(){_ = cmd.Process.Kill();_ = cmd.Wait()}()
 port,wsURL,err:=waitPort(ctx,filepath.Join(dir,"DevToolsActivePort"));if err!=nil{return Result{},err}
 if err=waitForUser();err!=nil{return Result{},err}
 result,err:=fromDevTools(ctx,port,wsURL,base)
 if conn,e:=openSocket(ctx,wsURL);e==nil{_,_=conn.command("Browser.close");_ = conn.conn.Close()}
 return result,err
}
func waitPort(ctx context.Context,path string)(string,string,error){
 deadline:=time.NewTimer(12*time.Second);defer deadline.Stop()
 tick:=time.NewTicker(120*time.Millisecond);defer tick.Stop()
 for {
  if raw,err:=os.ReadFile(path);err==nil {
   lines:=strings.Split(strings.TrimSpace(string(raw)),"\n")
   if len(lines)>=2 {n,e:=strconv.Atoi(strings.TrimSpace(lines[0]));if e==nil&&n>0&&n<65536&&strings.HasPrefix(strings.TrimSpace(lines[1]),"/devtools/browser/"){port:=fmt.Sprintf("127.0.0.1:%d",n);return port,"ws://"+port+strings.TrimSpace(lines[1]),nil}}
  }
  select {case <-ctx.Done():return "","",ctx.Err();case <-deadline.C:return "","",errors.New("browser remote-debugging endpoint not ready");case <-tick.C:}
 }
}
func fromDevTools(ctx context.Context,endpoint,wsURL string,base *url.URL)(Result,error){
 if !strings.HasPrefix(endpoint,"127.0.0.1:"){return Result{},errors.New("browser endpoint is not loopback")}
 socket,err:=openSocket(ctx,wsURL);if err!=nil{return Result{},err};defer socket.conn.Close()
 raw,err:=socket.command("Storage.getCookies");if err!=nil{return Result{},err}
 var response struct{Cookies []browserCookie `json:"cookies"`}
 if err=json.Unmarshal(raw,&response);err!=nil{return Result{},err}
 header,err:=scopeCookies(response.Cookies,base)
 if err!=nil{return Result{},err}
 if header==""{return Result{},errors.New("no session cookies for this application; finish signing in and retry")}
 return Result{CookieHeader:header,Paths:readHistory(ctx,endpoint,base)},nil
}
func scopeCookies(cs []browserCookie,base *url.URL)(string,error){
 host:=strings.ToLower(base.Hostname());var kept []string
 for _,c:=range cs{
  domain:=strings.TrimPrefix(strings.ToLower(c.Domain),".")
  if domain==""||(host!=domain&&!strings.HasSuffix(host,"."+domain))||c.Path!="/"{continue}
  if c.Secure&&base.Scheme!="https"{continue}
  if c.Expires>0&&c.Expires<float64(time.Now().Unix()){continue}
  if strings.ContainsAny(c.Name+c.Value,"\r\n"){return "",errors.New("unsafe browser cookie")}
  if value:=(&http.Cookie{Name:c.Name,Value:c.Value}).String();value!=""{kept=append(kept,value)}
 }
 header:=strings.Join(kept,"; ")
 if len(header)>8192{return "",errors.New("session cookie header exceeds 8 KiB")}
 return header,nil
}
// History is only a suggestion: navigations may have used non-GET methods.
// The wizard always asks before adding ANY URL as a read-only GET workload.
func readHistory(ctx context.Context,endpoint string,base *url.URL)[]string{
 client:=http.Client{Timeout:3*time.Second}
 resp,err:=client.Get("http://"+endpoint+"/json/list");if err!=nil{return nil};defer resp.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(resp.Body,1<<20));if err!=nil||resp.StatusCode!=200{return nil}
 var targets []struct{Type string `json:"type"`;WS string `json:"webSocketDebuggerUrl"`}
 if json.Unmarshal(data,&targets)!=nil{return nil}
 seen:=map[string]bool{};var paths []string
 for _,target:=range targets{
  if target.Type!="page"||!strings.HasPrefix(target.WS,"ws://"+endpoint+"/devtools/"){continue}
  ws,err:=openSocket(ctx,target.WS);if err!=nil{continue}
  raw,err:=ws.command("Page.getNavigationHistory");ws.conn.Close();if err!=nil{continue}
  var history struct{Entries []struct{URL string `json:"url"`} `json:"entries"`}
  if json.Unmarshal(raw,&history)!=nil{continue}
  for _,entry:=range history.Entries{
   u,e:=url.Parse(entry.URL);if e!=nil||u.Scheme!=base.Scheme||!strings.EqualFold(u.Host,base.Host)||u.RawQuery!=""||u.Fragment!=""{continue}
   path:=u.EscapedPath();if path==""{path="/"};lower:=strings.ToLower(path)
   if strings.Contains(lower,"login")||strings.Contains(lower,"logout")||strings.Contains(lower,"callback")||strings.Contains(lower,"oauth")||strings.Contains(lower,"auth/"){continue}
   if !seen[path]&&len(paths)<20{seen[path]=true;paths=append(paths,path)}
  }
 }
 return paths
}

// Minimal RFC6455 client. Used ONLY for local Chrome DevTools, not the load engine.
type socket struct{conn net.Conn;in *bufio.Reader;id int}
func openSocket(ctx context.Context,raw string)(*socket,error){
 u,err:=url.Parse(raw);if err!=nil||u.Scheme!="ws"||!strings.HasPrefix(u.Host,"127.0.0.1:")||!strings.HasPrefix(u.Path,"/devtools/"){return nil,errors.New("refusing nonlocal browser websocket")}
 conn,err:=(&net.Dialer{Timeout:4*time.Second}).DialContext(ctx,"tcp",u.Host);if err!=nil{return nil,err}
 good:=false;defer func(){if !good{conn.Close()}}()
 conn.SetDeadline(time.Now().Add(5*time.Second))
 seed:=make([]byte,16);if _,err=rand.Read(seed);err!=nil{return nil,err}
 key:=base64.StdEncoding.EncodeToString(seed)
 req:=fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",u.RequestURI(),u.Host,key)
 if _,err=io.WriteString(conn,req);err!=nil{return nil,err}
 reader:=bufio.NewReader(conn);status,err:=reader.ReadString('\n')
 if err!=nil||!strings.Contains(status," 101 "){return nil,errors.New("browser websocket upgrade denied")}
 var accept string;complete:=false
 for i:=0;i<64;i++{line,e:=reader.ReadString('\n');if e!=nil{return nil,e};if len(line)>8192{return nil,errors.New("websocket header too large")};if line=="\r\n"{complete=true;break};p:=strings.SplitN(line,":",2);if len(p)==2&&strings.EqualFold(strings.TrimSpace(p[0]),"sec-websocket-accept"){accept=strings.TrimSpace(p[1])}}
 hash:=sha1.Sum([]byte(key+"258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
 if !complete||accept!=base64.StdEncoding.EncodeToString(hash[:]){return nil,errors.New("invalid browser websocket handshake")}
 conn.SetDeadline(time.Time{});good=true
 return &socket{conn:conn,in:reader},nil
}
func(s *socket)send(op byte,b []byte)error{
 if len(b)>4<<20{return errors.New("oversized browser command")}
 var mask [4]byte;if _,err:=rand.Read(mask[:]);err!=nil{return err}
 h:=[]byte{0x80|op,0x80};n:=len(b)
 if n<126{h[1]|=byte(n)}else if n<=65535{h[1]|=126;h=binary.BigEndian.AppendUint16(h,uint16(n))}else{h[1]|=127;h=binary.BigEndian.AppendUint64(h,uint64(n))}
 packet:=make([]byte,0,len(h)+4+n);packet=append(packet,h...);packet=append(packet,mask[:]...)
 for i,x:=range b{packet=append(packet,x^mask[i%4])}
 _,err:=s.conn.Write(packet);return err
}
func(s *socket)read()([]byte,error){
 for{
  var h [2]byte;if _,e:=io.ReadFull(s.in,h[:]);e!=nil{return nil,e}
  if h[0]&0x80==0||h[1]&0x80!=0{return nil,errors.New("invalid websocket frame")}
  n:=uint64(h[1]&0x7f)
  if n==126{var b [2]byte;if _,e:=io.ReadFull(s.in,b[:]);e!=nil{return nil,e};n=uint64(binary.BigEndian.Uint16(b[:]))}else if n==127{var b [8]byte;if _,e:=io.ReadFull(s.in,b[:]);e!=nil{return nil,e};n=binary.BigEndian.Uint64(b[:])}
  if n>4<<20{return nil,errors.New("browser frame too large")}
  b:=make([]byte,int(n));if _,e:=io.ReadFull(s.in,b);e!=nil{return nil,e}
  switch h[0]&0x0f{case 0x9:if e:=s.send(0xA,b);e!=nil{return nil,e};continue;case 0x8:return nil,io.EOF;case 0x1:return b,nil;default:continue}
 }
}
func(s *socket)command(method string)(json.RawMessage,error){
 s.id++;msg,_:=json.Marshal(map[string]any{"id":s.id,"method":method})
 s.conn.SetDeadline(time.Now().Add(5*time.Second));defer s.conn.SetDeadline(time.Time{})
 if err:=s.send(0x1,msg);err!=nil{return nil,err}
 for i:=0;i<500;i++{
  b,err:=s.read();if err!=nil{return nil,err}
  var v struct{ID int `json:"id"`;Result json.RawMessage `json:"result"`;Error *struct{Message string `json:"message"`} `json:"error"`}
  if json.Unmarshal(b,&v)!=nil||v.ID!=s.id{continue}
  if v.Error!=nil{return nil,errors.New(v.Error.Message)}
  return v.Result,nil
 }
 return nil,errors.New("too many unsolicited browser events")
}
