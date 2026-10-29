package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer answers each connection's one request line with reply(method, params) and closes it, like herdr.
func fakeServer(t *testing.T, reply func(method string, params map[string]any) string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "herdr") // short: unix socket paths are limited to ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string         `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if json.Unmarshal(line, &req) != nil {
					return
				}
				body := reply(req.Method, req.Params)
				_, _ = conn.Write([]byte(`{"id":"` + req.ID + `",` + body + "}\n"))
			}()
		}
	}()
	return sock
}

// cliScript is a stand-in herdr binary that records its arguments and prints a canned response.
func cliScript(t *testing.T, stdout string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "herdr")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\ncat <<'EOF'\n" + stdout + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func TestRPCDecodesResultAndErrors(t *testing.T) {
	var got map[string]any
	sock := fakeServer(t, func(method string, params map[string]any) string {
		switch method {
		case "agent.get":
			got = params
			return `"result":{"type":"agent_info","agent":{"agent":"claude","pane_id":"w1:p2","agent_status":"working","agent_session":{"kind":"id","value":"s1"}}}`
		case "pane.read":
			return `"result":{"type":"pane_read","read":{"pane_id":"w1:p2","source":"` + params["source"].(string) + `","text":"❯ hi\n"}}`
		default:
			return `"error":{"code":"pane_not_found","message":"pane w9:p9 not found"}`
		}
	})
	c := &Client{Bin: "/nonexistent/herdr", Socket: sock}
	ctx := context.Background()
	a, err := c.AgentGet(ctx, "w1:p2")
	if err != nil || a.PaneID != "w1:p2" || a.SessionID() != "s1" || got["target"] != "w1:p2" {
		t.Fatalf("agent.get: %+v %v params %v", a, err, got)
	}
	text, err := c.PaneRead(ctx, "w1:p2", "recent-unwrapped", 30)
	if err != nil || text != "❯ hi\n" {
		t.Fatalf("pane.read: %q %v", text, err)
	}
	err = c.PaneClose(ctx, "w9:p9")
	if !IsNotFound(err) {
		t.Fatalf("an error envelope is a herdr error, not a CLI fallback: %v", err)
	}
}

func TestFallsBackToCLIWithoutSocket(t *testing.T) {
	bin, args := cliScript(t, `{"id":"cli:agent:list","result":{"agents":[{"agent":"claude","pane_id":"w1:p1"}]}}`)
	c := &Client{Bin: bin, Socket: filepath.Join(t.TempDir(), "missing.sock")}
	agents, err := c.Agents(context.Background())
	if err != nil || len(agents) != 1 || agents[0].PaneID != "w1:p1" {
		t.Fatalf("agents via CLI: %+v %v", agents, err)
	}
	if b, _ := os.ReadFile(args); strings.TrimSpace(string(b)) != "agent list" {
		t.Fatalf("CLI args %q", b)
	}
}

func TestFallsBackToCLIForUnknownMethod(t *testing.T) {
	sock := fakeServer(t, func(string, map[string]any) string {
		return `"error":{"code":"invalid_request","message":"invalid request: unknown variant ` + "`session.snapshot`" + `, expected one of ..."}`
	})
	bin, _ := cliScript(t, `{"id":"cli:api:snapshot","result":{"snapshot":{"agents":[{"agent":"codex","pane_id":"w2:p1"}]}}}`)
	c := &Client{Bin: bin, Socket: sock}
	snap, err := c.Snapshot(context.Background())
	if err != nil || len(snap.Agents) != 1 || snap.Agents[0].Agent != "codex" {
		t.Fatalf("snapshot via CLI: %+v %v", snap, err)
	}
}

func TestPaneReadCLIFallbackIsRawText(t *testing.T) {
	bin, args := cliScript(t, "line one")
	c := &Client{Bin: bin}
	text, err := c.PaneRead(context.Background(), "w1:p1", "visible", 60)
	if err != nil || text != "line one\n" {
		t.Fatalf("pane read via CLI: %q %v", text, err)
	}
	if b, _ := os.ReadFile(args); strings.TrimSpace(string(b)) != "pane read w1:p1 --source visible --lines 60" {
		t.Fatalf("CLI args %q", b)
	}
}

func TestReadLineCapsLength(t *testing.T) {
	long := strings.Repeat("x", 300) + "\n"
	if _, err := readLine(bufio.NewReaderSize(strings.NewReader(long), 16), 100); err == nil {
		t.Fatal("a line over the cap must fail")
	}
	got, err := readLine(bufio.NewReaderSize(strings.NewReader(long), 16), 1000)
	if err != nil || len(got) != 300 {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/x/h.sock")
	if p := SocketPath(); p != "/x/h.sock" {
		t.Fatal(p)
	}
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if p := SocketPath(); p != "/cfg/herdr/herdr.sock" {
		t.Fatal(p)
	}
}

func TestSubscribeStreamsUntilHerdrCloses(t *testing.T) {
	dir, err := os.MkdirTemp("", "herdr")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotReq := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadBytes('\n')
		gotReq <- string(line)
		_, _ = conn.Write([]byte(`{"id":"jarvis:events","result":{"type":"subscription_started"}}` + "\n" +
			`{"event":"pane_agent_status_changed","data":{"pane_id":"w1:p2","workspace_id":"w1","agent_status":"blocked"}}` + "\n" +
			`{"event":"pane_created","data":{"pane":{"pane_id":"w1:p3"}}}` + "\n" +
			`{"id":"jarvis:events","error":{"code":"events_lost","message":"subscriber fell behind"}}` + "\n"))
	}()
	c := &Client{Socket: sock}
	var got []string
	err = c.Subscribe(context.Background(), []Subscription{{"type": "pane.created"}, {"type": "pane.agent_status_changed", "pane_id": "w1:p2"}},
		func(e Event) { got = append(got, e.Event) })
	if err == nil || !strings.Contains(err.Error(), "events_lost") {
		t.Fatalf("stream end: %v", err)
	}
	if strings.Join(got, ",") != SubscriptionStarted+",pane_agent_status_changed,pane_created" {
		t.Fatalf("events %v", got)
	}
	req := <-gotReq
	if !strings.Contains(req, `"method":"events.subscribe"`) || !strings.Contains(req, `"pane_id":"w1:p2"`) {
		t.Fatalf("request %s", req)
	}
}

func TestSubscribeEndsWithContext(t *testing.T) {
	dir, err := os.MkdirTemp("", "herdr")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(`{"id":"jarvis:events","result":{"type":"subscription_started"}}` + "\n"))
		// then silence, connection left open
	}()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- (&Client{Socket: sock}).Subscribe(ctx, nil, func(e Event) {
			if e.Event == SubscriptionStarted {
				close(started)
			}
		})
	}()
	<-started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestFailureEpisodesLogOnce(t *testing.T) {
	var lines []string
	c := &Client{Socket: filepath.Join(t.TempDir(), "gone.sock"), Log: func(f string, a ...any) { lines = append(lines, f) }}
	for range 5 {
		_, _ = c.Agents(context.Background())
	}
	if len(lines) != 1 {
		t.Fatalf("one line for a run of failures, got %v", lines)
	}
	c.Socket = fakeServer(t, func(string, map[string]any) string { return `"result":{"agents":[]}` })
	_, _ = c.Agents(context.Background())
	_, _ = c.Agents(context.Background())
	if len(lines) != 2 || !strings.Contains(lines[1], "reachable again") {
		t.Fatalf("recovery logged once: %v", lines)
	}
}
