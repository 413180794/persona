package lark

import (
	"context"
	"encoding/json"
	"io"
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
			_ = json.NewEncoder(w).Encode(map[string]any{"groups": []any{map[string]any{"title": "默认表情", "keys": []any{"THUMBSUP"}}}, "items": []any{
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
	emojis, groups, err := c.Emojis(context.Background())
	if len(groups) != 1 || groups[0].Keys[0] != "THUMBSUP" {
		t.Fatalf("groups = %+v", groups)
	}
	if err != nil || emojis["mediumthumbsup"].URL != "/api/v1/lark/media/emoji_thumbsup.png" || emojis["thumbsup"].Label != "Like" {
		t.Fatalf("emojis = %+v %v", emojis, err)
	}
	rec := httptest.NewRecorder()
	if err := c.Media(context.Background(), rec, "/media/k1", url.Values{"size": {"120"}}, nil); err != nil || rec.Body.String() != "png" || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("media = %q %v", rec.Body.String(), err)
	}
	if err := c.Media(context.Background(), httptest.NewRecorder(), "/media/missing", nil, nil); err == nil {
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

func TestProfileReactionsAndMoments(t *testing.T) {
	var reacted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /agent/v1/users/u1":
			if r.URL.Query().Get("feedId") != "f1" {
				t.Errorf("feedId not forwarded")
			}
			_, _ = w.Write([]byte(`{"displayName":"Ann","avatarUrl":"/api/media/a1"}`))
		case "POST /agent/v1/conversations":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["type"] != "p2p" {
				t.Errorf("chat body = %v", in)
			}
			_, _ = w.Write([]byte(`{"created":false,"feedId":"f9"}`))
		case "POST /agent/v1/messages/m1/reactions":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			reacted = append(reacted, "+"+in["key"])
		case "DELETE /agent/v1/messages/m1/reactions/OK":
			reacted = append(reacted, "-OK")
		case "GET /agent/v1/reactions/recent":
			_, _ = w.Write([]byte(`{"quick":["OK","THUMBSUP"],"recent":["Fire","ok"]}`))
		case "GET /agent/v1/moments/posts/p1":
			_, _ = w.Write([]byte(`{"id":"p1","images":[{"url":"/api/media/i1"}]}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()
	raw, err := c.Profile(ctx, "u1", "f1")
	if err != nil || !strings.Contains(string(raw), `"/api/v1/lark/media/a1"`) {
		t.Fatalf("profile = %s %v", raw, err)
	}
	if feed, err := c.DirectChat(ctx, "u1"); err != nil || feed != "f9" {
		t.Fatalf("chat = %q %v", feed, err)
	}
	if err := c.React(ctx, "m1", "Fire", false); err != nil {
		t.Fatal(err)
	}
	if err := c.React(ctx, "m1", "OK", true); err != nil || strings.Join(reacted, ",") != "+Fire,-OK" {
		t.Fatalf("reacted = %v %v", reacted, err)
	}
	keys, err := c.QuickReactions(ctx)
	if err != nil || strings.Join(keys[:3], ",") != "Fire,ok,THUMBSUP" {
		t.Fatalf("quick = %v %v", keys, err)
	}
	post, err := c.MomentsPost(ctx, "p1")
	if err != nil || !strings.Contains(string(post), "/api/v1/lark/media/i1") {
		t.Fatalf("post = %s %v", post, err)
	}
}

func TestRawProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /agent/v1/documents/recent":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"items":[{"a":"/api/media/k","b":"/api/documents/media/x","c":"/api/messages/1/media/k"}]}`))
		case "GET /agent/v1/mail/threads/a%2Fb":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "GET /agent/v1/minutes/t/media":
			w.Header().Set("Content-Type", "audio/mp4")
			if r.Header.Get("Range") == "bytes=0-1" {
				w.Header().Set("Content-Range", "bytes 0-1/3")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write([]byte("mp"))
				return
			}
			_, _ = w.Write([]byte("mp4"))
		case "POST /agent/v1/search/page":
			b, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()
	rec := httptest.NewRecorder()
	if err := c.Raw(ctx, rec, "GET", "/documents/recent", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != `{"items":[{"a":"/api/v1/lark/media/k","b":"/api/v1/lark/raw/documents/media/x","c":"/api/v1/lark/messages/1/media/k"}]}` {
		t.Fatalf("rewritten = %s", got)
	}
	rec = httptest.NewRecorder()
	if err := c.Raw(ctx, rec, "GET", "/mail/threads/a%2Fb", nil, nil, nil); err != nil || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("encoded path = %q %v", rec.Body.String(), err)
	}
	rec = httptest.NewRecorder()
	if err := c.Raw(ctx, rec, "GET", "/minutes/t/media", nil, nil, nil); err != nil || rec.Body.String() != "mp4" || rec.Header().Get("Content-Type") != "audio/mp4" {
		t.Fatalf("binary = %q %v", rec.Body.String(), err)
	}
	rec = httptest.NewRecorder()
	if err := c.Raw(ctx, rec, "GET", "/minutes/t/media", nil, nil, http.Header{"Range": {"bytes=0-1"}}); err != nil || rec.Code != 206 || rec.Body.String() != "mp" || rec.Header().Get("Content-Range") != "bytes 0-1/3" {
		t.Fatalf("range = %d %q %v", rec.Code, rec.Body.String(), err)
	}
	rec = httptest.NewRecorder()
	if err := c.Raw(ctx, rec, "POST", "/search/page", nil, strings.NewReader(`{"tab":"messages"}`), nil); err != nil || rec.Body.String() != `{"tab":"messages"}` {
		t.Fatalf("search page = %q %v", rec.Body.String(), err)
	}
	for _, bad := range []struct{ method, path string }{{"POST", "/conversations"}, {"DELETE", "/flags"}, {"GET", "/conversations/1/qr-code"},
		{"GET", "/links/open"}, {"GET", "/maintenance/export"}, {"GET", "/../x"}} {
		if err := c.Raw(ctx, httptest.NewRecorder(), bad.method, bad.path, nil, nil, nil); err == nil {
			t.Errorf("%s %s should be refused", bad.method, bad.path)
		}
	}
}

func TestParseEvent(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"type":"message.upserted","feedId":"f1","messageId":"m","message":{}}`))
	if !ok || ev.Type != "message.upserted" || len(ev.FeedIDs) != 1 || ev.FeedIDs[0] != "f1" {
		t.Fatalf("message event = %+v %v", ev, ok)
	}
	ev, ok = parseEvent([]byte(`{"type":"conversations.upserted","conversations":[{"feedId":"a"},{"feedId":"b"}]}`))
	if !ok || len(ev.FeedIDs) != 2 {
		t.Fatalf("conversations event = %+v %v", ev, ok)
	}
	if _, ok := parseEvent([]byte(`{"type":"message.cot.updated","messageId":"m"}`)); ok {
		t.Fatal("unrelated events should be dropped")
	}
}

func TestParseEventCarriesTheMessage(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"type":"message.upserted","feedId":"f1","messageId":"m1","message":{"id":"m1","senderName":"我","isMine":true,
		"messageTypeName":"TEXT","textPreview":"/agent 看下","createdAt":"2026-10-31T01:00:00Z","threadId":"t1","threadMessageType":"reply"}}`))
	if !ok || ev.Message == nil || ev.Message.ID != "m1" || !ev.Message.IsMine || ev.Message.Text != "/agent 看下" ||
		ev.Message.ThreadID != "t1" || ev.Message.ThreadKind != "reply" || len(ev.FeedIDs) != 1 {
		t.Fatalf("%+v %+v", ev, ev.Message)
	}
	if ev, _ := parseEvent([]byte(`{"type":"conversation.upserted","feedId":"f1"}`)); ev.Message != nil {
		t.Fatal("only message events carry a message")
	}
}

func TestSendClientID(t *testing.T) {
	var got []string
	answer := func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"messageId":"7000000000000000001"}`) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/agent/v1/conversations/feed-1/messages" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Text     string `json:"text"`
			ClientID string `json:"clientId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body.ClientID)
		answer(w)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()

	const fixed = "6F9619FF-8B86-4011-B42D-00C04FC964FF"
	id, err := c.Send(ctx, "feed-1", "hi", fixed)
	if err != nil || id != "7000000000000000001" {
		t.Fatalf("Send = %q, %v", id, err)
	}
	if got[0] != fixed {
		t.Fatalf("clientId = %q, want it passed through as %q", got[0], fixed)
	}

	if _, err := c.Send(ctx, "feed-1", "hi", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(ctx, "feed-1", "hi", ""); err != nil {
		t.Fatal(err)
	}
	if !ValidClientID(got[1]) || got[1][14] != '4' || got[1] == got[2] {
		t.Fatalf("generated clientIds %q, %q: want two distinct UUIDv4", got[1], got[2])
	}

	answer = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"MESSAGE_ALREADY_SENT","message":"Lark accepted this message"}}`)
	}
	if id, err := c.Send(ctx, "feed-1", "hi", fixed); err != nil || id != "" {
		t.Fatalf("MESSAGE_ALREADY_SENT: Send = %q, %v; want success", id, err)
	}

	answer = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"MESSAGE_DISCARDED","message":"This message was discarded"}}`)
	}
	if _, err := c.Send(ctx, "feed-1", "hi", fixed); err == nil || !strings.Contains(err.Error(), "MESSAGE_DISCARDED") {
		t.Fatalf("MESSAGE_DISCARDED: err = %v, want a failure", err)
	}
}

func TestValidClientID(t *testing.T) {
	for _, s := range []string{
		"6f9619ff-8b86-4011-b42d-00c04fc964ff",
		"6F9619FF-8B86-4011-B42D-00C04FC964FF",
		"6f9619ff-8b86-5011-a42d-00c04fc964ff",
		NewClientID(),
	} {
		if !ValidClientID(s) {
			t.Errorf("ValidClientID(%q) = false", s)
		}
	}
	for _, s := range []string{
		"",
		"not-a-uuid",
		"6f9619ff8b864011b42d00c04fc964ff",
		"6f9619ff-8b86-4011-b42d-00c04fc964f",
		"6f9619ff-8b86-4011-b42d-00c04fc964fg",
		"6f9619ff-8b86-0011-b42d-00c04fc964ff", // version 0
		"6f9619ff-8b86-4011-c42d-00c04fc964ff", // not the RFC variant
		"{6f9619ff-8b86-4011-b42d-00c04fc964f}",
	} {
		if ValidClientID(s) {
			t.Errorf("ValidClientID(%q) = true", s)
		}
	}
}

func TestNamedClientID(t *testing.T) {
	// Same as Python's uuid.uuid5(UUID("a51e3f21-e1d7-4ccd-89bf-cb027f8cb8dc"), name).
	if got := NamedClientID("lark:feed-1:100\n好的，我看一下"); got != "c05999ee-029d-5eee-8afe-3c1058078b2a" {
		t.Fatalf("NamedClientID = %s", got)
	}
	a, b := NamedClientID("x\none"), NamedClientID("x\ntwo")
	if a == b || a != NamedClientID("x\none") || !ValidClientID(a) || a[14] != '5' {
		t.Fatalf("NamedClientID: %s %s", a, b)
	}
}
