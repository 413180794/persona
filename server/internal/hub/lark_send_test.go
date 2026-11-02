package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestLarkSendRoutePassesClientID(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/conversations/feed-1/messages" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			ClientID string `json:"clientId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body.ClientID)
		_, _ = io.WriteString(w, `{"messageId":"7000000000000000002"}`)
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	const id = "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c"
	code, out := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/messages", `{"text":"hi","client_id":"`+id+`"}`)
	if code != 200 || out["message_id"] != "7000000000000000002" {
		t.Fatalf("%d %v", code, out)
	}
	if got[0] != id {
		t.Fatalf("clientId = %q, want %q", got[0], id)
	}

	// Older apps send no client_id; a malformed one is treated the same. Both still send, with a fresh id.
	for _, body := range []string{`{"text":"hi"}`, `{"text":"hi","client_id":"local-1"}`} {
		if code, out := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/messages", body); code != 200 {
			t.Fatalf("%s: %d %v", body, code, out)
		}
	}
	if len(got) != 3 || !lark.ValidClientID(got[1]) || !lark.ValidClientID(got[2]) || got[1] == got[2] || got[2] == "local-1" {
		t.Fatalf("clientIds = %q", got)
	}
}

func TestLarkReplyRoute(t *testing.T) {
	type call struct {
		path, replyMode, clientID, text string
	}
	var got []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text      string `json:"text"`
			ReplyMode string `json:"replyMode"`
			ClientID  string `json:"clientId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, call{r.URL.Path, body.ReplyMode, body.ClientID, body.Text})
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"messageId":"7000000000000000004","feedId":"feed-1","replyToMessageId":"m-1"}`)
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	const id = "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c"
	code, out := do(t, mux, "POST", "/api/v1/lark/messages/m-1/replies", `{"text":"got it","thread":true,"client_id":"`+id+`"}`)
	if code != 200 || out["ok"] != true || out["message_id"] != "7000000000000000004" {
		t.Fatalf("%d %v", code, out)
	}
	if g := got[0]; g.path != "/agent/v1/messages/m-1/replies" || g.replyMode != "thread" || g.clientID != id || g.text != "got it" {
		t.Fatalf("thread reply sent %+v", g)
	}

	// Without thread it quotes in the chat; a missing or malformed client_id gets a fresh one.
	for _, body := range []string{`{"text":"got it"}`, `{"text":"got it","thread":false,"client_id":"local-1"}`} {
		if code, out := do(t, mux, "POST", "/api/v1/lark/messages/m-1/replies", body); code != 200 {
			t.Fatalf("%s: %d %v", body, code, out)
		}
	}
	if len(got) != 3 || got[1].replyMode != "" || got[2].replyMode != "" ||
		!lark.ValidClientID(got[1].clientID) || !lark.ValidClientID(got[2].clientID) || got[1].clientID == got[2].clientID {
		t.Fatalf("direct replies sent %+v", got[1:])
	}

	for _, body := range []string{`{"text":"   "}`, `{"thread":true}`, `not json`} {
		if code, _ := do(t, mux, "POST", "/api/v1/lark/messages/m-1/replies", body); code != 400 {
			t.Fatalf("%s: %d, want 400", body, code)
		}
	}
	if len(got) != 3 {
		t.Fatalf("a rejected request reached lark-connector: %+v", got[3:])
	}
}
