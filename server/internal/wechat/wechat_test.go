package wechat

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
)

// memSecrets keeps the sealed blob in memory.
type memSecrets struct {
	mu   sync.Mutex
	data []byte
}

func (s *memSecrets) Load(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, nil
}
func (s *memSecrets) Save(_ context.Context, d []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append([]byte(nil), d...)
	return nil
}
func (s *memSecrets) Delete(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = nil
	return nil
}

func (s *memSecrets) state(t *testing.T) state {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st state
	if len(s.data) > 0 {
		if err := json.Unmarshal(s.data, &st); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// fakeILink is iLink: QR statuses as scripted, messages as queued, what the bot sends recorded.
type fakeILink struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	statuses []map[string]any
	queue    []map[string]any
	seq      int
	replaced bool
	sent     []sentText
	typing   []bool
	image    []byte
}

type sentText struct{ To, Context, Text string }

func newFakeILink(t *testing.T) *fakeILink {
	f := &fakeILink{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeILink) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(f.srv.URL)
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = u.Scheme, u.Host, u.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func (f *fakeILink) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	out := map[string]any{"ret": 0}
	f.mu.Lock()
	switch r.URL.Path {
	case "/ilink/bot/get_bot_qrcode":
		out = map[string]any{"qrcode": "QC", "qrcode_img_content": "https://liteapp.weixin.qq.com/q/example"}
	case "/ilink/bot/get_qrcode_status":
		out = map[string]any{"status": "wait"}
		if len(f.statuses) > 0 {
			out, f.statuses = f.statuses[0], f.statuses[1:]
		}
	case "/ilink/bot/getupdates":
		if f.replaced {
			out = map[string]any{"errcode": -14}
			break
		}
		msgs := f.queue
		f.queue = nil
		if len(msgs) == 0 {
			f.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			f.mu.Lock()
		}
		f.seq++
		out = map[string]any{"ret": 0, "msgs": msgs, "get_updates_buf": "cursor-" + strconv.Itoa(f.seq)}
	case "/ilink/bot/sendmessage":
		msg := body["msg"].(map[string]any)
		item := msg["item_list"].([]any)[0].(map[string]any)
		f.sent = append(f.sent, sentText{To: msg["to_user_id"].(string), Context: msg["context_token"].(string),
			Text: item["text_item"].(map[string]any)["text"].(string)})
	case "/ilink/bot/getconfig":
		out["typing_ticket"] = "TT"
	case "/ilink/bot/sendtyping":
		f.typing = append(f.typing, body["status"] == float64(1))
	case "/c2c/download":
		f.mu.Unlock()
		_, _ = w.Write(f.image)
		return
	}
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(out)
}

var msgSeq struct {
	sync.Mutex
	n int
}

// say queues a message from `from` (with items, text when none are given).
func (f *fakeILink) say(from, text string, items ...map[string]any) {
	msgSeq.Lock()
	msgSeq.n++
	id := msgSeq.n
	msgSeq.Unlock()
	if len(items) == 0 {
		items = []map[string]any{{"type": 1, "text_item": map[string]any{"text": text}}}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, map[string]any{"message_id": id, "from_user_id": from, "context_token": "ctx-" + strconv.Itoa(id),
		"message_type": 1, "item_list": items})
}

func (f *fakeILink) texts() []sentText {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentText(nil), f.sent...)
}

// fakeHub is the hub's API: one machine m1 with the assistant's conversations.
type fakeHub struct {
	t            *testing.T
	srv          *httptest.Server
	mu           sync.Mutex
	main         string
	sessions     map[string]*proto.Session
	items        map[string][]proto.Item
	sends        []hubSend
	starts       []hubSend
	interactions []proto.Interaction
	resolved     map[string]proto.Resolution
	interrupted  []string
	seq          int
}

type hubSend struct {
	Session, Text, ClientMsgID string
	Images                     int
	Main                       bool
}

func newFakeHub(t *testing.T) *fakeHub {
	h := &fakeHub{t: t, main: "main1", sessions: map[string]*proto.Session{}, items: map[string][]proto.Item{}, resolved: map[string]proto.Resolution{}}
	h.sessions["main1"] = &proto.Session{MachineID: "m1", SessionID: "main1", Status: "idle", State: "idle", Title: "主对话"}
	h.srv = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeHub) add(session string, it proto.Item) {
	h.seq++
	it.Seq, it.ID = h.seq, "it"+strconv.Itoa(h.seq)
	h.items[session] = append(h.items[session], it)
}

func (h *fakeHub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(401)
		return
	}
	var body map[string]any
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &body)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	p := r.URL.Path
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	images := func() int { a, _ := body["images"].([]any); return len(a) }
	switch {
	case p == "/api/v1/assistant":
		reply(map[string]any{"machine_id": "m1", "main": h.main})
	case p == "/api/v1/assistant/conversations":
		id := "conv" + strconv.Itoa(len(h.starts)+1)
		main, _ := body["main"].(bool)
		h.starts = append(h.starts, hubSend{Session: id, Text: body["prompt"].(string), Images: images(), Main: main})
		h.sessions[id] = &proto.Session{MachineID: "m1", SessionID: id, Status: "working", State: "working"}
		h.add(id, proto.Item{Role: "user", Kind: proto.KindText, Text: body["prompt"].(string)})
		if main {
			h.main = id
		}
		reply(map[string]any{"session": h.sessions[id]})
	case p == "/api/v1/sessions":
		var list []proto.Session
		for _, s := range h.sessions {
			list = append(list, *s)
		}
		reply(map[string]any{"sessions": list})
	case p == "/api/v1/interactions":
		reply(map[string]any{"interactions": h.interactions})
	case strings.HasPrefix(p, "/api/v1/interactions/") && strings.HasSuffix(p, "/resolve"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/v1/interactions/"), "/resolve")
		for i, ix := range h.interactions {
			if ix.ID == id {
				var res proto.Resolution
				b, _ := json.Marshal(body)
				_ = json.Unmarshal(b, &res)
				h.resolved[id] = res
				h.interactions = append(h.interactions[:i], h.interactions[i+1:]...)
				reply(map[string]any{"ok": true})
				return
			}
		}
		w.WriteHeader(http.StatusGone)
		reply(map[string]any{"error": "not pending"})
	case strings.HasPrefix(p, "/api/v1/sessions/m1/"):
		rest := strings.Split(strings.TrimPrefix(p, "/api/v1/sessions/m1/"), "/")
		sid := rest[0]
		s := h.sessions[sid]
		if s == nil {
			w.WriteHeader(404)
			return
		}
		switch rest[1] {
		case "messages":
			text := body["text"].(string)
			id, _ := body["client_msg_id"].(string)
			h.sends = append(h.sends, hubSend{Session: sid, Text: text, ClientMsgID: id, Images: images()})
			h.add(sid, proto.Item{Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: id})
			s.Status, s.State = "working", "working"
			reply(map[string]any{"ok": true})
		case "interrupt":
			h.interrupted = append(h.interrupted, sid)
			reply(map[string]any{"ok": true})
		case "items":
			items := h.items[sid]
			if r.URL.Query().Get("limit") == "1" {
				to := 0
				if len(items) > 0 {
					to = items[len(items)-1].Seq
				}
				reply(map[string]any{"items": items[max(len(items)-1, 0):], "to_seq": to})
				return
			}
			after, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
			var out []proto.Item
			for _, it := range items {
				if it.Seq > after {
					out = append(out, it)
				}
			}
			reply(map[string]any{"items": out, "more_after": false})
		}
	default:
		w.WriteHeader(404)
	}
}

// answer ends a session's turn: a tool call, then the assistant's words.
func (h *fakeHub) answer(sid, text string, background int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.add(sid, proto.Item{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"})
	h.add(sid, proto.Item{Role: "assistant", Kind: proto.KindText, Text: "我先看看。"})
	h.add(sid, proto.Item{Role: "user", Kind: proto.KindToolResult, Text: "ok"})
	h.add(sid, proto.Item{Role: "assistant", Kind: proto.KindText, Text: text})
	s := h.sessions[sid]
	s.Status, s.State = "idle", "unread"
	s.Background = nil
	for range background {
		s.Background = append(s.Background, proto.BackgroundTask{})
	}
}

func (h *fakeHub) snapshot() (sends, starts []hubSend) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hubSend(nil), h.sends...), append([]hubSend(nil), h.starts...)
}

// clock is a time the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type rig struct {
	t       *testing.T
	ilink   *fakeILink
	hub     *fakeHub
	secrets *memSecrets
	clock   *clock
	bot     *Bot
	cancel  context.CancelFunc
}

const owner = "person@example.com"

// newRig is a bot bound to owner's WeChat, running against the fakes.
func newRig(t *testing.T, bound bool) *rig {
	r := &rig{t: t, ilink: newFakeILink(t), hub: newFakeHub(t), secrets: &memSecrets{}, clock: &clock{t: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}}
	if bound {
		st := state{Creds: &ilink.Credentials{Token: "BT", BotID: "person@example.com", UserID: owner, BaseURL: "https://ilinkai.weixin.qq.com/"}}
		data, _ := json.Marshal(st)
		r.secrets.data = data
	}
	r.bot = New(Options{Hub: NewHub(r.hub.srv.URL, "tok"), Agent: "Persona/test", Now: r.clock.now, Tick: 10 * time.Millisecond,
		NewClient: func(base, token string) (*ilink.Client, error) {
			return ilink.New(ilink.Options{BaseURL: base, Token: token, Agent: "Persona/test", HTTP: &http.Client{Transport: r.ilink}})
		}}, r.secrets)
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	t.Cleanup(cancel)
	go r.bot.Run(ctx)
	return r
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sentTo waits for the bot to have sent a text to peer containing want; it returns that text.
func (r *rig) sentTo(peer, want string) sentText {
	r.t.Helper()
	var got sentText
	eventually(r.t, fmt.Sprintf("a message to %s with %q (sent: %+v)", peer, want, r.ilink.texts()), func() bool {
		for _, s := range r.ilink.texts() {
			if s.To == peer && strings.Contains(s.Text, want) {
				got = s
				return true
			}
		}
		return false
	})
	return got
}

func TestBindByQRCode(t *testing.T) {
	r := newRig(t, false)
	ctx := context.Background()
	eventually(t, "not signed in", func() bool { return r.bot.Status(ctx).State == connector.StateNotSignedIn })
	r.ilink.statuses = []map[string]any{{"status": "scaned"}, {"status": "need_verifycode"},
		{"status": "confirmed", "bot_token": "BT", "ilink_bot_id": "person@example.com", "ilink_user_id": owner, "baseurl": "https://ilinkai.weixin.qq.com"}}
	p, err := r.bot.LoginStep(ctx, "", nil)
	if err != nil || p.Kind != connector.PromptQRCode || p.URL != "https://liteapp.weixin.qq.com/q/example" || p.Interval == 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if p, _ = r.bot.LoginStep(ctx, "qr", nil); !strings.Contains(p.Hint, "已经扫码") {
		t.Fatalf("scanned %+v", p)
	}
	if p, _ = r.bot.LoginStep(ctx, "qr", nil); p.Step != "code" || len(p.Fields) != 1 {
		t.Fatalf("code %+v", p)
	}
	if p, _ = r.bot.LoginStep(ctx, "code", map[string]string{"code": "x1"}); p.Error == "" {
		t.Fatalf("a bad code %+v", p)
	}
	if p, _ = r.bot.LoginStep(ctx, "code", map[string]string{"code": "123456"}); !p.Done || !strings.Contains(p.Account, "微信用户") {
		t.Fatalf("done %+v", p)
	}
	if st := r.secrets.state(t); st.Creds == nil || st.Creds.Token != "BT" || st.Creds.UserID != owner {
		t.Fatalf("stored %+v", st)
	}
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	if err := r.bot.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	st := r.secrets.state(t)
	if st.Creds != nil || len(st.Old) != 1 || st.Old[0] != "BT" {
		t.Fatalf("after logout %+v", st)
	}
	if s := r.bot.Status(ctx); s.State != connector.StateNotSignedIn {
		t.Fatalf("status %+v", s)
	}
}

func TestMessageToMainAndAnswerBack(t *testing.T) {
	r := newRig(t, true)
	r.ilink.say(owner, "明天有什么会？")
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	sends, _ := r.hub.snapshot()
	if sends[0].Session != "main1" || !strings.HasPrefix(sends[0].ClientMsgID, "wechat-message:") ||
		!strings.Contains(sends[0].Text, "微信机器人") || !strings.HasSuffix(sends[0].Text, "明天有什么会？") {
		t.Fatalf("send %+v", sends[0])
	}
	r.hub.answer("main1", "**明天** 10 点有周会。", 0)
	r.clock.add(30 * time.Second)
	got := r.sentTo(owner, "10 点有周会")
	if got.Context == "" || strings.Contains(got.Text, "我先看看") || !strings.HasPrefix(got.Text, "**明天**") { // WeChat shows bold
		t.Fatalf("answer %+v", got)
	}
	eventually(t, "the run to end", func() bool { return len(r.secrets.state(t).Runs) == 0 })
	if st := r.secrets.state(t); st.Cursor == "" || st.Context == "" {
		t.Fatalf("cursor and context not kept: %+v", st)
	}
}

func TestStrangerHearsOnce(t *testing.T) {
	r := newRig(t, true)
	r.ilink.say("person@example.com", "你好")
	r.sentTo("person@example.com", "只为绑定它的人服务")
	r.ilink.say("person@example.com", "在吗")
	r.ilink.say(owner, "/status")
	r.sentTo(owner, "消息发给：主对话")
	n := 0
	for _, s := range r.ilink.texts() {
		if s.To == "person@example.com" {
			n++
		}
	}
	if sends, _ := r.hub.snapshot(); n != 1 || len(sends) != 0 {
		t.Fatalf("stranger heard %d times; sends %+v", n, sends)
	}
}

func TestNewConversationAndStop(t *testing.T) {
	r := newRig(t, true)
	r.ilink.say(owner, "/new 帮我写个周报")
	eventually(t, "a new conversation", func() bool { _, s := r.hub.snapshot(); return len(s) == 1 })
	_, starts := r.hub.snapshot()
	if starts[0].Main || !strings.HasSuffix(starts[0].Text, "帮我写个周报") {
		t.Fatalf("start %+v", starts[0])
	}
	eventually(t, "the target", func() bool { return r.secrets.state(t).Target.Session == "conv1" })
	r.ilink.say(owner, "再加上下周计划")
	eventually(t, "the send to the new one", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 && s[0].Session == "conv1" })
	r.ilink.say(owner, "/stop")
	r.sentTo(owner, "已停下")
	r.hub.mu.Lock()
	interrupted := r.hub.interrupted
	r.hub.mu.Unlock()
	if len(interrupted) != 1 || interrupted[0] != "conv1" {
		t.Fatalf("interrupted %v", interrupted)
	}
	r.ilink.say(owner, "/main")
	r.sentTo(owner, "主对话")
	eventually(t, "back to main", func() bool { return r.secrets.state(t).Target.main() })
}

func TestNoMainConversationStartsOne(t *testing.T) {
	r := newRig(t, true)
	r.hub.mu.Lock()
	r.hub.main = ""
	r.hub.mu.Unlock()
	r.ilink.say(owner, "你好")
	eventually(t, "a main conversation", func() bool { _, s := r.hub.snapshot(); return len(s) == 1 && s[0].Main })
	r.hub.answer("conv1", "你好，我在。", 0)
	r.clock.add(30 * time.Second)
	r.sentTo(owner, "你好，我在。")
}

func TestPermissionAnsweredInWeChat(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	r.ilink.say(owner, "清一下临时文件")
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	r.hub.mu.Lock()
	r.hub.interactions = []proto.Interaction{{ID: "ix1", MachineID: "m1", SessionID: "main1", Kind: proto.IxPermission, ToolName: "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /tmp/x"}`), SessionTitle: "主对话"}}
	r.hub.mu.Unlock()
	r.bot.Hear(ctx, "agent.needs_you", json.RawMessage(`{"machine_id":"m1","session_id":"main1","interaction_id":"ix1","kind":"permission"}`))
	r.sentTo(owner, "rm -rf /tmp/x")
	r.ilink.say(owner, "嗯？")
	r.sentTo(owner, "回复「允许」或「拒绝」")
	r.ilink.say(owner, "允许")
	r.sentTo(owner, "已允许")
	r.hub.mu.Lock()
	res := r.hub.resolved["ix1"]
	r.hub.mu.Unlock()
	if res.Allow == nil || !*res.Allow {
		t.Fatalf("resolution %+v", res)
	}
	if sends, _ := r.hub.snapshot(); len(sends) != 1 {
		t.Fatalf("an answer was sent as a message: %+v", sends)
	}
}

func TestQuestionsOneAtATime(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	r.ilink.say(owner, "部署一下")
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	r.hub.mu.Lock()
	r.hub.interactions = []proto.Interaction{{ID: "ix2", MachineID: "m1", SessionID: "main1", Kind: proto.IxQuestion, Questions: []proto.Question{
		{Question: "部署到哪？", Options: []proto.QuestionOption{{Label: "测试环境"}, {Label: "正式环境"}}},
		{Question: "要通知谁？", MultiSelect: true, Options: []proto.QuestionOption{{Label: "张三"}, {Label: "李四"}, {Label: "王五"}}},
	}}}
	r.hub.mu.Unlock()
	r.bot.Hear(ctx, "agent.needs_you", json.RawMessage(`{"machine_id":"m1","session_id":"main1","interaction_id":"ix2"}`))
	r.sentTo(owner, "（1/2）Agent 问你：部署到哪？")
	r.ilink.say(owner, "2")
	r.sentTo(owner, "（2/2）Agent 问你：要通知谁？")
	r.ilink.say(owner, "1 3")
	r.sentTo(owner, "已回答")
	r.hub.mu.Lock()
	res := r.hub.resolved["ix2"]
	r.hub.mu.Unlock()
	if res.Answers["部署到哪？"] != "正式环境" || res.Answers["要通知谁？"] != "张三, 王五" {
		t.Fatalf("answers %+v", res.Answers)
	}
}

// A card answered in the app is let go: the next WeChat message is an ordinary one.
func TestAnsweredElsewhere(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	r.ilink.say(owner, "/status") // a chat to send to (iLink needs the owner's latest message)
	r.sentTo(owner, "消息发给")
	r.hub.mu.Lock()
	r.hub.interactions = []proto.Interaction{{ID: "ix3", MachineID: "m1", SessionID: "main1", Kind: proto.IxPlan, Plan: "1. 改代码"}}
	r.hub.mu.Unlock()
	r.bot.Hear(ctx, "agent.needs_you", json.RawMessage(`{"machine_id":"m1","session_id":"main1","interaction_id":"ix3"}`))
	r.sentTo(owner, "1. 改代码")
	r.hub.mu.Lock()
	r.hub.interactions = nil
	r.hub.mu.Unlock()
	r.ilink.say(owner, "好的")
	eventually(t, "sent as a message", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	if st := r.secrets.state(t); len(st.Asks) != 0 {
		t.Fatalf("asks %+v", st.Asks)
	}
}

// An agent not driven from WeChat asks: nothing goes to WeChat.
func TestOtherAgentsStayQuiet(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	r.hub.mu.Lock()
	r.hub.interactions = []proto.Interaction{{ID: "ix4", MachineID: "m1", SessionID: "agent9", Kind: proto.IxPermission, ToolName: "Bash"}}
	r.hub.mu.Unlock()
	r.bot.Hear(ctx, "agent.needs_you", json.RawMessage(`{"machine_id":"m1","session_id":"agent9","interaction_id":"ix4"}`))
	time.Sleep(100 * time.Millisecond)
	if got := r.ilink.texts(); len(got) != 0 {
		t.Fatalf("sent %+v", got)
	}
}

func TestBackgroundWorkAnswersTwice(t *testing.T) {
	r := newRig(t, true)
	r.ilink.say(owner, "跑一下全量测试")
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	r.hub.answer("main1", "测试已经在后台跑了。", 1)
	r.clock.add(30 * time.Second)
	r.sentTo(owner, "后台还有 1 个任务")
	eventually(t, "held", func() bool { st := r.secrets.state(t); return len(st.Runs) == 1 && st.Runs[0].Held })
	r.hub.mu.Lock()
	r.hub.add("main1", proto.Item{Role: "user", Kind: proto.KindNotice, Text: "task done"})
	r.hub.sessions["main1"].Status, r.hub.sessions["main1"].State = "working", "working"
	r.hub.mu.Unlock()
	r.hub.answer("main1", "全部通过，142 个。", 0)
	r.clock.add(30 * time.Second)
	r.sentTo(owner, "全部通过，142 个")
	eventually(t, "the run to end", func() bool { return len(r.secrets.state(t).Runs) == 0 })
}

func TestTokenReplacedUnbinds(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	r.ilink.mu.Lock()
	r.ilink.replaced = true
	r.ilink.mu.Unlock()
	eventually(t, "unbound", func() bool {
		s := r.bot.Status(ctx)
		return s.State == connector.StateNotSignedIn && strings.Contains(s.Error, "失效")
	})
	if st := r.secrets.state(t); st.Creds != nil || len(st.Old) != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestPictureGoesWithTheMessage(t *testing.T) {
	r := newRig(t, true)
	key, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	enc, _ := ilink.EncryptECB(png, key)
	r.ilink.mu.Lock()
	r.ilink.image = enc
	r.ilink.mu.Unlock()
	r.ilink.say(owner, "", map[string]any{"type": 2, "image_item": map[string]any{"aeskey": hex.EncodeToString(key),
		"media": map[string]any{"encrypt_query_param": "Q"}}},
		map[string]any{"type": 4, "file_item": map[string]any{"file_name": "合同.pdf", "media": map[string]any{"encrypt_query_param": "F"}}})
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) == 1 })
	sends, _ := r.hub.snapshot()
	if sends[0].Images != 1 || !strings.Contains(sends[0].Text, "文件「合同.pdf」") {
		t.Fatalf("send %+v", sends[0])
	}
}

func TestDuplicateMessageHandledOnce(t *testing.T) {
	r := newRig(t, true)
	item := []map[string]any{{"type": 1, "text_item": map[string]any{"text": "一次就好"}}}
	r.ilink.mu.Lock()
	for range 2 {
		r.ilink.queue = append(r.ilink.queue, map[string]any{"message_id": 9001, "from_user_id": owner, "context_token": "c", "message_type": 1, "item_list": item})
	}
	r.ilink.mu.Unlock()
	eventually(t, "the send", func() bool { s, _ := r.hub.snapshot(); return len(s) >= 1 })
	time.Sleep(100 * time.Millisecond)
	if s, _ := r.hub.snapshot(); len(s) != 1 {
		t.Fatalf("sent %d times", len(s))
	}
}

// Before the owner has written to the bot there is no chat to ask in: nothing is kept as asked.
func TestNoChatYetNothingAsked(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	eventually(t, "serving", func() bool { return r.bot.Status(ctx).State == connector.StateOK })
	r.hub.mu.Lock()
	r.hub.interactions = []proto.Interaction{{ID: "ix5", MachineID: "m1", SessionID: "main1", Kind: proto.IxPermission, ToolName: "Bash"}}
	r.hub.mu.Unlock()
	r.bot.Hear(ctx, "agent.needs_you", json.RawMessage(`{"machine_id":"m1","session_id":"main1","interaction_id":"ix5"}`))
	time.Sleep(100 * time.Millisecond)
	if st := r.secrets.state(t); len(st.Asks) != 0 {
		t.Fatalf("asks %+v", st.Asks)
	}
}

// A picture with no words that starts a new conversation is answered as soon as its turn ends: there is nothing
// before it to tell it apart from.
func TestPictureStartsAConversation(t *testing.T) {
	r := newRig(t, true)
	key, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	enc, _ := ilink.EncryptECB(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...), key)
	r.ilink.mu.Lock()
	r.ilink.image = enc
	r.ilink.mu.Unlock()
	r.hub.mu.Lock()
	r.hub.main = ""
	r.hub.mu.Unlock()
	r.ilink.say(owner, "", map[string]any{"type": 2, "image_item": map[string]any{"aeskey": hex.EncodeToString(key),
		"media": map[string]any{"encrypt_query_param": "Q"}}})
	eventually(t, "a main conversation", func() bool { _, s := r.hub.snapshot(); return len(s) == 1 && s[0].Images == 1 })
	r.hub.answer("conv1", "这是一张空白的图。", 0)
	r.clock.add(30 * time.Second)
	r.sentTo(owner, "这是一张空白的图。")
}
