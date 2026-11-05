package extmgr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// agentsHub is a hub with no personal agents sources (they come from plugins: extension_agents_test.go).
func agentsHub(t *testing.T) (*Manager, http.Handler, *[]string) {
	h := newTestManager(t)
	h.initAgents()
	mux := http.NewServeMux()
	h.agentRoutes(mux)
	return h, mux, &[]string{}
}

func do(t *testing.T, hd http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	hd.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Without a plugin's source the Agents tab is empty, not an error (lark-cli or not).
func TestAgentRoutesWithoutSources(t *testing.T) {
	for _, cli := range []*lark.CLIConfig{nil, {}} {
		h := newTestManager(t)
		h.cfg.Lark = lark.Config{CLI: cli}
		h.initAgents()
		mux := http.NewServeMux()
		h.agentRoutes(mux)
		for path, key := range map[string]string{
			"/api/v1/agents":          "sources",
			"/api/v1/agents/sessions": "sessions",
		} {
			code, out := do(t, mux, "GET", path, "")
			items, ok := out[key].([]any)
			if code != http.StatusOK || !ok || len(items) != 0 {
				t.Fatalf("%s without sources: %d %v", path, code, out)
			}
		}
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
