package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
)

// fakeChannel is a channel plugin's program as the hub's pipeline reaches it: what was sent, typing on and off.
type fakeChannel struct {
	mu      sync.Mutex
	sent    []channel.SendParams
	typing  []bool
	refused bool // cannot_send: nowhere to send to yet
}

func (f *fakeChannel) call(_ context.Context, id, method string, params, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case channel.MSend:
		if f.refused {
			return channel.ErrCannotSend
		}
		p := params.(channel.SendParams)
		for _, s := range f.sent {
			if s.Ref == p.Ref {
				return nil // once by its ref
			}
		}
		f.sent = append(f.sent, p)
	case channel.MTyping:
		f.typing = append(f.typing, params.(channel.TypingParams).On)
	}
	return nil
}

func (f *fakeChannel) find(t *testing.T, want string) channel.SendParams {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, s := range f.sent {
			if strings.Contains(s.Text, want) {
				f.mu.Unlock()
				return s
			}
		}
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("nothing sent with %q; sent %+v", want, f.sent)
	return channel.SendParams{}
}

func (f *fakeChannel) count(want string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.sent {
		if strings.Contains(s.Text, want) {
			n++
		}
	}
	return n
}

// The connectors' secrets, kept with the settings (prefixed).
func (s *botStore) ConnectorSecret(_ context.Context, id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals["secret:"+id]
	return string(b), ok, nil
}

func (s *botStore) PutConnectorSecret(_ context.Context, id, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals["secret:"+id] = []byte(sealed)
	return nil
}

func (s *botStore) DeleteConnectorSecret(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vals, "secret:"+id)
	return nil
}

// exampleChannel is a third-party chat app's bot, as a plugin contributes it: plain text, answers in text.
var exampleChannel = channel.Item{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM,
	Capabilities: channel.Capabilities{ImagesIn: true, Format: channel.FormatPlain, MaxText: 400, Asks: channel.AsksText, Push: true, Typing: true}}

type channelWorld struct {
	*botWorld
	ch *fakeChannel
}

// newChannelWorld is the bot world (a hub, the assistant's machine "box" with its main conversation main-1) with a
// plugin contributing examplechat, connected.
func newChannelWorld(t *testing.T) *channelWorld {
	t.Helper()
	w := newBotWorld(t)
	raw, _ := json.Marshal([]channel.Item{exampleChannel})
	m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: "example-chat", Name: "Example Chat", Version: "1.0.0",
		Engines: extension.Engines{ExtensionProtocol: extension.Protocol}, ActivationEvents: []string{extension.OnStartup},
		Runtime: &extension.Runtime{Command: extension.Command{Path: "example-chat"}}, Contributes: map[string]json.RawMessage{"channel": raw}}
	reg := extension.NewRegistry(t.TempDir(), nil, nil, 0)
	if err := reg.AddBuiltin(m, "/opt/example", true); err != nil {
		t.Fatal(err)
	}
	if err := reg.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.h.extensions = newExtensions(t.TempDir(), nil, w.h.logf)
	w.h.extensions.reg = reg
	ch := &fakeChannel{}
	w.h.channels.call = ch.call
	w.h.channels.statuses = map[string]*channel.StatusParams{"examplechat": {State: channel.StateConnected, Account: &channel.Account{Name: "示例账号"}}}
	w.h.channelRoutes(w.mux.(*http.ServeMux))
	return &channelWorld{botWorld: w, ch: ch}
}

func (w *channelWorld) receive(t *testing.T, id, text string) {
	t.Helper()
	p, _ := json.Marshal(channel.ReceiveParams{Contribution: "examplechat", Message: channel.Message{ID: id, Text: text}})
	if _, err := w.h.extensionChannelCall("example-chat", channel.MReceive, p); err != nil {
		t.Fatal(err)
	}
}

func (w *channelWorld) turn(items ...proto.Item) {
	w.node.mu.Lock()
	w.node.items = append(w.node.items, items...)
	w.node.last = items[len(items)-1].Seq
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
}

// A message from a plugin's channel goes into the main conversation once (marked with the channel), the turn's
// answer comes back through channel.send in pieces the channel takes, 「正在输入」 meanwhile; the phone is not told.
func TestChannelMessageGoesToTheMainConversationAndTheAnswerComesBack(t *testing.T) {
	w := newChannelWorld(t)
	w.receive(t, "m1", "明天有什么会？")
	w.receive(t, "m1", "明天有什么会？") // delivered again
	sent := w.node.sent(t, 0)
	text, _ := sent["text"].(string)
	if sent["session_id"] != "main-1" || sent["client_msg_id"] != "examplechat-m1" || !strings.Contains(text, `title="Example Chat机器人"`) ||
		!strings.HasSuffix(text, "明天有什么会？") || !strings.Contains(text, "纯文字") {
		t.Fatalf("send %v", sent)
	}
	long := strings.Repeat("会议安排在上午十点。", 60)
	w.turn(proto.Item{Seq: 11, Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: "examplechat-m1"},
		proto.Item{Seq: 12, Role: "assistant", Kind: proto.KindText, Text: "我查一下。"},
		proto.Item{Seq: 12, Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"},
		proto.Item{Seq: 13, Role: "user", Kind: proto.KindToolResult},
		proto.Item{Seq: 14, Role: "assistant", Kind: proto.KindText, Text: "明天两个会。" + long})
	first := w.ch.find(t, "明天两个会")
	if first.Kind != channel.OutAnswer || first.ReplyTo != "m1" || len([]rune(first.Text)) > 400 {
		t.Fatalf("answer %+v", first)
	}
	if w.ch.count("我查一下") != 0 {
		t.Fatal("the words before the tool call were sent")
	}
	if n := len(w.node.sends); n != 1 {
		t.Fatalf("%d sends, want 1", n)
	}
	if !w.h.channelAnswered("main-1") {
		t.Fatal("a turn answered in a channel must not also push 完成 to the phone")
	}
	w.ch.mu.Lock()
	typing := append([]bool(nil), w.ch.typing...)
	pieces := len(w.ch.sent)
	w.ch.mu.Unlock()
	if len(typing) < 2 || !typing[0] || typing[len(typing)-1] || pieces < 2 {
		t.Fatalf("typing %v, %d pieces", typing, pieces)
	}
	// The conversation says where its messages came from.
	if got := w.h.decorated(w.m); len(got) != 1 || len(got[0].Channels) != 1 || got[0].Channels[0] != "examplechat" {
		t.Fatalf("channels on the conversation: %+v", got)
	}
}

// A permission asked in a conversation the channel drives goes there as text; 「允许」 answers it.
func TestChannelAnswersAPermissionInText(t *testing.T) {
	w := newChannelWorld(t)
	ix := proto.Interaction{ID: "ix_p", MachineID: "box", SessionID: "main-1", SessionTitle: "Persona", Kind: proto.IxPermission,
		ToolName: "Bash", Summary: "rm -rf build", ExpiresAt: time.Now().Add(time.Minute)}
	got := make(chan proto.Resolution, 1)
	go func() {
		r, _ := w.h.waitInteraction(context.Background(), ix)
		got <- r
	}()
	ask := w.ch.find(t, "要你批准「Bash」")
	if ask.Kind != channel.OutAsk {
		t.Fatalf("ask %+v", ask)
	}
	w.receive(t, "m2", "好像不对") // not an answer: asked again
	w.ch.find(t, "回复「允许」或「拒绝」")
	w.receive(t, "m3", "允许")
	select {
	case r := <-got:
		if r.Allow == nil || !*r.Allow {
			t.Fatalf("resolution %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not answered")
	}
	w.ch.find(t, "已允许")
	if len(w.node.sends) != 0 {
		t.Fatalf("the answers went to the conversation: %v", w.node.sends)
	}
}

// Answered in the app: the channel stops waiting, and a reply is an ordinary message.
func TestChannelAskAnsweredElsewhere(t *testing.T) {
	w := newChannelWorld(t)
	ix := proto.Interaction{ID: "ix_e", MachineID: "box", SessionID: "main-1", Kind: proto.IxPermission, ToolName: "Bash", ExpiresAt: time.Now().Add(time.Minute)}
	go func() { _, _ = w.h.waitInteraction(context.Background(), ix) }()
	w.ch.find(t, "要你批准")
	if err := w.h.resolve("ix_e", proto.Resolution{Allow: new(bool)}); err != nil {
		t.Fatal(err)
	}
	if n := w.h.channelWaiting("examplechat").Asks; n != 0 {
		t.Fatalf("%d asks still wait", n)
	}
	w.receive(t, "m4", "允许")
	if text, _ := w.node.sent(t, 0)["text"].(string); !strings.HasSuffix(text, "允许") {
		t.Fatalf("send %q", text)
	}
}

// Nowhere to send to yet (cannot_send): nothing is kept as asked there.
func TestChannelCannotSendAsksNothing(t *testing.T) {
	w := newChannelWorld(t)
	w.ch.refused = true
	ix := proto.Interaction{ID: "ix_n", MachineID: "box", SessionID: "main-1", Kind: proto.IxPermission, ToolName: "Bash", ExpiresAt: time.Now().Add(time.Minute)}
	go func() { _, _ = w.h.waitInteraction(context.Background(), ix) }()
	time.Sleep(300 * time.Millisecond)
	if n := w.h.channelWaiting("examplechat").Asks; n != 0 {
		t.Fatalf("%d asks kept", n)
	}
}

// The commands: /status says where messages go; /main and the app's PUT change it.
func TestChannelCommandsAndTarget(t *testing.T) {
	w := newChannelWorld(t)
	w.receive(t, "c1", "/status")
	w.ch.find(t, "消息发给：主对话")
	w.receive(t, "c2", "/new")
	w.ch.find(t, "下一条消息会开一段新对话")
	if code, out := w.do(t, "GET", "/api/v1/channels/examplechat", ""); code != 200 || out["target"].(map[string]any)["kind"] != "new" {
		t.Fatalf("detail %d %v", code, out)
	}
	if code, out := w.do(t, "PUT", "/api/v1/channels/examplechat", `{"target":{"kind":"conversation","machine_id":"box","session_id":"nope"}}`); code != 400 {
		t.Fatalf("a conversation that is not there: %d %v", code, out)
	}
	if code, out := w.do(t, "PUT", "/api/v1/channels/examplechat", `{"target":{"kind":"main"},"features":{"all_asks":true}}`); code != 200 {
		t.Fatalf("put %d %v", code, out)
	} else {
		on := map[string]bool{}
		for _, f := range out["features"].([]any) {
			m := f.(map[string]any)
			on[m["id"].(string)] = m["on"].(bool)
		}
		if !on["asks"] || !on["all_asks"] || on["sync"] {
			t.Fatalf("features %v", on)
		}
	}
	if code, _ := w.do(t, "PUT", "/api/v1/channels/examplechat", `{"features":{"nope":true}}`); code != 400 {
		t.Fatalf("an unknown feature: %d", code)
	}
	if code, _ := w.do(t, "PUT", "/api/v1/channels/app", `{"enabled":false}`); code != 409 {
		t.Fatalf("the apps are not set up here: %d", code)
	}
}

// GET /api/v1/channels: the apps first, then the connected, the rest.
func TestChannelList(t *testing.T) {
	w := newChannelWorld(t)
	code, out := w.do(t, "GET", "/api/v1/channels", "")
	if code != 200 {
		t.Fatal(code)
	}
	list := out["channels"].([]any)
	byID := map[string]map[string]any{}
	var order []string
	for _, c := range list {
		m := c.(map[string]any)
		byID[m["id"].(string)] = m
		order = append(order, m["id"].(string))
	}
	if order[0] != "app" {
		t.Fatalf("order %v", order)
	}
	ex := byID["examplechat"]
	if ex == nil || ex["state"] != "connected" || ex["source"] != "plugin" || ex["plugin"] != "example-chat" ||
		ex["account"].(map[string]any)["name"] != "示例账号" || ex["capabilities"].(map[string]any)["asks"] != "text" {
		t.Fatalf("examplechat %v", ex)
	}
	if fb := byID["feishu"]; fb == nil || fb["state"] == "connected" || fb["bind"] != "fields" { // not paired
		t.Fatalf("feishu %v", fb)
	}
	if byID["lark_agent"] != nil {
		t.Fatal("the hub's own /agent channel is a plugin's now")
	}
}

// The test message goes to the owner only, through the channel.
func TestChannelTestMessage(t *testing.T) {
	w := newChannelWorld(t)
	if code, out := w.do(t, "POST", "/api/v1/channels/examplechat/test", ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	w.ch.find(t, "测试消息")
}

// The WeChat program's state from before the pipeline: where its messages went, its turns and asks move to the hub;
// its token stays its own.
func TestWeChatStateMoves(t *testing.T) {
	w := newBotWorld(t)
	sec := w.h.secretsFor("wechat")
	old := `{"creds":{"token":"BT"},"cursor":"c9","context":"ctx","target":{"machine":"box","session":"conv-2"},` +
		`"runs":[{"machine":"box","session":"main-1","base":10,"msg_ids":["wechat-message:1"],"started":"2026-11-04T09:00:00Z"}],` +
		`"asks":[{"id":"ix9","machine":"box","session":"main-1","kind":"permission","at":"2026-11-04T09:00:00Z"}]}`
	if err := sec.Save(context.Background(), []byte(old)); err != nil {
		t.Fatal(err)
	}
	w.h.migrateWeChatState(context.Background())
	data, _ := sec.Load(context.Background())
	var left map[string]any
	_ = json.Unmarshal(data, &left)
	if left["target"] != nil || left["runs"] != nil || left["asks"] != nil || left["cursor"] != "c9" || left["creds"] == nil {
		t.Fatalf("left %s", data)
	}
	runs := w.h.channelRuns("wechat")
	if len(runs) != 1 || runs[0].SessionID != "main-1" || runs[0].MsgIDs[0] != "wechat-message:1" {
		t.Fatalf("runs %+v", runs)
	}
	w.h.channels.mu.Lock()
	tg := w.h.channels.prefs["wechat"].Target
	asks := len(w.h.channels.st.Asks)
	w.h.channels.mu.Unlock()
	if tg == nil || tg.Kind != channel.TargetConversation || tg.SessionID != "conv-2" || asks != 1 {
		t.Fatalf("target %+v, %d asks", tg, asks)
	}
	w.h.migrateWeChatState(context.Background()) // once
	if n := len(w.h.channelRuns("wechat")); n != 1 {
		t.Fatalf("%d runs after a second move", n)
	}
}
