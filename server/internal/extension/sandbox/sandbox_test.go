package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension/egress"
)

// TestMain: this test binary is also the sandbox's first process (RunCommand) and the program it runs (probe).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case RunCommand:
			os.Exit(Run(os.Args[2:], os.Stderr))
		case "probe":
			os.Exit(probeInside(os.Args[2:]))
		}
	}
	os.Exit(m.Run())
}

// probeInside tries what a sandboxed program may and may not do, and prints a line for each.
func probeInside(args []string) int {
	data, home := args[0], args[1]
	say := func(what string, err error) {
		if err != nil {
			fmt.Printf("%s: no\n", what)
		} else {
			fmt.Printf("%s: yes\n", what)
		}
	}
	say("write data", os.WriteFile(filepath.Join(data, "x"), []byte("x"), 0o600))
	say("write home", os.WriteFile(filepath.Join(home, ".persona-sandbox-probe"), []byte("x"), 0o600))
	say("write tmp", os.WriteFile(filepath.Join(os.TempDir(), "x"), []byte("x"), 0o600))
	_, err := os.ReadDir(home)
	say("read home", err)
	c, err := net.DialTimeout("tcp", "192.0.2.1:80", 2*time.Second)
	if c != nil {
		c.Close()
	}
	say("dial out", err)
	for _, host := range []string{"allowed.test", "denied.test"} {
		resp, err := http.Get("http://" + host + "/")
		if err == nil && resp.StatusCode != 200 {
			err = fmt.Errorf("%d", resp.StatusCode)
		}
		if resp != nil {
			resp.Body.Close()
		}
		say("get "+host, err)
	}
	return 0
}

func TestLinuxCommandLine(t *testing.T) {
	plan := Plan{ID: "notes", Command: "/x/pkg/bin/notes", Args: []string{"serve"}, Pkg: "/x/pkg", Data: "/x/data", Run: "/x/run",
		ProxySocket: "/x/run/proxy.sock", Self: "/usr/local/bin/personad", MemoryMB: 512, CPUPercent: 50,
		Env: []string{"XDG_RUNTIME_DIR=/run/user/1", "SECRET=1"}}
	name, args, env := Linux("/usr/bin/bwrap", plan)
	if name != "/usr/bin/bwrap" || slices.Contains(args, "systemd-run") {
		t.Fatalf("%s %v", name, args)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"--unshare-net", "--die-with-parent", "--ro-bind /x/pkg /x/pkg", "--bind /x/data /x/data",
		"--bind /x/run /x/run", "--ro-bind /usr/local/bin/personad " + SelfPath, "--setenv HOME /x/data",
		"-- " + SelfPath + " " + RunCommand + " --proxy-socket /x/run/proxy.sock --listen 127.0.0.1:3128 -- /x/pkg/bin/notes serve"} {
		if !strings.Contains(line, want) {
			t.Errorf("no %q in %s", want, line)
		}
	}
	if strings.Contains(line, os.Getenv("HOME")+" ") && os.Getenv("HOME") != "" {
		t.Errorf("the home directory is bound: %s", line)
	}
	if !slices.Contains(env, "HTTPS_PROXY=http://127.0.0.1:3128") || !slices.Contains(env, "NO_PROXY=") {
		t.Errorf("env %v", env)
	}
	plan.Scope = true
	name, args, env = Linux("/usr/bin/bwrap", plan)
	line = strings.Join(args, " ")
	if name != "systemd-run" || !strings.Contains(line, "--user --scope") || !strings.Contains(line, "MemoryMax=512M") ||
		!strings.Contains(line, "CPUQuota=50%") || !strings.Contains(line, "-- /usr/bin/bwrap --die-with-parent") {
		t.Fatalf("%s %s", name, line)
	}
	if !slices.Contains(env, "XDG_RUNTIME_DIR=/run/user/1") || slices.Contains(env, "SECRET=1") {
		t.Errorf("env %v", env)
	}
}

func TestDarwinProfile(t *testing.T) {
	data := t.TempDir()
	plan := Plan{Command: "/x/pkg/bin/notes", Pkg: "/x/pkg", Data: data, Run: "/x/run", ProxyURL: "http://ext:c@127.0.0.1:40001"}
	name, args, env := Darwin(plan)
	if name != "/usr/bin/sandbox-exec" || args[0] != "-p" || args[2] != "/x/pkg/bin/notes" {
		t.Fatalf("%s %v", name, args)
	}
	for _, want := range []string{"(deny network-outbound)", `(remote ip "localhost:40001")`, "(deny file-write*)", `(subpath "/x/pkg")`} {
		if !strings.Contains(args[1], want) {
			t.Errorf("no %s in\n%s", want, args[1])
		}
	}
	if !slices.Contains(env, "HTTPS_PROXY=http://ext:c@127.0.0.1:40001") || !slices.Contains(env, "HOME="+data) {
		t.Errorf("env %v", env)
	}
}

// TestLinuxSandboxForReal runs this binary in bubblewrap with the egress proxy: it writes its data, not the home
// directory, cannot dial out, and reaches through the proxy only the host it may.
func TestLinuxSandboxForReal(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	sup := Probe()
	if sup.Bwrap == "" {
		t.Skip("no sandbox here: " + sup.Why)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	target := strings.TrimPrefix(srv.URL, "http://")
	px := &egress.Proxy{HubAddr: target, Upstream: func(*url.URL) (*url.URL, error) { return nil, nil },
		Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.0.2.1")}, nil },
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, target)
		}}
	if err := px.Start(); err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	px.Register("probe", egress.Rule{Hosts: []string{"allowed.test"}})
	self, _ := os.Executable()
	run, data := t.TempDir(), t.TempDir()
	sock := filepath.Join(run, "proxy.sock")
	if err := px.ListenUnix("probe", sock); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	plan := Plan{ID: "probe", Command: self, Args: []string{"probe", data, home}, Pkg: filepath.Dir(self), Data: data, Run: run,
		ProxySocket: sock, Self: self}
	name, args, env := Linux(sup.Bwrap, plan)
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"write data: yes", "write home: no", "write tmp: yes", "read home: no", "dial out: no", "get allowed.test: yes", "get denied.test: no"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in\n%s", want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(data, "x")); err != nil {
		t.Errorf("its data: %v", err)
	}
	if st := px.Stats("probe"); st.Allowed != 1 || st.Denied != 1 {
		t.Errorf("proxy counts %+v", st)
	}
}
