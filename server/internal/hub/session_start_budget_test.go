package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

type startBudgetStore struct {
	*memStore
	remaining time.Duration
}

func (s *startBudgetStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == modelKey("agent.new") {
		if deadline, ok := ctx.Deadline(); ok {
			s.remaining = time.Until(deadline)
		}
	}
	return s.memStore.Setting(ctx, key, out)
}

// Codex and TRAEX return their session only after startup even when a client asks for async.
// Observe the operation context on the actual HTTP/model/RPC path without waiting for a timeout.
func TestSessionStartBudgetMatchesNodeStartup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		agentType  string
		configured string
		async      bool
		short      bool
	}{
		{name: "default Claude async", async: true, short: true},
		{name: "explicit Claude async", agentType: "claude", async: true, short: true},
		{name: "explicit Codex async", agentType: "codex", async: true},
		{name: "explicit TRAEX async", agentType: "traex", async: true},
		{name: "configured Codex async", configured: "codex", async: true},
		{name: "configured TRAEX async", configured: "traex", async: true},
		{name: "Claude sync"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHub(t)
			store := &startBudgetStore{memStore: newMemStore()}
			h.store = store
			startReceiptNode(t, h, func(_ context.Context, params json.RawMessage) (any, error) {
				var in struct {
					AgentType string `json:"agent_type"`
				}
				if err := json.Unmarshal(params, &in); err != nil {
					return nil, err
				}
				want := tc.agentType
				if want == "" {
					want = tc.configured
				}
				if in.AgentType != want {
					t.Errorf("node agent_type = %q, want %q", in.AgentType, want)
				}
				return map[string]any{"session": proto.Session{SessionID: "created"}}, nil
			})
			h.byID["machine"].cfg.Launchers = []sessionLauncher{{Launcher: proto.Launcher{Name: "wrapper"}, AgentType: tc.configured}}
			body, err := json.Marshal(map[string]any{
				"machine_id": "machine", "cwd": "/code", "launcher": "wrapper", "prompt": "hello", "tab_name": "test",
				"agent_type": tc.agentType, "async": tc.async,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			h.startSession(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("start = %d %s", w.Code, w.Body)
			}
			min, max := 60*time.Second, 101*time.Second
			if tc.short {
				min, max = 10*time.Second, 16*time.Second
			}
			if store.remaining < min || store.remaining > max {
				t.Fatalf("operation budget = %s, want between %s and %s", store.remaining, min, max)
			}
		})
	}
}
