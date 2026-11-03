package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMediaURLPrefixes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/agent/v1/media/image?size=large&ol_account=2", "/api/v1/lark/media/image?size=large&ol_account=2"},
		{"/agent/v1/messages/message/media/image?ol_account=2", "/api/v1/lark/messages/message/media/image?ol_account=2"},
		{"/api/media/avatar?size=small", "/api/v1/lark/media/avatar?size=small"},
		{"/api/messages/message/media/image", "/api/v1/lark/messages/message/media/image"},
		{"https://example.com/agent/v1/media/image", "https://example.com/agent/v1/media/image"},
		{"/agent/v1/documents/document", "/agent/v1/documents/document"},
		{"/agent/v10/media/image", "/agent/v10/media/image"},
		{"/api/v1/lark/media/image", "/api/v1/lark/media/image"},
		{"text /agent/v1/media/image", "text /agent/v1/media/image"},
		{"", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := MediaURL(tc.input); got != tc.want {
				t.Fatalf("MediaURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeAgentMediaURLs(t *testing.T) {
	var raw rawMessage
	if err := json.Unmarshal([]byte(`{
 "id":"message", "senderAvatarUrl":"/agent/v1/media/avatar?ol_account=2",
 "textPreview":"See /agent/v1/media/image",
 "parts":[{"kind":"image","url":"/agent/v1/media/image?size=large&ol_account=2",
 "metadata":{"thumbnail":"/agent/v1/messages/message/media/thumb?size=small"}},
 {"kind":"text","text":"See /agent/v1/media/image"},
 {"kind":"image","url":"https://example.com/agent/v1/media/external"},
 {"kind":"image","url":"/api/media/legacy"}],
 "replyTo":{"id":"reply","parts":[{"kind":"image","url":"/agent/v1/messages/reply/media/image?ol_account=2"}]}
 }`), &raw); err != nil {
		t.Fatal(err)
	}
	m := raw.normalize()
	if m.SenderAvatar != "/api/v1/lark/media/avatar?ol_account=2" || m.Text != "See /agent/v1/media/image" {
		t.Errorf("unexpected normalized message: %+v", m)
	}
	assertMediaJSON(t, m.Parts, `[{"kind":"image","url":"/api/v1/lark/media/image?size=large&ol_account=2",
 "metadata":{"thumbnail":"/api/v1/lark/messages/message/media/thumb?size=small"}},
 {"kind":"text","text":"See /agent/v1/media/image"},
 {"kind":"image","url":"https://example.com/agent/v1/media/external"},
 {"kind":"image","url":"/api/v1/lark/media/legacy"}]`)
	if m.ReplyTo == nil {
		t.Fatal("reply missing")
	}
	assertMediaJSON(t, m.ReplyTo.Parts, `[{"kind":"image","url":"/api/v1/lark/messages/reply/media/image?ol_account=2"}]`)
	if rewriteMedia(nil) != nil || rewriteMedia(json.RawMessage("null")) != nil {
		t.Fatal("absent parts must stay absent")
	}
}

func TestRawAgentMediaURLs(t *testing.T) {
	const upstream = `{"image":"/agent/v1/media/image?ol_account=2&size=large",
 "messageImage":"/agent/v1/messages/message/media/image?size=small",
 "document":"/agent/v1/documents/document?ol_account=2",
 "legacyImage":"/api/media/legacy", "legacyMessage":"/api/messages/message/media/legacy",
 "legacyDocument":"/api/documents/document", "text":"See /agent/v1/media/image",
 "external":"https://example.com/agent/v1/media/image", "unknown":"/agent/v10/media/image"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/documents/recent" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstream))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test-token"})
	out := httptest.NewRecorder()
	if err := c.Raw(context.Background(), out, "GET", "/documents/recent", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if out.Code != http.StatusOK || out.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: status=%d headers=%v", out.Code, out.Header())
	}
	assertMediaJSON(t, out.Body.Bytes(), `{"image":"/api/v1/lark/media/image?ol_account=2&size=large",
 "messageImage":"/api/v1/lark/messages/message/media/image?size=small",
 "document":"/api/v1/lark/raw/documents/document?ol_account=2",
 "legacyImage":"/api/v1/lark/media/legacy", "legacyMessage":"/api/v1/lark/messages/message/media/legacy",
 "legacyDocument":"/api/v1/lark/raw/documents/document", "text":"See /agent/v1/media/image",
 "external":"https://example.com/agent/v1/media/image", "unknown":"/agent/v10/media/image"}`)
}

func assertMediaJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
