package hub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// Every Feishu route answers 502 with code lark_not_logged_in when lark-connector has lost its Feishu session, and
// without a code for any other upstream failure.
func TestLarkRoutesNotLoggedInCode(t *testing.T) {
	answer := func(w http.ResponseWriter) {}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { answer(w) }))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/lark/conversations", ""},
		{"GET", "/api/v1/lark/unread-count", ""},
		{"GET", "/api/v1/lark/conversations/feed-1/messages", ""},
		{"GET", "/api/v1/lark/messages/m-1/thread", ""},
		{"POST", "/api/v1/lark/conversations/feed-1/messages", `{"text":"hi"}`},
		{"POST", "/api/v1/lark/messages/m-1/replies", `{"text":"hi"}`},
		{"POST", "/api/v1/lark/conversations/feed-1/read", `{"position":"3"}`},
		{"POST", "/api/v1/lark/messages/m-1/reactions", `{"key":"OK"}`},
		{"GET", "/api/v1/lark/home/user", ""},
		{"GET", "/api/v1/lark/media/avatar-1", ""},
		{"POST", "/api/v1/lark/conversations/feed-1/read-states", `{"message_ids":["m-1"]}`},
		{"GET", "/api/v1/lark/conversations/feed-1/members?q=To", ""},
	}
	cases := []struct {
		name, status, body, code string
	}{
		{"logged out", "401", `{"error":{"code":"AUTH_SESSION_EXPIRED","message":"session expired"}}`, "lark_not_logged_in"},
		{"other failure", "500", `{"error":{"code":"INTERNAL","message":"boom"}}`, ""},
		{"rejected token", "401", `{"error":{"code":"TOKEN_INVALID","message":"bad token"}}`, ""},
	}
	for _, c := range cases {
		answer = func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(map[string]int{"401": 401, "500": 500}[c.status])
			_, _ = io.WriteString(w, c.body)
		}
		for _, rt := range routes {
			code, out := do(t, mux, rt.method, rt.path, rt.body)
			if code != http.StatusBadGateway {
				t.Fatalf("%s: %s %s = %d %v, want 502", c.name, rt.method, rt.path, code, out)
			}
			// Any other failure carries the generic code every error has (errors.go), never lark_not_logged_in.
			got, has := out["code"]
			if c.code == "" && has && got != "internal" || c.code != "" && got != c.code {
				t.Fatalf("%s: %s %s code = %v, want %q", c.name, rt.method, rt.path, got, c.code)
			}
			if msg, _ := out["error"].(string); msg == "" || c.code != "" && !strings.Contains(msg, lark.ErrNotLoggedIn.Error()) {
				t.Fatalf("%s: %s %s error = %q", c.name, rt.method, rt.path, msg)
			}
		}
	}
}
