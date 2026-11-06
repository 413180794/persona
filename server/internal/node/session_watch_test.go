package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

const watchedSession = "aaaaaaaa-0000-0000-0000-000000000003"

func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// A chat's live view of a Claude session: after the hub's session.watch is answered, a new message, the work it starts
// and the reply are all pushed, the message first. Every node shipped with #2645 and without #3219 answered the watch
// and then pushed nothing, so a second message and what it started showed up only after a reload.
func TestSessionWatchOutlivesTheWatchRequest(t *testing.T) {
	n, home := testNode(t)
	tp := filepath.Join(home, ".claude", "projects", "-src-app", watchedSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(tp), 0o700); err != nil {
		t.Fatal(err)
	}
	appendLines(t, tp,
		`{"type":"user","uuid":"u1","timestamp":"2026-11-06T03:00:00Z","message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-06T03:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"first reply"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"d1","timestamp":"2026-11-06T03:00:03Z","durationMs":3000}`)
	n.paths[watchedSession] = tp
	call, hub := servedNode(t, n)
	if err := call("session.watch", map[string]any{"session_id": watchedSession, "since_seq": 0}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.unwatch(watchedSession) })

	appendLines(t, tp,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-06T03:01:00Z","message":{"role":"user","content":"second"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-06T03:01:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"date"}}]}}`,
		`{"type":"user","uuid":"r2","timestamp":"2026-11-06T03:01:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"Fri"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-06T03:01:03Z","message":{"role":"assistant","content":[{"type":"text","text":"second reply"}]}}`)
	waitFor(t, "the second exchange to be pushed after the reply to session.watch", func() bool { return hasText(hub, "second reply") })

	seqOf := func(match func(proto.Item) bool) int {
		for _, it := range hub.got() {
			if match(it) {
				return it.Seq
			}
		}
		return -1
	}
	msg := seqOf(func(it proto.Item) bool { return it.Role == "user" && it.Kind == proto.KindText && it.Text == "second" })
	tool := seqOf(func(it proto.Item) bool { return it.Kind == proto.KindToolUse && it.ToolUseID == "toolu_2" })
	reply := seqOf(func(it proto.Item) bool { return it.Text == "second reply" })
	if msg < 0 || tool < 0 {
		t.Fatalf("pushed without the second message (%d) or its tool call (%d)", msg, tool)
	}
	if !(msg < tool && tool < reply) {
		t.Fatalf("the second message must come before its work: message %d, tool %d, reply %d", msg, tool, reply)
	}
}
