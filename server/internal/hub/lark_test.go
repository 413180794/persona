package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestLarkSendClientID(t *testing.T) {
	var received []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/conversations/chat/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Text     string `json:"text"`
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Text != "hello" || body.ClientID == "" {
			t.Errorf("unexpected body: %+v", body)
		}
		received = append(received, body.ClientID)
		if len(received) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"messageId":"message-1"}`))
	}))
	defer upstream.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"}),
		store: &clientOpsStore{ops: map[string]db.ClientOp{}}}
	mux := http.NewServeMux()
	h.larkRoutes(mux)
	const clientID = "00000000-0000-4000-8000-000000000001"
	for i, body := range []string{
		`{"text":"hello","client_id":"` + clientID + `"}`,
		`{"text":"hello","client_id":"` + clientID + `"}`,
		`{"text":"hello"}`,
		`{"text":"hello"}`,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/lark/conversations/chat/messages", strings.NewReader(body)))
		want := http.StatusOK
		if i < 2 {
			want = http.StatusBadGateway
		}
		if rec.Code != want {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, want)
		}
	}
	if len(received) != 3 || received[0] != clientID || received[1] == received[2] {
		t.Fatalf("upstream client IDs = %v", received)
	}
	if id := larkInboxClientID("item-1"); id == "" || id != larkInboxClientID("item-1") || id == larkInboxClientID("item-2") {
		t.Fatalf("unstable or shared inbox client ID: %q", id)
	}
}

// threadReplyID is the app's client_id for a thread reply (a UUID: lark-connector takes nothing else).
const threadReplyID = "00000000-0000-4000-8000-000000000002"

func TestLarkThreadReplyReceipt(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/agent/v1/messages/root%2Fmessage/replies" {
					t.Errorf("unexpected target: %s %s", r.Method, r.URL.EscapedPath())
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["text"] != "hello" || body["clientId"] != threadReplyID || body["replyMode"] != "thread" {
					t.Errorf("unexpected body: %v", body)
				}
				w.WriteHeader(status)
				if status < 300 {
					_, _ = w.Write([]byte(`{"messageId":"message-1"}`))
				}
			}))
			defer upstream.Close()
			h := &Hub{lark: lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"}),
				store: &clientOpsStore{ops: map[string]db.ClientOp{}}}
			mux := http.NewServeMux()
			h.larkRoutes(mux)
			request := func(path, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
				return w
			}
			const path = "/api/v1/lark/messages/root%2Fmessage/replies"
			const body = `{"text":"hello","thread":true,"client_id":"` + threadReplyID + `"}`
			first := request(path, body)
			want := http.StatusOK
			if status == http.StatusBadGateway {
				want = status
			}
			second := request(path, body)
			if first.Code != want || second.Code != first.Code || second.Body.String() != first.Body.String() || calls != 1 {
				t.Fatalf("reply/replay = %d %s / %d %s, calls = %d", first.Code, first.Body, second.Code, second.Body, calls)
			}
			if w := request("/api/v1/lark/messages/other/replies", body); w.Code != http.StatusConflict || calls != 1 {
				t.Fatalf("changed target = %d %s, calls = %d", w.Code, w.Body, calls)
			}
			if w := request(path, `{"text":" "}`); w.Code != http.StatusBadRequest || calls != 1 {
				t.Fatalf("empty text = %d %s, calls = %d", w.Code, w.Body, calls)
			}
		})
	}
}
