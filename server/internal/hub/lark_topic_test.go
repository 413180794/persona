package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// topiclark-connector is an lark-connector with one topic group (f-topic): its messages endpoint holds only the
// 创建了话题群 notice, as lark-connector answers for most topic groups; its content is in the threads.
func topiclark-connector(t *testing.T) *lark.Client {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conversations/f-topic", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"feedId": "f-topic", "isGroup": true, "chatMode": "topic"})
	})
	mux.HandleFunc("GET /conversations/f-topic/messages", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{"id": "sys", "position": "0", "messageTypeName": "SYSTEM", "textPreview": "创建了话题群", "createdAt": "2026-10-01T00:00:00Z"}}})
	})
	mux.HandleFunc("GET /conversations/f-topic/threads", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"hasOlder": true, "nextOlderCursor": "5863", "items": []any{
			map[string]any{"id": "root-1", "position": "5863", "senderName": "张三", "messageTypeName": "TEXT", "textPreview": "反馈一",
				"threadReplyCount": 14, "threadMessageType": "root", "createdAt": "2026-11-01T02:00:00Z"},
			map[string]any{"id": "root-2", "position": "5882", "senderName": "李四", "messageTypeName": "TEXT", "textPreview": "反馈二",
				"threadReplyCount": 26, "threadMessageType": "root", "createdAt": "2026-11-02T02:00:00Z"},
		}})
	})
	mux.HandleFunc("GET /messages/root-2/thread", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{"id": "root-2", "position": "5882", "senderName": "李四", "messageTypeName": "TEXT", "textPreview": "反馈二", "threadMessageType": "root", "createdAt": "2026-11-02T02:00:00Z"},
			map[string]any{"id": "r1", "position": "5911", "senderName": "王五", "messageTypeName": "TEXT", "textPreview": "收到，在看", "threadMessageType": "reply", "createdAt": "2026-11-02T03:00:00Z"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return lark.New(lark.Config{BaseURL: srv.URL, Token: "t"})
}

// The app opens a topic group without saying so (from a profile, an old inbox item): it gets the threads, marked topic.
func TestLarkMessagesRouteFindsTopicGroup(t *testing.T) {
	h := newTestHub(t)
	h.lark = topiclark-connector(t)
	mux := http.NewServeMux()
	h.larkRoutes(mux)
	code, out := do(t, mux, "GET", "/api/v1/lark/conversations/f-topic/messages?limit=40", "")
	msgs, _ := out["messages"].([]any)
	if code != 200 || out["topic"] != true || len(msgs) != 2 || msgs[1].(map[string]any)["thread_reply_count"] != float64(26) ||
		out["has_older"] != true || out["older_cursor"] != "5863" {
		t.Fatalf("%d %v", code, out)
	}
}

func TestLarkMessagesToolReadsTopicGroup(t *testing.T) {
	h := newTestHub(t)
	h.lark = topiclark-connector(t)
	out, err := h.runLarkTool(context.Background(), "lark_messages", json.RawMessage(`{"chat_id":"f-topic"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"topic group", "before=5863", "反馈二 (thread=root-2, 26 条回复)", "反馈一 (thread=root-1, 14 条回复)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "创建了话题群") {
		t.Fatalf("read the messages endpoint:\n%s", out)
	}
	out, err = h.runLarkTool(context.Background(), "lark_messages", json.RawMessage(`{"chat_id":"f-topic","thread":"root-2"}`))
	if err != nil || !strings.Contains(out, "王五：收到，在看") || strings.Contains(out, "topic group") {
		t.Fatalf("thread: %v\n%s", err, out)
	}
}
