package devices

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/tunnel"
)

// The whole way with real personad binaries over Tailscale's public relays: a machine the hub cannot ssh to is set up
// with `personad node tailcat init`, its resident node serves the hub over tailcat, and "Redeploy" sends it the hub's
// build over tailcat too. Needs the network and two builds of personad:
//
//	go build -ldflags "-X main.version=v1" -o /tmp/personad-v1 ./cmd/personad
//	go build -ldflags "-X main.version=v2" -o /tmp/personad-v2 ./cmd/personad
//	PERSONA_TEST_TAILCAT=1 PERSONA_TEST_V1=/tmp/personad-v1 PERSONA_TEST_V2=/tmp/personad-v2 go test ./internal/devices -run TailcatEndToEnd
func TestTailcatEndToEnd(t *testing.T) {
	v1, v2 := os.Getenv("PERSONA_TEST_V1"), os.Getenv("PERSONA_TEST_V2")
	if os.Getenv("PERSONA_TEST_TAILCAT") != "1" || v1 == "" || v2 == "" {
		t.Skip("needs the network and two personad builds: see the comment")
	}
	short := func(prefix string) string { // unix socket paths must stay short (node-attach.sock)
		d, err := os.MkdirTemp("/tmp", prefix)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		return d
	}
	hubHome, nodeHome := short("jh"), short("jn")
	t.Setenv("HOME", hubHome)        // the hub's key file and node builds live under its home
	t.Setenv("USERPROFILE", hubHome) // os.UserHomeDir on Windows
	hk, err := tunnel.LoadOrCreateHubKey(tunnel.HubKeyPath())
	if err != nil {
		t.Fatal(err)
	}

	// The node's machine: personad v1 installed where deploy puts it, set up for tailcat with this hub's key.
	nodeBin := filepath.Join(nodeHome, ".persona", "bin", "personad")
	copyFile(t, v1, nodeBin)
	nodeEnv := append(os.Environ(), "HOME="+nodeHome, "XDG_CONFIG_HOME="+filepath.Join(nodeHome, ".config"))
	initCmd := exec.Command(nodeBin, "node", "tailcat", "init", "--hub", hk.Public())
	initCmd.Env = nodeEnv
	var stderr bytes.Buffer
	initCmd.Stderr = &stderr
	out, err := initCmd.Output()
	if err != nil {
		t.Fatalf("tailcat init: %v %s", err, stderr.String())
	}
	addr := strings.TrimSpace(string(out))
	if !strings.HasPrefix(addr, "tc") {
		t.Fatalf("init printed %q", addr)
	}
	addrCmd := exec.Command(nodeBin, "node", "tailcat", "addr")
	addrCmd.Env = nodeEnv
	if again, err := addrCmd.Output(); err != nil || strings.TrimSpace(string(again)) != addr {
		t.Fatalf("tailcat addr: %q %v, want what init printed", again, err)
	}

	// The resident node, restarted when it exits (what launchd / systemd do).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exited := make(chan struct{}, 4)
	resident := func() {
		cmd := exec.CommandContext(ctx, nodeBin, "node", "--attach")
		cmd.Env = nodeEnv
		cmd.Stderr = logTo(t)
		if err := cmd.Start(); err != nil {
			t.Error(err)
			return
		}
		go func() { _ = cmd.Wait(); exited <- struct{}{} }()
	}
	resident()
	go func() {
		for range exited {
			if ctx.Err() != nil {
				return
			}
			t.Log("resident node exited: starting it again")
			time.Sleep(time.Second)
			resident()
		}
	}()

	// The hub: ssh to that machine does not work (no such host); tailcat does.
	h := newTestDevices(t)
	h.cfg.HubVersion = "v2"
	h.logf = log.New(logTo(t), "", 0).Printf
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "persona-e2e.invalid", TailcatAddr: addr})
	runDone := make(chan struct{})
	go func() { defer close(runDone); m.run(ctx) }()
	t.Cleanup(func() { cancel(); <-runDone })
	defer h.closeTailcat()
	snap := waitMachine(t, m, 3*time.Minute, func(s proto.Machine) bool { return s.Online })
	if snap.Transport != "tailcat" || snap.NodeVersion != "v1" || !snap.NodeMismatch {
		t.Fatalf("connected: transport %q node %q mismatch %v, want tailcat, v1, a mismatch", snap.Transport, snap.NodeVersion, snap.NodeMismatch)
	}
	if snap.Hostname == "" {
		t.Fatalf("no node info over tailcat: %+v", snap)
	}
	t.Logf("online over tailcat (%s), path %q", snap.Hostname, snap.TailcatPath)
	var list struct {
		Sessions []proto.Session `json:"sessions"`
	}
	cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
	if err := m.call(cctx, "sessions.list", map[string]any{}, &list); err != nil {
		t.Fatalf("a call over tailcat: %v", err)
	}
	ccancel()

	// "Redeploy" over tailcat: the hub's build v2 replaces v1, the node restarts on it and comes back.
	copyFile(t, v2, filepath.Join(nodeBinaryDir(), "personad-"+runtime.GOOS+"-"+runtime.GOARCH))
	if err := m.startDeploy(); err != nil {
		t.Fatal(err)
	}
	snap = waitMachine(t, m, 4*time.Minute, func(s proto.Machine) bool { return !s.Deploying && s.Online && s.NodeVersion == "v2" })
	if snap.DeployError != "" || snap.NodeMismatch || snap.Transport != "tailcat" {
		t.Fatalf("after deploy: %+v", snap)
	}
	if b, _ := exec.Command(nodeBin, "version").Output(); strings.TrimSpace(string(b)) != "v2" {
		t.Fatalf("installed binary says %q, want v2", b)
	}
}

func waitMachine(t *testing.T, m *Machine, within time.Duration, ok func(proto.Machine) bool) proto.Machine {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if s := m.snapshot(); ok(s) {
			return s
		}
		time.Sleep(500 * time.Millisecond)
	}
	s := m.snapshot()
	t.Fatalf("timed out: %+v", s)
	return s
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// logTo is a writer into the test log that goes quiet once the test is over (the node's stderr may still be
// draining then, and logging after the end panics).
func logTo(t *testing.T) io.Writer {
	w := &testLog{t: t}
	t.Cleanup(func() { w.mu.Lock(); w.done = true; w.mu.Unlock() })
	return w
}

type testLog struct {
	t    *testing.T
	mu   sync.Mutex
	done bool
}

func (w *testLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.done {
		w.t.Log(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}
