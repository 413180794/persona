package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeUpstream is a GPT-Live service: it records what it was sent and answers with what a test scripts.
type fakeUpstream struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	got     []map[string]any
	headers []http.Header
	conns   []*websocket.Conn
	frames  chan map[string]any
	// onFrame answers one frame (the first is the session start).
	onFrame func(u *fakeUpstream, c *websocket.Conn, f map[string]any)
}

func newFakeUpstream(t *testing.T, onFrame func(u *fakeUpstream, c *websocket.Conn, f map[string]any)) *fakeUpstream {
	u := &fakeUpstream{t: t, onFrame: onFrame, frames: make(chan map[string]any, 1024)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/live/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, `{"error":{"message":"no"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"models":[{"id":"m1","displayName":"Model One"},{"id":"m2"}]}`)
	})
	mux.HandleFunc("GET /v1/live/voices", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"voices":[{"id":"v_`+r.URL.Query().Get("model")+`","name":"V"}],"defaultVoice":"v_x"}`)
	})
	mux.HandleFunc("GET /v1/live", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 20)
		u.mu.Lock()
		u.headers = append(u.headers, r.Header.Clone())
		u.conns = append(u.conns, c)
		u.mu.Unlock()
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var f map[string]any
			_ = json.Unmarshal(b, &f)
			u.mu.Lock()
			u.got = append(u.got, f)
			u.mu.Unlock()
			if f["type"] != "session.input_audio.append" && f["type"] != "input_audio.append" {
				u.frames <- f
			}
			u.onFrame(u, c, f)
		}
	})
	u.srv = httptest.NewServer(mux)
	t.Cleanup(u.srv.Close)
	return u
}

func (u *fakeUpstream) url() string { return "ws" + strings.TrimPrefix(u.srv.URL, "http") + "/v1/live" }

func (u *fakeUpstream) send(c *websocket.Conn, v any) {
	b, _ := json.Marshal(v)
	_ = c.Write(context.Background(), websocket.MessageText, b)
}

// next is the next frame the service got that is not audio, of type typ.
func (u *fakeUpstream) next(typ string) map[string]any {
	u.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-u.frames:
			if f["type"] == typ {
				return f
			}
		case <-deadline:
			u.t.Fatalf("the service never got %s", typ)
		}
	}
}

// fakeHub is the Persona hub's API as the plugin uses it.
type fakeHub struct {
	mu       sync.Mutex
	sent     []string
	state    string
	lastText string
	updated  time.Time
}

func (h *fakeHub) server(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/kb-search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []map[string]string{{"path": "people/x.md", "line": "x likes " + r.URL.Query().Get("q")}}})
	})
	mux.HandleFunc("GET /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"machine_id":"m","main":"main1"}`)
	})
	mux.HandleFunc("POST /api/v1/sessions/m/main1/messages", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Text string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		h.mu.Lock()
		h.sent = append(h.sent, in.Text)
		h.state, h.updated = "working", time.Now()
		h.mu.Unlock()
		go func() {
			time.Sleep(50 * time.Millisecond)
			h.mu.Lock()
			h.state, h.lastText, h.updated = "unread", "明天下午三点有会", time.Now()
			h.mu.Unlock()
		}()
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []map[string]any{
			{"machine_id": "m", "session_id": "main1", "assistant": true, "state": h.state, "last_text": h.lastText, "updated_at": h.updated},
			{"machine_id": "m", "session_id": "a1", "title": "fix login", "state": "needs_you", "last_text": "May I run tests?"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (h *fakeHub) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.sent...)
}

// app is the phone's side of a conversation.
type app struct {
	t      *testing.T
	c      *websocket.Conn
	frames chan map[string]any
	audio  chan []byte
}

func dialApp(t *testing.T, pl *Plugin, start map[string]any) *app {
	t.Helper()
	srv := httptest.NewServer(pl.Handler())
	t.Cleanup(srv.Close)
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	a := &app{t: t, c: c, frames: make(chan map[string]any, 1024), audio: make(chan []byte, 1024)}
	go func() {
		for {
			typ, b, err := c.Read(context.Background())
			if err != nil {
				close(a.frames)
				return
			}
			if typ == websocket.MessageBinary {
				a.audio <- b
				continue
			}
			var f map[string]any
			_ = json.Unmarshal(b, &f)
			a.frames <- f
		}
	}()
	a.send(start)
	return a
}

func (a *app) send(v any) {
	b, _ := json.Marshal(v)
	_ = a.c.Write(context.Background(), websocket.MessageText, b)
}

// until reads the app's frames until one matches.
func (a *app) until(match func(map[string]any) bool) map[string]any {
	a.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-a.frames:
			if !ok {
				a.t.Fatal("the plugin closed the conversation")
			}
			if match(f) {
				return f
			}
		case <-deadline:
			a.t.Fatal("timed out waiting for a frame")
		}
	}
}

func ofType(typ string) func(map[string]any) bool {
	return func(f map[string]any) bool { return f["type"] == typ }
}

func finalLine(role, text string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		return f["type"] == "transcript" && f["role"] == role && f["final"] == true && f["text"] == text
	}
}

func testPlugin(t *testing.T, cfg Config, hub *httptest.Server) *Plugin {
	t.Helper()
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	pl := New(cfg, hub.URL, "hubtoken", log.New(io.Discard, "", 0))
	pl.delegate = delegateOptions{poll: 20 * time.Millisecond, timeout: 5 * time.Second, now: time.Now}
	return pl
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestLiveDialectConversation(t *testing.T) {
	hub := &fakeHub{}
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{"audio": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 16000}}}})
		case "session.commentary.append":
			if f["delegation_id"] == nil {
				// The user typed: the model hears it and delegates the work to the client.
				u.send(c, map[string]any{"type": "session.input_transcript.delta", "delta": "明天"})
				u.send(c, map[string]any{"type": "session.input_transcript.delta", "delta": "有什么会"})
				u.send(c, map[string]any{"type": "session.output_transcript.delta", "delta": "我去查一下。"})
				u.send(c, map[string]any{"type": "session.output_audio.delta", "delta": b64([]byte{1, 2, 3, 4})})
				u.send(c, map[string]any{"type": "session.delegation.created", "delegation": map[string]any{"id": "dlg1", "target": "client", "type": "delegation"}})
			}
		}
	})
	pl := testPlugin(t, Config{Owner: "老板", Providers: []Provider{{ID: "rt", URL: up.url(), Key: "k"}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start", "model": "rt/m1", "voice": "v1"})

	ready := a.until(ofType("ready"))
	if ready["input_rate"] != float64(16000) || ready["output_rate"] != float64(16000) || ready["model"] != "rt/m1" || ready["voice"] != "v1" {
		t.Fatalf("ready: %v", ready)
	}
	start := up.next("session.start")["session"].(map[string]any)
	if start["model"] != "m1" || start["tools"] != nil || !strings.Contains(start["instructions"].(string), "老板") {
		t.Fatalf("session.start: %v", start)
	}
	if v := start["audio"].(map[string]any)["output"].(map[string]any)["voice"]; v != "v1" {
		t.Fatalf("voice: %v", v)
	}
	if got := up.headers[0].Get("Authorization"); got != "Bearer k" {
		t.Fatalf("authorization: %q", got)
	}

	// Microphone audio goes up as session.input_audio.append.
	_ = a.c.Write(context.Background(), websocket.MessageBinary, []byte{9, 9})
	a.send(map[string]any{"type": "text", "text": "明天有什么会"})
	if f := up.next("session.commentary.append"); f["content"] != "明天有什么会" {
		t.Fatalf("typed: %v", f)
	}
	a.until(finalLine("user", "明天有什么会")) // the typed line
	select {
	case b := <-a.audio:
		if string(b) != string([]byte{1, 2, 3, 4}) {
			t.Fatalf("audio %v", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no audio")
	}

	// The delegation goes to the main conversation; its answer comes back as the delegation's commentary.
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["label"] == "Persona 主对话处理中" })
	if f := up.next("session.thinking.append"); f["delegation_id"] != "dlg1" {
		t.Fatalf("progress: %v", f)
	}
	f := up.next("session.commentary.append")
	if f["delegation_id"] != "dlg1" || !strings.Contains(f["content"].(string), "明天下午三点有会") {
		t.Fatalf("result: %v", f)
	}
	msgs := hub.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "明天有什么会") || !strings.Contains(msgs[0], "语音对话里交代的") {
		t.Fatalf("main conversation got %q", msgs)
	}

	var audio int
	up.mu.Lock()
	for _, g := range up.got {
		if g["type"] == "session.input_audio.append" && g["audio"] == b64([]byte{9, 9}) {
			audio++
		}
	}
	up.mu.Unlock()
	if audio != 1 {
		t.Fatalf("microphone frames upstream: %d", audio)
	}
}

func TestLiveDialectTools(t *testing.T) {
	hub := &fakeHub{}
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.start":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
			u.send(c, map[string]any{"type": "session.delegation.created", "delegation": map[string]any{"id": "dlg9", "target": "client"}})
			u.send(c, map[string]any{"type": "response.event", "delegation_id": "dlg9", "event": map[string]any{
				"type": "function_call", "call_id": "c1", "name": "search_knowledge", "arguments": `{"query":"咖啡"}`,
			}})
		}
	})
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "rt", URL: up.url(), Key: "k", Models: []string{"m1"}, Tools: true}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	start := up.next("session.start")["session"].(map[string]any)
	tools, _ := start["tools"].([]any)
	if len(tools) != 4 || tools[0].(map[string]any)["name"] != "search_knowledge" {
		t.Fatalf("tools: %v", start["tools"])
	}
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["label"] == "查知识库" && f["done"] == false })
	f := up.next("session.commentary.append")
	if f["delegation_id"] != "dlg9" || !strings.Contains(f["content"].(string), "x likes 咖啡") {
		t.Fatalf("tool result: %v", f)
	}
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["id"] == "c1" && f["done"] == true })
	// The delegation was the tool call's envelope: nothing went to the main conversation.
	time.Sleep(toolCallWait + 300*time.Millisecond)
	if msgs := hub.messages(); len(msgs) != 0 {
		t.Fatalf("main conversation got %q", msgs)
	}
}

func TestCodexDialect(t *testing.T) {
	hub := &fakeHub{}
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		switch f["type"] {
		case "session.update":
			u.send(c, map[string]any{"type": "session.started", "session": map[string]any{"id": "s1"}})
			u.send(c, map[string]any{"type": "input_transcript.added", "item": map[string]any{"text": "帮我"}})
			u.send(c, map[string]any{"type": "input_transcript.added", "item": map[string]any{"text": "查日程"}})
			u.send(c, map[string]any{"type": "turn.done", "turn": map[string]any{"role": "user", "transcript": "帮我查日程"}})
			u.send(c, map[string]any{"type": "output_transcript.added", "item": map[string]any{"text": "好的"}})
			u.send(c, map[string]any{"type": "output_audio.delta", "audio": b64([]byte{7, 7})})
			u.send(c, map[string]any{"type": "turn.done", "turn": map[string]any{"role": "assistant", "transcript": "好的，我去查。"}})
			u.send(c, map[string]any{"type": "delegation.created", "item": map[string]any{
				"id": "h1", "type": "delegation", "target": "client", "content": []map[string]string{{"type": "input_text", "text": "查明天的日程"}},
			}})
		}
	})
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "codex", URL: up.url(), Dialect: DialectCodex, Key: "k"}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start", "voice": "cove"})
	ready := a.until(ofType("ready"))
	if ready["input_rate"] != float64(24000) || ready["output_rate"] != float64(24000) || ready["model"] != "codex/gpt-live-1-codex" {
		t.Fatalf("ready: %v", ready)
	}
	upd := up.next("session.update")["session"].(map[string]any)
	if upd["model"] != nil || upd["delegation"].(map[string]any)["type"] != "client" || upd["audio"].(map[string]any)["output"].(map[string]any)["voice"] != "cove" {
		t.Fatalf("session.update: %v", upd)
	}
	h := up.headers[0]
	if h.Get("Openai-Alpha") != "quicksilver=v2" || h.Get("Authorization") != "Bearer k" {
		t.Fatalf("headers: %v", h)
	}
	a.until(finalLine("user", "帮我查日程"))
	a.until(finalLine("assistant", "好的，我去查。"))

	if f := up.next("delegation.context.append"); f["delegation_item_id"] != "h1" || f["channel"] != "commentary" {
		t.Fatalf("progress: %v", f)
	}
	f := up.next("delegation.context.append")
	content := f["content"].([]any)[0].(map[string]any)
	if f["delegation_item_id"] != "h1" || f["channel"] != "speakable" || !strings.Contains(content["text"].(string), "明天下午三点有会") {
		t.Fatalf("result: %v", f)
	}
	if msgs := hub.messages(); len(msgs) != 1 || !strings.HasPrefix(msgs[0], "查明天的日程") {
		t.Fatalf("main conversation got %q", msgs)
	}
	// The socket's URL names the model.
	if !strings.Contains(up.srv.URL, "127.0.0.1") {
		t.Fatal(up.srv.URL)
	}

	// Microphone audio as input_audio.append.
	_ = a.c.Write(context.Background(), websocket.MessageBinary, []byte{5})
	deadline := time.Now().Add(5 * time.Second)
	for {
		up.mu.Lock()
		found := false
		for _, g := range up.got {
			found = found || (g["type"] == "input_audio.append" && g["audio"] == b64([]byte{5}))
		}
		up.mu.Unlock()
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no input_audio.append")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReconnectTellsTheNewSessionWhatWasSaid(t *testing.T) {
	hub := &fakeHub{}
	var sessions int
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		if f["type"] != "session.start" {
			return
		}
		u.mu.Lock()
		sessions++
		n := sessions
		u.mu.Unlock()
		u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
		if n == 1 {
			u.send(c, map[string]any{"type": "session.input_transcript.delta", "delta": "记住数字七"})
			u.send(c, map[string]any{"type": "session.output_transcript.delta", "delta": "好的，记住了。"})
			u.send(c, map[string]any{"type": "response.event", "delegation_id": "r1", "event": map[string]any{"type": "response.completed"}})
			_ = c.Close(websocket.StatusGoingAway, "upstream restarts")
		}
	})
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "rt", URL: up.url(), Models: []string{"m1"}}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start"})
	a.until(ofType("ready"))
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["id"] == "reconnect" && f["done"] == false })
	a.until(func(f map[string]any) bool { return f["type"] == "tool" && f["id"] == "reconnect" && f["done"] == true })
	up.next("session.start")
	second := up.next("session.start")["session"].(map[string]any)["instructions"].(string)
	if !strings.Contains(second, "刚才的对话") || !strings.Contains(second, "用户：记住数字七") || !strings.Contains(second, "你：好的，记住了。") {
		t.Fatalf("second session's instructions: %s", second)
	}
}

func TestModelsAcrossProviders(t *testing.T) {
	hub := &fakeHub{}
	up := newFakeUpstream(t, func(*fakeUpstream, *websocket.Conn, map[string]any) {})
	pl := testPlugin(t, Config{Providers: []Provider{
		{ID: "rt", Name: "公司", URL: up.url(), Key: "k"},
		{ID: "codex", Name: "Codex", URL: "wss://gateway.example.com/v1/live", Dialect: DialectCodex},
	}}, hub.server(t))
	srv := httptest.NewServer(pl.Handler())
	defer srv.Close()
	get := func(path string, out any) int {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_ = json.NewDecoder(res.Body).Decode(out)
		return res.StatusCode
	}
	var models struct {
		Models  []ModelChoice
		Default string
	}
	get("/models", &models)
	want := []ModelChoice{{"rt/m1", "Model One · 公司"}, {"rt/m2", "m2 · 公司"}, {"codex/gpt-live-1-codex", "gpt-live-1-codex · Codex"}}
	if len(models.Models) != 3 || models.Models[0] != want[0] || models.Models[1] != want[1] || models.Models[2] != want[2] || models.Default != "rt/m1" {
		t.Fatalf("%+v", models)
	}
	var voices voiceList
	get("/voices?model=rt/m2", &voices)
	if len(voices.Voices) != 1 || voices.Voices[0].ID != "v_m2" || voices.Default != "v_x" {
		t.Fatalf("%+v", voices)
	}
	get("/voices?model=codex/gpt-live-1-codex", &voices)
	if voices.Default != "cove" || len(voices.Voices) != len(codexVoices) {
		t.Fatalf("%+v", voices)
	}
	if code := get("/voices?model=nope/x", &voices); code != http.StatusNotFound {
		t.Fatalf("unknown provider: %d", code)
	}
	if code := get("/voices?model=bad", &voices); code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", code)
	}
}

func TestStartFailureIsFatal(t *testing.T) {
	hub := &fakeHub{}
	up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
		u.send(c, map[string]any{"type": "error", "error": map[string]any{"message": "unknown model", "code": "invalid_request"}})
	})
	pl := testPlugin(t, Config{Providers: []Provider{{ID: "rt", URL: up.url(), Models: []string{"m1"}}}}, hub.server(t))
	a := dialApp(t, pl, map[string]any{"type": "start", "model": "rt/zzz"})
	f := a.until(ofType("error"))
	if f["fatal"] != true || !strings.Contains(f["message"].(string), "unknown model") {
		t.Fatalf("%v", f)
	}
}

func TestClipTokens(t *testing.T) {
	cjk := strings.Repeat("字", 600)
	if got := clipTokens(cjk, 450); len([]rune(got)) != 451 || !strings.HasSuffix(got, "…") {
		t.Fatalf("%d runes", len([]rune(got)))
	}
	if got := clipTokens("short", 450); got != "short" {
		t.Fatal(got)
	}
	for _, c := range chunkBytes(strings.Repeat("中a", 400), 500) {
		if len(c) > 500 || !strings.HasPrefix(c, "中") && !strings.HasPrefix(c, "a") {
			t.Fatalf("chunk of %d bytes: %q…", len(c), c[:6])
		}
	}
}

func TestConfigValidation(t *testing.T) {
	for _, bad := range []Config{
		{},
		{Providers: []Provider{{ID: "Bad", URL: "wss://x"}}},
		{Providers: []Provider{{ID: "a", URL: "https://x"}}},
		{Providers: []Provider{{ID: "a", URL: "wss://x", Dialect: "other"}}},
		{Providers: []Provider{{ID: "a", URL: "wss://x"}, {ID: "a", URL: "wss://y"}}},
		{Providers: []Provider{{ID: "a", URL: "wss://x", InputRate: 8000}}},
	} {
		if err := bad.validate(); err == nil {
			t.Errorf("%+v: no error", bad)
		}
	}
}
