package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestLarkMessageFeedPreservesAuthorsWithoutSendingToModel(t *testing.T) {
	ctx := context.Background()
	hub := newFakeHub()
	s := newServiceOn(t, hub, &noticeTestStore{inboxMemStore: newInboxMemStore()})
	s.originSet(ctx, "server", "lark-1", OriginLarkAgent, larkContextExtension, larkContextExtension)
	s.originSet(ctx, "server", "user-1", OriginUser, "", "")
	now := time.Now().UTC().Truncate(time.Millisecond)
	author := proto.MessageAuthor{ID: "51", Name: "测试联系人", Avatar: "avatar:51:" + strings.Repeat("a", 64),
		Source: proto.MessageSource{Kind: "lark", ConversationID: "100", Title: "测试群"}}
	owner := proto.MessageAuthor{ID: "50", Name: "测试机主", Source: author.Source, Owner: true, OnBehalf: true}
	first := larkFeedMessage{"90", now.Add(-time.Minute), "普通群消息", author}
	reply := larkFeedMessage{"91", now, "确认发出的答复", owner}

	call := func(conversation, extension, machine string, messages ...larkFeedMessage) int {
		t.Helper()
		data, err := json.Marshal(map[string]any{"machine_id": machine, "messages": messages})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/conversations/"+conversation+"/lark-messages", bytes.NewReader(data))
		r.Header.Set(RoleHeader, roleExtension)
		r.Header.Set(ExtensionHeader, extension)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w.Code
	}
	if got := call("lark-1", "other-extension", "server", first); got != http.StatusForbidden {
		t.Fatalf("unrelated extension status = %d", got)
	}
	for _, target := range [][2]string{{"user-1", "server"}, {"lark-1", "other-machine"}} {
		if got := call(target[0], larkContextExtension, target[1], first); got != http.StatusNotFound {
			t.Fatalf("foreign conversation status = %d", got)
		}
	}
	bad := first
	bad.Author.Avatar = "https://example.test/person.png"
	if got := call("lark-1", larkContextExtension, "server", first, bad); got != http.StatusBadRequest {
		t.Fatalf("external avatar status = %d", got)
	}
	if rows, err := s.rawFeed(ctx, "lark-1", time.Time{}); err != nil || len(rows) != 0 {
		t.Fatalf("invalid batch wrote %d rows: %v", len(rows), err)
	}
	for range 2 {
		if got := call("lark-1", larkContextExtension, "server", first, reply); got != http.StatusNoContent {
			t.Fatalf("publish status = %d", got)
		}
	}
	rows, err := s.rawFeed(ctx, "lark-1", first.At.Add(time.Second))
	if err != nil || len(rows) != 2 || hub.sent() != 0 {
		t.Fatalf("rows=%d sends=%d err=%v", len(rows), hub.sent(), err)
	}
	for i, want := range []larkFeedMessage{first, reply} {
		var got larkFeedMessage
		if rows[i].Kind != feedLarkMessage || json.Unmarshal(rows[i].Body, &got) != nil || got != want {
			t.Errorf("message = %+v, want %+v", got, want)
		}
	}
}
