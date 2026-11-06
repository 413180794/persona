package denohost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/egress"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/extension/sourcevalidate"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.ValidationRunCommand {
		os.Exit(sandbox.RunValidation(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == sourcevalidate.Command {
		os.Exit(sourcevalidate.Run(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == sandbox.RunCommand {
		os.Exit(sandbox.Run(os.Args[2:], os.Stderr))
	}
	os.Exit(m.Run())
}

func testEngine(t *testing.T) Config {
	t.Helper()
	c := Config{Path: os.Getenv("TEST_DENO_PATH"), SHA256: os.Getenv("TEST_DENO_SHA256"), Version: Version}
	if c.Path == "" {
		t.Skip("TEST_DENO_PATH and TEST_DENO_SHA256 required for actual engine tests")
	}
	if err := c.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPinnedEngineIntegrity(t *testing.T) {
	c := testEngine(t)
	c.SHA256 = strings.Repeat("0", 64)
	if err := c.Verify(context.Background()); err == nil {
		t.Fatal("accepted tampered engine")
	}
	c = testEngine(t)
	c.Version = "0.0.0"
	if err := c.Verify(context.Background()); err == nil {
		t.Fatal("accepted wrong runtime version")
	}
	root := t.TempDir()
	link := filepath.Join(root, "deno")
	if err := os.Symlink(c.Path, link); err != nil {
		t.Fatal(err)
	}
	c.Path = link
	if err := c.Verify(context.Background()); err == nil {
		t.Fatal("accepted symlink runtime")
	}
}
func TestTrustedAdapterIntegrity(t *testing.T) {
	root := t.TempDir()
	p, err := InstallAdapter(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("guest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallAdapter(root); err == nil {
		t.Fatal("accepted tampered adapter")
	}
}

// These tests use real namespaces and the actual engine, but deliberately do NOT
// claim kernel resource acceptance. Only TestDenoFullIsolationAcceptance does that.
func TestDenoEngineNamespacesOnly(t *testing.T) { runPair(t, false) }
func TestDenoFullIsolationAcceptance(t *testing.T) {
	self, _ := os.Executable()
	if err := sandbox.ProbeStrict(self); err != nil {
		t.Skipf("UNAVAILABLE: hostile full-resource acceptance blocked: %v", err)
	}
	runPair(t, true)
}

func runPair(t *testing.T, full bool, development ...bool) {
	dev := len(development) > 0 && development[0]
	c := testEngine(t)
	sup := sandbox.Probe()
	if sup.OS != "linux" || !sup.Available() {
		t.Skipf("actual namespace sandbox unavailable: %s", sup.Why)
	}
	self, _ := os.Executable()
	root := t.TempDir()
	adapter, err := InstallAdapter(filepath.Join(root, "adapter"))
	if err != nil {
		t.Fatal(err)
	}
	memoryStarted := make(chan struct{}, 2)
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wait":
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
		case "/head":
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
		case "/body-redirect":
			w.Header().Set("Location", "/after-body-redirect")
			w.WriteHeader(http.StatusSeeOther)
		case "/after-body-redirect":
			if r.Method != http.MethodGet || r.Header.Get("Content-Encoding") != "" || r.Header.Get("Content-Language") != "" || r.Header.Get("Content-Location") != "" || r.Header.Get("Content-Type") != "" {
				http.Error(w, "body headers survived redirect", http.StatusBadRequest)
				return
			}
			io.WriteString(w, "allowed")
		case "/redirect":
			w.Header().Set("Location", "http://denied.test/")
			w.WriteHeader(302)
		default:
			io.WriteString(w, "allowed")
		}
	}))
	defer server.Close()
	px := &egress.Proxy{Upstream: func(*url.URL) (*url.URL, error) { return nil, nil }, Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil }, Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}}
	if err := px.Start(); err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	oldStrikes := host.StrikesToKill
	host.StrikesToKill = 1
	defer func() { host.StrikesToKill = oldStrikes }()
	makeProc := func(id string) *host.Proc {
		bundle := filepath.Join(root, id+".mjs")
		src := fmt.Sprintf(`let init;
export default {id:%q,version:"1.0.0",init(v){init=v},namespaces:{probe:async(method,p)=>{
if(method==="inspect")return {config:init.config,secrets:init.secrets};
if(method==="cpu"){while(true){}}
if(method==="memory"){console.error("memory-probe-entered");const arrays=[];while(true){arrays.push(new Array(1000000).fill(1))}}
if(method==="external-memory"){console.error("memory-probe-entered");const arrays=[];while(true){const a=new Uint8Array(16777216);a.fill(1);arrays.push(a)}}
if(method==="fetch"){const r=await fetch(p.url);return {status:r.status,text:await r.text(),url:r.url}}
if(method==="raw-proxy"){const c=await Deno.connect({hostname:"127.0.0.1",port:3128});try{const data=new TextEncoder().encode(p.request);for(let at=0;at<data.length;)at+=await c.write(data.subarray(at));const reply=new Uint8Array(4096);const n=await c.read(reply);return {reply:new TextDecoder().decode(reply.subarray(0,n??0))}}finally{c.close()}}
if(method==="invalid-fetches"){for(let i=0;i<80;i++){try{await fetch("not a URL")}catch{}}const r=await fetch(p.url);return {status:r.status,text:await r.text()}}
if(method==="head"){const r=await fetch(p.url,{method:"HEAD"});return {status:r.status,length:r.headers.get("content-length"),empty:r.body===null,text:await r.text()}}
if(method==="body-redirect"){const r=await fetch(p.url,{method:"POST",body:"example",headers:{"content-encoding":"gzip","content-language":"en","content-location":"/entity","content-type":"text/plain"}});return {status:r.status,text:await r.text()}}
if(method==="cancel-stalled-body"){try{await fetch("http://allowed.test/",{method:"POST",body:new ReadableStream({start(c){c.enqueue(new Uint8Array(8388609))},cancel(){return new Promise(()=>{})}})});return {rejected:false}}catch{return {rejected:true}}}
if(method==="abort-body"){const controller=new AbortController();setTimeout(()=>controller.abort(),10);try{await fetch("http://allowed.test/",{method:"POST",body:new ReadableStream({pull(){}}),signal:controller.signal});return {aborted:false}}catch{return {aborted:true}}}
if(method==="forbidden"){const result={};for(const [key,f] of Object.entries({env:()=>Deno.env.get("HOME"),read:()=>Deno.readTextFileSync("/etc/passwd"),write:()=>Deno.writeTextFileSync("/tmp/guest","x"),run:()=>new Deno.Command("/bin/true").outputSync(),ffi:()=>Deno.dlopen("/lib/libc.so.6",{}),eval:()=>eval("1+1"),direct:()=>Deno.connect({hostname:"allowed.test",port:80}),worker:()=>new Worker(URL.createObjectURL(new Blob(["import('node:fs')"])),{type:"module"})})){try{await f();result[key]="allowed"}catch{result[key]="denied"}}return result}
return {};
}}};`, id)
		if err := os.WriteFile(bundle, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256([]byte(src))
		if _, err := extension.CheckSourceBundle(root, &extension.SourceRuntime{Bundle: id + ".mjs", SHA256: hex.EncodeToString(h[:])}); err != nil {
			if !full && errors.Is(err, sourcevalidate.ErrUnavailable) {
				t.Log("UNAVAILABLE: source-validator resources; only trusted-fixture engine namespace regressions run here")
			} else {
				t.Fatal(err)
			}
		}
		mounts, err := Mounts(c, adapter, bundle, self)
		if err != nil {
			t.Fatal(err)
		}
		run := filepath.Join(root, id)
		if err := os.Mkdir(run, 0o700); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(run, "proxy.sock")
		px.Register(id, egress.Rule{Hosts: []string{"allowed.test"}})
		if err := px.ListenRuntimeUnix(id, sock); err != nil {
			t.Fatal(err)
		}
		args := Args(BundlePath, AdapterPath, "http://127.0.0.1:3128")
		if !full {
			for i, a := range args {
				if strings.HasPrefix(a, "--v8-flags=") {
					args[i] += ",--max-old-space-size=64"
				}
			}
		}
		spec := host.Spec{ID: id, Command: c.Path, Args: args, Dir: root, CleanEnv: true, RPC: true, RPCBounds: rpc.Limits{FrameBytes: 8 << 20, ConcurrentRequests: 8, PendingCalls: 128}, Startup: true, CallTimeout: 300 * time.Millisecond, Logf: func(f string, a ...any) {
			line := fmt.Sprintf(f, a...)
			if strings.Contains(line, "memory-probe-entered") {
				select {
				case memoryStarted <- struct{}{}:
				default:
				}
			}
			t.Log(line)
		}, Env: []string{"DENO_DIR=/tmp/deno", "DENO_NO_UPDATE_CHECK=1"}, Init: func(context.Context) (extension.InitializeParams, error) {
			return extension.InitializeParams{Protocol: extension.Protocol, Config: json.RawMessage(fmt.Sprintf(`{"owner":%q}`, id)), Secrets: map[string]string{"key": id + "-secret"}}, nil
		}}
		spec.Wrap = func(_ string, args []string) (string, []string, []string, error) {
			returnWrap := sandbox.Plan{ID: id, Command: EnginePath, Args: args, Self: self, ProxySocket: sock, Strict: true, Scope: full, ReadOnly: mounts, MemoryMB: 128, CPUPercent: 20, Env: os.Environ()}
			if dev {
				returnWrap.Development = sandbox.DefaultDevelopmentLimits()
			}
			name, a, env := sandbox.Linux(sup.Bwrap, returnWrap)
			return name, a, env, nil
		}
		p := host.Start(context.Background(), spec)
		t.Cleanup(p.Stop)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := p.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := makeProc("a"), makeProc("b")
	call := func(p *host.Proc, method string, params any) (json.RawMessage, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var out json.RawMessage
		err := p.Call(ctx, "probe."+method, params, &out)
		return out, err
	}
	for id, p := range map[string]*host.Proc{"a": a, "b": b} {
		out, err := call(p, "inspect", nil)
		if err != nil || !strings.Contains(string(out), id+"-secret") || strings.Contains(string(out), map[string]string{"a": "b-secret", "b": "a-secret"}[id]) {
			t.Fatalf("cross-extension initialization: %s %v", out, err)
		}
	}
	out, err := call(b, "cancel-stalled-body", nil)
	if err != nil || !strings.Contains(string(out), `"rejected":true`) {
		t.Fatalf("guest-controlled cancellation blocked size rejection: %s %v", out, err)
	}
	out, err = call(b, "abort-body", nil)
	if err != nil || !strings.Contains(string(out), `"aborted":true`) {
		t.Fatalf("pending body did not abort: %s %v", out, err)
	}
	out, err = call(b, "forbidden", nil)
	if err != nil || strings.Contains(string(out), "allowed") {
		t.Fatalf("forbidden API: %s %v", out, err)
	}
	for _, tc := range []struct {
		url    string
		status int
	}{{"http://allowed.test/", 200}, {"http://denied.test/", 403}, {"http://allowed.test/redirect", 403}} {
		out, err := call(b, "fetch", map[string]string{"url": tc.url})
		if err != nil || !strings.Contains(string(out), fmt.Sprintf(`"status":%d`, tc.status)) {
			t.Fatalf("fetch %s: %s %v", tc.url, out, err)
		}
	}
	for _, method := range []string{"invalid-fetches", "body-redirect"} {
		path := "/"
		if method == "body-redirect" {
			path = "/body-redirect"
		}
		out, err := call(b, method, map[string]string{"url": "http://allowed.test" + path})
		if err != nil || !strings.Contains(string(out), `"status":200`) || !strings.Contains(string(out), `"text":"allowed"`) {
			t.Fatalf("%s: %s %v", method, out, err)
		}
	}
	out, err = call(b, "head", map[string]string{"url": "http://allowed.test/head"})
	if err != nil || !strings.Contains(string(out), `"length":"1000"`) || !strings.Contains(string(out), `"empty":true`) || !strings.Contains(string(out), `"text":""`) {
		t.Fatalf("HEAD semantics: %s %v", out, err)
	}
	// The permitted broker address must not expose legacy HTTP proxy or CONNECT
	// routes, even when the guest bypasses global fetch through native raw sockets.
	for _, request := range []string{
		"GET http://allowed.test/ HTTP/1.1\r\nHost: allowed.test\r\nConnection: close\r\n\r\n",
		"CONNECT allowed.test:443 HTTP/1.1\r\nHost: allowed.test:443\r\n\r\n",
	} {
		out, err := call(b, "raw-proxy", map[string]string{"request": request})
		if err != nil || !strings.Contains(string(out), "403 Forbidden") {
			t.Fatalf("raw broker route escaped bounded fetch: %s %v", out, err)
		}
	}
	// Infinite CPU work must time out and be terminated without blocking B.
	if _, err := call(a, "cpu", nil); err == nil {
		t.Fatal("runaway CPU call returned")
	}
	if out, err := call(b, "inspect", nil); err != nil || !strings.Contains(string(out), "b-secret") {
		t.Fatalf("B affected by A CPU: %s %v", out, err)
	}
	// Wait for a genuinely new initialized process, not an error from A's restart window.
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	sawDown := false
	for {
		ch := a.Changes()
		ready := a.Ready()
		if !ready {
			sawDown = true
		}
		if sawDown && ready {
			break
		}
		select {
		case <-ch:
		case <-readyCtx.Done():
			readyCancel()
			t.Fatal("A did not restart for memory probe")
		}
	}
	readyCancel()
	// V8 heap OOM here is ENGINE evidence only; full mode tests total memory, including ArrayBuffers.
	method := "memory"
	if full {
		method = "external-memory"
	}
	if _, err := call(a, method, nil); err == nil {
		t.Fatal("runaway memory call returned")
	}
	select {
	case <-memoryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("memory fixture never executed")
	}
	if _, err := call(b, "inspect", nil); err != nil {
		t.Fatal("B affected by A memory", err)
	}
	if px.Stats("b").Allowed < 2 || px.Stats("b").Denied < 2 {
		t.Fatal("fetch bypassed registered egress", px.Stats("b"))
	}
	pending := make(chan error, 1)
	go func() {
		_, err := call(b, "fetch", map[string]string{"url": "http://allowed.test/wait"})
		pending <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("pending I/O not started")
	}
	px.Unregister("b")
	select {
	case <-pending:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not cancel pending fetch")
	}
	t.Logf("actual engine + independent namespace tests passed; kernel-resource acceptance=%v", full)
}
