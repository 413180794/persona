//go:build hclive

package node

// Live checks against a real herdr-core (go test -tags hclive -run HerdrCoreLive ./internal/node/ -v):
//
//   - TestHerdrCoreLiveIsolated starts its own herdr-core (HC_SOCKET, HC_SESSION and XDG dirs under /tmp, so the
//     user's instance and Herdr app are never touched), runs a small program that reports its state with OSC 7501
//     like Claude Code does, drives it through the host, and stops that core.
//   - TestHerdrCoreLiveCloseKeep closes, as the phone does, an agent in a terminal the way the Herdr app runs one (a
//     shell with keep set that runs the agent and then stays a shell), on its own core too.
//   - TestHerdrCoreLiveReadOnly lists the default instance's sessions the way the node does, with read-only calls
//     only (terminal.list, doc.get, terminal.get), printing session ids, panes and states — no screen text.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdrcore"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func hcBin(t *testing.T) string {
	t.Helper()
	for _, p := range []string{os.Getenv("HC_BIN"), filepath.Join(os.Getenv("HOME"), ".local", "bin", "herdr-core")} {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	if p, err := exec.LookPath("herdr-core"); err == nil {
		return p
	}
	t.Skip("herdr-core not installed")
	return ""
}

// fakeClaude says it works (OSC 7501, as Claude Code does), waits for a line, says it is done and echoes the line.
const fakeClaude = `package main

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Print("\x1b]7501;state=working:app=claude-code\x1b\\")
	fmt.Println("ready")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Print("\x1b]7501;state=done:app=claude-code\x1b\\")
	fmt.Printf("got:%s", line)
	time.Sleep(time.Minute)
}
`

func buildFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(fakeClaude), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	cmd := exec.Command("go", "build", "-o", bin, "main.go")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GO111MODULE=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// isolatedHC starts a herdr-core of its own (HC_SOCKET, HC_SESSION and XDG dirs under a /tmp dir, so the user's
// instance and Herdr app are never touched) and stops it when the test ends; it returns the dir, a client and a host.
func isolatedHC(t *testing.T) (string, *herdrcore.Client, *herdrCoreHost) {
	t.Helper()
	bin := hcBin(t)
	dir, err := os.MkdirTemp("/tmp", "hcl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "c.sock")
	env := append(os.Environ(), "HC_SOCKET="+sock, "HC_SESSION=jarvislive",
		"XDG_STATE_HOME="+filepath.Join(dir, "state"), "XDG_CONFIG_HOME="+filepath.Join(dir, "cfg"), "XDG_RUNTIME_DIR=")
	cmd := exec.Command(bin, "server", "start")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hc server start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		cmd := exec.Command(bin, "server", "stop")
		cmd.Env = env
		_ = cmd.Run()
	})
	c := &herdrcore.Client{Socket: sock, Log: t.Logf}
	return dir, c, &herdrCoreHost{c: c}
}

func TestHerdrCoreLiveIsolated(t *testing.T) {
	dir, c, h := isolatedHC(t)
	sock := c.Socket
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Logf("herdr-core %s", h.Version(ctx))

	// The program is a small Go build named "claude": a terminal whose foreground is a shell is not an agent.
	claude := buildFakeClaude(t, dir)
	// The core's own records, as sent: the fields its schema requires (the structs and fixtures were written from it).
	events := rawEvents(t, ctx, sock)

	var run struct {
		Address string `json:"address"`
	}
	if err := c.Call(ctx, 20*time.Second, "terminal.run", map[string]any{"argv": []string{claude},
		"cwd": dir, "labels": map[string]string{"jarvis": "live"}}, &run); err != nil {
		t.Fatal(err)
	}
	ref := host.PaneRef{Kind: host.HerdrCore, ID: run.Address}

	changes := make(chan struct{}, 64)
	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	go func() { _ = h.Watch(wctx, nil, func() { changes <- struct{}{} }) }()

	waitAgent := func(status string) host.Agent {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			l, err := h.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range l.Agents {
				if a.PaneID == ref.String() && a.AgentStatus == status {
					return a
				}
			}
		}
		tm, _ := c.Get(ctx, ref.ID)
		if tm != nil {
			t.Fatalf("%s never %s: status %+v foreground %+v", ref, status, tm.Status, tm.Foreground)
		}
		t.Fatalf("%s never %s", ref, status)
		return host.Agent{}
	}
	a := waitAgent(proto.StatusWorking)
	if a.Agent != "claude" || a.WorkspaceID != dirSpace+dir {
		t.Errorf("agent %+v", a)
	}
	if pids, err := h.PaneProcesses(ctx, ref); err != nil || len(pids) == 0 {
		t.Errorf("PaneProcesses = %v, %v", pids, err)
	}
	if text, err := h.Read(ctx, ref, host.ReadOpts{Source: "visible", Lines: 20}); err != nil || !strings.Contains(text, "ready") {
		t.Errorf("Read = %q, %v", text, err)
	}
	if _, err := h.Read(ctx, ref, host.ReadOpts{Source: "visible", ANSI: true}); err == nil {
		t.Error("an ANSI read worked")
	}
	if err := h.Prompt(ctx, ref, "hello"); err != nil {
		t.Fatal(err)
	}
	waitAgent(proto.StatusDone)
	if text, err := h.Read(ctx, ref, host.ReadOpts{Source: "recent", Lines: 50}); err != nil || !strings.Contains(text, "got:hello") {
		t.Errorf("after the prompt: %q, %v", text, err)
	}
	var raw struct {
		Terminals []map[string]any `json:"terminals"`
	}
	if err := c.Call(ctx, 5*time.Second, "terminal.list", nil, &raw); err != nil || len(raw.Terminals) == 0 {
		t.Fatalf("raw list: %v %v", raw, err)
	}
	for _, f := range []string{"address", "labels", "argv", "cwd", "state", "created_ms", "screen", "cols", "rows",
		"status", "foreground", "pid", "title", "turn"} {
		if _, ok := raw.Terminals[0][f]; !ok {
			t.Errorf("terminal.list: no %s in %v", f, raw.Terminals[0])
		}
	}
	fg, _ := raw.Terminals[0]["foreground"].(map[string]any)
	st, _ := raw.Terminals[0]["status"].(map[string]any)
	if fg["pid"] == nil || fg["argv"] == nil || st["app"] != "claude-code" || st["state"] == nil {
		t.Errorf("foreground %v status %v", fg, st)
	}
	select {
	case ev := <-events:
		for _, f := range []string{"type", "terminal", "seq"} {
			if _, ok := ev[f]; !ok {
				t.Errorf("event without %s: %v", f, ev)
			}
		}
	case <-time.After(5 * time.Second):
		t.Error("no raw event")
	}
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Error("no event from the subscription")
	}
	if err := h.SendKeys(ctx, ref, "ctrl+c"); err != nil {
		t.Error(err)
	}
}

// The Herdr app runs an agent as `sh -ic '"$@"; exec "$0" -i' sh <agent>` with keep set. Closing it from the phone
// (ctrl+c twice, then Close) must end the terminal for good: a kill is final even with keep (keep only replaces a
// program lost some other way with a shell), so it stays finished and leaves the live list. Closing it again, or once
// it is removed, is not an error.
func TestHerdrCoreLiveCloseKeep(t *testing.T) {
	dir, c, h := isolatedHC(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	claude := buildFakeClaude(t, dir)
	var run struct {
		Address string `json:"address"`
	}
	if err := c.Call(ctx, 20*time.Second, "terminal.run", map[string]any{
		"argv": []string{"/bin/sh", "-ic", `"$@"; exec "$0" -i`, "/bin/sh", claude}, "cwd": dir, "keep": true,
		"labels": map[string]string{herdrSpaceLabel: "S1", herdrTabLabel: "T1", herdrWhereLabel: "live / 1"}}, &run); err != nil {
		t.Fatal(err)
	}
	ref := host.PaneRef{Kind: host.HerdrCore, ID: run.Address}
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if a, err := h.Agent(ctx, ref); err == nil && a.Agent == "claude" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never ran claude", ref)
		}
	}
	// What the node's close does. The interrupts end the agent, but the terminal lives on as the app's shell: without
	// the kill the pane would stay open.
	for range 2 {
		if err := h.SendKeys(ctx, ref, "ctrl+c"); err != nil {
			t.Logf("ctrl+c: %v", err)
		}
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		tm, err := c.Get(ctx, ref.ID)
		if err != nil {
			t.Fatal(err)
		}
		if tm.State == "live" && tm.Foreground != nil && isShell(tm.Foreground) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after ctrl+c: %s is %s, foreground %+v", ref, tm.State, tm.Foreground)
		}
	}
	if err := h.Close(ctx, ref, "S1", "T1"); err != nil {
		t.Fatal(err)
	}
	gone := func(when string) {
		t.Helper()
		tm, err := c.Get(ctx, ref.ID)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if tm.State != "finished" {
			t.Errorf("%s: %s is %s (foreground %+v)", when, ref, tm.State, tm.Foreground)
		}
		l, err := h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range l.Panes {
			if p.PaneID == ref.String() {
				t.Errorf("%s: still listed %+v", when, p)
			}
		}
	}
	gone("after close")
	time.Sleep(3 * time.Second) // past keep's grace before it would restart a lost program as a shell
	gone("later")
	if err := h.Close(ctx, ref, "S1", "T1"); err != nil {
		t.Errorf("closing a finished terminal: %v", err)
	}
	if err := c.Call(ctx, 5*time.Second, "terminal.rm", map[string]any{"terminal": ref.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(ctx, ref, "S1", "T1"); err != nil {
		t.Errorf("closing a removed terminal: %v", err)
	}
}

// rawEvents subscribes on its own connection and passes on the params of each event notification, undecoded.
func rawEvents(t *testing.T, ctx context.Context, sock string) <-chan map[string]any {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	go func() { <-ctx.Done(); conn.Close() }()
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"events.subscribe","params":{}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	out := make(chan map[string]any, 64)
	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var m struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(sc.Bytes(), &m) == nil && m.Method == "event" {
				select {
				case out <- m.Params:
				default:
				}
			}
		}
	}()
	return out
}

func TestHerdrCoreLiveReadOnly(t *testing.T) {
	c := herdrcore.New()
	if c.Socket == "" {
		t.Skip("no herdr-core socket")
	}
	if _, err := os.Stat(c.Socket); err != nil {
		t.Skip("herdr-core not running")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{home: home, paths: map[string]string{}, agents: map[string]host.Agent{}, panes: map[string]string{},
		panePIDs: map[string]panePID{}, state: newState(), cdirs: newClaudeDirs()}
	n.hosts = host.NewRegistry(&herdrCoreHost{c: c})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := n.refreshHosts(ctx); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	var lines []string
	for id, a := range n.agents {
		lines = append(lines, a.PaneID+"  "+a.Agent+"  "+a.AgentStatus+"  "+id)
	}
	n.mu.Unlock()
	sort.Strings(lines)
	for _, l := range lines {
		t.Log(l)
	}
	if len(lines) == 0 {
		t.Log("no claude / codex terminals")
	}
}
