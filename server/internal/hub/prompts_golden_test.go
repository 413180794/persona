package hub

// Golden tests for every prompt the hub writes (docs/prompts.md): representative inputs, the output compared byte for
// byte with testdata/prompts/<name>.golden. After changing a prompt on purpose, regenerate with
//
//	UPDATE_GOLDEN=1 go test ./internal/hub -run Golden
//
// and read the diff before committing it. All inputs are placeholders.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_GOLDEN=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

// capturedCall is one request a captureModel answered.
type capturedCall struct{ System, User string }

// captureModel is a model that records what it is asked and answers reply.
func captureModel(t *testing.T, reply string) (*ai.Client, func() []capturedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []capturedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			System   string `json:"system"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		c := capturedCall{System: in.System}
		if len(in.Messages) > 0 {
			c.User = in.Messages[0].Content
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
	}))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	return ai.New(ai.Config{BaseURL: srv.URL, APIKeyFile: key, Model: "m"}), func() []capturedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedCall(nil), calls...)
	}
}

func onlyCall(t *testing.T, calls []capturedCall) capturedCall {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("%d model calls, want 1", len(calls))
	}
	return calls[0]
}

func goldenCall(t *testing.T, name string, c capturedCall) {
	t.Helper()
	golden(t, name+".system", c.System)
	golden(t, name+".user", c.User)
}

// fakelark-connector serves the lark-connector reads the prompts are built from.
func fakelark-connector(t *testing.T) *lark.Client {
	t.Helper()
	at := func(s string) string { return s } // times are RFC 3339 literals below
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conversations", func(w http.ResponseWriter, r *http.Request) {
		preview := func(s string) map[string]any {
			return map[string]any{"digest": map[string]any{"elements": []any{map[string]any{"kind": "text", "content": s}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{"feedId": "f1", "name": "发布群", "isGroup": true, "unreadCount": 3, "hasUnreadAtMe": true, "preview": preview("@我 周五灰度能不能提前到周四？这条预览很长很长很长很长很长很长很长很长很长很长很长")},
			map[string]any{"feedId": "f2", "name": "张三", "unreadCount": 1, "preview": preview("在吗")},
			map[string]any{"feedId": "f3", "name": "闲聊群", "isGroup": true, "unreadCount": 40, "isMuted": true},
			map[string]any{"feedId": "f4", "name": "已读群", "isGroup": true},
			map[string]any{"feedId": "f5", "name": "李四", "isMarkedUnread": true},
		}})
	})
	mux.HandleFunc("GET /calendar/week", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{
			map[string]any{"title": "周会", "startAt": at("2099-01-01T11:00:00+08:00"), "endAt": at("2099-01-01T12:00:00+08:00")},
			map[string]any{"title": "评审", "startAt": at("2099-01-01T09:30:00+08:00"), "endAt": at("2099-01-01T10:00:00+08:00")},
			map[string]any{"title": "早就开完的会", "startAt": at("2000-01-01T09:00:00+08:00"), "endAt": at("2000-01-01T10:00:00+08:00")},
			map[string]any{"title": "拒绝了的会", "startAt": at("2099-01-01T13:00:00+08:00"), "endAt": at("2099-01-01T14:00:00+08:00"), "selfAttendeeStatus": 4},
			map[string]any{"title": "全天", "startAt": at("2099-01-01T00:00:00+08:00"), "endAt": at("2099-01-02T00:00:00+08:00"), "allDay": true},
		}})
	})
	msgs := []any{
		map[string]any{"id": "m1", "senderName": "张三", "messageTypeName": "TEXT", "textPreview": "构建挂了，看下 https://example.feishu.cn/docx/AbC123 里的步骤", "createdAt": at("2026-10-31T09:00:00+08:00")},
		map[string]any{"id": "m2", "senderName": "李四", "messageTypeName": "TEXT", "textPreview": "我也遇到了", "createdAt": at("2026-10-31T09:01:00+08:00"), "isRecalled": true},
		map[string]any{"id": "m0", "messageTypeName": "SYSTEM", "textPreview": "张三 加入了群", "createdAt": at("2026-10-31T08:00:00+08:00")},
	}
	mux.HandleFunc("GET /conversations/{feed}/messages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": msgs})
	})
	mux.HandleFunc("GET /messages/{id}/thread", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": append(append([]any{}, msgs...),
			map[string]any{"id": "req", "senderName": "我", "isMine": true, "messageTypeName": "TEXT", "textPreview": "/agent 看下怎么修", "createdAt": at("2026-10-31T09:05:00+08:00")})})
	})
	mux.HandleFunc("GET /documents/read", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("url"), "Broken") {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "NOT_FOUND", "message": "document not found"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "构建手册", "markdown": "# 构建手册\n\n1. 清缓存\n2. 重跑"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return lark.New(lark.Config{BaseURL: srv.URL, Token: "t"})
}

// goldenKB is a knowledge base with one chat, its summary and two projects.
func goldenKB(t *testing.T) *kb.Engine {
	t.Helper()
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &kb.Engine{Store: st, Owner: "示例用户"}
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	for _, p := range []*kb.Project{
		{Slug: "demo-release", Name: "示例发布", Kind: "short", Status: "active", Tagging: kb.Tagging{Importance: "P0", Relevance: 95}, OneLine: "周五灰度", Updated: now},
		{Slug: "demo-infra", Name: "示例基建", Kind: "long", Status: "active", Tagging: kb.Tagging{Importance: "P2", Relevance: 40}, OneLine: "", Updated: now},
		{Slug: "demo-old", Name: "做完的项目", Kind: "short", Status: "done", Tagging: kb.Tagging{Importance: "P1", Relevance: 80}, Updated: now},
	} {
		if err := st.PutProject(p); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.PutSummary(kb.KindProject, "demo-release", "# 示例发布\n\n## 目标\n周五灰度")
	c := &kb.Chat{ID: kb.ChatID("lark", "f1"), Source: "lark", SourceID: "f1", Kind: "group", Name: "发布群", Created: now,
		LastAt: now, OneLine: "在推示例发布", Tagging: kb.Tagging{Importance: "P1", Relevance: 70}, Projects: []string{"demo-release", "missing"}}
	_ = st.PutChat(c)
	_ = st.PutSummary(kb.KindChat, c.ID, "# 发布群\n\n## 这是什么\n发布协调")
	m := &kb.Chat{ID: kb.ChatID("mail", "acc1-sender@example.com"), Source: "mail", SourceID: "acc1-sender@example.com", Kind: "mail",
		Name: "示例来信", Created: now, LastAt: now, Tagging: kb.Tagging{Importance: "P3", Relevance: 10}}
	_ = st.PutChat(m)
	return e
}

func goldenMachines(h *Hub) {
	for _, mc := range []MachineConfig{
		{ID: "server", Name: "服务器", Kind: "local"},
		{ID: "laptop", Name: "笔记本", Kind: "ssh", SSHHost: "laptop.example"},
		{ID: "desk", Name: "台式机", Kind: "ssh"},
	} {
		m := newMachine(h, mc)
		h.machines = append(h.machines, m)
		h.byID[mc.ID] = m
	}
}

func TestGoldenAssistantClaudeMD(t *testing.T) {
	instructions := func(h *Hub, lark-connector bool) string {
		return strings.ReplaceAll(h.assistantClaudeMD(context.Background(), lark-connector), h.cfg.dir, "<hub>")
	}
	h := newTestHub(t)
	goldenMachines(h)
	h.cfg.Assistant.Owner = "示例用户"
	h.cfg.Assistant.Notes = "  我在示例公司做后端。\n常用 skills：demo-skill。  "
	h.kb.cfg.ExtraDirs = []string{"/home/me/notes", "/home/me/facts"}
	golden(t, "assistant_claude_md.full", instructions(h, true))

	bare := newTestHub(t)
	m := newMachine(bare, MachineConfig{ID: "server", Name: "服务器", Kind: "local"})
	bare.machines, bare.byID["server"] = []*Machine{m}, m
	golden(t, "assistant_claude_md.bare", instructions(bare, true))
	// No lark-connector skill on the assistant's machine (~/.jarvis/skills): no ol, Feishu through the MCP tools.
	golden(t, "assistant_claude_md.no_lark-connector", instructions(bare, false))
	// lark-cli only (docs/plan-lark-cli.md): no lark-connector configured, no lark-connector skill.
	bare.cfg.Lark.CLI = &lark.CLIConfig{}
	golden(t, "assistant_claude_md.lark_cli", instructions(bare, false))
	// An lark-connector skill left on the machine changes nothing without lark-connector: no ol to offer.
	if got := instructions(bare, true); got != instructions(bare, false) {
		t.Error("lark-cli only: the lark-connector skill alone should not bring ol back")
	}
	// Both sources, lark-cli preferred.
	bare.cfg.Lark.Token, bare.cfg.Lark.Prefer = "test-token", "cli"
	golden(t, "assistant_claude_md.both", instructions(bare, true))
}

var (
	snapHeader = regexp.MustCompile(`^- 现在：\d{4}-\d\d-\d\d 周[日一二三四五六] \d\d:\d\d（([^）]+)）\n`)
)

// normalSnapshot replaces the clock in the snapshot's first line.
func normalSnapshot(t *testing.T, s string) string {
	t.Helper()
	m := snapHeader.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("snapshot header: %q", s[:min(len(s), 200)])
	}
	return "- 现在：YYYY-MM-DD 周X HH:MM（" + m[1] + "）\n" + s[len(m[0]):]
}

func snapshotHub(t *testing.T, full bool) *Hub {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	goldenMachines(h)
	h.lark = fakelark-connector(t)
	if !full {
		h.lark = lark.New(lark.Config{BaseURL: "http://127.0.0.1:1", Token: "t"}) // unreachable: chats and calendar left out
		h.sched.loaded = true
		return h
	}
	h.byID["server"].online = true
	h.byID["laptop"].online = true
	now := time.Now()
	h.byID["server"].sessions = []proto.Session{
		{SessionID: "aaaaaaaa-1111", Cwd: "/code/demo", Title: "修登录", Status: proto.StatusBlocked, UpdatedAt: now},
		{SessionID: "bbbbbbbb-2222", Cwd: "/code/demo", Title: "写文档", Status: proto.StatusDone, UpdatedAt: now},
		{SessionID: "cccccccc-3333", Cwd: "/code/demo", Title: "跑测试", Status: proto.StatusWorking, UpdatedAt: now, SpaceName: "示例空间", SlotName: "测试"},
		{SessionID: "dddddddd-4444", Cwd: "/code/old", Status: proto.StatusDone, UpdatedAt: now.Add(-48 * time.Hour)},
	}
	var working []proto.Session
	for i := range 9 {
		working = append(working, proto.Session{SessionID: "w" + string(rune('a'+i)), Cwd: "/code/w" + string(rune('a'+i)), Status: proto.StatusWorking, UpdatedAt: now})
	}
	h.byID["laptop"].sessions = working
	h.kb.eng = goldenKB(t)
	h.sched.loaded = true
	at := time.Date(2099, 1, 2, 1, 30, 0, 0, time.UTC)
	h.sched.tasks = []Task{
		{ID: "a", Title: "看 CI", Repeat: repeatOnce, At: at, Enabled: true},
		{ID: "b", Prompt: "巡检一下各台电脑上的 Agent 有没有卡住的，这句话会被截断", Repeat: repeatInterval, EveryMinutes: 30, At: at, Enabled: true},
		{ID: "c", Title: "早报", Repeat: repeatDaily, Time: "09:00", Enabled: true},
		{ID: "d", Title: "停了的", Repeat: repeatOnce, At: at, Enabled: false},
		{ID: "e", Repeat: repeatInterval, EveryMinutes: 120, At: at, Enabled: true, Prompt: "x"},
	}
	return h
}

func TestGoldenAssistantSnapshot(t *testing.T) {
	ctx := context.Background()
	golden(t, "assistant_snapshot.full", normalSnapshot(t, snapshotHub(t, true).assistantSnapshot(ctx)))
	golden(t, "assistant_snapshot.bare", normalSnapshot(t, snapshotHub(t, false).assistantSnapshot(ctx)))
}

// The main conversation's context starts with a line saying so (needs Postgres for the saved main conversation).
func TestGoldenAssistantContextMain(t *testing.T) {
	store := scheduleStore(t)
	ctx := context.Background()
	if err := store.PutSetting(ctx, "assistant", AssistantConfig{Main: "main-session"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.PutSetting(ctx, "assistant", nil) })
	h := snapshotHub(t, false)
	h.store = store
	h.cfg.Assistant.Owner = "示例用户"
	raw, _ := json.Marshal(map[string]string{"session_id": "main-session"})
	res, err := h.byID["server"].onRequest(ctx, "assistant.context", raw)
	if err != nil {
		t.Fatal(err)
	}
	text := res.(map[string]any)["text"].(string)
	prefix, rest, ok := strings.Cut(text, "\n\n")
	if !ok {
		t.Fatalf("%q", text)
	}
	golden(t, "assistant_context.main", prefix+"\n\n"+normalSnapshot(t, rest))
	raw, _ = json.Marshal(map[string]string{"session_id": "side-session"})
	res, _ = h.byID["server"].onRequest(ctx, "assistant.context", raw)
	golden(t, "assistant_context.side", normalSnapshot(t, res.(map[string]any)["text"].(string)))
}

func TestGoldenTaskPrompt(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai") // passed in: the output doesn't depend on the machine's zone
	due := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name      string
		task      Task
		late      time.Duration
		manual    bool
		kb        bool
		continued bool
	}{
		{"once_late", Task{Title: "看 CI", Prompt: "看看 CI 跑完没有", Repeat: repeatOnce}, 25 * time.Minute, false, true, false},
		{"once_untitled", Task{Prompt: "提醒我喝水", Repeat: repeatOnce}, time.Minute, false, true, false},
		{"interval_late", Task{Prompt: "巡检各台电脑上的 Agent，有卡住的告诉我", Repeat: repeatInterval, EveryMinutes: 120}, 3 * time.Hour, false, false, false},
		{"interval_manual", Task{Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30}, 3 * time.Hour, true, true, false},
		{"daily_kb", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 2 * time.Hour, false, true, false},
		{"daily_nokb", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 0, false, false, false},
		// Later runs in the task's own conversation: no knowledge-base hint or "don't reschedule" again.
		{"daily_continued", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 0, false, true, true},
		{"interval_continued_late", Task{Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30}, 3 * time.Hour, false, true, true},
	} {
		golden(t, "task_prompt."+c.name, taskPrompt(c.task, due, due.Add(c.late), loc, c.manual, c.kb, c.continued))
	}
}

func TestGoldenDefaultRoutines(t *testing.T) {
	for _, r := range defaultRoutines() {
		golden(t, "routine."+r.ID, r.Prompt)
	}
}

func TestGoldenLarkAgentPrompts(t *testing.T) {
	ctxText := "<jarvis-context kind=\"context\" title=\"请求的位置\">\n位置：群聊「发布群」\n</jarvis-context>\n"
	golden(t, "lark_agent.chat_new", larkAgentPrompt("示例用户", "发布群", "/agent", "看下构建为什么挂了", ctxText, false))
	golden(t, "lark_agent.chat_resumed_notask", larkAgentPrompt("", "发布群", "/agent", "", ctxText, true))
	golden(t, "lark_agent.doc_new", larkAgentDocPrompt("示例用户", "构建手册", "/agent", "这段对吗", ctxText, false))
	golden(t, "lark_agent.doc_resumed_notask", larkAgentDocPrompt("", "构建手册", "/agent", "", ctxText, true))
}

func TestGoldenLarkAgentContext(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.lark = fakelark-connector(t)
	h.userTZ = "Asia/Shanghai" // the hub itself runs in UTC; times read in the user's zone
	at := time.Date(2026, 10, 31, 1, 5, 0, 0, time.UTC)
	req := lark.Message{ID: "req", Position: "9", Text: "/agent 看下怎么修 https://example.feishu.cn/wiki/Broken1", CreatedAt: at,
		ReplyTo: &lark.ReplyTo{SenderName: "张三", Text: "构建挂了"}}
	// No knowledge base, not in a thread: the messages before it.
	chat, text := h.larkAgentContext(ctx, LarkAgentConfig{}, "f1", req)
	golden(t, "lark_context.plain", chat+"\n---\n"+text)

	// With the knowledge base, in a thread.
	h.kb.eng = goldenKB(t)
	req.ThreadKind, req.ThreadID, req.ReplyTo = "reply", "th1", nil
	chat, text = h.larkAgentContext(ctx, LarkAgentConfig{}, "f1", req)
	golden(t, "lark_context.kb_thread", chat+"\n---\n"+text)
}

func TestGoldenLarkAgentDocContext(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.lark = fakelark-connector(t)
	h.userTZ = "Asia/Shanghai"
	at := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	reply := func(name string, mine bool, text string, min int) lark.CommentReply {
		r := lark.CommentReply{Content: []lark.CommentSegment{{Kind: "text", Text: text}}, Mine: mine, CreatedAt: at.Add(time.Duration(min) * time.Minute)}
		r.Author.Name = name
		return r
	}
	th := lark.CommentThread{ID: "c1", Quote: "第二步重跑", Replies: []lark.CommentReply{
		reply("张三", false, "这一步要先清缓存吧", 0), reply("我", true, "/agent 这段对吗", 1), reply("李四", false, "后面的", 2)}}
	golden(t, "lark_doc_context.sheet", h.larkAgentDocContext(ctx, lark.RecentDoc{Title: "构建手册", URL: "https://example.feishu.cn/sheets/AbC", Type: 3}, th, 1))
	th.Whole = true
	golden(t, "lark_doc_context.broken_whole", h.larkAgentDocContext(ctx, lark.RecentDoc{Title: "坏文档", URL: "https://example.feishu.cn/docx/Broken", Type: 99}, th, 0))
}

func goldenItems() map[string]*InboxItem {
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	agent := &InboxAgent{State: AgentQueued, Task: "查清楚构建为什么挂了，回复对方结论"}
	return map[string]*InboxItem{
		"dm": {ID: "lark:f2:1", Kind: KindDM, Title: "张三", Context: "张三：1013 合了吗？", Summary: "张三问 1013 是否合入", UpdatedAt: at,
			Lark: &InboxLark{FeedID: "f2"}, Agent: agent},
		"mention": {ID: "lark:f1:1", Kind: KindMention, Title: "发布群", Context: "张三：@我 周五灰度能提前吗", UpdatedAt: at,
			Lark: &InboxLark{FeedID: "f1", IsGroup: true}, Agent: agent},
		"group": {ID: "lark:f3:1", Kind: KindGroup, Title: "闲聊群", Context: "李四：午饭吃啥", UpdatedAt: at, Lark: &InboxLark{FeedID: "f3"}},
		"bot":   {ID: "lark:f9:1", Kind: KindBot, Title: "告警机器人", Context: "告警：示例服务 5xx 升高", UpdatedAt: at, Lark: &InboxLark{FeedID: "f9"}},
		"doc_rows": {ID: "doc:1", Kind: KindDoc, Title: "排期表", Context: "表格「排期表」第 3 行 @我：状态 进行中", UpdatedAt: at,
			Doc: &InboxDoc{URL: "https://example.feishu.cn/sheets/AbC", Rows: []DocRow{{}}}, Agent: agent},
		"doc_plain": {ID: "doc:2", Kind: KindDoc, Title: "设计文档", Context: "评论：这里要改吗", UpdatedAt: at,
			Doc: &InboxDoc{URL: "https://example.feishu.cn/docx/AbC"}, Agent: agent},
		"moment": {ID: "moment:1", Kind: KindMoment, Title: "我的帖子", Context: "王五评论：写得好", UpdatedAt: at,
			Moment: &InboxMoment{PostID: "p1", Notice: "comment"}, Agent: agent},
		"mail_direct": {ID: "mail:1", Kind: KindMail, Title: "季度汇报", Context: "来信：请周五前提交", UpdatedAt: at,
			Mail: &InboxMail{AccountID: "acc1", Mailbox: "INBOX", UIDs: []uint32{7, 9}, Direct: true, From: mail.Address{Email: "Sender@Example.com"}}, Agent: agent},
		"mail_cc": {ID: "mail:2", Kind: KindMail, Title: "周报", Context: "抄送：本周进展", UpdatedAt: at,
			Mail: &InboxMail{AccountID: "acc1", Mailbox: "INBOX"}},
		"github_review": {ID: "github:1", Source: "github", Kind: "review_requested", Title: "Add the example",
			Context: "GitHub PR example-org/example-repo#12：Add the example\n状态：open · 作者 alice · 等这些人 review：me", UpdatedAt: at,
			Connector: &InboxConnector{Thread: "example-org/example-repo#12", Label: "请你 review", URL: "https://github.com/example-org/example-repo/pull/12",
				ReplyTo: "评论到 example-org/example-repo#12（仓库可见的人都能看到）", CanReply: true, RuleTier: TierAction}, Agent: agent},
		"imessage_dm": {ID: "imessage:1", Source: "imessage", Kind: "dm", Title: "张三", UpdatedAt: at,
			Context: "我（10-30 07:58）：在吗\n张三（10-30 07:59）：示例：周五下午三点见，地方你定",
			Connector: &InboxConnector{Thread: "imessage-0123456789ab", OpenURL: "sms:+15550100", ReplyTo: "在「信息」里回复（Persona 不替你发）",
				RuleTier: TierAction}, Agent: agent},
		"imessage_group": {ID: "imessage:2", Source: "imessage", Kind: "group", Title: "周末徒步", UpdatedAt: at,
			Context: "李四（10-30 08:01）：示例：明天十点出发", Connector: &InboxConnector{Thread: "imessage-ba9876543210", Label: "群聊", RuleTier: TierFYI}},
		"telegram_dm": {ID: "telegram:u1", Source: "telegram", Kind: "dm", Title: "示例用户", UpdatedAt: at,
			Context:   "示例用户（Telegram 私聊）\n2026-10-30 07:58 我：在吗\n2026-10-30 07:59 示例用户：示例：周五下午三点见",
			Connector: &InboxConnector{Thread: "u1", Label: "私聊", OpenURL: "tg://resolve?domain=example", RuleTier: TierAction}, Agent: agent},
		"telegram_mention": {ID: "telegram:c2", Source: "telegram", Kind: "mention", Title: "示例群", UpdatedAt: at,
			Context:   "示例群（Telegram 群）\n2026-10-30 08:01 另一个示例用户：@example 示例方案你怎么看？",
			Connector: &InboxConnector{Thread: "c2", Label: "@你", OpenURL: "https://t.me/c/2/12", RuleTier: TierAction}},
	}
}

func TestGoldenInboxTriage(t *testing.T) {
	ctx := context.Background()
	for name, it := range goldenItems() {
		h := agentHub(t)
		model, calls := captureModel(t, `{"tier":"action","summary":"s","reason":"r"}`)
		h.triageAI = model
		if name == "dm" {
			h.cfg.Assistant.Owner = "示例用户"
			h.cfg.Inbox.Rules = "  老板的消息一律需要我  "
		}
		h.inbox.items[it.ID] = it
		h.triageOne(ctx, it)
		goldenCall(t, "inbox_triage."+name, onlyCall(t, calls()))
	}
}

func TestGoldenInboxDraft(t *testing.T) {
	ctx := context.Background()
	items := goldenItems()
	for _, c := range []struct {
		name, item, owner, notes, instruction string
	}{
		{"plain", "dm", "", "", ""},
		{"owner_notes_instruction", "mail_direct", "示例用户", " 我在示例公司做后端 ", " 说周四给 "},
	} {
		h := agentHub(t)
		model, calls := captureModel(t, "好的")
		h.ai = model
		h.cfg.Assistant.Owner, h.cfg.Assistant.Notes = c.owner, c.notes
		it := items[c.item]
		h.inbox.items[it.ID] = it
		if _, err := h.draftReply(ctx, it.ID, c.instruction); err != nil {
			t.Fatal(err)
		}
		goldenCall(t, "inbox_draft."+c.name, onlyCall(t, calls()))
	}
}

func TestGoldenInboxJudge(t *testing.T) {
	ctx := context.Background()
	items := goldenItems()
	for _, c := range []struct{ name, item, owner, rules string }{
		{"summary_rules", "dm", "示例用户", " 告警都不用交 "},
		{"plain", "bot", "", ""},
	} {
		h := agentHub(t)
		model, calls := captureModel(t, `{"handle":false,"reason":"r"}`)
		h.triageAI = model
		h.cfg.Assistant.Owner, h.cfg.Inbox.Agent.Rules = c.owner, c.rules
		it := *items[c.item]
		h.inbox.items[it.ID] = &it
		h.judgeOne(ctx, &it)
		goldenCall(t, "inbox_judge."+c.name, onlyCall(t, calls()))
	}
}

func TestGoldenInboxAgentPrompt(t *testing.T) {
	items := goldenItems()
	for _, name := range []string{"dm", "doc_rows", "doc_plain", "moment", "mail_direct", "github_review"} {
		h := agentHub(t)
		h.cfg.Assistant.Owner = "示例用户"
		golden(t, "inbox_agent."+name, inboxAgentPrompt(items[name], h.ownerName(), h.kbContextFor(items[name])))
	}
	// With the knowledge base: the chat's summary and projects follow.
	h := agentHub(t)
	h.kb.eng = goldenKB(t)
	it := *items["mention"]
	golden(t, "inbox_agent.mention_kb", inboxAgentPrompt(&it, h.ownerName(), h.kbContextFor(&it)))
	// A chat in another Feishu account than lark.account: the prompt names it for lark_messages.
	it = *items["dm"]
	it.Lark = &InboxLark{AccountID: 2, FeedID: "f2"}
	golden(t, "inbox_agent.dm_second_account", inboxAgentPromptFor(&it, "示例用户", nil, nil, 1))
	it = *items["mail_direct"]
	it.Mail = &InboxMail{AccountID: "acc1", Mailbox: "INBOX", UIDs: []uint32{3}, From: mail.Address{Email: "Sender@Example.com"}}
	golden(t, "inbox_agent.mail_kb", inboxAgentPrompt(&it, "", h.kbContextFor(&it)))
}

func TestGoldenTabName(t *testing.T) {
	h := newTestHub(t)
	model, calls := captureModel(t, "fix-login")
	h.ai = model
	if got := h.autoTabName(context.Background(), "  修一下登录页的报错  "); got != "fix-login" {
		t.Fatalf("%q", got)
	}
	goldenCall(t, "tab_name", onlyCall(t, calls()))
}

func TestGoldenSessionTitle(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	started := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	model, calls := replies(t, "改个名字", "修复 | 搜索框和顶栏重叠")
	h.ai = model
	if got := h.makeTitle(context.Background(), started, "搜索的时候搜索框和顶栏叠在一起了", "是两个组件一起用导致的。"); got != "1101 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("%q", got)
	}
	c := calls()
	if len(c) != 2 {
		t.Fatalf("%d calls", len(c))
	}
	goldenCall(t, "session_title", c[0])
	golden(t, "session_title.retry", c[1].User)
}

func TestGoldenTranslateTexts(t *testing.T) {
	h := newTestHub(t)
	model, calls := captureModel(t, `{"texts":["你好","明天见"]}`)
	h.triageAI = model
	if _, err := h.translateTexts(context.Background(), []string{"Hello", "", "See you tomorrow"}, "zh-Hans"); err != nil {
		t.Fatal(err)
	}
	goldenCall(t, "translate_texts", onlyCall(t, calls()))
}

type goldenMail struct {
	Mail
	msg *mail.MailMessage
}

func (g goldenMail) GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	return g.msg, nil
}

func TestGoldenTranslateMail(t *testing.T) {
	h := newTestHub(t)
	model, calls := captureModel(t, `{"subject":"季度汇报","text":"请在周五前提交。"}`)
	h.triageAI = model
	h.mail = goldenMail{msg: &mail.MailMessage{MailSummary: mail.MailSummary{Subject: "Quarterly report"}, Text: "  Please submit it by Friday.  "}}
	if _, err := h.translateMail(context.Background(), "acc1", 7, "INBOX", "zh-Hans"); err != nil {
		t.Fatal(err)
	}
	goldenCall(t, "translate_mail", onlyCall(t, calls()))
}

func TestGoldenRelay(t *testing.T) {
	golden(t, "relay", relayText(relayMsg{From: "b2e4d6f8-0000-4000-8000-000000000002", FromTitle: `1101 | 研究 | "示例"<主题>`,
		Tool: "conversation_send", Text: "示例消息：把结果整理一下", Hop: 2}))
}

func TestGoldenBotMessage(t *testing.T) {
	golden(t, "bot_message.jarvis", prompts.RenderBotMessage(prompts.BotMessage{Jarvis: true, Owner: "示例用户", Text: "今天有什么会？",
		Quote: &prompts.BotQuote{Who: "Persona", Text: "上午 10 点有周会。"}}))
	golden(t, "bot_message.agent_files", prompts.RenderBotMessage(prompts.BotMessage{Text: "看下这个日志",
		Forward: "11-02 10:00 张三：构建挂了\n11-02 10:01 李四：我看下", Files: []string{"/tmp/jarvis-uploads/build.log"}}))
	golden(t, "bot_message.only_files", prompts.RenderBotMessage(prompts.BotMessage{Jarvis: true, Files: []string{"/tmp/a.pdf", "/tmp/b.png"}}))
}

// A message to a plugin's chat app bot (docs/channels.md): plain text and text answers (WeChat), Markdown without
// answering there, what came that cannot be passed on, a picture alone.
func TestGoldenChannelMessage(t *testing.T) {
	golden(t, "channel_message.plain", prompts.RenderChannelMessage(prompts.ChannelMessage{Persona: true, Name: "微信", Owner: "示例用户",
		Plain: true, Asks: true, Text: "明天有什么会？"}))
	golden(t, "channel_message.quote_unsupported", prompts.RenderChannelMessage(prompts.ChannelMessage{Persona: true, Name: "微信", Plain: true,
		Asks: true, Text: "看下这个", Quote: "张三\n合同明天要签", Unsupported: []string{"文件「合同.pdf」", "一段视频"}}))
	golden(t, "channel_message.markdown_files", prompts.RenderChannelMessage(prompts.ChannelMessage{Persona: true, Name: "Example Chat",
		Text: "看下这个日志", Forward: "11-02 10:00 张三：构建挂了", Files: []string{"/tmp/jarvis-uploads/build.log"}}))
	golden(t, "channel_message.only_unsupported", prompts.RenderChannelMessage(prompts.ChannelMessage{Persona: true, Name: "微信", Plain: true,
		Asks: true, Unsupported: []string{"一段没有转写的语音"}}))
}

// An item about one of the user's tasks: the judge's input lists the tasks, the AI 分身 is told the linked one.
func TestGoldenInboxTask(t *testing.T) {
	items := goldenItems()
	task := prompts.InboxTask{ID: "todo_1", Title: "修登录崩溃", State: "在做", Last: "测试通过 42/42"}
	golden(t, "inbox_judge_input.with_task", prompts.RenderInboxJudgeInput(prompts.InboxJudgeInput{Summary: "张三问修复进度",
		Item: promptItem(items["dm"]), Tasks: []prompts.InboxTask{task}}))
	golden(t, "inbox_agent.with_task", inboxAgentPromptFor(items["dm"], "示例用户", nil, &task, 1))
}

// Closing a finished task (docs/todos.md「自动完成」): the judge's system prompt and its input, with and without hints.
func TestGoldenTaskDone(t *testing.T) {
	golden(t, "task_done", prompts.RenderTaskDone(prompts.TaskDone{Owner: "示例用户"}))
	golden(t, "task_done_input.hints", prompts.RenderTaskDoneInput(prompts.TaskDoneInput{Title: "修登录崩溃",
		Prompt: "iPad 横屏时登录页崩溃，修好并补测试，走 PR 合并后发 TestFlight",
		Replies: []prompts.TaskDoneReply{{Machine: "box", Text: "修好了，PR https://github.com/o/r/pull/901 已合并，TestFlight 构建号 202611041200"}},
		MergedPR: true, Build: true}))
	golden(t, "task_done_input.two_agents", prompts.RenderTaskDoneInput(prompts.TaskDoneInput{Title: "用新 logo 设计默认形象",
		Prompt: "按新 logo 重画默认形象的全部表情",
		Replies: []prompts.TaskDoneReply{{Machine: "box", Text: "没有全改完，还差三个表情"}, {Machine: "mac", Text: "预览图放在 /tmp/preview.png"}}}))
	golden(t, "task_done_input.follow_up", prompts.RenderTaskDoneInput(prompts.TaskDoneInput{Title: "修任务启动失败",
		Prompt: "任务启动失败时一直显示正在开，修好",
		Replies: []prompts.TaskDoneReply{{Machine: "box", Ask: "再把「全部 N」挪到这一行最左边",
			Text: "挪好了，PR 已合并，TestFlight 构建号 202611040923。没截图的：Mac。"}}, Build: true}))
}
