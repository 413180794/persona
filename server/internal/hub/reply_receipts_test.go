package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/lark"
)

func TestReplyReceiptsPersist(t *testing.T) {
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set")
	}
	store, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var calls atomic.Int32
	var wantID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var in struct {
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ClientID != wantID {
			t.Errorf("upstream clientId = %q, want %q (%v)", in.ClientID, wantID, err)
		}
		_, _ = w.Write([]byte(`{"messageId":"message-1"}`))
	}))
	defer upstream.Close()
	for _, path := range []string{"/api/v1/lark/conversations/chat/messages", "/api/v1/inbox/item/reply", "/api/v1/lark/messages/root/replies"} {
		t.Run(path, func(t *testing.T) {
			wantID = lark.NewClientID() // lark-connector takes a UUID only
			newHub := func() (*Hub, *http.ServeMux) {
				h := newTestHub(t)
				h.store = store
				h.lark = lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"})
				h.connectors = connector.NewRegistry(nil) // an inbox reply goes to the item's connector (inbox_own.go)
				h.registerConnectors()
				mux := http.NewServeMux()
				h.larkRoutes(mux)
				h.inboxRoutes(mux)
				return h, mux
			}
			h, mux := newHub()
			h.inbox.items["item"] = &InboxItem{ID: "item", Lark: &InboxLark{FeedID: "chat"}}
			request := func(mux *http.ServeMux, body string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
				return w
			}
			body := `{"text":"hello","client_id":"` + wantID + `"}`
			before := calls.Load()
			first := request(mux, body)
			if first.Code != http.StatusOK || calls.Load() != before+1 {
				t.Fatalf("initial reply = %d %s, calls = %d", first.Code, first.Body, calls.Load())
			}
			// The new hub has no inbox item: the original reply must come from Postgres.
			_, restarted := newHub()
			second := request(restarted, body)
			if second.Code != first.Code || second.Body.String() != first.Body.String() || calls.Load() != before+1 {
				t.Fatalf("replay = %d %s, calls = %d", second.Code, second.Body, calls.Load())
			}
			conflict := request(restarted, `{"text":"changed","client_id":"`+wantID+`"}`)
			if conflict.Code != http.StatusConflict || calls.Load() != before+1 {
				t.Fatalf("reused ID = %d %s", conflict.Code, conflict.Body)
			}
		})
	}
}

type failingPrefsStore struct{ Store }

func (failingPrefsStore) SetDevicePrefs(context.Context, string, json.RawMessage) error {
	return errors.New("storage unavailable")
}

func TestDevicePrefsStorageFailure(t *testing.T) {
	h := newTestHub(t)
	if err := h.push.setPrefs("device", defaultPrefs()); err != nil {
		t.Fatal(err)
	}
	h.push.store = failingPrefsStore{}
	srv := httptest.NewServer(h.routes())
	defer srv.Close()
	r, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/devices/device/prefs", strings.NewReader(`{"input":false,"done":"off"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || h.push.prefs("device") != defaultPrefs() {
		t.Fatalf("failed write = %d, prefs = %+v", resp.StatusCode, h.push.prefs("device"))
	}
}
