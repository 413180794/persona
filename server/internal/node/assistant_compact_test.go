package node

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// The user's compaction: refused while the conversation answers or compacts, /compact sent otherwise; it is running
// until the boundary comes (done), the turn ends without one (failed, with claude's words), or the process is gone.
func TestAssistantManualCompaction(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	in := &stdinBuf{}
	p := &assistantProc{id: id, agent: "claude", stdin: in, busy: true}
	n.procs[id] = p
	stream := func(lines ...string) { n.readAssistant(p, strings.NewReader(strings.Join(lines, "\n")+"\n")) }
	state := func() proto.Compaction {
		t.Helper()
		st, _ := n.state.get(id)
		if st.Compaction == nil {
			t.Fatal("no compaction recorded")
		}
		return *st.Compaction
	}

	if code := refusalCode(n.assistantCompact(t.Context(), id)); code != "busy" {
		t.Fatalf("while it answers: %q", code)
	}
	p.busy = false
	if err := n.assistantCompact(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(in.String()); got != `{"message":{"content":"/compact","role":"user"},"type":"user"}` {
		t.Fatalf("sent %s", got)
	}
	if c := state(); c.State != proto.CompactionRunning || c.StartedAt.IsZero() || c.FinishedAt != nil {
		t.Fatalf("started: %+v", c)
	}
	if code := refusalCode(n.assistantCompact(t.Context(), id)); code != "compacting" {
		t.Fatalf("twice: %q", code)
	}
	stream(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"manual","pre_tokens":180902,"post_tokens":9500}}`,
		`{"type":"result","subtype":"success"}`)
	if c := state(); c.State != proto.CompactionDone || c.FinishedAt == nil || c.Error != "" {
		t.Fatalf("after the boundary: %+v", c)
	}
	// An automatic compaction in a turn of its own is not the user's: what they see stays.
	stream(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"auto"}}`)
	if c := state(); c.State != proto.CompactionDone {
		t.Fatalf("an automatic one: %+v", c)
	}

	if err := n.assistantCompact(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	stream(`{"type":"result","subtype":"success","is_error":false,"result":"Error: Not enough messages to compact."}`)
	if c := state(); c.State != proto.CompactionFailed || c.Error != "Error: Not enough messages to compact." {
		t.Fatalf("without a boundary: %+v", c)
	}
	p.mu.Lock()
	compacting := p.compacting
	p.mu.Unlock()
	if compacting {
		t.Fatal("still compacting after the turn ended")
	}

	if err := n.assistantCompact(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	n.sweepCompactions()
	if c := state(); c.State != proto.CompactionRunning {
		t.Fatalf("its process is still there: %+v", c)
	}
	delete(n.procs, id) // it exited, or the node restarted under it
	n.sweepCompactions()
	if c := state(); c.State != proto.CompactionFailed || !strings.Contains(c.Error, "进程退出") {
		t.Fatalf("its process is gone: %+v", c)
	}
	s := proto.Session{SessionID: id}
	n.decorate(&s)
	if s.Compaction == nil || s.Compaction.State != proto.CompactionFailed {
		t.Fatalf("on the session list: %+v", s.Compaction)
	}
}

// What a new main conversation is told: the last compaction's summary (Claude Code's or Codex's), and the messages
// after it, the newest kept.
func TestHandoffReadsTheLastSummaryAndWhatCameAfter(t *testing.T) {
	jsonl := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"第一个问题"}}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"旧的摘要"}}`,
		`{"type":"system","subtype":"compact_boundary"}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":[{"type":"text","text":"示例摘要"}]}}`,
		`{"type":"user","message":{"role":"user","content":"之后的问题"}}`,
	}, "\n")
	summary, at := lastCompactSummary(strings.NewReader(jsonl))
	if summary != "示例摘要" || at != 4 {
		t.Fatalf("claude: %q at %d", summary, at)
	}
	if summary, at := lastCompactSummary(strings.NewReader(`{"type":"compacted","payload":{"message":"Codex 的摘要"}}`)); summary != "Codex 的摘要" || at != 1 {
		t.Fatalf("codex: %q at %d", summary, at)
	}
	if summary, at := lastCompactSummary(strings.NewReader(`{"type":"user","message":{"role":"user","content":"x"}}`)); summary != "" || at != 0 {
		t.Fatalf("never compacted: %q at %d", summary, at)
	}

	items := []proto.Item{
		{Seq: 1, Role: "user", Kind: proto.KindText, Text: "压缩前的问题"},
		{Seq: 5, Role: "user", Kind: proto.KindText, Text: "<persona-context kind=\"context\" title=\"接上旧主对话\" url=\"\">\n更早的\n</persona-context>\n<persona-context>\n快照\n</persona-context>\n之后的问题"},
		{Seq: 6, Role: "assistant", Kind: proto.KindToolUse, Text: "工具"},
		{Seq: 7, Role: "assistant", Kind: proto.KindText, Text: strings.Repeat("长", handoffTurnMax+50)},
		{Seq: 8, Role: "system", Kind: proto.KindNotice, Text: "提示"},
		{Seq: 9, Role: "user", Kind: proto.KindText, Text: "<persona-context kind=\"context\" title=\"接上旧主对话\" url=\"\">\n更早的\n</persona-context>\n<persona-context>\n快照\n</persona-context>\n"},
	}
	got := recentTurns(items, 4)
	if len(got) != 2 || got[0] != (HandoffTurn{Role: "user", Text: "之后的问题"}) || got[1].Role != "assistant" ||
		len([]rune(got[1].Text)) != handoffTurnMax+1 {
		t.Fatalf("recent: %+v", got)
	}
	var many []proto.Item
	for i := range 40 {
		many = append(many, proto.Item{Seq: i + 1, Role: "user", Kind: proto.KindText, Text: strings.Repeat("字", 700)})
	}
	got = recentTurns(many, 0)
	total := 0
	for _, t := range got {
		total += len([]rune(t.Text))
	}
	if len(got) == 0 || len(got) > handoffTurns || total > handoffRecentMax || got[len(got)-1].Text != many[39].Text {
		t.Fatalf("bounded, newest kept: %d messages, %d characters", len(got), total)
	}
}
