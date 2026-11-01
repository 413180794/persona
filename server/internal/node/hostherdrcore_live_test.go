//go:build hclive

package node

// Live checks against a real herdr-core (go test -tags hclive -run HerdrCoreLive ./internal/node/ -v):
//
//   - TestHerdrCoreLiveIsolated starts its own herdr-core (HC_SOCKET, HC_SESSION and XDG dirs under /tmp, so the
//     user's instance and Herdr app are never touched), runs a small program that reports its state with OSC 7501
//     like Claude Code does, drives it through the host, and stops that core.
//   - TestHerdrCoreLiveReadOnly lists the default instance's sessions the way the node does, with read-only calls
//     only (terminal.list, doc.get, terminal.get), printing session ids, panes and states — no screen text.

import (
	"context"
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

// A program that says it works, waits for a line, says it is done and echoes the line.
const osc7501Program = `printf '\033]7501;state=working:app=claude-code\033\\'; printf 'ready\n'; read line; ` +
	`printf '\033]7501;state=done:app=claude-code\033\\'; printf 'got:%s\n' "$line"; sleep 60`

func TestHerdrCoreLiveIsolated(t *testing.T) {
	bin := hcBin(t)
	dir, err := os.MkdirTemp("/tmp", "hcl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "c.sock")
	env := append(os.Environ(), "HC_SOCKET="+sock, "HC_SESSION=jarvislive",
		"XDG_STATE_HOME="+filepath.Join(dir, "state"), "XDG_CONFIG_HOME="+filepath.Join(dir, "cfg"), "XDG_RUNTIME_DIR=")
	hc := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hc %v: %v\n%s", args, err, out)
		}
	}
	hc("server", "start")
	t.Cleanup(func() {
		cmd := exec.Command(bin, "server", "stop")
		cmd.Env = env
		_ = cmd.Run()
	})
	c := &herdrcore.Client{Socket: sock, Log: t.Logf}
	h := &herdrCoreHost{c: c}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Logf("herdr-core %s", h.Version(ctx))

	var run struct {
		Address string `json:"address"`
	}
	if err := c.Call(ctx, 20*time.Second, "terminal.run", map[string]any{"argv": []string{"sh", "-c", osc7501Program},
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
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Error("no event from the subscription")
	}
	if err := h.SendKeys(ctx, ref, "ctrl+c"); err != nil {
		t.Error(err)
	}
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
