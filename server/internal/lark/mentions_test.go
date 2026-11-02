package lark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSendAndReplyMentions(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = io.WriteString(w, `{"messageId":"7000000000000000005"}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()

	ms := []Mention{{UserID: "u-1", Name: "Tom"}, {UserID: "all", Name: "所有人"}}
	if _, err := c.Send(ctx, "feed-1", "@Tom @所有人 周六见", "", ms...); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reply(ctx, "m-1", "@Tom 收到", true, false, "", ms[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(ctx, "feed-1", "no one", ""); err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"userId": "u-1", "name": "Tom"}, map[string]any{"userId": "all", "name": "所有人"}}
	if got := bodies[0]["mentions"]; !reflect.DeepEqual(got, want) || bodies[0]["text"] != "@Tom @所有人 周六见" {
		t.Fatalf("send body %v", bodies[0])
	}
	if got := bodies[1]["mentions"]; !reflect.DeepEqual(got, want[:1]) || bodies[1]["replyMode"] != "thread" {
		t.Fatalf("reply body %v", bodies[1])
	}
	if _, ok := bodies[2]["mentions"]; ok {
		t.Fatalf("no mentions: body still has the key: %v", bodies[2])
	}
}

func TestMembers(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = io.WriteString(w, `{"items":[
			{"userId":"u-1","displayName":"Tom Lee","groupNickname":"Tom","avatarUrl":"/api/media/avatar-1"},
			{"userId":"u-2","displayName":"小周","avatarUrl":"https://example.com/a.png"},
			{"displayName":"no id"}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()

	got, err := c.Members(ctx, "feed-1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Member{{UserID: "u-1", Name: "Tom", Avatar: "/api/v1/lark/media/avatar-1"}, {UserID: "u-2", Name: "小周", Avatar: "https://example.com/a.png"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Members = %+v", got)
	}
	if _, err := c.Members(ctx, "feed-1", " 周 ", 500); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Members(ctx, "feed-1", "  ", 20); err != nil {
		t.Fatal(err)
	}
	wantAsked := []string{
		"/agent/v1/conversations/feed-1/members?limit=50",
		"/agent/v1/conversations/feed-1/members/search?limit=200&q=%E5%91%A8",
		"/agent/v1/conversations/feed-1/members?limit=20",
	}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Fatalf("asked %q", asked)
	}
}
