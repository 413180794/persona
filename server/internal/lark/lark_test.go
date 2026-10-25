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
			if r.URL.Query().Get("scope") != "chats" {
				t.Errorf("default scope = %q, want chats", r.URL.Query().Get("scope"))
			}
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
			if r.URL.Query().Get("scope") != "" {
				t.Errorf("scope leaked into messages")
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
	convs, next, err := c.Conversations(context.Background(), "", "", 0)
	if len(convs) == 2 && !convs[1].IsTopic {
		t.Fatalf("topic group not detected: %+v", convs[1])
	}
	if err != nil || len(convs) != 2 || next != "c2" || convs[0].Preview != "hello world" || !convs[0].AtMe || convs[0].UpdatedAt.IsZero() {
		t.Fatalf("convs = %+v next=%q err=%v", convs, next, err)
	}
	page, err := c.Messages(context.Background(), "1", Window{}, 0, false)
	msgs := page.Messages
	if err != nil || len(msgs) != 1 || !page.HasOlder || msgs[0].Text != "hi" || page.OlderCursor != "9" {
		t.Fatalf("page = %+v %v", page, err)
	}
	threads, err := c.Messages(context.Background(), "3", Window{After: "1"}, 0, true)
	if err != nil || len(threads.Messages) != 1 || threads.Messages[0].ThreadReplyCount != 4 {
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

func TestFeedScopesPinnedAndActions(t *testing.T) {
	var patched, read map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.Method + " " + r.URL.Path {
		case "GET /agent/v1/conversations":
			switch q.Get("scope") {
			case "inbox":
				_ = json.NewEncoder(w).Encode(map[string]any{"hasMore": true, "nextCursor": "x", "items": []any{
					map[string]any{"feedId": "a", "onTopRankAt": "2096-01-01T00:00:00Z", "isBot": true, "groupTags": []any{}},
					map[string]any{"feedId": "b"},
				}})
			case "done":
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
					map[string]any{"feedId": "c", "onTopRankAt": "2097-01-01T00:00:00Z", "isGroup": true, "groupTags": []any{"外部"},
						"preview": map[string]any{"sender": map[string]any{"displayName": "Ann"}, "digest": map[string]any{"elements": []any{}}}},
				}})
			case "group:42":
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"feedId": "d", "unreadCount": 2, "readPosition": "17",
					"unreadMention": map[string]any{"position": "19"}, "autoMute": map[string]any{"reason": "large_chat"}}}})
			default:
				t.Errorf("unexpected scope %q", q.Get("scope"))
			}
		case "GET /agent/v1/conversations/1/messages":
			if q.Get("aroundPosition") != "18" || q.Get("tail") != "" {
				t.Errorf("around query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hasOlder": true, "hasNewer": true, "nextOlderCursor": "10", "nextNewerCursor": "30",
				"items": []any{map[string]any{"id": "m", "position": "18", "badgeCount": 7, "isAtAll": true}}})
		case "PATCH /agent/v1/conversations/c/settings":
			_ = json.NewDecoder(r.Body).Decode(&patched)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "POST /agent/v1/conversations/c/read-position":
			_ = json.NewDecoder(r.Body).Decode(&read)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()
	pinned, err := c.Pinned(ctx)
	if err != nil || len(pinned) != 2 || pinned[0].ID != "c" || pinned[1].ID != "a" {
		t.Fatalf("pinned = %+v %v", pinned, err)
	}
	if pinned[1].Tags[0] != "机器人" || pinned[0].Tags[0] != "外部" || pinned[0].PreviewSender != "Ann" || !pinned[0].Done {
		t.Fatalf("tags/sender/done = %+v", pinned)
	}
	labeled, _, err := c.Conversations(ctx, "label:42", "", 0)
	if err != nil || len(labeled) != 1 || !labeled[0].Muted || labeled[0].ReadPosition != "17" || labeled[0].MentionAt != "19" {
		t.Fatalf("label scope = %+v %v", labeled, err)
	}
	if _, _, err := c.Conversations(ctx, "bogus", "", 0); err == nil {
		t.Fatal("unknown scope should fail")
	}
	page, err := c.Messages(ctx, "1", Window{Around: "18"}, 0, false)
	if err != nil || !page.HasNewer || page.NewerCursor != "30" || page.OlderCursor != "10" || !page.Messages[0].AtMe || *page.Messages[0].BadgeCount != 7 {
		t.Fatalf("around page = %+v %v", page, err)
	}
	yes, no := true, false
	if err := c.UpdateSettings(ctx, "c", Settings{Done: &yes, Pinned: &no}); err != nil || patched["isArchived"] != true || patched["isShortcut"] != false || len(patched) != 2 {
		t.Fatalf("patch = %v %v", patched, err)
	}
	if err := c.UpdateSettings(ctx, "c", Settings{}); err == nil {
		t.Fatal("empty settings should fail")
	}
	n := 3
	if err := c.MarkRead(ctx, "c", Read{Position: "20", BadgeCount: &n, MessageIDs: []string{"m"}}); err != nil || read["maxPosition"] != "20" || read["maxPositionBadgeCount"] != float64(3) {
		t.Fatalf("read = %v %v", read, err)
	}
}
