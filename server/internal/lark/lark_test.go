package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConversationsAndMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/agent/v1/conversations":
			_ = json.NewEncoder(w).Encode(map[string]any{"hasMore": true, "nextCursor": "c2", "items": []any{
				map[string]any{"feedId": "1", "name": "Team", "isGroup": true, "unreadCount": 3, "hasUnreadAtMe": true,
					"preview": map[string]any{"displayAt": "2026-10-24T15:07:41.916Z", "isMine": false,
						"digest": map[string]any{"elements": []any{map[string]any{"content": "hello  ", "kind": "text"}, map[string]any{"content": "world", "kind": "text"}}}}},
				map[string]any{"feedId": "2", "name": "Hidden", "isHidden": true},
			}})
		case "/agent/v1/conversations/1/messages":
			if r.URL.Query().Get("tail") != "1" {
				t.Errorf("latest page should use tail")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hasOlder": true, "items": []any{
				map[string]any{"id": "m1", "position": "9", "senderName": "A", "messageTypeName": "TEXT", "textPreview": "hi", "createdAt": "2026-10-24T15:00:00Z"},
				map[string]any{"id": "m2", "position": "10", "isDeleted": true},
			}})
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	convs, next, err := c.Conversations(context.Background(), "", 0)
	if err != nil || len(convs) != 1 || next != "c2" || convs[0].Preview != "hello world" || !convs[0].AtMe || convs[0].UpdatedAt.IsZero() {
		t.Fatalf("convs = %+v next=%q err=%v", convs, next, err)
	}
	msgs, older, err := c.Messages(context.Background(), "1", "", 0)
	if err != nil || len(msgs) != 1 || !older || msgs[0].Text != "hi" {
		t.Fatalf("msgs = %+v %v %v", msgs, older, err)
	}
}
