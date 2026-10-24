package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
				map[string]any{"feedId": "3", "name": "Topics", "chatMode": "topic"},
			}})
		case "/agent/v1/conversations/3/threads":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "t1", "position": "5", "threadReplyCount": 4}}})
		case "/agent/v1/conversations/1/messages":
			if r.URL.Query().Get("tail") != "1" {
				t.Errorf("latest page should use tail")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hasOlder": true, "items": []any{
				map[string]any{"id": "m1", "position": "9", "senderName": "A", "messageTypeName": "TEXT", "textPreview": "hi", "createdAt": "2026-10-24T15:00:00Z",
					"senderAvatarUrl": "/api/media/av1",
					"parts":           []any{map[string]any{"kind": "image", "url": "/api/media/img1", "metadata": map[string]any{"width": 10}}},
					"replyTo":         map[string]any{"id": "m0", "senderName": "B", "textPreview": "q", "parts": []any{}},
					"reactions":       []any{map[string]any{"key": "THUMBSUP", "count": 2, "isMine": true, "userNames": []any{"A", "B"}}, map[string]any{"key": "OK", "count": 0}}},
				map[string]any{"id": "m2", "position": "10", "isDeleted": true},
			}})
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	convs, next, err := c.Conversations(context.Background(), "", 0)
	if len(convs) == 2 && !convs[1].IsTopic {
		t.Fatalf("topic group not detected: %+v", convs[1])
	}
	if err != nil || len(convs) != 2 || next != "c2" || convs[0].Preview != "hello world" || !convs[0].AtMe || convs[0].UpdatedAt.IsZero() {
		t.Fatalf("convs = %+v next=%q err=%v", convs, next, err)
	}
	msgs, older, err := c.Messages(context.Background(), "1", "", 0, false)
	if err != nil || len(msgs) != 1 || !older || msgs[0].Text != "hi" {
		t.Fatalf("msgs = %+v %v %v", msgs, older, err)
	}
	threads, _, err := c.Messages(context.Background(), "3", "", 0, true)
	if err != nil || len(threads) != 1 || threads[0].ThreadReplyCount != 4 {
		t.Fatalf("threads = %+v %v", threads, err)
	}
	m := msgs[0]
	if m.SenderAvatar != "/api/v1/lark/media/av1" || !strings.Contains(string(m.Parts), `"/api/v1/lark/media/img1"`) {
		t.Fatalf("media not rewritten: avatar=%q parts=%s", m.SenderAvatar, m.Parts)
	}
	if m.ReplyTo == nil || m.ReplyTo.SenderName != "B" || len(m.Reactions) != 1 || !m.Reactions[0].Mine || len(m.Reactions[0].Users) != 2 {
		t.Fatalf("reply/reactions = %+v %+v", m.ReplyTo, m.Reactions)
	}
}

func TestEmojisAndMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agent/v1/resources/emojis":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
				map[string]any{"key": "THUMBSUP", "label": "Like", "imageUrl": "/api/media/emoji_thumbsup.png", "skinKeys": []any{"MediumThumbsup", "THUMBSUP"}},
			}})
		case "/agent/v1/media/k1":
			if r.URL.Query().Get("size") != "120" {
				t.Errorf("size not forwarded: %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	emojis, err := c.Emojis(context.Background())
	if err != nil || emojis["mediumthumbsup"].URL != "/api/v1/lark/media/emoji_thumbsup.png" || emojis["thumbsup"].Label != "Like" {
		t.Fatalf("emojis = %+v %v", emojis, err)
	}
	rec := httptest.NewRecorder()
	if err := c.Media(context.Background(), rec, "/media/k1", url.Values{"size": {"120"}}); err != nil || rec.Body.String() != "png" || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("media = %q %v", rec.Body.String(), err)
	}
	if err := c.Media(context.Background(), httptest.NewRecorder(), "/media/missing", nil); err == nil {
		t.Fatal("missing media should fail")
	}
}
