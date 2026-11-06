package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
)

// serveMessagingExtensionForTest is a messaging source "examplechat": a good page of messages, a page that does not
// hold to the schema, a send that counts, media bytes; it pushes messaging.changed for its own source and for one it
// does not have.
func serveMessagingExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
	if err != nil {
		os.Exit(1)
	}
	var sends atomic.Int32
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/messaging/examplechat/")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(rest, "/messages"):
			_ = json.NewEncoder(w).Encode(map[string]any{"message_id": fmt.Sprintf("s%d", sends.Add(1))})
		case rest == "conversations/good/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": "1", "has_older": false, "messages": []any{
				map[string]any{"id": "m1", "position": "1", "sender": map[string]any{"name": "李雷"}, "mine": false, "text": "你好",
					"parts": []any{map[string]any{"kind": "text", "text": "你好"}}, "device": r.Header.Get("X-Persona-Device")}}})
		case rest == "conversations/bad/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{"has_older": false, "messages": []any{
				map[string]any{"id": "m1", "position": "first", "sender": map[string]any{"name": "x"}, "mine": false, "text": "", "parts": []any{}}}})
		case rest == "conversations/gone/messages":
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "没登录", "code": "not_signed_in"})
		case strings.HasPrefix(rest, "posts/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "7", "author": map[string]any{"name": "李雷"}, "parts": []any{}})
		case strings.HasPrefix(rest, "media/"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG not json"))
		case rest == "vendor/raw":
			_, _ = w.Write([]byte(`{"secret": "a vendor's own route"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-chat", Version: "1.0.0",
		Init: func(_ context.Context, _ extension.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			go func() {
				time.Sleep(300 * time.Millisecond)
				_ = h.Notify("messaging.changed", map[string]any{"source": "examplechat", "conversations": []string{"good"}})
				_ = h.Notify("messaging.changed", map[string]any{"source": "examplechat", "conversations": []string{"other"}, "settings_changed": true})
				_ = h.Notify("messaging.changed", map[string]any{"source": "someoneelse", "conversations": []string{"x"}})
			}()
			return nil, nil
		}})
}

func messagingPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "chat"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=messaging exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-chat", "name": "Example chat", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/chat"}, "http": true},
		"activation_events": ["on_startup"],
		"contributes": {
			"messaging": [{"id": "examplechat", "name": "Chat", "icon": "chat", "capabilities": ["reactions", "nonsense_later"],
			               "composer_modes": [{"id": "sealed", "title": "加密", "attachments": false, "mentions": false, "prepare": true}],
			               "settings_ui": "display", "links": [{"hosts": ["*.example.com"], "opens": "document"}]}],
			"ui": [{"id": "display", "title": "显示设置", "placement": "extension_page"}]}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A messaging source: listed for the apps, its routes relayed with who is asking, an answer that does not hold to the
// schema stopped at the hub, errors and bytes passed as they are, routes outside the protocol refused, sends left to
// the extension, and its changes coalesced into one push per source.
func TestExtensionMessaging(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token

	if _, out := call(t, mux, "GET", "/api/v1/messaging", master, ""); fmt.Sprint(out["sources"]) != "[]" {
		t.Fatalf("no source before the extension: %v", out)
	}
	code0, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+messagingPackage(t)+`"}`)
	if code0 != 200 {
		t.Fatalf("preview: %d %v", code0, pv)
	}
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	waitLong(t, "the extension up", func() bool {
		hp := h.extensions.get("example-chat")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	_, out := call(t, mux, "GET", "/api/v1/messaging", master, "")
	sources, _ := out["sources"].([]any)
	if len(sources) != 1 {
		t.Fatalf("sources %v", out)
	}
	src := sources[0].(map[string]any)
	if src["extension"] != "example-chat" || src["id"] != "examplechat" || src["name"] != "Chat" || src["settings_ui"] != "display" {
		t.Fatalf("the source: %v", src)
	}
	if !slices.Contains(h.features(), "messaging") {
		t.Fatal("the hub has the messaging feature while a source is there")
	}

	code, out := call(t, mux, "GET", "/api/v1/messaging/examplechat/conversations/good/messages?before=10", master, "")
	if code != 200 || out["account"] != "1" {
		t.Fatalf("a good page: %d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/messaging/examplechat/conversations/bad/messages", master, ""); code != http.StatusBadGateway || out["code"] != "extension_bad_response" {
		t.Fatalf("a page that does not hold to the schema: %d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/messaging/examplechat/conversations/gone/messages", master, ""); code != http.StatusBadGateway || out["code"] != "not_signed_in" {
		t.Fatalf("the extension's error as it is: %d %v", code, out)
	}
	req, _ := http.NewRequest("GET", "/api/v1/messaging/examplechat/media/a.png?size=96", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+master)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "\x89PNG") {
		t.Fatalf("media bytes: %d %q", w.Code, w.Body.String())
	}
	// A link escaped into one segment stays one: posts/{id} gets it.
	if code, _ := call(t, mux, "GET", "/api/v1/messaging/examplechat/posts/https%3A%2F%2Fapplink.example.com%2Fp%3FpostId%3D7", master, ""); code == http.StatusNotFound {
		t.Fatalf("an escaped link as a post id: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/messaging/examplechat/vendor/raw", master, ""); code != http.StatusNotFound {
		t.Fatalf("a route outside the protocol: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/messaging/nosuch/conversations", master, ""); code != http.StatusNotFound {
		t.Fatalf("a source nobody has: %d", code)
	}

	// A send reaches the extension every time (it drops a repeat by client_id itself): no receipt replays a refusal
	// the user can fix (the hub keeps none for these routes: they are no extension's api prefix).
	send := `{"client_id": "0b6d3c1e-7f2a-4a55-9d8e-2c1a4f5b6e7d", "text": "hi"}`
	for i, want := range []string{"s1", "s2"} {
		if code, out := call(t, mux, "POST", "/api/v1/messaging/examplechat/conversations/good/messages", master, send); code != 200 || out["message_id"] != want {
			t.Fatalf("send %d: %d %v", i, code, out)
		}
	}

	th := hubOf(h)
	eventually(t, 10*time.Second, "the source's changes reaching the app", func() bool { return len(th.broadcastsOf("messaging")) > 0 })
	ev := th.broadcastsOf("messaging")[0]
	if ev["source"] != "examplechat" || fmt.Sprint(ev["conversations"]) != "[good other]" || ev["settings_changed"] != true {
		t.Fatalf("one push for the source's changes: %v", ev)
	}
	time.Sleep(1500 * time.Millisecond)
	if got := th.broadcastsOf("messaging"); len(got) != 1 {
		t.Fatalf("a change for a source it does not have went out: %v", got)
	}
}

// Changes folded within the second keep what each said: the chats of both, and every chat when one of them names none
// (read everything again) or together they name more than a push can.
func TestMergeMessagingChanged(t *testing.T) {
	many := make([]string, maxMessagingChanged)
	for i := range many {
		many[i] = fmt.Sprint("c", i)
	}
	for _, c := range []struct {
		first, then, want []string
	}{
		{[]string{"a"}, []string{"b", "a"}, []string{"a", "b"}},
		{nil, []string{"b"}, nil},
		{[]string{"a"}, nil, nil},
		{many, []string{"one more"}, nil},
	} {
		have := &wire.MessagingChanged{Source: "examplechat", Conversations: slices.Clone(c.first)}
		mergeMessagingChanged(have, &wire.MessagingChanged{Source: "examplechat", Conversations: c.then, SettingsChanged: true})
		if !slices.Equal(have.Conversations, c.want) || !have.SettingsChanged {
			t.Fatalf("%v then %v: %v (settings %v), want %v", len(c.first), c.then, have.Conversations, have.SettingsChanged, c.want)
		}
	}
}

// A manifest's messaging is checked: its settings page and the pages its links open are its own extension_page.
func TestMessagingManifestChecks(t *testing.T) {
	base := `{"manifest_version": 1, "id": "x-chat", "name": "X", "version": "1.0.0", "engines": {"plugin_protocol": 1},
		"runtime": {"command": {"linux-amd64": "bin/x"}, "http": true}, "activation_events": [%s],
		"contributes": {"messaging": [%s], "ui": [{"id": "display", "title": "显示", "placement": "extension_page"}]}}`
	for name, c := range map[string]struct {
		activation, item string
		ok               bool
	}{
		"fine":            {`"on_startup"`, `{"id": "x", "name": "X", "settings_ui": "display", "links": [{"hosts": ["x.com"], "opens": "ui:display"}]}`, true},
		"not on startup":  {`"on_call"`, `{"id": "x", "name": "X"}`, false},
		"no such page":    {`"on_startup"`, `{"id": "x", "name": "X", "settings_ui": "other"}`, false},
		"link to nowhere": {`"on_startup"`, `{"id": "x", "name": "X", "links": [{"hosts": ["x.com"], "opens": "ui:nope"}]}`, false},
		"bad id":          {`"on_startup"`, `{"id": "X!", "name": "X"}`, false},
		"unknown field":   {`"on_startup"`, `{"id": "x", "name": "X", "colour": "red"}`, false},
	} {
		_, err := extension.Parse([]byte(fmt.Sprintf(base, c.activation, c.item)))
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A newer extension's part and block kinds: in their plainest form, not a refused page.
func TestFitMessagingAnswer(t *testing.T) {
	raw := `{"has_older": false, "messages": [{"id": "m1", "position": "1", "sender": {"name": "x"}, "mine": false, "text": "t",
		"parts": [{"kind": "text", "text": "a"}, {"kind": "poll", "text": "投票：午饭吃什么", "options": ["面"]}, {"kind": "hologram"}]}]}`
	if wire.ValidateJSON("MessagingMessagePage", []byte(raw)) == nil {
		t.Fatal("an unknown kind does not hold as it is")
	}
	out, changed := fitMessagingAnswer([]byte(raw))
	if !changed || wire.ValidateJSON("MessagingMessagePage", out) != nil {
		t.Fatalf("fitted: %v %s", changed, out)
	}
	if !strings.Contains(string(out), `{"kind":"text","text":"投票：午饭吃什么"}`) || strings.Contains(string(out), "hologram") {
		t.Fatalf("fitted: %s", out)
	}
	doc := `{"title": "d", "url": "https://x", "blocks": [{"kind": "mindmap", "parts": [{"kind": "text", "text": "x"}]}]}`
	if out, changed := fitMessagingAnswer([]byte(doc)); !changed || wire.ValidateJSON("MessagingDocument", out) != nil || !strings.Contains(string(out), `"paragraph"`) {
		t.Fatalf("a block: %v %s", changed, out)
	}
}

// The routes the hub passes on: exactly the protocol's, a media path of any depth, nothing else.
func TestMessagingRouteMatch(t *testing.T) {
	for _, c := range []struct {
		method, path, answer string
		ok                   bool
	}{
		{"GET", "conversations", "MessagingConversationPage", true},
		{"GET", "conversations/7123/messages", "MessagingMessagePage", true},
		{"POST", "conversations/7123/messages", "MessagingSendReceipt", true},
		{"GET", "messages/m1", "MessagingMessageAnswer", true},
		{"GET", "messages/m1/thread", "MessagingMessagePage", true},
		{"GET", "media/a/b/c.png", "", true},
		{"GET", "media/", "", false},
		{"GET", "conversations//messages", "", false},
		{"DELETE", "conversations/7123/messages", "", false},
		{"GET", "raw/accounts", "", false},
	} {
		rt, ok := lookupMessagingRoute(c.method, c.path)
		if ok != c.ok || (ok && rt.Answer != c.answer) {
			t.Errorf("%s %s: %v %+v", c.method, c.path, ok, rt)
		}
	}
}
