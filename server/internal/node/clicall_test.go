package node

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A caller is the assistant's when its process, or one of its ancestors, is an assistant process the node started.
func TestAssistantOf(t *testing.T) {
	n, _ := testNode(t)
	n.procs["s1"] = &assistantProc{id: "s1", cmd: &exec.Cmd{Process: &os.Process{Pid: 100}}}
	n.procs["s2"] = &assistantProc{id: "s2"} // not started yet
	parents := map[int]int{300: 200, 200: 100, 100: 1, 400: 1, 500: 500}
	parent := func(pid int) (int, error) {
		if p, ok := parents[pid]; ok {
			return p, nil
		}
		return 0, os.ErrNotExist
	}
	for _, c := range []struct {
		pid  int
		want string
	}{{300, "s1"}, {100, "s1"}, {400, ""}, {500, ""}, {999, ""}} {
		if got, ok := n.assistantOf(c.pid, parent); got != c.want || ok != (c.want != "") {
			t.Errorf("pid %d: %q %v, want %q", c.pid, got, ok, c.want)
		}
	}
}

// The other end of the node's socket is known on the systems the node runs on, and so are its parents.
func TestPeerProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no peer process here: every caller is refused")
	}
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := net.Dial("unix", ln.Addr().String())
		if err == nil {
			defer c.Close()
			_, _ = c.Read(make([]byte, 1))
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if pid, err := peerPID(c); err != nil || pid != os.Getpid() {
		t.Fatalf("peer %d %v, want %d", pid, err, os.Getpid())
	}
	if pid, err := parentPID(os.Getpid()); err != nil || pid != os.Getppid() {
		t.Fatalf("parent %d %v, want %d", pid, err, os.Getppid())
	}
}

// tg's calls reach the hub only from the assistant's processes, as the assistant's, and only for Telegram.
func TestCLICallOnlyForTheAssistant(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no peer process here")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, _ := testNode(t)
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	var mu sync.Mutex
	var asked []map[string]any
	hub.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		asked = append(asked, p)
		mu.Unlock()
		return proto.TextResult(`{"chats":[]}`, false), nil
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { _ = n2hW.Close(); _ = h2nW.Close() })

	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
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
			go n.serveSock(ctx, c)
		}
	}()
	ask := func(name string) proto.ToolResult {
		t.Helper()
		c, err := net.Dial("unix", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		b, _ := json.Marshal(map[string]any{"kind": "cli.call", "name": name, "args": map[string]any{}, "assistant": true})
		_, _ = c.Write(append(b, '\n'))
		line, err := bufio.NewReader(c).ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var res proto.ToolResult
		_ = json.Unmarshal(line, &res)
		return res
	}

	// This test is no assistant process, whatever it says.
	if res := ask("telegram_list_chats"); !res.IsError || !strings.Contains(res.Content[0].Text, "只能在助手的对话里用") || len(asked) != 0 {
		t.Fatalf("not the assistant: %+v, the hub was asked %v", res, asked)
	}
	n.mu.Lock()
	n.procs["s1"] = &assistantProc{id: "s1", cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}}
	n.mu.Unlock()
	if res := ask("phone_contacts"); !res.IsError || len(asked) != 0 {
		t.Fatalf("not Telegram's: %+v", res)
	}
	if res := ask("telegram_list_chats"); res.IsError {
		t.Fatalf("the assistant: %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0]["name"] != "telegram_list_chats" || asked[0]["assistant"] != true || asked[0]["session_id"] != "s1" {
		t.Fatalf("the hub was asked %v", asked)
	}
}

// Before each message the hub says which connectors are signed in: the telegram skill and tg are there while
// Telegram is, and gone once it is not.
func TestPrepareFollowsTheConnectors(t *testing.T) {
	t.Setenv("JARVIS_SKILLS_DIR", t.TempDir())
	n, home := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(conns ...string) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"dir": ".jarvis/assistant", "claude_md": "# 示例", "connectors": conns})
		if _, err := n.handle(context.Background(), "assistant.prepare", params); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join(home, ".jarvis/assistant/.claude/skills/telegram/SKILL.md")
	tg := filepath.Join(n.dir, "assistant-bin", "tg")
	prepare("github")
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatalf("Telegram not signed in, yet its skill is there: %v", err)
	}
	prepare("github", "telegram")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("Telegram signed in, no skill: %v", err)
	}
	if b, err := os.ReadFile(tg); err != nil || !strings.Contains(string(b), "exec /opt/example/personad tg \"$@\"") {
		t.Fatalf("tg: %q %v", b, err)
	}
	prepare()
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatalf("signed out, the skill stayed: %v", err)
	}
	if _, err := os.Stat(tg); !os.IsNotExist(err) {
		t.Fatalf("signed out, tg stayed: %v", err)
	}
}
