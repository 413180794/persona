package hub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestLarkReadStatesRoute(t *testing.T) {
	var asked [][]map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/messages/read-states" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			Pairs []map[string]string `json:"pairs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		asked = append(asked, body.Pairs)
		_, _ = io.WriteString(w, `{"items":[{"messageId":"m-1","readCount":3,"unreadCount":1}]}`)
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	code, out := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/read-states", `{"message_ids":["m-1","m-2"]}`)
	states, _ := out["states"].([]any)
	if code != 200 || len(states) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if s := states[0].(map[string]any); s["message_id"] != "m-1" || s["read_count"] != 3.0 || s["unread_count"] != 1.0 {
		t.Fatalf("state = %v", s)
	}
	if len(asked) != 1 || len(asked[0]) != 2 || asked[0][1]["feedId"] != "feed-1" || asked[0][1]["messageId"] != "m-2" {
		t.Fatalf("pairs sent %v", asked)
	}

	// No ids: an empty list, not null, without asking lark-connector.
	if code, out := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/read-states", `{"message_ids":[]}`); code != 200 || fmt.Sprint(out["states"]) != "[]" {
		t.Fatalf("empty: %d %v", code, out)
	}

	ids := make([]string, lark.MaxReadStates+1)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"m-%d"`, i)
	}
	for _, body := range []string{`{"message_ids":[` + strings.Join(ids, ",") + `]}`, `not json`} {
		if code, _ := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/read-states", body); code != 400 {
			t.Fatalf("%.40s: %d, want 400", body, code)
		}
	}
	// Exactly the cap is fine.
	if code, _ := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/read-states", `{"message_ids":[`+strings.Join(ids[:lark.MaxReadStates], ",")+`]}`); code != 200 {
		t.Fatalf("100 ids: %d, want 200", code)
	}
	if len(asked) != 2 || len(asked[1]) != lark.MaxReadStates {
		t.Fatalf("lark-connector asked %d times", len(asked))
	}
}
