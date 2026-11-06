package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// An agent started for a todo gets the background of its directory's project before its first prompt (#2820): with
// the prompt on the start, or, started in the background without one (the apps' Claude), on its first message,
// whether that names the todo (web) or not (iOS, Android). A message to an agent already running gets no background.
func TestNewAgentCarriesProjectContext(t *testing.T) {
	h := newTestHub(t)
	e := withKB(t, h)
	if _, err := e.Apply(kb.KindProject, "天气 App", kb.Update{Dirs: []string{"macbook:/home/me/weather"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store.AppendLog("天气-app", time.Now(), kb.TodoLogSource+"修登录页", "结论：修好了 https://github.com/o/r/pull/1"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sent := map[string]string{}
	n := 0
	m := capturingNode(t, h, "macbook", func(method string, params json.RawMessage) any {
		var q struct {
			Prompt    string `json:"prompt"`
			Text      string `json:"text"`
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &q)
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "session.start":
			n++
			sid := "s_new" + string(rune('0'+n))
			sent["start:"+sid] = q.Prompt
			return map[string]any{"session": proto.Session{MachineID: "macbook", SessionID: sid, Cwd: "/home/me/weather", Agent: "claude"}}
		case "session.send":
			sent["send:"+q.SessionID] = q.Text
			return map[string]any{"ok": true}
		}
		return map[string]any{}
	})
	m.mu.Lock()
	m.info.Home = "/home/me"
	m.mu.Unlock()
	m.sessions = append(m.sessions, proto.Session{MachineID: "macbook", SessionID: "s_old", Cwd: "/home/me/weather", Agent: "claude"})
	connectAssistant(t, h)
	todoID := plainTodo(t, h, "加深色模式").ID
	call := func(path string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		if w := hubCall(h, "POST", path, string(b)); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	get := func(k string) string {
		mu.Lock()
		defer mu.Unlock()
		return sent[k]
	}
	withBackground := func(what, got, text string) {
		t.Helper()
		if !strings.HasPrefix(got, `<persona-context kind="context" title="项目 · 天气 App · 背景" url="">`) ||
			!strings.Contains(got, "「修登录页」结论：修好了 https://github.com/o/r/pull/1") || !strings.HasSuffix(got, "</persona-context>\n\n"+text) {
			t.Fatalf("%s = %q", what, got)
		}
	}

	// The prompt on the start (Codex), in ~/ of the machine's home.
	call("/api/v1/sessions", map[string]any{"machine_id": "macbook", "cwd": "~/weather", "prompt": "加深色模式", "todo_id": todoID, "tab_name": "t"})
	withBackground("a start's prompt", get("start:s_new1"), "加深色模式")

	// Started without a prompt; the first message, without todo_id (iOS), gets it; the second does not.
	call("/api/v1/sessions", map[string]any{"machine_id": "macbook", "cwd": "~/weather", "async": true, "todo_id": todoID, "tab_name": "t"})
	call("/api/v1/sessions/macbook/s_new2/messages", map[string]any{"text": "加深色模式"})
	withBackground("the first message", get("send:s_new2"), "加深色模式")
	call("/api/v1/sessions/macbook/s_new2/messages", map[string]any{"text": "继续"})
	if got := get("send:s_new2"); got != "继续" {
		t.Fatalf("second message = %q", got)
	}

	// With todo_id (web) it is the same.
	call("/api/v1/sessions", map[string]any{"machine_id": "macbook", "cwd": "/home/me/weather", "async": true, "todo_id": todoID, "tab_name": "t"})
	call("/api/v1/sessions/macbook/s_new3/messages", map[string]any{"text": "加深色模式", "todo_id": todoID})
	withBackground("the first message with todo_id", get("send:s_new3"), "加深色模式")

	// Handed to an agent already running: no background.
	call("/api/v1/sessions/macbook/s_old/messages", map[string]any{"text": "加深色模式", "todo_id": todoID})
	if got := get("send:s_old"); got != "加深色模式" {
		t.Fatalf("running agent = %q", got)
	}
}

// A first message that does not go through (the node refuses it, nothing typed) leaves the fresh start: sent again,
// it still gets the background.
func TestFreshStartSurvivesAFailedFirstMessage(t *testing.T) {
	var f freshStarts
	f.add("m", "s", "todo_1", "/code")
	if _, ok := f.peek("m", "s"); !ok {
		t.Fatal("peek lost it")
	}
	if v, ok := f.peek("m", "s"); !ok || v.todoID != "todo_1" || v.cwd != "/code" {
		t.Fatalf("second peek = %+v %v", v, ok)
	}
	f.done("m", "s")
	if _, ok := f.peek("m", "s"); ok {
		t.Fatal("still there after it went through")
	}
}
