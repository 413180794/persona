package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Reject canceled writes as Postgres does, so a successful RPC cannot mask a lost todo link.
type todoDisconnectStore struct {
	*sentStore
	linked  bool
	linkErr error
}

func (s *todoDisconnectStore) LinkTodoAgentWithSource(ctx context.Context, _, _, _, _, _, _, _, _, _ string, _ time.Time) ([]string, error) {
	s.linkErr = ctx.Err()
	if s.linkErr != nil {
		return nil, s.linkErr
	}
	s.linked = true
	return nil, nil
}

func TestAcceptedOperationLinksTodoAfterCallerDisconnects(t *testing.T) {
	for _, operation := range []string{"start", "message", "message_dedup"} {
		t.Run(operation, func(t *testing.T) {
			h := newTestHub(t)
			store := &todoDisconnectStore{sentStore: newSentStore()}
			h.store = store
			h.todos.loaded = true
			h.todos.todos["todo-1"] = &Todo{ID: "todo-1", Title: "Test task"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var request *http.Request
			recorder := httptest.NewRecorder()
			if operation == "start" {
				startReceiptNode(t, h, func(context.Context, json.RawMessage) (any, error) {
					cancel()
					return map[string]any{"session": proto.Session{SessionID: "created", Cwd: "/code", Agent: "claude"}}, nil
				})
				request = httptest.NewRequest("POST", "/api/v1/sessions", strings.NewReader(`{"machine_id":"machine","cwd":"/code","tab_name":"test","todo_id":"todo-1"}`)).WithContext(ctx)
				request.Header.Set("X-Jarvis-Op-Id", "00000000-0000-4000-8000-000000000001")
				h.startSession(recorder, request)
			} else {
				pipeNode(t, h, "machine", func(context.Context) error { cancel(); return nil })
				body := `{"text":"hello","todo_id":"todo-1"}`
				if operation == "message_dedup" {
					body = `{"text":"hello","todo_id":"todo-1","client_msg_id":"test-message"}`
				}
				request = httptest.NewRequest("POST", "/api/v1/sessions/machine/s1/messages", strings.NewReader(body)).WithContext(ctx)
				request.SetPathValue("sid", "s1")
				h.sendMessage(recorder, request, h.byID["machine"])
			}
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("caller did not disconnect")
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
			}
			if !store.linked {
				t.Fatalf("accepted %s succeeded but todo link was lost: %v", operation, store.linkErr)
			}
			h.todos.mu.RLock()
			sessionID := "s1"
			if operation == "start" {
				sessionID = "created"
			}
			linked := h.todos.activeBySession[[2]string{"machine", sessionID}]
			h.todos.mu.RUnlock()
			if linked != "todo-1" {
				t.Fatalf("todo active session = %q", linked)
			}
		})
	}
}
