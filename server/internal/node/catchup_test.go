package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

// hubEnd wires n.peer to a fake hub that records every session.items event.
type hubEnd struct {
	mu    sync.Mutex
	items []proto.Item
}

func (h *hubEnd) got() []proto.Item {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]proto.Item(nil), h.items...)
}

func withHub(t *testing.T, n *Node) *hubEnd {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	end := &hubEnd{}
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event != "session.items" {
			return
		}
		var p struct {
			Items []proto.Item `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		end.mu.Lock()
		end.items = append(end.items, p.Items...)
		end.mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return end
}

func testNode(t *testing.T) (*Node, string) {
	t.Helper()
	home := t.TempDir()
	n := &Node{home: home, paths: map[string]string{}, files: map[string]*transcript.File{}, procs: map[string]*assistantProc{},
		agents: map[string]herdr.Agent{}, watches: map[string]*watch{}, state: &stateFile{Sessions: map[string]sessionState{}}}
	return n, home
}

// writeCodexToolLines writes n Codex tool calls: two items (call + result) on every line.
func writeCodexToolLines(t *testing.T, path string, from, n int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := from; i < from+n; i++ {
		fmt.Fprintf(f, `{"timestamp":"2026-01-01T00:00:00Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"c%d","command":["ls"],"aggregated_output":"x","exit_code":0}}}`+"\n", i)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A watch that resumes after since must push everything appended since, not just the first 500 items; and never half
// a line (the result of a tool call without its call).
func TestWatchCatchUpSendsEverything(t *testing.T) {
	n, home := testNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	writeCodexToolLines(t, path, 0, 10)
	f, err := n.file(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(); err != nil { // the hub has seen these 10 lines
		t.Fatal(err)
	}
	writeCodexToolLines(t, path, 10, 700) // 1400 items while nobody watched
	hub := withHub(t, n)
	if err := n.watch(id, 10); err != nil {
		t.Fatal(err)
	}
	defer n.unwatch(id)
	waitFor(t, "1400 items", func() bool { return len(hub.got()) >= 1400 })
	got := hub.got()
	if len(got) != 1400 {
		t.Fatalf("got %d items, want 1400", len(got))
	}
	for i, it := range got {
		if want := 11 + i/2; it.Seq != want {
			t.Fatalf("item %d has seq %d, want %d", i, it.Seq, want)
		}
	}
}

// The first watch of a session the hub has not seen (since 0) must not push the whole history it reads.
func TestWatchFromZeroDoesNotPushHistory(t *testing.T) {
	n, home := testNode(t)
	const id = "11111111-2222-3333-4444-555555555556"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	_ = os.MkdirAll(dir, 0o700)
	writeCodexToolLines(t, filepath.Join(dir, id+".jsonl"), 0, 50)
	hub := withHub(t, n)
	if err := n.watch(id, 0); err != nil {
		t.Fatal(err)
	}
	defer n.unwatch(id)
	writeCodexToolLines(t, filepath.Join(dir, id+".jsonl"), 50, 1) // then one new line: streamed
	waitFor(t, "the new line", func() bool { return len(hub.got()) >= 2 })
	time.Sleep(300 * time.Millisecond)
	if got := hub.got(); len(got) != 2 || got[0].Seq != 51 {
		t.Fatalf("want only the new line's 2 items, got %d (first seq %d)", len(got), got[0].Seq)
	}
}

func TestQuickMetaCountsBigToolResultsAsActivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	big := strings.Repeat("x", 20<<10)
	lines := []string{
		`{"type":"user","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","timestamp":"2026-01-01T00:03:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + big + `"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	m := readQuickMeta(path, st)
	if want := time.Date(2026, 1, 1, 0, 3, 0, 0, time.UTC); !m.activity.Equal(want) {
		t.Fatalf("activity %v, want %v (the big tool result)", m.activity, want)
	}
}
