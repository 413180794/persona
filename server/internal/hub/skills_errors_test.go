package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

type skillsErrorNode struct {
	err   error
	calls int
}

func (n *skillsErrorNode) Call(_ context.Context, method string, _, _ any) error {
	if method != "skills.list" {
		return fmt.Errorf("unexpected method: %s", method)
	}
	n.calls++
	return n.err
}

func (*skillsErrorNode) Notify(string, any) error { return nil }
func (*skillsErrorNode) Done() <-chan struct{}    { return nil }
func (*skillsErrorNode) Close(error)              {}

func TestSkillsNodeErrorsUseSharedHTTPMapping(t *testing.T) {
	for _, tt := range []struct {
		name, code, key string
		err             error
		status          int
		delivered       any
		retryable       any
	}{
		{"offline", "node_unreachable", "rpc-errors.offline", nil, http.StatusServiceUnavailable, "none", true},
		{"manager offline", "assistant_offline", "hub-assistant_link.offline", fmt.Errorf("manager: %w", errManagerOffline), http.StatusServiceUnavailable, nil, nil},
		{"deadline", "timeout", "rpc-errors.timeout", context.DeadlineExceeded, http.StatusBadGateway, nil, nil},
		{"rpc timeout", "timeout", "rpc-errors.timeout", &rpc.Error{Code: "timeout", Message: "node deadline"}, http.StatusBadGateway, nil, nil},
		{"rpc unreachable", "node_unreachable", "rpc-errors.unreachable", &rpc.Error{Code: "node_unreachable", Message: "connection lost"}, http.StatusBadGateway, nil, nil},
		{"rpc outdated", "node_outdated", "rpc-errors.node_outdated", &rpc.Error{Code: "node_outdated", Message: "unknown method skills.list", Data: json.RawMessage(`{"delivered":"none","retryable":false}`)}, http.StatusConflict, "none", false},
	} {
		for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
			t.Run(tt.name+"/"+string(lang), func(t *testing.T) {
				h := newTestHub(t)
				m := newMachine(h, MachineConfig{ID: "box", Name: "Example computer"})
				registerMachine(h, m)
				node := &skillsErrorNode{err: tt.err}
				if tt.err != nil {
					m.peer = node
				}
				h.assistant.profile = &assistantwire.Profile{MachineID: m.cfg.ID}
				r := httptest.NewRequest(http.MethodGet, "/api/v1/skills", nil)
				r.Header.Set("Accept-Language", string(lang))
				w := httptest.NewRecorder()
				logRequests(h, http.HandlerFunc(h.serveSkills)).ServeHTTP(w, r)
				body := errorResponse(t, w)
				if w.Code != tt.status || body["code"] != tt.code || body["error"] != i18n.T(lang, tt.key) {
					t.Fatalf("response: %d %v", w.Code, body)
				}
				if body["delivered"] != tt.delivered || body["retryable"] != tt.retryable {
					t.Fatalf("outcome fields changed: %v", body)
				}
				if tt.err != nil && node.calls != 1 {
					t.Fatalf("skills.list calls = %d, want 1", node.calls)
				}
			})
		}
	}
}
