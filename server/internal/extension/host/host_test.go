package host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/rpc"
)

// TestMain doubles as the plugin: HOST_TEST_PLUGIN=<mode> runs the test binary as one.
func TestMain(m *testing.M) {
	switch os.Getenv("HOST_TEST_PLUGIN") {
	case "":
		RestartFirst, RanAWhile, StopGrace, PingEvery, PingTimeout, HealthFirst = 50*time.Millisecond, time.Hour, time.Second, time.Hour, time.Second, 50*time.Millisecond
		os.Exit(m.Run())
	case "rpc":
		_ = serve.Serve(context.Background(), os.Stdin, os.Stdout, demo())
	case "badproto":
		peer := rpc.New("p", os.Stdout)
		peer.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			return extension.InitializeResult{Protocol: 9, ID: "demo", Version: "1.0.0"}, nil
		}
		_ = peer.Serve(context.Background(), os.Stdin)
	case "quit": // a program that ends by itself as soon as it starts
		os.Exit(3)
	case "http":
		ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
		if err != nil {
			os.Exit(1)
		}
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	}
	os.Exit(0)
}

// demo is the test plugin: demo.echo, demo.env, demo.pid, demo.sleep, demo.exit, demo.ask (calls the hub back).
func demo() serve.Program {
	var init extension.InitializeParams
	var hub *serve.Hub
	return serve.Program{ID: "demo", Version: "1.0.0",
		Init: func(_ context.Context, p extension.InitializeParams, h *serve.Hub) (map[string]any, error) {
			init, hub = p, h
			return map[string]any{"connectors": []map[string]any{{"id": "demo"}}}, nil
		},
		Namespaces: map[string]serve.Handler{"demo": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "echo":
				return params, nil
			case "init":
				return init, nil
			case "env":
				return os.Getenv("DEMO_ENV"), nil
			case "leak":
				return os.Getenv("HOST_TEST_HUB_SECRET"), nil
			case "pid":
				return os.Getpid(), nil
			case "sleep":
				time.Sleep(10 * time.Second)
				return nil, nil
			case "exit":
				go func() { time.Sleep(10 * time.Millisecond); os.Exit(3) }()
				return nil, nil
			case "ask":
				var out string
				err := hub.Call(ctx, "demo.secret", map[string]any{}, &out)
				return out, err
			}
			return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: method}
		}},
	}
}

func spec(t *testing.T, mode string) Spec {
	self, _ := os.Executable()
	return Spec{ID: "demo", Name: "Demo", Command: self, RPC: true, Env: []string{"HOST_TEST_PLUGIN=" + mode, "DEMO_ENV=yes"},
		Init: func(context.Context) (extension.InitializeParams, error) {
			return extension.InitializeParams{Extension: extension.ExtensionInfo{ID: "demo", Version: "1.0.0"}, Config: json.RawMessage(`{"a":1}`),
				Secrets: map[string]string{"k": "v"}}, nil
		},
		Handle: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method == "demo.secret" {
				return "from the hub", nil
			}
			return nil, errors.New("no")
		},
		Logf: t.Logf}
}

func waitState(t *testing.T, p *Proc, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := p.Status(); s == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, e := p.Status()
	t.Fatalf("state %s (%s), want %s", s, e, want)
}

func pid(t *testing.T, p *Proc) int {
	t.Helper()
	var n int
	if err := p.Call(context.Background(), "demo.pid", struct{}{}, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func alive(pid int) bool { return osproc.Alive(pid) }

func TestStartedByCallsStopsWhenIdle(t *testing.T) {
	t.Setenv("HOST_TEST_HUB_SECRET", "the hub's own")
	s := spec(t, "rpc")
	s.OnCall, s.Idle, s.CleanEnv = true, 300*time.Millisecond, true
	p := Start(context.Background(), s)
	defer p.Stop()
	if st, _ := p.Status(); st != StateStopped {
		t.Fatal("nothing needs it yet:", st)
	}
	var got map[string]any
	if err := p.Call(context.Background(), "demo.echo", map[string]any{"x": 1}, &got); err != nil || got["x"] != float64(1) {
		t.Fatal(got, err)
	}
	var init extension.InitializeParams
	if err := p.Call(context.Background(), "demo.init", struct{}{}, &init); err != nil {
		t.Fatal(err)
	}
	if init.Protocol != extension.Protocol || string(init.Config) != `{"a":1}` || init.Secrets["k"] != "v" {
		t.Fatalf("%+v", init)
	}
	var env, asked string
	_ = p.Call(context.Background(), "demo.env", struct{}{}, &env)
	var leaked string
	_ = p.Call(context.Background(), "demo.leak", struct{}{}, &leaked)
	if leaked != "" {
		t.Fatal("a clean environment has none of the hub's own variables:", leaked)
	}
	_ = p.Call(context.Background(), "demo.ask", struct{}{}, &asked)
	if env != "yes" || asked != "from the hub" {
		t.Fatal(env, asked)
	}
	if res, ok := p.Result(); !ok || string(res.Contributes["connectors"]) != `[{"id":"demo"}]` {
		t.Fatal(res)
	}
	n := pid(t, p)
	waitState(t, p, StateStopped)
	time.Sleep(100 * time.Millisecond)
	if alive(n) {
		t.Fatal("the program is still there")
	}
	if pid(t, p) == n {
		t.Fatal("a new run")
	}
}

func TestHeldRunsWhileHeld(t *testing.T) {
	p := Start(context.Background(), spec(t, "rpc"))
	defer p.Stop()
	if err := p.Call(context.Background(), "demo.echo", struct{}{}, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatal("not started by calls:", err)
	}
	release := p.Hold()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	n := pid(t, p)
	release()
	waitState(t, p, StateStopped)
	time.Sleep(100 * time.Millisecond)
	if alive(n) {
		t.Fatal("released: it stops")
	}
}

func TestRestartsAfterExitAndTimeouts(t *testing.T) {
	s := spec(t, "rpc")
	s.Startup, s.CallTimeout = true, 200*time.Millisecond
	ups := 0
	var mu sync.Mutex
	s.OnUp = func() { mu.Lock(); ups++; mu.Unlock() }
	p := Start(context.Background(), s)
	defer p.Stop()
	if err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := pid(t, p)
	_ = p.Call(context.Background(), "demo.exit", struct{}{}, nil)
	waitState(t, p, StateError)
	waitState(t, p, StateRunning)
	second := pid(t, p)
	if second == first {
		t.Fatal("restarted")
	}
	for i := 0; i < StrikesToKill; i++ {
		err := p.Call(context.Background(), "demo.sleep", struct{}{}, nil)
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != extension.CodeTimeout {
			t.Fatal(err)
		}
	}
	waitState(t, p, StateError)
	waitState(t, p, StateRunning)
	if pid(t, p) == second {
		t.Fatal("restarted after the timeouts")
	}
	mu.Lock()
	defer mu.Unlock()
	if ups != 3 {
		t.Fatal("up three times:", ups)
	}
}

func TestRefusesWrongProtocolAndIdentity(t *testing.T) {
	s := spec(t, "badproto")
	s.Startup = true
	p := Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, "协议版本") {
		t.Fatal(why)
	}
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Init = func(context.Context) (extension.InitializeParams, error) {
		return extension.InitializeParams{Extension: extension.ExtensionInfo{ID: "other", Version: "1.0.0"}}, nil
	}
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); !strings.Contains(why, `"demo"`) {
		t.Fatal(why)
	}
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Accept = func(extension.InitializeResult) error { return errors.New("not what was installed") }
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	p.Stop()

	s = spec(t, "rpc")
	s.Startup = true
	s.Check = func(context.Context) error { return errors.New("another build") }
	p = Start(context.Background(), s)
	waitState(t, p, StateError)
	if _, why := p.Status(); why != "another build" {
		t.Fatal(why)
	}
	p.Stop()
}

func TestHTTPOnly(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := spec(t, "http")
	sock := filepath.Join(dir, "demo.sock")
	s.RPC, s.HTTP, s.Socket, s.Startup = false, true, sock, true
	s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+sock)
	_ = os.WriteFile(sock, nil, 0o600) // a stale one is removed before the start
	p := Start(context.Background(), s)
	waitState(t, p, StateRunning)
	if !p.Healthy(context.Background()) || !p.Ready() {
		t.Fatal("healthy")
	}
	if err := p.Call(context.Background(), "x.y", nil, nil); err == nil {
		t.Fatal("no protocol")
	}
	end, err := p.BeginHTTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	end()
	p.Stop()
	if st, _ := p.Status(); st != StateStopped {
		t.Fatal(st)
	}
}

// A program that ends by itself is an error and is started again, and Stop still returns (#2277). The run used to
// take the program's exit and then wait for it a second time while ending the run, so the host hung for good.
func TestAProgramThatQuitsIsStartedAgainAndStops(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst") // a unix socket path stays short
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	o := &observed{}
	s := spec(t, "quit")
	s.RPC, s.HTTP, s.Socket, s.Startup, s.Observe = false, true, filepath.Join(dir, "demo.sock"), true, o
	p := Start(context.Background(), s)
	defer func() { // on its own goroutine, so that a Stop that hangs fails this test instead of the whole run
		stopped := make(chan struct{})
		go func() { p.Stop(); close(stopped) }()
		select {
		case <-stopped:
			if st, _ := p.Status(); st != StateStopped {
				t.Errorf("state %s after Stop", st)
			}
		case <-time.After(10 * time.Second):
			t.Error("Stop did not return")
		}
	}()
	startedAgain := func() bool {
		_, runs, _ := o.snapshot()
		i := slices.Index(runs, "exit exit status 3")
		return i >= 0 && slices.Contains(runs[i:], RunStart)
	}
	for deadline := time.Now().Add(10 * time.Second); !startedAgain(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			_, runs, _ := o.snapshot()
			t.Fatalf("runs %q: the program's exit was not seen, or it was not started again", runs)
		}
	}
}

func TestElsewhere(t *testing.T) {
	dir, err := os.MkdirTemp("", "hst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "rpc.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _ = serve.Serve(context.Background(), c, c, demo()) }()
		}
	}()
	s := spec(t, "")
	s.Command, s.RPCSocket, s.OnCall = "", sock, true
	p := Start(context.Background(), s)
	defer p.Stop()
	var n int
	if err := p.Call(context.Background(), "demo.pid", struct{}{}, &n); err != nil || n != os.Getpid() {
		t.Fatal(n, err)
	}

	s = spec(t, "")
	s.Command, s.RPCSocket, s.Startup = "", filepath.Join(dir, "none.sock"), true
	q := Start(context.Background(), s)
	defer q.Stop()
	waitState(t, q, StateError)

	s = spec(t, "")
	s.Command, s.RPC, s.HTTP, s.Socket, s.Startup = "", false, true, filepath.Join(dir, "far.sock"), true
	far := Start(context.Background(), s)
	defer far.Stop()
	waitState(t, far, StateError)
	hl, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer hl.Close()
	go http.Serve(hl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	waitState(t, far, StateRunning)
}

// observed records what an Observer is told.
type observed struct {
	mu    sync.Mutex
	calls []string // method code
	runs  []string // event detail
	logs  []string // source: line
}

func (o *observed) Call(method string, took time.Duration, code string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, strings.TrimSpace(method+" "+code))
}

func (o *observed) Run(event, detail string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.runs = append(o.runs, strings.TrimSpace(event+" "+detail))
}

func (o *observed) Log(source, line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, source+": "+line)
}

func (o *observed) snapshot() (calls, runs, logs []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...), append([]string(nil), o.runs...), append([]string(nil), o.logs...)
}

// The host tells the Observer each call with its code, each run and why it ended, and the host's lines about it.
func TestObserver(t *testing.T) {
	o := &observed{}
	s := spec(t, "rpc")
	s.OnCall, s.Observe = true, o
	p := Start(context.Background(), s)
	defer p.Stop()
	if err := p.Call(context.Background(), "demo.echo", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	_ = p.Call(context.Background(), "demo.nothing", map[string]any{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = p.Call(ctx, "demo.echo", map[string]any{}, nil)
	_ = p.Call(context.Background(), "demo.exit", map[string]any{}, nil)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, runs, _ := o.snapshot(); len(runs) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	calls, runs, logs := o.snapshot()
	want := []string{"demo.echo", "demo.nothing " + extension.CodeUnknownMethod, "demo.echo " + CodeCanceled, "demo.exit"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q, want %q", calls, want)
	}
	if len(runs) < 2 || runs[0] != RunStart || !strings.HasPrefix(runs[1], RunExit+" ") {
		t.Fatalf("runs %q", runs)
	}
	found := false
	for _, l := range logs {
		if strings.HasPrefix(l, LogHub+": ") && strings.Contains(l, "again in") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host's line about the exit: %q", logs)
	}
}
