package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/proto"
)

// replies is a model that answers each call with the next of its replies (the last one again when they run out).
func replies(t *testing.T, out ...string) (*ai.Client, func() []capturedCall) {
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
		mu.Lock()
		c := capturedCall{System: in.System}
		if len(in.Messages) > 0 {
			c.User = in.Messages[0].Content
		}
		calls = append(calls, c)
		reply := out[min(len(calls), len(out))-1]
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

func TestParseTitle(t *testing.T) {
	for _, c := range []struct {
		out, kind, topic string
		ok               bool
	}{
		{"修复 | 搜索框和顶栏重叠", "修复", "搜索框和顶栏重叠", true},
		{"  \n「研究｜竞品定价」\n", "研究", "竞品定价", true},
		{"1101 | 发布 | 周五灰度安排。", "发布", "周五灰度安排", true}, // a date it added anyway, a full stop
		{"闲聊 | 周末去哪", "", "周末去哪", false},              // not a kind
		{"修复 | 搜索框在搜索时和顶栏重叠而且键盘弹起后错位", "修复", "搜索框在搜索时和顶栏重叠而且键盘弹起后错位", false},
		{"修复搜索框", "", "", false},
		{"", "", "", false},
	} {
		kind, topic, ok := parseTitle(c.out)
		if kind != c.kind || topic != c.topic || ok != c.ok {
			t.Errorf("parseTitle(%q) = %q %q %v", c.out, kind, topic, ok)
		}
	}
}

// MMDD is the day in the user's time zone though the hub runs in UTC; a broken answer is asked for once more, then
// cut or replaced by 「探索 | the first 14 characters」.
func TestMakeTitle(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	started := time.Date(2026, 11, 1, 16, 30, 0, 0, time.UTC) // 00:30 on 11-02 in Shanghai
	ctx := context.Background()
	first, reply := "搜索的时候搜索框和顶栏叠在一起了，帮我看看", "是 safeAreaBar 和搜索框抽屉一起用导致的……"

	model, calls := replies(t, "修复 | 搜索框和顶栏重叠")
	h.ai = model
	if got := h.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("%q", got)
	}
	if c := onlyCall(t, calls()); !strings.Contains(c.User, first) || !strings.Contains(c.User, reply) {
		t.Fatalf("the model reads the first exchange: %q", c.User)
	}

	model, calls = replies(t, "Bug fix: search bar", "修复 | 搜索框和顶栏重叠")
	h.ai = model
	if got := h.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("retried once: %q", got)
	}
	if c := calls(); len(c) != 2 || !strings.Contains(c[1].User, "Bug fix: search bar") {
		t.Fatalf("the retry says what was wrong: %+v", c)
	}

	model, _ = replies(t, "修复 | 搜索框在搜索时和顶栏重叠而且键盘弹起后错位")
	h.ai = model
	if got := h.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框在搜索时和顶栏重叠而且" {
		t.Fatalf("a topic still too long is cut: %q", got)
	}

	model, _ = replies(t, "随便聊聊")
	h.ai = model
	if got := h.makeTitle(ctx, started, "<jarvis-context>\n今天\n</jarvis-context>\n**帮我** 看看明天的会议安排和要准备的材料", ""); got != "1102 | 探索 | 帮我 看看明天的会议安排和要" {
		t.Fatalf("the fallback: %q", got)
	}
	h.ai = nil
	if got := h.makeTitle(ctx, started, "", ""); got != "1102 | 探索 | 新对话" {
		t.Fatalf("no model, no message: %q", got)
	}
}

// A conversation the hub started is named once, after its first turn; the user's own name wins and stays; a
// conversation from before (not noted) is left alone; the list the apps get shows the title.
func TestAConversationIsNamedOnceAfterItsFirstTurn(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	n := newAssistantNode(t, h)
	n.m.info.Home = "/home/me"
	n.items = []proto.Item{{Role: "user", Kind: proto.KindText, Text: "帮我查一下竞品的定价"},
		{Role: "assistant", Kind: proto.KindText, Text: "查到三家……"}}
	model, calls := replies(t, "研究 | 竞品定价")
	h.ai = model
	started := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	h.titleNew("server", "new", started, "")

	h.titleAfterTurn(n.m, "old") // from before: not noted, not named
	if len(calls()) != 0 {
		t.Fatal("an old conversation is not named")
	}
	h.titleAfterTurn(n.m, "new")
	if got := h.conversationTitles()["new"]; got != "1101 | 研究 | 竞品定价" {
		t.Fatalf("%q", got)
	}
	h.titleAfterTurn(n.m, "new") // the second turn
	if len(calls()) != 1 {
		t.Fatal("named once")
	}
	n.m.sessions = []proto.Session{{SessionID: "new", Cwd: "/home/me/.jarvis/assistant", Title: "帮我查一下竞品的定价"},
		{SessionID: "x", Cwd: "/code/app", Title: "帮我查一下竞品的定价"}}
	list := h.decorated(n.m)
	if list[0].Title != "1101 | 研究 | 竞品定价" || list[1].Title != "帮我查一下竞品的定价" {
		t.Fatalf("only the assistant conversation shows it: %+v", list)
	}

	// The user renames it in the app: theirs from now on.
	srv := httptest.NewServer(func() http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /r/{sid}", func(w http.ResponseWriter, r *http.Request) { h.renameSession(w, r, n.m) })
		return mux
	}())
	defer srv.Close()
	res, err := http.Post(srv.URL+"/r/new", "application/json", strings.NewReader(`{"title":"竞品  定价表"}`))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("%v %v", res, err)
	}
	if got := h.conversationTitles()["new"]; got != "竞品 定价表" {
		t.Fatalf("the user's title: %q", got)
	}
	if p := n.lastSend(t)["rename"].(map[string]any); p["title"] != "竞品  定价表" {
		t.Fatalf("the node renames a Claude conversation too: %v", p)
	}

	// Renamed before its first turn ended: nothing replaces it.
	h.titleNew("server", "early", started, "")
	h.titleRenamed("server", "early", "我自己起的")
	h.titleAfterTurn(n.m, "early")
	if got := h.conversationTitles()["early"]; got != "我自己起的" || len(calls()) != 1 {
		t.Fatalf("%q, %d calls", got, len(calls()))
	}

	// A Codex conversation's title is the hub's only: the node would write Claude's line into the rollout.
	n.m.sessions = append(n.m.sessions, proto.Session{SessionID: "cx", Cwd: "/home/me/.jarvis/assistant", Agent: "codex"})
	sends := n.sent()
	res, _ = http.Post(srv.URL+"/r/cx", "application/json", strings.NewReader(`{"title":"codex 的对话"}`))
	if res.StatusCode != http.StatusOK || n.sent() != sends || h.conversationTitles()["cx"] != "codex 的对话" {
		t.Fatalf("%d, %d calls to the node, %q", res.StatusCode, n.sent()-sends, h.conversationTitles()["cx"])
	}
}

// A scheduled task's conversation is 「MMDD | 定时 | 任务名」 from its first run, with no model call, and keeps it.
func TestATaskConversationIsTitledAfterTheTask(t *testing.T) {
	h := newTestHub(t)
	h.userTZ = "Asia/Shanghai"
	n := newAssistantNode(t, h)
	model, calls := replies(t, "探索 | 不该用到")
	h.ai = model
	setTasks(h, Task{ID: "r", Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily, Time: "09:00", Enabled: true})
	first, err := runNow(t, h, "r")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().In(h.userLocation()).Format("0102")
	if got := h.conversationTitles()[first.SessionID]; got != day+" | 定时 | 早报" {
		t.Fatalf("%q", got)
	}
	h.titleAfterTurn(n.m, first.SessionID)
	if _, err := runNow(t, h, "r"); err != nil {
		t.Fatal(err)
	}
	if got := h.conversationTitles()[first.SessionID]; got != day+" | 定时 | 早报" || len(calls()) != 0 {
		t.Fatalf("the same title every run, no model: %q, %d calls", got, len(calls()))
	}
}
