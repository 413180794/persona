package hub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestLarkSendAndReplyMentions(t *testing.T) {
	type call struct {
		path     string
		text     string
		mentions any
	}
	var got []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, call{r.URL.Path, fmt.Sprint(body["text"]), body["mentions"]})
		_, _ = io.WriteString(w, `{"messageId":"7000000000000000006"}`)
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	ms := `[{"user_id":"u-1","name":"Tom"},{"user_id":"all","name":"所有人"}]`
	if code, out := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/messages", `{"text":"@Tom @所有人 周六见","mentions":`+ms+`}`); code != 200 {
		t.Fatalf("send: %d %v", code, out)
	}
	if code, out := do(t, mux, "POST", "/api/v1/lark/messages/m-1/replies", `{"text":"@Tom 收到","thread":true,"mentions":[{"user_id":"u-1","name":"Tom"}]}`); code != 200 {
		t.Fatalf("reply: %d %v", code, out)
	}
	if code, _ := do(t, mux, "POST", "/api/v1/lark/conversations/feed-1/messages", `{"text":"hi"}`); code != 200 {
		t.Fatal("send without mentions")
	}
	want := []call{
		{"/agent/v1/conversations/feed-1/messages", "@Tom @所有人 周六见", []any{map[string]any{"userId": "u-1", "name": "Tom"}, map[string]any{"userId": "all", "name": "所有人"}}},
		{"/agent/v1/messages/m-1/replies", "@Tom 收到", []any{map[string]any{"userId": "u-1", "name": "Tom"}}},
		{"/agent/v1/conversations/feed-1/messages", "hi", nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %+v", got)
	}

	many := make([]string, lark.MaxMentions+1)
	for i := range many {
		many[i] = fmt.Sprintf(`{"user_id":"u-%d","name":"n%d"}`, i, i)
	}
	for _, bad := range []string{"[" + strings.Join(many, ",") + "]", `[{"user_id":"","name":"Tom"}]`, `[{"user_id":"u-1"}]`} {
		for _, path := range []string{"/api/v1/lark/conversations/feed-1/messages", "/api/v1/lark/messages/m-1/replies"} {
			if code, _ := do(t, mux, "POST", path, `{"text":"@Tom hi","mentions":`+bad+`}`); code != 400 {
				t.Fatalf("%s %.40s: %d, want 400", path, bad, code)
			}
		}
	}
	if len(got) != 3 {
		t.Fatalf("a rejected request reached lark-connector: %+v", got[3:])
	}
}

func TestLarkMembersRoute(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = io.WriteString(w, `{"items":[{"userId":"u-1","displayName":"Tom Lee","groupNickname":"Tom","avatarUrl":"/api/media/avatar-1"}]}`)
	}))
	defer srv.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	mux := http.NewServeMux()
	h.larkRoutes(mux)

	code, out := do(t, mux, "GET", "/api/v1/lark/conversations/feed-1/members", "")
	members, _ := out["members"].([]any)
	if code != 200 || len(members) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if m := members[0].(map[string]any); m["user_id"] != "u-1" || m["name"] != "Tom" || m["avatar"] != "/api/v1/lark/media/avatar-1" {
		t.Fatalf("member %v", m)
	}
	if code, _ := do(t, mux, "GET", "/api/v1/lark/conversations/feed-1/members?q=To&limit=300", ""); code != 200 {
		t.Fatal(code)
	}
	want := []string{"/agent/v1/conversations/feed-1/members?limit=50", "/agent/v1/conversations/feed-1/members/search?limit=200&q=To"}
	if !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked %q", asked)
	}
}
