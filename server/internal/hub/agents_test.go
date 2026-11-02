package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/chyroc/jarvis/server/internal/agents"
	"github.com/chyroc/jarvis/server/internal/lark"
)

const bridgeKey = "agent:grok_bot:a6094fe692a7b:8f798b0c-5cb9-4d5f-bdf5-fde194dbe5a3/"

// fakelark-connector answers the agent-bridge routes and records the calls.
func fakeAgentslark-connector(t *testing.T) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		mu.Unlock()
		p := strings.TrimPrefix(r.URL.EscapedPath(), "/agent/v1")
		esc := url.PathEscape(bridgeKey)
		switch {
		case p == "/agent-bridge":
			io.WriteString(w, `{"enabled":true,"mixIntoInbox":true,"providers":[],"accounts":[{"accountId":"a6094fe692a7b","provider":"grok_bot","sessionCount":1}]}`)
		case p == "/agent-sessions":
			io.WriteString(w, `{"sessions":[{"sessionKey":"`+bridgeKey+`","provider":"grok_bot","title":"jarvis","kind":"main","canSend":true,"unreadCount":2,"rankTimeMs":5},
			{"sessionKey":"agent:muse:x:main","provider":"muse","canSend":false}]}`)
		case p == "/agent-sessions/"+esc+"/messages" && r.Method == "GET":
			io.WriteString(w, `{"messages":[{"externalId":"e","role":"assistant","blocks":[{"type":"text","text":"hi"}]}],"hasMore":true,"nextCursor":"c2"}`)
		case p == "/agent-sessions/"+esc+"/messages" && r.Method == "POST":
			io.WriteString(w, `{}`)
		case p == "/agent-sessions/"+esc+"/read":
			io.WriteString(w, `{}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":{"code":"NOT_FOUND","message":"nope"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func agentsHub(t *testing.T) (*Hub, http.Handler, *[]string) {
	srv, calls := fakeAgentslark-connector(t)
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	h.initAgents()
	mux := http.NewServeMux()
	h.agentRoutes(mux)
	return h, mux, calls
}

func do(t *testing.T, hd http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	hd.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAgentRoutesRead(t *testing.T) {
	_, hd, _ := agentsHub(t)
	if code, out := do(t, hd, "GET", "/api/v1/agents", ""); code != 200 || out["sources"].([]any)[0].(map[string]any)["enabled"] != true {
		t.Fatalf("%d %v", code, out)
	}
	code, out := do(t, hd, "GET", "/api/v1/agents/sessions?provider=grok_bot", "")
	ss := out["sessions"].([]any)
	if code != 200 || len(ss) != 1 || ss[0].(map[string]any)["id"] != "removedbridge:"+bridgeKey {
		t.Fatalf("%d %v", code, out)
	}
	id := url.PathEscape("removedbridge:" + bridgeKey)
	code, out = do(t, hd, "GET", "/api/v1/agents/sessions/"+id+"/messages?limit=5", "")
	if code != 200 || out["nextCursor"] != "c2" || len(out["messages"].([]any)) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := do(t, hd, "GET", "/api/v1/agents/sessions/"+url.PathEscape("removedbridge:nope")+"/messages", ""); code != 404 {
		t.Fatalf("unknown session: %d", code)
	}
}

func TestAgentRoutesSend(t *testing.T) {
	_, hd, calls := agentsHub(t)
	id := url.PathEscape("removedbridge:" + bridgeKey)
	agents.FollowUps = nil
	cid := agents.NewClientID()
	for i := 0; i < 3; i++ {
		code, out := do(t, hd, "POST", "/api/v1/agents/sessions/"+id+"/messages", `{"clientId":"`+cid+`","text":"hello"}`)
		if code != 200 || out["duplicate"] != (i > 0) {
			t.Fatalf("send %d: %d %v", i, code, out)
		}
	}
	code, out := do(t, hd, "POST", "/api/v1/agents/sessions/"+id+"/messages", `{"text":"x"}`)
	gen, _ := out["outbox"].(map[string]any)["clientId"].(string)
	if code != 200 || len(gen) != 36 {
		t.Fatalf("generated clientId: %d %v", code, out)
	}
	muse := url.PathEscape("removedbridge:agent:muse:x:main")
	if code, _ := do(t, hd, "POST", "/api/v1/agents/sessions/"+muse+"/messages", `{"text":"x"}`); code != 409 {
		t.Fatalf("canSend=false: %d", code)
	}
	if code, _ := do(t, hd, "POST", "/api/v1/agents/sessions/"+id+"/messages", `{"text":"`+strings.Repeat("a", 20001)+`"}`); code != 413 {
		t.Fatalf("too long: %d", code)
	}
	if code, _ := do(t, hd, "POST", "/api/v1/agents/sessions/"+id+"/read", ``); code != 200 {
		t.Fatalf("read: %d", code)
	}
	posts := 0
	for _, c := range *calls {
		if strings.HasPrefix(c, "POST") && strings.HasSuffix(c, "/messages") {
			posts++
		}
	}
	if posts != 2 {
		t.Fatalf("lark-connector got %d sends, want 2: %v", posts, *calls)
	}
	if code, out := do(t, hd, "GET", "/api/v1/agents/outbox", ""); code != 200 || len(out["outbox"].([]any)) != 2 {
		t.Fatalf("outbox %d %v", code, out)
	}
}

func TestAgentPrefsRoutes(t *testing.T) {
	_, hd, _ := agentsHub(t)
	code, out := do(t, hd, "GET", "/api/v1/agents/prefs", "")
	if code != 200 || len(out["pins"].([]any)) != 0 || out["view"] == nil {
		t.Fatalf("defaults %d %v", code, out)
	}
	body := `{"pins":["a","b","a",""],"view":{"group":"provider","sort":"unread","show":"unread","providers":["grok_bot"]},
		"sections":[{"id":"s1","name":"工作"}],"assign":{"a":"s1","b":"gone"}}`
	if code, out = do(t, hd, "PUT", "/api/v1/agents/prefs", body); code != 200 {
		t.Fatalf("put %d %v", code, out)
	}
	code, out = do(t, hd, "GET", "/api/v1/agents/prefs", "")
	pins := out["pins"].([]any)
	assign := out["assign"].(map[string]any)
	if code != 200 || len(pins) != 2 || pins[0] != "a" || out["view"].(map[string]any)["group"] != "provider" || len(assign) != 1 || assign["a"] != "s1" {
		t.Fatalf("get %d %v", code, out)
	}
	if code, _ = do(t, hd, "PUT", "/api/v1/agents/prefs", `{"sections":[{"id":"","name":"x"}]}`); code != 400 {
		t.Fatalf("bad section %d", code)
	}
	if code, _ = do(t, hd, "PUT", "/api/v1/agents/prefs", `nope`); code != 400 {
		t.Fatalf("bad json %d", code)
	}
}
