package assistant

import (
	"context"
	"encoding/json"
	"errors"
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

// The kb_* tools read the hub's knowledge base as data and write what the model reads: defaults and caps of their
// arguments, the lines, the list, an entry with its meta.json as the file has it and dates in the user's zone.
func TestKBToolsFormatTheData(t *testing.T) {
	f := newFakeHub()
	f.kbReady = true
	hits := []kb.Hit{{Path: "messages/lark-f1/2026-11-04.md", Line: "张三：周五灰度"}}
	f.answerApp(AppKB, assistantwire.KBSearch, func(json.RawMessage) (any, error) {
		out := hits
		hits = nil // found nothing the next time
		return map[string]any{"dir": "/data", "hits": out}, nil
	})
	items := []kb.Entry{{Kind: "projects", ID: "jarvis-发布", Name: "Jarvis 发布", Importance: "P0", Relevance: 95, Tags: []string{"发布", "客户端"},
		Extra: "long · 进行中", OneLine: "周五灰度"}, {Kind: "projects", ID: "other", Name: "别的"}}
	f.answerApp(AppKB, assistantwire.KBList, func(json.RawMessage) (any, error) {
		out := items
		items = []kb.Entry{}
		return map[string]any{"items": out}, nil
	})
	f.answerApp(AppKB, assistantwire.KBGet, func(params json.RawMessage) (any, error) {
		var a struct{ ID string }
		_ = json.Unmarshal(params, &a)
		if a.ID == "nope" {
			return nil, errors.New("not found")
		}
		return map[string]any{"detail": kb.Detail{Kind: "projects", ID: "jarvis-发布", Path: "/data/projects/jarvis-发布",
			Meta: json.RawMessage(`{"name":"Jarvis 发布","tags":["发布"]}`), Summary: "在推发布", Log: []string{"- 定了周五灰度"},
			Moments: []kb.MomentRef{{ID: "p1", At: time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC), Comments: 2, Note: "讲发布", Text: "发布了"}},
			Recent:  "张三：周五灰度"}}, nil
	})
	f.answerApp(AppKB, assistantwire.KBUpdate, func(json.RawMessage) (any, error) {
		return map[string]any{"id": "jarvis-发布"}, nil
	})
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.Timezone = "Asia/Shanghai" })

	if out, err := runTool(s, "c1", "kb_search", map[string]any{"query": "灰度"}); err != nil || out != "messages/lark-f1/2026-11-04.md: 张三：周五灰度\n" {
		t.Fatalf("search: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_search", map[string]any{"query": "灰度", "messages": false, "limit": 500}); err != nil || out != "No match in /data." {
		t.Fatalf("search, nothing found: %q %v", out, err)
	}
	if p := f.appParams(AppKB, assistantwire.KBSearch); len(p) != 2 || p[0]["messages"] != true || p[0]["limit"] != 40.0 ||
		p[1]["messages"] != false || p[1]["limit"] != 200.0 || p[1]["query"] != "灰度" {
		t.Fatalf("search params: %v", p)
	}

	out, err := runTool(s, "c1", "kb_list", map[string]any{"kind": "projects"})
	if err != nil || out != "- jarvis-发布 · Jarvis 发布 [P0 相关95] 发布,客户端 long · 进行中 — 周五灰度\n- other · 别的 [- 相关0]  \n" {
		t.Fatalf("list: %q %v", out, err)
	}
	if out, err := runTool(s, "c1", "kb_list", map[string]any{"kind": "chats", "tag": "发布", "limit": 5}); err != nil || out != "Nothing yet." {
		t.Fatalf("list, empty: %q %v", out, err)
	}
	if p := f.appParams(AppKB, assistantwire.KBList); len(p) != 2 || p[0]["limit"] != 30.0 || p[1]["limit"] != 5.0 || p[1]["tag"] != "发布" {
		t.Fatalf("list params: %v", p)
	}

	out, err = runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "Jarvis 发布"})
	want := "Path: /data/projects/jarvis-发布\n\nmeta.json:\n{\n  \"name\": \"Jarvis 发布\",\n  \"tags\": [\n    \"发布\"\n  ]\n}\n\n" +
		"\nsummary.md:\n在推发布\n\nlog.md (latest):\n- 定了周五灰度\n" +
		"\n公司圈 posts about it (full text: moments/posts/<id>.md):\n- p1 2026-11-04 · 2 comments · 讲发布 — 发布了\n" +
		"\nMessages, last 7 days:\n张三：周五灰度\n"
	if err != nil || out != want {
		t.Fatalf("get:\n%q\nwant\n%q (%v)", out, want, err)
	}
	if _, err := runTool(s, "c1", "kb_get", map[string]any{"kind": "projects", "id": "nope"}); err == nil || err.Error() != "projects/nope: not found" {
		t.Fatalf("get, not found: %v", err)
	}
	if p := f.appParams(AppKB, assistantwire.KBGet); len(p) != 2 || p[0]["id"] != "Jarvis 发布" || p[0]["recent"] != 12000.0 {
		t.Fatalf("get params: %v", p)
	}

	if out, err := runTool(s, "c1", "kb_update", map[string]any{"kind": "projects", "id": "Jarvis 发布", "project_kind": "long",
		"relevance": 95, "log": "定了周五灰度", "chats": []string{"lark-f1"}}); err != nil || out != "Saved projects/jarvis-发布." {
		t.Fatalf("update: %q %v", out, err)
	}
	p := f.appParams(AppKB, assistantwire.KBUpdate)
	if len(p) != 1 || p[0]["kind"] != "projects" || p[0]["id"] != "Jarvis 发布" {
		t.Fatalf("update params: %v", p)
	}
	if u, _ := p[0]["update"].(map[string]any); u["project_kind"] != "long" || u["relevance"] != 95.0 || u["log"] != "定了周五灰度" || u["kind"] != nil {
		t.Fatalf("update: %v", p[0]["update"])
	}
	if _, err := runTool(s, "c1", "kb_update", map[string]any{"kind": "projects", "id": "x", "relevance": "high"}); err == nil {
		t.Fatal("an update that does not parse was sent")
	}
	if len(f.appParams(AppKB, assistantwire.KBUpdate)) != 1 {
		t.Fatal("an update that does not parse went to the hub")
	}
}

// Without a knowledge base on the hub the kb_* tools say it is off (and how to turn it on).
func TestKBToolsKnowledgeBaseOff(t *testing.T) {
	f := newFakeHub()
	for _, m := range []string{assistantwire.KBSearch, assistantwire.KBList, assistantwire.KBGet, assistantwire.KBUpdate} {
		f.answerApp(AppKB, m, func(json.RawMessage) (any, error) { return nil, errors.New("no knowledge base") })
	}
	s := newTestService(t, f)
	for _, name := range []string{"kb_search", "kb_list", "kb_get", "kb_update"} {
		if _, err := runTool(s, "c1", name, map[string]any{"kind": "projects", "id": "x", "query": "x"}); err == nil ||
			err.Error() != "the knowledge base is off (hub.json kb.enabled)" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// With one, the hub's error is said as it is.
	f.mu.Lock()
	f.kbReady = true
	f.mu.Unlock()
	if _, err := runTool(s, "c1", "kb_search", map[string]any{"query": "x"}); err == nil || err.Error() != "no knowledge base" {
		t.Fatalf("an error with the knowledge base on: %v", err)
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

// inbox_draft's rules: a draft with text and no blank left to fill in, trimmed, the note clipped; the hub says whose
// item it is and which change it does not offer.
func TestInboxDraftRules(t *testing.T) {
	f := newFakeHub()
	answer := map[string]any{"item_id": "lark:f1:1"}
	f.answerApp(AppInbox, assistantwire.InboxDraft, func(json.RawMessage) (any, error) { return answer, nil })
	s := newTestService(t, f)

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
	if n := len(f.appParams(AppInbox, assistantwire.InboxDraft)); n != 0 {
		t.Fatalf("%d refused drafts went to the inbox", n)
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
	p := f.appParams(AppInbox, assistantwire.InboxDraft)
	if len(p) != 1 || p[0]["session_id"] != "s1" || p[0]["text"] != "合了，MR 已经进主干" || p[0]["note"] != strings.Repeat("查", 600)+"…" {
		t.Fatalf("draft params: %v", p)
	}
	if ch, _ := p[0]["changes"].([]any); len(ch) != 1 || ch[0].(map[string]any)["ref"] != "r1" {
		t.Fatalf("changes: %v", p[0]["changes"])
	}

	answer = map[string]any{"item_id": ""}
	if _, err := runTool(s, "s9", "inbox_draft", map[string]any{"text": "hi"}); err == nil ||
		err.Error() != "this conversation was not started for an inbox item" {
		t.Fatalf("not an inbox agent: %v", err)
	}
	answer = map[string]any{"item_id": "doc:1", "refused": map[string]string{"ref": "r1", "field": "状态", "value": "完成"}}
	if _, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": "hi"}); err == nil ||
		err.Error() != "change {Ref:r1 Field:状态 Value:完成} is not one the item lists (ref, field and one of its values)" {
		t.Fatalf("a change the item does not offer: %v", err)
	}
}
