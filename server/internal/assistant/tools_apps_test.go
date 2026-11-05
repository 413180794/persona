package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// runTool runs one of the assistant's tools for conversation self on machine mac: its text, or the error it said.
func runTool(s *Service, self, name string, args any) (string, error) {
	raw, _ := json.Marshal(args)
	r := s.Tool(context.Background(), ToolCall{Name: name, Arguments: raw, MachineID: "mac", SessionID: self})
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	if r.IsError {
		return "", errors.New(b.String())
	}
	return b.String(), nil
}

// ---- kb_* ----

// The kb_* tools read the assistant's knowledge base and write what the model reads: defaults and caps of their
// arguments, the lines, the list, an entry with its meta.json as the file has it and dates in the user's zone.
func TestKBToolsFormatTheData(t *testing.T) {
	f := newFakeHub()
	s := newKBService(t, f)
	setSettings(t, s, func(v *Settings) { v.Timezone = "Asia/Shanghai" })
	e := kbEng(t, s)
	dir := e.Store.Dir
	c := &kb.Chat{ID: kb.ChatID("lark", "f1"), Source: "lark", SourceID: "f1", Kind: "group", Name: "发布群", Created: time.Now()}
	_ = e.Store.PutChat(c)
	_ = e.Store.AppendMessages(c, []kb.Line{{At: time.Now(), Sender: "张三", Text: "周五灰度"}, {At: time.Now(), Sender: "李四", Text: "灰度名单"}})

	out, err := runTool(s, "c1", "kb_search", map[string]any{"query": "灰度"})
	if err != nil || strings.Count(out, "\n") != 2 || !strings.Contains(out, "chats/lark-f1/messages/") || !strings.Contains(out, ": - ") {
		t.Fatalf("search: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_search", map[string]any{"query": "灰度", "limit": 1}); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("search, one: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_search", map[string]any{"query": "灰度", "messages": false}); err != nil || out != "No match in "+dir+"." {
		t.Fatalf("search without the messages: %q %v", out, err)
	}

	if out, err := runTool(s, "c1", "kb_update", map[string]any{"kind": "projects", "id": "Jarvis 发布", "project_kind": "long", "importance": "P0",
		"relevance": 95, "tags": []string{"发布", "客户端"}, "log": "定了周五灰度", "chats": []string{"lark-f1"}}); err != nil || out != "Saved projects/jarvis-发布." {
		t.Fatalf("update: %q %v", out, err)
	}
	if _, err := runTool(s, "c1", "kb_update", map[string]any{"kind": "projects", "id": "别的"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runTool(s, "c1", "kb_update", map[string]any{"kind": "projects", "id": "x", "relevance": "high"}); err == nil {
		t.Fatal("an update that does not parse was applied")
	}
	if _, ok := e.Store.Project("x"); ok {
		t.Fatal("an update that does not parse made a project")
	}

	out, err = runTool(s, "c1", "kb_list", map[string]any{"kind": "projects"})
	if err != nil || !strings.HasPrefix(out, "- jarvis-发布 · Jarvis 发布 [P0 相关95] 发布,客户端 ") || !strings.Contains(out, "\n- 别的 · 别的 [- 相关0] ") {
		t.Fatalf("list: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_list", map[string]any{"kind": "projects", "limit": 1}); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("list, one: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_list", map[string]any{"kind": "meetings"}); err != nil || out != "Nothing yet." {
		t.Fatalf("list, empty: %q %v", out, err)
	}

	// A project's 公司圈 posts, dated in the user's zone (20:00 UTC is the next day in Shanghai).
	if err := e.Store.PutMomentPost(&kb.MomentPost{ID: "p1", Author: "王五", Text: "发布了", CommentCount: 2,
		At: time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC)}, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := e.Store.Project("jarvis-发布")
	p.Moments = []kb.ProjectMoment{{ID: "p1", Note: "讲发布"}}
	_ = e.Store.PutProject(p)
	_ = e.Store.PutSummary(kb.KindProject, "jarvis-发布", "在推发布")
	out, err = runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "Jarvis 发布"})
	if err != nil || !strings.HasPrefix(out, "Path: "+dir+"/projects/jarvis-发布\n\nmeta.json:\n{\n  \"") ||
		!strings.Contains(out, "}\n\n\nsummary.md:\n在推发布\n") || !strings.Contains(out, "\nlog.md (latest):\n- ") || !strings.Contains(out, "定了周五灰度") ||
		!strings.Contains(out, "\n公司圈 posts about it (full text: moments/posts/<id>.md):\n- p1 2026-11-04 · 2 comments · 讲发布 — 发布了\n") {
		t.Fatalf("get:\n%s (%v)", out, err)
	}
	// A chat's latest messages; meta.json as the file has it.
	meta, _ := os.ReadFile(filepath.Join(dir, "chats", "lark-f1", "meta.json"))
	if out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "chats", "id": "lark-f1"}); err != nil ||
		!strings.Contains(out, "meta.json:\n"+strings.TrimSpace(string(meta))+"\n\n") || !strings.Contains(out, "\nMessages, last 7 days:\n") ||
		!strings.Contains(out, "周五灰度") {
		t.Fatalf("get chat: %q %v", out, err)
	}
	if _, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "nope"}); err == nil || err.Error() != "projects/nope: not found" {
		t.Fatalf("get, not found: %v", err)
	}
	// One edited by hand into something else reads as its text.
	_ = os.WriteFile(filepath.Join(dir, "chats", "lark-f1", "meta.json"), []byte("not json"), 0o600)
	if out, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "chats", "id": "lark-f1"}); err != nil || !strings.Contains(out, "meta.json:\nnot json\n") {
		t.Fatalf("get, meta not JSON: %q %v", out, err)
	}
}

// Without a knowledge base (hub.json kb.enabled false) the kb_* tools say it is off (and how to turn it on).
func TestKBToolsKnowledgeBaseOff(t *testing.T) {
	f := newFakeHub()
	f.hello.KB = &KBSettings{Off: true}
	s := withSettings(t, f, New(Options{Hub: f, Logf: testLogf(t), KB: &KBOptions{Dir: t.TempDir()}}))
	for _, name := range []string{"kb_search", "kb_list", "kb_get", "kb_update"} {
		if _, err := runTool(s, "c1", name, map[string]any{"kind": "projects", "id": "x", "query": "x"}); err == nil ||
			err.Error() != "the knowledge base is off (hub.json kb.enabled)" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if s.kbReady(context.Background()) || s.kb.dir == "" {
		t.Fatal("off, yet ready; or its directory forgotten (CLAUDE.md names it)")
	}
}

// ---- interaction_answer ----

// answerService is an assistant whose hub has machine mac with an agent (abcdef12-agent) whose question 发吗？ waits,
// and the assistant's own conversation conv-assistant there.
func answerService(t *testing.T) (*Service, *fakeHub, *fakeQueue, proto.Interaction) {
	t.Helper()
	f := newFakeHub()
	f.sessions["mac"] = []proto.Session{
		{SessionID: "abcdef12-agent", MachineID: "mac", Title: "修登录", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: time.Now().UTC()},
		{SessionID: "conv-assistant", MachineID: "mac", Title: "Persona", Assistant: true, UpdatedAt: time.Now().UTC()},
	}
	ix := askOn("ix_ask1", "发吗？", false, "发", "不发")
	q := f.withQueue(ix)
	return newTestService(t, f), f, q, ix
}

// askOn is abcdef12-agent's question on mac, waiting for an hour.
func askOn(id, text string, multi bool, options ...string) proto.Interaction {
	q := proto.Question{Question: text, MultiSelect: multi}
	for _, o := range options {
		q.Options = append(q.Options, proto.QuestionOption{Label: o})
	}
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxQuestion, Questions: []proto.Question{q},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
}

func answerArgs(id string, answers any) map[string]any {
	return map[string]any{"machine": "mac", "session_id": "abcdef12", "interaction_id": id, "answers": answers}
}

// A question another agent asks is answered for the user: the hub gets the answers by the question's wording.
func TestInteractionAnswerAnswersAQuestion(t *testing.T) {
	s, f, q, ix := answerService(t)
	out, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(" "+ix.ID+" ", map[string]string{"发吗？": "发"}))
	if err != nil || out != "Answered for the user:\n- 发吗？ → 发" {
		t.Fatalf("result: %q %v", out, err)
	}
	if q.answered[ix.ID]["发吗？"] != "发" {
		t.Fatalf("answered: %v", q.answered)
	}
	if p := f.appParams(AppInteractions, assistantwire.InteractionsAnswer); len(p) != 1 || p[0]["machine_id"] != "mac" ||
		p[0]["session_id"] != "abcdef12-agent" || p[0]["interaction_id"] != ix.ID {
		t.Fatalf("answer params: %v", p)
	}
}

// Only questions may be answered for the user; an approval is theirs alone.
func TestInteractionAnswerRefusesApprovals(t *testing.T) {
	for _, ix := range []proto.Interaction{
		{Kind: proto.IxPermission, ToolName: "Bash", Summary: "rm -rf"},
		{Kind: proto.IxPlan, Plan: "1. do it"},
		{Kind: proto.IxScreen},
	} {
		s, _, q, _ := answerService(t)
		ix.ID, ix.MachineID, ix.SessionID = "ix_"+ix.Kind, "mac", "abcdef12-agent"
		ix.CreatedAt, ix.ExpiresAt = time.Now().UTC(), time.Now().Add(time.Hour).UTC()
		q.pending[ix.ID] = ix
		_, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(ix.ID, map[string]string{"any": "yes"}))
		if err == nil || !strings.Contains(err.Error(), ix.Kind) || !strings.Contains(err.Error(), "only the user") {
			t.Fatalf("%s: %v", ix.Kind, err)
		}
		if len(q.answered) != 0 {
			t.Fatalf("%s answered", ix.Kind)
		}
	}
}

// The assistant never answers its own conversation's question, nor another assistant conversation's.
func TestInteractionAnswerRefusesAssistantConversation(t *testing.T) {
	s, _, q, ix := answerService(t)
	if _, err := runTool(s, ix.SessionID, "interaction_answer", answerArgs(ix.ID, map[string]string{"发吗？": "发"})); err == nil ||
		!strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("own conversation: %v", err)
	}
	own := askOn("ix_ask_user", "发吗？", false, "发")
	own.SessionID = "conv-assistant"
	q.pending[own.ID] = own
	args := answerArgs(own.ID, map[string]string{"发吗？": "发"})
	args["session_id"] = "conv-assist"
	if _, err := runTool(s, "conv-other", "interaction_answer", args); err == nil || !strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("ask_user: %v", err)
	}
	if len(q.answered) != 0 {
		t.Fatalf("answered: %v", q.answered)
	}
}

// The answers are checked against the question before anything is answered.
func TestInteractionAnswerValidatesAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers any
		want    string
	}{
		{"no answers", nil, "answers is required"},
		{"a question left unanswered", map[string]any{}, "every question"},
		{"a key that is not the question", map[string]any{"another question": "发"}, "not a question"},
		{"an option that is not offered", map[string]any{"发吗？": "也许"}, "发, 不发"},
		{"an answer with no option at all", map[string]any{"发吗？": ""}, "发, 不发"},
		{"not a label", map[string]any{"发吗？": 1}, "takes an option label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, q, ix := answerService(t)
			args := answerArgs(ix.ID, tc.answers)
			if tc.answers == nil {
				delete(args, "answers")
			}
			_, err := runTool(s, "conv-assistant", "interaction_answer", args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if len(q.answered) != 0 {
				t.Fatal("a refused answer was given")
			}
		})
	}
}

// A single-choice question takes one label; a multi-select takes the labels the app shows, "A, B".
func TestInteractionAnswerSingleAndMultiSelect(t *testing.T) {
	s, _, q, ix := answerService(t)
	multi := askOn("ix_multi", "哪几个？", true, "A", "B", "C")
	q.pending[multi.ID] = multi
	if out, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(multi.ID, map[string]any{"哪几个？": []string{"A", "C"}})); err != nil ||
		!strings.Contains(out, "哪几个？ → A, C") {
		t.Fatalf("multi-select: %q %v", out, err)
	}
	if got := q.answered[multi.ID]["哪几个？"]; got != "A, C" {
		t.Fatalf("multi-select answer: %q", got)
	}
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(ix.ID, map[string]any{"发吗？": []string{"发", "不发"}})); err == nil ||
		!strings.Contains(err.Error(), "one option") {
		t.Fatalf("two labels on a single-choice question: %v", err)
	}
	if _, answered := q.answered[ix.ID]; answered {
		t.Fatal("a refused answer was given")
	}
}

// An expired question, one answered already (before it was read, or while the answer was on its way) and one that
// never existed each say which.
func TestInteractionAnswerExpiredAnsweredAndUnknown(t *testing.T) {
	s, _, q, _ := answerService(t)
	expired := askOn("ix_expired", "发吗？", false, "发")
	expired.CreatedAt, expired.ExpiresAt = time.Now().Add(-2*time.Hour).UTC(), time.Now().Add(-time.Hour).UTC()
	q.pending[expired.ID] = expired
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(expired.ID, map[string]string{"发吗？": "发"})); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired: %v", err)
	}

	for status, want := range map[string]string{"resolved": "was already answered", "dismissed": "was dismissed", "expired": "expired",
		"cancelled": "was cancelled", "": "no pending interaction ix_x_ (use agents_list)"} {
		id := "ix_x_" + status
		q.ended[id] = status
		if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(id, map[string]string{"发吗？": "发"})); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Fatalf("%q: %v", status, err)
		}
	}

	raced := askOn("ix_raced", "发吗？", false, "发")
	q.pending[raced.ID], q.raced[raced.ID] = raced, "resolved"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(raced.ID, map[string]string{"发吗？": "发"})); err == nil ||
		err.Error() != "interaction ix_raced was already answered" {
		t.Fatalf("answered meanwhile: %v", err)
	}
}

// The machine and session named must be the interaction's own.
func TestInteractionAnswerKeepsToItsTarget(t *testing.T) {
	s, f, q, ix := answerService(t)
	args := answerArgs(ix.ID, map[string]string{"发吗？": "发"})
	args["machine"] = "another"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", args); err == nil {
		t.Fatal("a different machine was accepted")
	}
	f.setSessions("box", proto.Session{SessionID: "99999999-agent", MachineID: "box", Title: "别的", Cwd: "/code/other", UpdatedAt: time.Now().UTC()})
	args["machine"], args["session_id"] = "box", "99999999"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", args); err == nil || !strings.Contains(err.Error(), "belongs to another agent") {
		t.Fatalf("a session that is not the interaction's: %v", err)
	}
	if len(q.answered) != 0 {
		t.Fatal("a refused answer was given")
	}
}

// ---- inbox_draft ----

// inbox_draft's rules: a draft with text and no blank left to fill in, trimmed, the note clipped; it goes on the item
// whose AI 分身 runs in the conversation, and a change the item does not offer is refused.
func TestInboxDraftRules(t *testing.T) {
	h := newInboxHub(t)
	old := inboxDraftWait
	inboxDraftWait = 50 * time.Millisecond
	t.Cleanup(func() { inboxDraftWait = old })
	doc := &InboxItem{ID: "doc:1", Source: "lark", Kind: KindDoc, Last: "1", UpdatedAt: time.Now(),
		Doc:   &InboxDoc{NoticeID: "n1", Rows: []DocRow{{Ref: "r1", Status: []DocStatus{{Field: "状态", Options: []DocOption{{Name: "完成"}}}}}}},
		Agent: &InboxAgent{State: AgentRunning, SessionID: "s1"}}
	h.inboxReady(context.Background())
	h.inbox.mu.Lock()
	h.inbox.items[doc.ID] = doc
	h.inbox.mu.Unlock()
	s := h.Service

	for _, args := range []map[string]any{{"text": "  "}, {"note": "n"}, {"text": "hi", "changes": "x"}} {
		if _, err := runTool(s, "s1", "inbox_draft", args); err == nil || err.Error() != "text is required" {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, text := range []string{"修好了，【预计今晚 / 几点前】合进去", "MR 在 {{链接}}"} {
		if _, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": text}); err == nil || !strings.Contains(err.Error(), "fill in") {
			t.Fatalf("%q: %v", text, err)
		}
	}
	if doc.Agent.State != AgentRunning || doc.Agent.Draft != "" {
		t.Fatalf("a refused draft went to the inbox: %+v", doc.Agent)
	}
	if draftPlaceholder("【通知】 不算吗") == "" || draftPlaceholder("周五前给你") != "" {
		t.Fatal("draftPlaceholder")
	}

	note := strings.Repeat("查", 700)
	out, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": " 合了，MR 已经进主干 ", "note": " " + note + " ",
		"changes": []map[string]string{{"ref": "r1", "field": "状态", "value": "完成"}}})
	if err != nil || out != "The draft is in the user's inbox; they will send it. You're done." {
		t.Fatalf("draft: %q %v", out, err)
	}
	if a := doc.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 已经进主干" || a.Note != strings.Repeat("查", 600)+"…" ||
		len(a.Changes) != 1 || a.Changes[0].Ref != "r1" {
		t.Fatalf("the draft on the item: %+v", a)
	}

	if _, err := runTool(s, "s9", "inbox_draft", map[string]any{"text": "hi"}); err == nil ||
		err.Error() != "this conversation was not started for an inbox item" {
		t.Fatalf("not an inbox agent: %v", err)
	}
	if _, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": "hi", "changes": []map[string]string{{"ref": "r1", "field": "状态", "value": "作废"}}}); err == nil ||
		err.Error() != "change {Ref:r1 Field:状态 Value:作废} is not one the item lists (ref, field and one of its values)" {
		t.Fatalf("a change the item does not offer: %v", err)
	}
}
