package feishubot

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Bytes the official SDK (oapi-sdk-go v3.12.2, ws.Frame.Marshal / ws.NewPingFrame) wrote for the same frames.
func TestFrameMatchesTheSDK(t *testing.T) {
	data := &Frame{SeqID: 7, LogID: 300, Service: 42, Method: frameData,
		Headers: []Header{{"type", "event"}, {"message_id", "m1"}, {"sum", "1"}}, Payload: []byte(`{"a":1}`)}
	const sdkData = "080710ac02182a20012a0d0a047479706512056576656e742a100a0a6d6573736167655f696412026d312a080a0373756d12013132003a0042077b2261223a317d4a00"
	if got := hex.EncodeToString(data.Marshal()); got != sdkData {
		t.Fatalf("data frame\n got %s\nwant %s", got, sdkData)
	}
	ping := &Frame{Service: 42, Method: frameControl, Headers: []Header{{"type", "ping"}}}
	const sdkPing = "08001000182a20002a0c0a0474797065120470696e6732003a004a00"
	if got := hex.EncodeToString(ping.Marshal()); got != sdkPing {
		t.Fatalf("ping\n got %s\nwant %s", got, sdkPing)
	}
	raw, _ := hex.DecodeString(sdkData)
	f, err := UnmarshalFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.SeqID != 7 || f.LogID != 300 || f.Service != 42 || f.Method != frameData || f.Header("message_id") != "m1" || string(f.Payload) != `{"a":1}` {
		t.Fatalf("decoded %+v", f)
	}
	if _, err := UnmarshalFrame([]byte{0x0a, 0x05, 'a'}); err == nil {
		t.Fatal("a cut frame must not decode")
	}
}

func TestParseContent(t *testing.T) {
	mentions := []Mention{{Key: "@_user_1", Name: "Persona"}}
	if c := ParseContent("text", `{"text":"@_user_1 帮我看下"}`, mentions); c.Text != "@Persona 帮我看下" {
		t.Fatalf("text: %q", c.Text)
	}
	// A mention without a key must not be spliced between every rune of the text.
	keyless := []Mention{{Name: "Nobody"}}
	if c := ParseContent("text", `{"text":"你好"}`, keyless); c.Text != "你好" {
		t.Fatalf("keyless mention: %q", c.Text)
	}
	post := `{"title":"周报","content":[[{"tag":"text","text":"见 "},{"tag":"a","text":"文档","href":"https://example.com/d"}],[{"tag":"img","image_key":"img_1"}],[{"tag":"code_block","language":"GO","text":"x := 1\n"}]]}`
	c := ParseContent("post", post, nil)
	if !strings.Contains(c.Text, "**周报**") || !strings.Contains(c.Text, "[文档](https://example.com/d)") || !strings.Contains(c.Text, "```go\nx := 1\n```") {
		t.Fatalf("post: %q", c.Text)
	}
	if len(c.Media) != 1 || c.Media[0].Key != "img_1" {
		t.Fatalf("post media: %+v", c.Media)
	}
	// Read back through the API, a post is keyed by language.
	if c := ParseContent("post", `{"zh_cn":{"title":"","content":[[{"tag":"text","text":"你好"}]]}}`, nil); c.Text != "你好" {
		t.Fatalf("post by language: %q", c.Text)
	}
	if c := ParseContent("image", `{"image_key":"img_2"}`, nil); len(c.Media) != 1 || c.Media[0].Kind != "image" {
		t.Fatalf("image: %+v", c)
	}
	if c := ParseContent("file", `{"file_key":"f1","file_name":"a.pdf"}`, nil); len(c.Media) != 1 || c.Media[0].Name != "a.pdf" {
		t.Fatalf("file: %+v", c)
	}
	if c := ParseContent("merge_forward", `{"content":"Merged and Forwarded Message"}`, nil); !c.Forward {
		t.Fatal("merge_forward")
	}
	if c := ParseContent("interactive", `{"title":"告警","elements":[[{"tag":"text","text":"CPU 高"}]]}`, nil); !strings.Contains(c.Text, "告警") || !strings.Contains(c.Text, "CPU 高") {
		t.Fatalf("card: %q", c.Text)
	}
}

func TestParsePostAtFallback(t *testing.T) {
	mentions := []Mention{{
		Key:  "@_user_1",
		ID:   UserID{OpenID: "ou_mentioned", UnionID: "on_mentioned", UserID: "u_mentioned"},
		Name: "Mentioned",
	}}
	cases := []struct {
		name     string
		node     string
		mentions []Mention
		want     string
	}{
		{"user name", `{"tag":"at","user_id":"ou_mentioned","user_name":"Named"}`, nil, "@Named"},
		{"open id resolved via mentions", `{"tag":"at","user_id":"ou_mentioned"}`, mentions, "@Mentioned"},
		{"union id resolved via mentions", `{"tag":"at","user_id":"on_mentioned"}`, mentions, "@Mentioned"},
		{"user id resolved via mentions", `{"tag":"at","user_id":"u_mentioned"}`, mentions, "@Mentioned"},
		{"id without mentions", `{"tag":"at","user_id":"ou_unknown"}`, nil, "@ou_unknown"},
		{"resolved mention with empty name", `{"tag":"at","user_id":"ou_nameless"}`, []Mention{{
			Key: "@_user_2", ID: UserID{OpenID: "ou_nameless"},
		}}, "@ou_nameless"},
		{"at all", `{"tag":"at","user_id":"all"}`, nil, "@所有人"},
		{"empty at node", `{"tag":"at"}`, nil, "@某人"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := `{"title":"","content":[[{"tag":"text","text":"见 "},` + tc.node + `]]}`
			if got := ParseContent("post", content, tc.mentions).Text; got != "见 "+tc.want {
				t.Fatalf("got %q, want %q", got, "见 "+tc.want)
			}
		})
	}
}

func TestSplitKeepsCodeBlocksClosed(t *testing.T) {
	long := "开头\n```go\n" + strings.Repeat("fmt.Println(1)\n", 40) + "```\n结尾"
	parts := Split(long, 200)
	if len(parts) < 2 {
		t.Fatalf("not split: %d", len(parts))
	}
	for i, p := range parts {
		if n := len([]rune(p)); n > 200 {
			t.Fatalf("part %d has %d runes", i, n)
		}
		if strings.Count(p, "```")%2 != 0 {
			t.Fatalf("part %d leaves a code block open:\n%s", i, p)
		}
	}
	if joined := strings.Join(parts, ""); !strings.Contains(joined, "开头") || !strings.Contains(joined, "结尾") {
		t.Fatal("lost text")
	}
	if got := Split("短", 200); len(got) != 1 || got[0] != "短" {
		t.Fatalf("short: %q", got)
	}
	if got := Split(strings.Repeat("长", 500), 100); len(got) < 5 {
		t.Fatalf("one long line: %d parts", len(got))
	}
}

func TestCardMarkdown(t *testing.T) {
	in := "## 结论\n用 <at> 标签和 `<b>`\n```html\n<div>保持</div>\n```\n"
	got := CardMarkdown(in)
	if !strings.Contains(got, "## 结论") || !strings.Contains(got, "&#60;at>") || !strings.Contains(got, "`<b>`") || !strings.Contains(got, "<div>保持</div>") {
		t.Fatalf("got %q", got)
	}
	if e := EscapeMarkdown("a*b_c <x>"); strings.ContainsAny(e, "*_<>") {
		t.Fatalf("escaped %q", e)
	}
}

func TestCardJSONIsVersion2(t *testing.T) {
	c := NewCard("标题", Blue, "摘要").Markdown("**你好** <b>").Note("小字").
		Buttons(Btn("好", Primary, map[string]any{"a": "1"}), LinkBtn("打开", "https://example.com")).
		Form("f", []any{Input("x", "", "写点什么", true)}, Submit("提交", Primary, "s", map[string]any{"a": "2"}))
	var v map[string]any
	if err := json.Unmarshal([]byte(c.JSON()), &v); err != nil {
		t.Fatal(err)
	}
	if v["schema"] != "2.0" || v["body"] == nil || v["elements"] != nil {
		t.Fatalf("not a 2.0 card: %s", c.JSON())
	}
	if !strings.Contains(c.JSON(), "<b>") {
		t.Fatal("HTML characters must not be escaped as \\u003c (the card limit is in bytes)")
	}
	if !strings.Contains(c.JSON(), `"form_action_type":"submit"`) || !strings.Contains(c.JSON(), `"type":"callback"`) {
		t.Fatalf("buttons: %s", c.JSON())
	}
}

// fakeFeishu serves the endpoint call and the long connection, and records what the client answers.
type fakeFeishu struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	pings   int
	answers []*Frame
	conns   chan *websocket.Conn
}

func newFakeFeishu(t *testing.T) *fakeFeishu {
	f := &fakeFeishu{t: t, conns: make(chan *websocket.Conn, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /callback/ws/endpoint", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ AppID, AppSecret string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.AppSecret != "secret" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1000040343, "msg": "bad secret"})
			return
		}
		u := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/ws?device_id=d1&service_id=9"
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"URL": u, "ClientConfig": map[string]any{"PingInterval": 1}}})
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.conns <- c
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			fr, err := UnmarshalFrame(data)
			if err != nil {
				t.Errorf("client sent a bad frame: %v", err)
				return
			}
			f.mu.Lock()
			if fr.Method == frameControl && fr.Header("type") == "ping" {
				f.pings++
				if fr.Service != 9 {
					t.Errorf("ping service = %d", fr.Service)
				}
				pong := &Frame{Method: frameControl, Service: 9, Headers: []Header{{"type", "pong"}}, Payload: []byte(`{"PingInterval":1}`)}
				_ = c.Write(context.Background(), websocket.MessageBinary, pong.Marshal())
			} else {
				f.answers = append(f.answers, fr)
			}
			f.mu.Unlock()
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFeishu) send(c *websocket.Conn, fr *Frame) {
	if err := c.Write(context.Background(), websocket.MessageBinary, fr.Marshal()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeFeishu) waitAnswers(n int) []*Frame {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.answers) >= n {
			out := append([]*Frame{}, f.answers...)
			f.mu.Unlock()
			return out
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("got %d answers, want %d", len(f.answers), n)
	return nil
}

func TestConnDeliversEventsAndAnswers(t *testing.T) {
	f := newFakeFeishu(t)
	api := New("cli_x", "secret", f.srv.URL)
	got := make(chan string, 4)
	conn := NewConn(api, func(_ context.Context, payload []byte) (any, error) {
		got <- string(payload)
		if strings.Contains(string(payload), "card") {
			return CardResponse{Toast: &Toast{Type: "success", Content: "好"}}, nil
		}
		return nil, nil
	}, t.Logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go conn.Run(ctx)
	ws := <-f.conns

	// An event split in two parts arrives whole, once.
	whole := `{"schema":"2.0","header":{"event_type":"im.message.receive_v1"}}`
	for i, part := range []string{whole[:20], whole[20:]} {
		f.send(ws, &Frame{SeqID: uint64(i), Service: 9, Method: frameData, Payload: []byte(part),
			Headers: []Header{{"type", "event"}, {"message_id", "e1"}, {"sum", "2"}, {"seq", string(rune('0' + i))}}})
	}
	select {
	case p := <-got:
		if p != whole {
			t.Fatalf("payload %q", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event not delivered")
	}
	f.send(ws, &Frame{SeqID: 5, Service: 9, Method: frameData, Payload: []byte(`{"card":1}`), Headers: []Header{{"type", "card"}, {"message_id", "c1"}}})
	answers := f.waitAnswers(2)
	var card *Frame
	for _, a := range answers {
		if a.Header("message_id") == "c1" {
			card = a
		}
	}
	if card == nil || card.SeqID != 5 || card.Header("biz_rt") == "" {
		t.Fatalf("card answer: %+v", card)
	}
	var resp struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(card.Payload, &resp); err != nil || resp.Code != 200 {
		t.Fatalf("answer payload %s", card.Payload)
	}
	body, _ := base64.StdEncoding.DecodeString(resp.Data)
	if !strings.Contains(string(body), `"toast"`) {
		t.Fatalf("card answer data %s", body)
	}
	if st := conn.State(); !st.Connected || st.LastEvent.IsZero() {
		t.Fatalf("state %+v", st)
	}
	// Pings go out at the interval the server gave (1 s).
	time.Sleep(1500 * time.Millisecond)
	f.mu.Lock()
	pings := f.pings
	f.mu.Unlock()
	if pings < 2 {
		t.Fatalf("pings = %d", pings)
	}
}

func TestConnReportsABadSecret(t *testing.T) {
	f := newFakeFeishu(t)
	conn := NewConn(New("cli_x", "wrong", f.srv.URL), func(context.Context, []byte) (any, error) { return nil, nil }, t.Logf)
	err := conn.once(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bad secret") {
		t.Fatalf("err = %v", err)
	}
}

// The update errors are known from the APIError and from its text after the bus.
func TestUpdateErrorKinds(t *testing.T) {
	limited := &APIError{Op: "update card", Code: 230020, Msg: "This operation triggers the frequency limit."}
	gone := &APIError{Op: "update card", Code: 230011, Msg: "The message is recalled."}
	if !RateLimited(limited) || RateLimited(gone) || !RateLimited(errors.New(limited.Error())) || !RateLimited(errors.New("feishu update card: HTTP 429: bad json")) {
		t.Fatal("rate limit")
	}
	if !MessageGone(gone) || MessageGone(limited) || !MessageGone(errors.New("feishu update card: 230110 deleted")) || MessageGone(errors.New("offline")) {
		t.Fatal("gone")
	}
}
