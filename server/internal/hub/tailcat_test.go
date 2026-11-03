package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// silentNode answers node.info and then nothing: a connection whose other end went away without a word (a
// machine asleep or off the network: tailcat's userspace TCP has no keepalive).
func silentNode(context.Context) (net.Conn, error) {
	hubSide, nodeSide := net.Pipe()
	p := rpc.New("n", nodeSide)
	p.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "node.info" {
			return map[string]any{}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() { _ = p.Serve(context.Background(), nodeSide); nodeSide.Close() }()
	return hubSide, nil
}

// tailcatNode is a node reached over tailcat: in this process, answering every call with pid -1 (the ssh one is a
// subprocess with a real pid), so a test can tell which way a call went.
func tailcatNode(dials *atomic.Int32) func(context.Context) (net.Conn, error) {
	return func(context.Context) (net.Conn, error) {
		dials.Add(1)
		hubSide, nodeSide := net.Pipe()
		p := rpc.New("n", nodeSide)
		p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
			return map[string]any{"pid": -1}, nil
		}
		go func() { _ = p.Serve(context.Background(), nodeSide); nodeSide.Close() }()
		return hubSide, nil
	}
}

// sshDown is ssh failing the way it does to a machine behind NAT.
func sshDown(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "echo 'ssh: connect to host box port 22: Operation timed out' >&2; exit 255")
}

func sshUp(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeNode$")
	cmd.Env = append(os.Environ(), "JARVIS_FAKE_NODE=1")
	return cmd
}

// waitVia waits until a call reaches the node the given way, and returns the machine's snapshot then.
func waitVia(t *testing.T, m *Machine, via string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var out struct {
			PID int `json:"pid"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := m.call(ctx, "ping", map[string]any{}, &out)
		cancel()
		got := ""
		switch {
		case err != nil:
		case out.PID == -1:
			got = "tailcat"
		case out.PID > 0:
			got = "ssh"
		}
		if got == via {
			if s := m.snapshot(); !s.Online || s.Transport != via {
				t.Fatalf("calls go over %s but the machine says online=%v transport=%q", via, s.Online, s.Transport)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	s := m.snapshot()
	t.Fatalf("never reached over %s (online=%v transport=%q error=%q)", via, s.Online, s.Transport, s.Error)
}

// tailcatMachine is a machine reachable both ways, with its timings shortened.
func tailcatMachine(h *Hub, retry, probe, check time.Duration) *Machine {
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box", TailcatAddr: "tc-test"})
	m.timings = tailcatTimings{directRetry: retry, directProbeEvery: probe, checkEvery: check}
	return m
}

// ssh first; when it fails, tailcat right away; and the next reconnects go to tailcat first for a while instead
// of waiting on ssh again.
func TestFallsBackToTailcatWhenSSHFails(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, time.Hour, time.Hour)
	var sshTries, dials atomic.Int32
	m.nodeCmd = func(ctx context.Context) *exec.Cmd { sshTries.Add(1); return sshDown(ctx) }
	m.dialTailcat = tailcatNode(&dials)
	m.probeDirect = func(context.Context) error { return errors.New("still down") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	waitVia(t, m, "tailcat")
	if sshTries.Load() != 1 {
		t.Fatalf("ssh tried %d times before tailcat, want 1", sshTries.Load())
	}
	if got := m.transports(); strings.Join(got, ",") != "tailcat,ssh" {
		t.Fatalf("after ssh failed: transports %v, want tailcat first", got)
	}

	m.reconnect()
	waitVia(t, m, "tailcat")
	if dials.Load() < 2 {
		t.Fatalf("reconnect did not dial tailcat again (%d dials)", dials.Load())
	}
	if sshTries.Load() != 1 {
		t.Fatalf("reconnect waited on ssh again (%d tries) though it failed just now", sshTries.Load())
	}
}

// On tailcat the hub keeps checking ssh, and goes back to it once it works.
func TestGoesBackToSSHWhenItWorksAgain(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, 200*time.Millisecond, time.Hour)
	var fixed atomic.Bool
	m.nodeCmd = func(ctx context.Context) *exec.Cmd {
		if fixed.Load() {
			return sshUp(ctx)
		}
		return sshDown(ctx)
	}
	var dials atomic.Int32
	m.dialTailcat = tailcatNode(&dials)
	m.probeDirect = func(context.Context) error {
		if fixed.Load() {
			return nil
		}
		return errors.New("down")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	waitVia(t, m, "tailcat")
	fixed.Store(true)
	waitVia(t, m, "ssh")
	if got := m.transports(); strings.Join(got, ",") != "ssh,tailcat" {
		t.Fatalf("back on ssh: transports %v, want ssh first", got)
	}
}

// Not while a question waits on the phone: switching reconnects, which would cancel it.
func TestStaysOnTailcatWhileAnInteractionWaits(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, 100*time.Millisecond, time.Hour)
	var probes atomic.Int32
	m.nodeCmd = func(ctx context.Context) *exec.Cmd { return sshDown(ctx) }
	var dials atomic.Int32
	m.dialTailcat = tailcatNode(&dials)
	m.probeDirect = func(context.Context) error { probes.Add(1); return nil }
	h.interactions["q"] = &pending{}
	h.interactions["q"].ix.MachineID = "box"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	waitVia(t, m, "tailcat")
	time.Sleep(600 * time.Millisecond)
	if probes.Load() != 0 {
		t.Fatalf("probed ssh %d times with a question waiting", probes.Load())
	}
	if s := m.snapshot(); s.Transport != "tailcat" {
		t.Fatalf("switched to %q with a question waiting", s.Transport)
	}
}

// A machine without a tailcat address is ssh only, as before; kind tailcat never tries ssh.
func TestTransports(t *testing.T) {
	h := newTestHub(t)
	for _, c := range []struct {
		cfg  MachineConfig
		want string
	}{
		{MachineConfig{ID: "a", Kind: "local"}, "local"},
		{MachineConfig{ID: "a", Kind: "ssh", SSHHost: "a"}, "ssh"},
		{MachineConfig{ID: "a", Kind: "ssh", SSHHost: "a", TailcatAddr: "tc"}, "ssh,tailcat"},
		{MachineConfig{ID: "a", Kind: "tailcat", TailcatAddr: "tc"}, "tailcat"},
	} {
		if got := strings.Join(newMachine(h, c.cfg).transports(), ","); got != c.want {
			t.Errorf("%+v: %s, want %s", c.cfg, got, c.want)
		}
	}
}

// Both ways failing: the machine's error says why for each.
func TestBothWaysFailing(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, time.Hour, time.Hour)
	m.nodeCmd = sshDown
	m.dialTailcat = func(context.Context) (net.Conn, error) { return nil, errors.New("no answer") }
	err := m.connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ssh：ssh: connect to host box") || !strings.Contains(err.Error(), "tailcat：no answer") {
		t.Fatalf("error %v, want both reasons", err)
	}
	if s := m.snapshot(); s.Online || s.Transport != "" {
		t.Fatalf("online=%v transport=%q after both failed", s.Online, s.Transport)
	}
}

// A tailcat connection that stops answering is ended after two missed pings, and the machine reconnects.
func TestDeadTailcatConnectionIsNoticed(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, time.Hour, 100*time.Millisecond)
	m.cfg.Kind = "tailcat"
	var dials atomic.Int32
	m.dialTailcat = func(ctx context.Context) (net.Conn, error) { dials.Add(1); return silentNode(ctx) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	deadline := time.Now().Add(15 * time.Second)
	for dials.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the silent connection was kept (%d dials, error %q)", dials.Load(), m.snapshot().Error)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ssh logs in but the node does not answer over it: going back to ssh fails right away each time, so the hub
// waits twice as long before trying again instead of dropping the connection every few minutes.
func TestGoingBackToSSHBacksOffWhenItFailsAgain(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, 150*time.Millisecond, time.Hour)
	var sshTries, dials atomic.Int32
	m.nodeCmd = func(ctx context.Context) *exec.Cmd { sshTries.Add(1); return sshDown(ctx) }
	m.dialTailcat = tailcatNode(&dials)
	m.probeDirect = func(context.Context) error { return nil } // `ssh host true` works
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	deadline := time.Now().Add(20 * time.Second)
	for {
		m.mu.Lock()
		flaps := m.flaps
		m.mu.Unlock()
		if flaps >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("flaps %d after %d ssh tries", flaps, sshTries.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, want := m.probeEvery(), 150*time.Millisecond<<2; got < want {
		t.Fatalf("probe every %s after two failed switches, want at least %s", got, want)
	}
	waitVia(t, m, "tailcat")
}

// A node that stops reading after node.info: the hub's next write (the ping) blocks for good, as on a dead
// connection with a full send buffer. The watcher must not wait on it: the connection still ends.
func TestStuckWriteStillEndsTheConnection(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, time.Hour, 100*time.Millisecond)
	m.cfg.Kind = "tailcat"
	var dials atomic.Int32
	m.dialTailcat = func(context.Context) (net.Conn, error) {
		dials.Add(1)
		hubSide, nodeSide := net.Pipe()
		go func() {
			// answer node.info, then never read again (net.Pipe has no buffer: every hub write blocks)
			var req struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(nodeSide).Decode(&req); err == nil {
				_, _ = nodeSide.Write([]byte(`{"id":"` + req.ID + `","result":{}}` + "\n"))
			}
		}()
		return hubSide, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	deadline := time.Now().Add(15 * time.Second)
	for dials.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("a connection stuck on a write was kept (%d dials)", dials.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A connection busy sending (a big page over a slow relay) is alive even if a ping's answer is stuck behind it.
func TestBusyConnectionIsKept(t *testing.T) {
	h := newTestHub(t)
	m := tailcatMachine(h, time.Hour, time.Hour, 100*time.Millisecond)
	m.cfg.Kind = "tailcat"
	var dials atomic.Int32
	m.dialTailcat = func(ctx context.Context) (net.Conn, error) {
		dials.Add(1)
		hubSide, nodeSide := net.Pipe()
		p := rpc.New("n", nodeSide)
		p.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
			if method == "node.info" {
				return map[string]any{}, nil
			}
			<-ctx.Done() // never answers a ping
			return nil, ctx.Err()
		}
		go func() { _ = p.Serve(context.Background(), nodeSide); nodeSide.Close() }()
		go func() { // keeps sending, as a long transfer does
			for {
				if err := p.Notify("session.delta", map[string]any{}); err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
		return hubSide, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.run(ctx)
	time.Sleep(1500 * time.Millisecond)
	if n := dials.Load(); n != 1 {
		t.Fatalf("a busy connection was dropped (%d dials)", n)
	}
	if !m.snapshot().Online {
		t.Fatal("offline while the node was sending")
	}
}
