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

// The command lines' calls reach the hub only from the assistant's processes, as the assistant's, and only for the
// connectors signed in.
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

	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	t.Setenv("PERSONA_PLUGIN_SKILLS_DIR", t.TempDir())
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	first := prepareAssistantForTest(t, n, ".jarvis/assistant", []string{"example"}, nil)
	second := prepareAssistantForTest(t, n, ".jarvis/managers/second/assistant", []string{"other"}, nil)
	// This test is no assistant process, whatever it says.
	if res := ask("example_list"); !res.IsError || !strings.Contains(res.Content[0].Text, "只能在助手的对话里用") || len(asked) != 0 {
		t.Fatalf("not the assistant: %+v, the hub was asked %v", res, asked)
	}
	n.mu.Lock()
	n.procs["s1"] = &assistantProc{id: "s1", cwd: first, cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}}
	n.mu.Unlock()
	if res := ask("phone_contacts"); !res.IsError || len(asked) != 0 {
		t.Fatalf("not a signed-in connector's: %+v", res)
	}
	if res := ask("example_list"); res.IsError {
		t.Fatalf("the assistant: %+v", res)
	}
	if res := ask("other_list"); !res.IsError {
		t.Fatalf("first assistant called the second one's connector: %+v", res)
	}
	n.mu.Lock()
	delete(n.procs, "s1")
	n.procs["s2"] = &assistantProc{id: "s2", cwd: second, cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}}
	n.mu.Unlock()
	if res := ask("example_list"); !res.IsError {
		t.Fatalf("second assistant called the first one's connector: %+v", res)
	}
	if res := ask("other_list"); res.IsError {
		t.Fatalf("second assistant: %+v", res)
	}
	for _, cwd := range []string{"", filepath.Join(n.dir, "unprepared")} {
		n.mu.Lock()
		n.procs["s2"].cwd = cwd
		n.mu.Unlock()
		if res := ask("other_list"); !res.IsError {
			t.Fatalf("unprepared directory called a connector: %+v", res)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 2 || asked[0]["name"] != "example_list" || asked[0]["assistant"] != true || asked[0]["session_id"] != "s1" ||
		asked[1]["name"] != "other_list" || asked[1]["assistant"] != true || asked[1]["session_id"] != "s2" {
		t.Fatalf("the hub was asked %v", asked)
	}
}

// The tg an older node wrote for the built-in Telegram (personad tg) goes at the next message, Telegram signed in or
// not: Telegram's command line is its extension's now (#1704), which the hub names in clis like any connector's.
func TestPrepareTakesAwayTheBuiltInTg(t *testing.T) {
	t.Setenv("JARVIS_SKILLS_DIR", t.TempDir())
	n, _ := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(conns []string, clis map[string]string) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"dir": ".jarvis/assistant", "claude_md": "# 示例", "connectors": conns, "clis": clis})
		if _, err := n.handle(context.Background(), "assistant.prepare", params); err != nil {
			t.Fatal(err)
		}
	}
	tg := filepath.Join(n.dir, "assistant-bin", "tg")
	_ = os.MkdirAll(filepath.Dir(tg), 0o755)
	old := "#!/bin/sh\n# Telegram through the hub (the telegram skill), on the assistant's PATH while it is signed in (node/clicall.go).\n" +
		"exec /opt/example/personad tg \"$@\"\n"
	_ = os.WriteFile(tg, []byte(old), 0o755)
	prepare([]string{"telegram"}, nil)
	if _, err := os.Stat(tg); !os.IsNotExist(err) {
		t.Fatalf("the built-in's tg stayed: %v", err)
	}
	prepare([]string{"telegram"}, map[string]string{"tg": "telegram"})
	if b, err := os.ReadFile(tg); err != nil || !strings.Contains(string(b), "exec /opt/example/personad tool --cli telegram \"$@\"") {
		t.Fatalf("the extension's tg: %q %v", b, err)
	}
	prepare(nil, nil)
	if n.mayCall(n.assistantDirOf(".jarvis/assistant"), "telegram_list_chats") {
		t.Fatal("Telegram signed out, its tools are still called")
	}
}

// A connector with a command line of its own (connector.Manifest.CLI, #1704): the hub says it with the connectors, the
// node puts it on the assistant's PATH as personad tool --cli <id>, lets the
// command lines call that connector's tools, and takes away only the command lines it wrote once the connector is
// signed out (a plugin's gate commands live there too).
func TestConnectorCommandLines(t *testing.T) {
	t.Setenv("JARVIS_SKILLS_DIR", t.TempDir())
	n, _ := testNode(t)
	n.dir, n.bin, n.logger = t.TempDir(), "/opt/example/personad", log.New(io.Discard, "", 0)
	prepare := func(conns []string, clis map[string]string) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"dir": ".jarvis/assistant", "claude_md": "# 示例", "connectors": conns, "clis": clis})
		if _, err := n.handle(context.Background(), "assistant.prepare", params); err != nil {
			t.Fatal(err)
		}
	}
	cwd := n.assistantDirOf(".jarvis/assistant")
	bin := filepath.Join(n.dir, "assistant-bin")
	_ = os.MkdirAll(bin, 0o755)
	gate := filepath.Join(bin, "ol") // a plugin's gate command: not the node's to take away
	_ = os.WriteFile(gate, []byte("#!/bin/sh\nexec sh /x/ol.sh \"$@\"\n"), 0o755)

	prepare([]string{"telegram", "example"}, map[string]string{"tg": "telegram", "ex": "example"})
	for name, id := range map[string]string{"tg": "telegram", "ex": "example"} {
		if b, err := os.ReadFile(filepath.Join(bin, name)); err != nil || !strings.Contains(string(b), "exec /opt/example/personad tool --cli "+id+" \"$@\"") {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
	if !n.mayCall(cwd, "example_list") || !n.mayCall(cwd, "telegram_cli") || n.mayCall(cwd, "phone_contacts") || n.mayCall(cwd, "examplex_list") {
		t.Fatal("the command lines call the signed-in connectors' tools, and only those")
	}

	prepare([]string{"telegram"}, nil) // signed in, and no command line said: none on the PATH
	if _, err := os.Stat(filepath.Join(bin, "tg")); !os.IsNotExist(err) {
		t.Fatalf("no command line said, tg stayed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bin, "ex")); !os.IsNotExist(err) {
		t.Fatalf("example signed out, ex stayed: %v", err)
	}
	if n.mayCall(cwd, "example_list") {
		t.Fatal("example signed out, its tools are still called")
	}
	prepare(nil, nil)
	if _, err := os.Stat(filepath.Join(bin, "tg")); !os.IsNotExist(err) {
		t.Fatalf("signed out, tg stayed: %v", err)
	}
	if _, err := os.Stat(gate); err != nil {
		t.Fatal("a command the node did not write was taken away")
	}
}
