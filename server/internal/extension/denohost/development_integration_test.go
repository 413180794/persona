//go:build persona_dev_resources && linux

package denohost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/egress"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestDenoDevelopmentFallbackPair(t *testing.T) {
	self, _ := os.Executable()
	t.Setenv(sandbox.DevelopmentResourceEnv, "")
	if dev, err := sandbox.SourceDevelopmentResources(self); dev {
		t.Fatal("development fallback selected without manager opt-in")
	} else if err == nil {
		t.Skip("independent kernel resources available; fallback-only fixture is not applicable")
	}
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	if dev, err := sandbox.SourceDevelopmentResources(self); err != nil || !dev {
		t.Fatalf("development selection: %v %v", dev, err)
	}
	runPair(t, false, true)
}

// This suite does not confuse caller RPC timeouts with CPU/heap/watchdog kills.
// Calls last longer than child budgets; B is a separate real Deno process.
func TestDenoDevelopmentEnforcedBudgets(t *testing.T) {
	c := testEngine(t)
	self, _ := os.Executable()
	t.Setenv(sandbox.DevelopmentResourceEnv, "1")
	if dev, err := sandbox.SourceDevelopmentResources(self); err != nil {
		t.Fatalf("fallback unavailable: %v", err)
	} else if !dev {
		t.Skip("independent kernel resources available; fallback-only fixture is not applicable")
	}
	root := t.TempDir()
	sockets, err := os.MkdirTemp("", "dr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	adapter, err := InstallAdapter(filepath.Join(root, "adapter"))
	if err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}, 10), make(chan struct{}, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wait" {
			started <- struct{}{}
			<-r.Context().Done()
			canceled <- struct{}{}
			return
		}
		io.WriteString(w, "ok")
	}))
	defer server.Close()
	px := &egress.Proxy{Upstream: func(*url.URL) (*url.URL, error) { return nil, nil }, Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil }, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, n, strings.TrimPrefix(server.URL, "http://"))
	}}
	if err := px.Start(); err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	var mu sync.Mutex
	logs := ""
	logf := func(f string, a ...any) {
		line := fmt.Sprintf(f, a...)
		mu.Lock()
		logs += line + "\n"
		mu.Unlock()
		t.Log(line)
	}
	readLogs := func() string { mu.Lock(); defer mu.Unlock(); return logs }
	start := func(id string, limits *sandbox.DevelopmentLimits) *host.Proc {
		bundle := filepath.Join(root, id+".mjs")
		source := `export default {id:"example",version:"1.0.0",namespaces:{probe:async(m)=>{
if(m==="cpu"){while(true){}}
if(m==="heap"){console.error("budget_heap_entered");const a=[];while(true)a.push(new Array(1000000).fill(1))}
if(m==="rss"){console.error("budget_rss_entered");const a=[];while(true){const b=new Uint8Array(16777216);b.fill(1);a.push(b)}}
if(m==="wall")return new Promise(()=>{});
if(m==="incomplete-rpc"){console.error("budget_incomplete_rpc_entered");await Deno.stdout.write(new TextEncoder().encode('{"jsonrpc":"2.0"'));return new Promise(()=>{})}
if(m==="incomplete-broker"){console.error("budget_incomplete_broker_entered");const c=await Deno.connect({hostname:"127.0.0.1",port:3128});await c.write(new TextEncoder().encode('POST /fetch HTTP/1.1\r\nHost: broker\r\nContent-Length: 99999\r\n\r\n{'));await c.read(new Uint8Array(4096));return new Promise(()=>{})}
if(m==="io")return fetch("http://allowed.test/wait");
return {alive:true};
}}};`
		if err := os.WriteFile(bundle, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(source))
		if _, err := extension.CheckSourceBundleContext(context.Background(), root, &extension.SourceRuntime{Bundle: id + ".mjs", SHA256: hex.EncodeToString(hash[:])}); err != nil {
			t.Fatal("real bounded validation must precede the hostile Deno fixture", err)
		}
		mounts, err := Mounts(c, adapter, bundle, self)
		if err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(sockets, id+".sock")
		px.Register(id, egress.Rule{Hosts: []string{"allowed.test"}})
		if err := px.ListenRuntimeUnix(id, sock); err != nil {
			t.Fatal(err)
		}
		spec := host.Spec{ID: id, Command: c.Path, Args: DevelopmentArgs(Args(BundlePath, AdapterPath, "http://127.0.0.1:3128")), Dir: root, CleanEnv: true, RPC: true, RPCBounds: rpc.Limits{FrameBytes: 8 << 20, ConcurrentRequests: 8, PendingCalls: 128}, Startup: true, CallTimeout: 10 * time.Second, Env: []string{"DENO_DIR=/tmp/deno", "DENO_NO_UPDATE_CHECK=1"}, Logf: logf}
		spec.Wrap = func(_ string, a []string) (string, []string, []string, error) {
			n, args, e := sandbox.Linux(sandbox.Probe().Bwrap, sandbox.Plan{ID: id, Command: EnginePath, Args: a, Self: self, ProxySocket: sock, Strict: true, ReadOnly: mounts, Development: limits})
			return n, args, e, nil
		}
		spec.Init = func(context.Context) (extension.InitializeParams, error) {
			return extension.InitializeParams{Protocol: extension.Protocol}, nil
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
	b := start("neighbor", sandbox.DefaultDevelopmentLimits())
	for _, tc := range []struct {
		method, marker string
		cpu            int
		wall           time.Duration
	}{
		{"cpu", "signal: killed", 1, 8 * time.Second}, {"heap", "Reached heap limit", 30, 8 * time.Second}, {"rss", "terminated=sampled_rss", 30, 8 * time.Second}, {"wall", "terminated=wall_clock", 30, time.Second},
		{"incomplete-rpc", "budget_incomplete_rpc_entered", 30, time.Second},
		{"incomplete-broker", "budget_incomplete_broker_entered", 30, time.Second},
	} {
		t.Run(tc.method, func(t *testing.T) {
			a := start(tc.method, &sandbox.DevelopmentLimits{CPUSeconds: tc.cpu, RSSMB: 256, Wall: tc.wall})
			pending := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				pending <- a.Call(ctx, "probe.io", nil, nil)
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("I/O never started")
			}
			before := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			result := make(chan error, 1)
			go func() { result <- a.Call(ctx, "probe."+tc.method, nil, nil) }()
			tick := time.NewTicker(50 * time.Millisecond)
			defer tick.Stop()
			var err error
		wait:
			for {
				select {
				case err = <-result:
					break wait
				case <-tick.C:
					ping, stop := context.WithTimeout(context.Background(), time.Second)
					var alive json.RawMessage
					err := b.Call(ping, "probe.alive", nil, &alive)
					stop()
					if err != nil || !strings.Contains(string(alive), "true") {
						cancel()
						t.Fatal("neighbor blocked while A was hostile", err)
					}
				}
			}
			cancel()
			if err == nil || time.Since(before) > 6*time.Second {
				t.Fatalf("child budget did not interrupt before caller timeout: %v %s", err, time.Since(before))
			}
			select {
			case err := <-pending:
				if err == nil {
					t.Fatal("pending RPC succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("pending RPC leaked")
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("pending egress I/O leaked after child death")
			}
			deadline := time.After(2 * time.Second)
			for !strings.Contains(readLogs(), tc.marker) {
				select {
				case <-deadline:
					t.Fatalf("missing mechanism %q in %s", tc.marker, readLogs())
				case <-time.After(10 * time.Millisecond):
				}
			}
			var alive json.RawMessage
			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := b.Call(ctx, "probe.alive", nil, &alive); err != nil || !strings.Contains(string(alive), "true") {
				t.Fatal("neighbor affected", err)
			}
			t.Logf("mechanism=%s child_duration=%s neighbor_responsive=true pending_rpc_and_io_clean=true", tc.marker, time.Since(before))
			a.Stop()
		})
	}
}
