package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// botStore keeps settings, sends and interactions in memory.
type botStore struct {
	Store
	mu    sync.Mutex
	vals  map[string][]byte
	sends map[string]string
}

func newBotStore() *botStore {
	return &botStore{vals: map[string][]byte{}, sends: map[string]string{}}
}

func (s *botStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *botStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	return nil
}

func (s *botStore) ClaimSend(_ context.Context, id, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.sends[id]; ok {
		return st, nil
	}
	s.sends[id] = sendSending
	return "", nil
}

func (s *botStore) SettleSend(_ context.Context, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == "" {
		delete(s.sends, id)
	} else {
		s.sends[id] = state
	}
	return nil
}

func (s *botStore) InsertInteraction(context.Context, proto.Interaction) error { return nil }
func (s *botStore) FinishInteraction(context.Context, string, string, *proto.Resolution) error {
	return nil
}
func (s *botStore) InteractionOutcome(context.Context, string) (string, *proto.Resolution, error) {
	return "", nil, nil
}

// fakeOpenAPI is Feishu's open platform: it records every call the bot makes.
type fakeOpenAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string // "METHOD path body"
	n     int
	read  map[string]any // message id -> what GET /messages/:id answers (its items)
}

func newFakeOpenAPI(t *testing.T) *fakeOpenAPI {
	f := &fakeOpenAPI{read: map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+string(body))
		f.n++
		n := f.n
		f.mu.Unlock()
		p := r.URL.Path
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case p == "/open-apis/auth/v3/tenant_access_token/internal":
			reply(map[string]any{"code": 0, "tenant_access_token": "t-1", "expire": 7200})
		case p == "/open-apis/bot/v3/info":
			reply(map[string]any{"code": 0, "bot": map[string]any{"app_name": "Persona", "open_id": "ou_bot"}})
		case p == "/callback/ws/endpoint":
			reply(map[string]any{"code": 1, "msg": "no long connection in tests"})
		case strings.HasPrefix(p, "/open-apis/contact/v3/users/"):
			reply(map[string]any{"code": 0, "data": map[string]any{"user": map[string]any{"name": "示例用户"}}})
		case strings.HasSuffix(p, "/reactions") && r.Method == "POST":
			reply(map[string]any{"code": 0, "data": map[string]any{"reaction_id": fmt.Sprintf("r%d", n)}})
		case strings.Contains(p, "/reactions/"):
			reply(map[string]any{"code": 0})
		case r.Method == "GET" && strings.HasPrefix(p, "/open-apis/im/v1/messages/"):
			id := strings.TrimPrefix(p, "/open-apis/im/v1/messages/")
			f.mu.Lock()
			items := f.read[id]
			f.mu.Unlock()
			reply(map[string]any{"code": 0, "data": map[string]any{"items": items}})
		case r.Method == "PATCH":
			reply(map[string]any{"code": 0})
		case r.Method == "POST" && strings.HasPrefix(p, "/open-apis/im/v1/messages"):
			reply(map[string]any{"code": 0, "data": map[string]any{"message_id": fmt.Sprintf("om_bot%d", n)}})
		default:
			reply(map[string]any{"code": 404, "msg": "not faked: " + p})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// find waits for a call whose text contains every one of want.
func (f *fakeOpenAPI) find(t *testing.T, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, c := range f.calls {
			ok := true
			for _, w := range want {
				if !strings.Contains(c, w) {
					ok = false
					break
				}
			}
			if ok {
				f.mu.Unlock()
				return c
			}
		}
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("no call with %q; calls:\n%s", want, strings.Join(f.calls, "\n"))
	return ""
}

func (f *fakeOpenAPI) count(want string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, want) {
			n++
		}
	}
	return n
}

// botNode is the assistant's machine: it records sends and answers items with what the test sets.
type botNode struct {
	mu    sync.Mutex
	sends []map[string]any
	items []proto.Item
	last  int
}

func (n *botNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var p map[string]any
	_ = json.Unmarshal(params, &p)
	switch method {
	case "session.send":
		n.sends = append(n.sends, p)
		return map[string]any{}, nil
	case "session.items":
		if after, ok := p["after_seq"].(float64); ok {
			var out []proto.Item
			for _, it := range n.items {
				if it.Seq > int(after) {
					out = append(out, it)
				}
			}
			return map[string]any{"items": out, "to_seq": n.last}, nil
		}
		return map[string]any{"items": []proto.Item{}, "to_seq": n.last}, nil
	}
	return map[string]any{}, nil
}

func (n *botNode) sent(t *testing.T, i int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		if len(n.sends) > i {
			s := n.sends[i]
			n.mu.Unlock()
			return s
		}
		n.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("send %d never reached the node", i)
	return nil
}

type botWorld struct {
	h    *Hub
	api  *fakeOpenAPI
	node *botNode
	m    *Machine
	mux  http.Handler
	ctx  context.Context
}

func newBotWorld(t *testing.T) *botWorld {
	t.Helper()
	h := newTestHub(t)
	st := newBotStore()
	h.store = st
	_ = st.PutSetting(context.Background(), "assistant", assistant.Settings{MachineID: "box", Main: "main-1"})
	m := newMachine(h, MachineConfig{ID: "box", Name: "box", Kind: "local"})
	h.byID["box"], h.machines = m, []*Machine{m}
	node := &botNode{last: 10}
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = node.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hubIn.Close(); nodeIn.Close() })
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.info.Home = "/home/u"
	m.sessions = []proto.Session{{SessionID: "main-1", MachineID: "box", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}}
	m.mu.Unlock()
	connectAssistant(t, h) // the main conversation is its to say, and what a message to it runs on
	api := newFakeOpenAPI(t)
	h.startBots(ctx)
	mux := http.NewServeMux()
	h.botRoutes(mux)
	h.assistantRoutesRelayed(mux) // the channels' routes are the assistant's
	w := &botWorld{h: h, api: api, node: node, m: m, mux: mux, ctx: ctx}
	if code, out := w.do(t, "PUT", "/api/v1/bots/feishu",
		fmt.Sprintf(`{"enabled":true,"app_id":"cli_test","app_secret":"s3cret","domain":%q,"features":{"main":true,"cards":true}}`, api.srv.URL)); code != 200 {
		t.Fatalf("enable: %d %v", code, out)
	}
	return w
}

func (w *botWorld) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	w.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// message is an im.message.receive_v1 event from a user in the bot's private chat.
func botMessageEvent(id, from, msgType, content string, extra map[string]any) []byte {
	msg := map[string]any{"message_id": id, "chat_id": "oc_p2p", "chat_type": "p2p", "message_type": msgType, "content": content,
		"create_time": fmt.Sprint(time.Now().UnixMilli())}
	for k, v := range extra {
		msg[k] = v
	}
	b, _ := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"event_id": "ev_" + id, "event_type": "im.message.receive_v1"},
		"event": map[string]any{"sender": map[string]any{"sender_id": map[string]any{"open_id": from}, "sender_type": "user"}, "message": msg}})
	return b
}

func textContent(s string) string {
	b, _ := json.Marshal(map[string]string{"text": s})
	return string(b)
}

func (w *botWorld) pair(t *testing.T) {
	t.Helper()
	code, out := w.do(t, "POST", "/api/v1/bots/feishu/pair", "")
	pairing, _ := out["pairing"].(map[string]any)
	if code != 200 || pairing == nil {
		t.Fatalf("pair: %d %v", code, out)
	}
	if _, err := w.h.botEvent(w.ctx, botMessageEvent("om_pair", "ou_me", "text", textContent(pairing["code"].(string)), nil)); err != nil {
		t.Fatal(err)
	}
	w.api.find(t, "已配对")
	if _, out := w.do(t, "GET", "/api/v1/bots/feishu", ""); out["owner"] == nil {
		t.Fatalf("not paired: %v", out)
	}
}

// Pairing makes the sender of the code the owner; nobody else is answered, and the code works once.
func TestBotPairsWithTheCodeAndAnswersOnlyItsOwner(t *testing.T) {
	w := newBotWorld(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_early", "ou_me", "text", textContent("你好"), nil))
	w.api.find(t, "/messages", "还没配对")
	w.pair(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_other", "ou_stranger", "text", textContent("帮我删库"), nil))
	w.api.find(t, "只为它的主人服务")
	if n := len(w.node.sends); n != 0 {
		t.Fatalf("a stranger's message reached the assistant: %v", w.node.sends)
	}
	// A message in a group is never taken, even from the owner.
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_group", "ou_me", "text", textContent("hi"), map[string]any{"chat_type": "group"}))
	time.Sleep(200 * time.Millisecond)
	if n := len(w.node.sends); n != 0 {
		t.Fatalf("a group message reached the assistant: %v", w.node.sends)
	}
	// The secret is sealed, never returned.
	_, out := w.do(t, "GET", "/api/v1/bots/feishu", "")
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the secret leaked: %s", raw)
	}
	var saved FeishuBotSettings
	_, _ = w.h.store.Setting(context.Background(), botSettingsKey, &saved)
	if saved.SecretEnc == "" || strings.Contains(saved.SecretEnc, "s3cret") {
		t.Fatalf("secret not sealed: %+v", saved)
	}
}

// A message goes into the main conversation once (a redelivered event is not sent again), and the turn's answer
// comes back as a card replying to it.
func TestBotMessageGoesToTheMainConversationAndTheAnswerComesBack(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	ev := botMessageEvent("om_q1", "ou_me", "text", textContent("今天有什么会？"), nil)
	_, _ = w.h.botEvent(w.ctx, ev)
	_, _ = w.h.botEvent(w.ctx, ev) // Feishu delivers again
	sent := w.node.sent(t, 0)
	text, _ := sent["text"].(string)
	if sent["session_id"] != "main-1" || !strings.Contains(text, "今天有什么会？") || !strings.Contains(text, `title="飞书机器人"`) {
		t.Fatalf("send: %v", sent)
	}
	if sent["client_msg_id"] != "feishu-om_q1" {
		t.Fatalf("client_msg_id = %v", sent["client_msg_id"])
	}
	w.api.find(t, "/messages/om_q1/reactions", "OnIt")

	// The turn: Persona works, then answers.
	w.node.mu.Lock()
	w.node.items = []proto.Item{
		{Seq: 11, Role: "user", Kind: proto.KindText, Text: text},
		{Seq: 12, Role: "assistant", Kind: proto.KindText, Text: "我查一下。"},
		{Seq: 12, Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ol GET /calendar/week"}`)},
		{Seq: 13, Role: "user", Kind: proto.KindToolResult},
		{Seq: 14, Role: "assistant", Kind: proto.KindText, Text: "今天 **两个会**：10:00 周会，15:00 评审。"},
	}
	w.node.last = 14
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	answer := w.api.find(t, "两个会")
	if strings.Contains(answer, "我查一下") {
		t.Fatalf("the answer has the words before the tool call: %s", answer)
	}
	if !strings.Contains(answer, "/messages/om_q1/reply") && !strings.Contains(answer, "PATCH") {
		t.Fatalf("the answer does not reply to the message: %s", answer)
	}
	w.api.find(t, "/messages/om_q1/reactions", "DONE")
	if n := len(w.node.sends); n != 1 {
		t.Fatalf("%d sends, want 1", n)
	}
	// The assistant reports the turn answered (its snapshot): the phone's 完成 push leaves it alone.
	eventually(t, 5*time.Second, "the turn answered in Feishu must not also push 完成 to the phone", func() bool {
		return w.h.channelAnswered("main-1")
	})
}

// A question Persona asks shows as a card in the chat; a button there answers it like the app would, and the card says
// what was answered.
func TestBotQuestionCardAnswersTheInteraction(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	ix := proto.Interaction{ID: "ix_q", MachineID: "box", SessionID: "main-1", SessionTitle: "Persona", Kind: proto.IxQuestion,
		ToolName: "ask_user", Questions: []proto.Question{{Question: "发给谁？", Options: []proto.QuestionOption{{Label: "张三"}, {Label: "李四"}}}},
		ExpiresAt: time.Now().Add(time.Minute)}
	got := make(chan proto.Resolution, 1)
	go func() {
		r, _ := w.h.waitInteraction(context.Background(), ix)
		got <- r
	}()
	w.api.find(t, "发给谁？", "ix_q")
	press := func(from string) map[string]any {
		b, _ := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"event_type": "card.action.trigger"},
			"event": map[string]any{"operator": map[string]any{"open_id": from},
				"action":  map[string]any{"value": map[string]any{"a": "ix", "id": "ix_q", "q": "0", "v": "李四"}, "tag": "button"},
				"context": map[string]any{"open_message_id": "om_card"}}})
		res, err := w.h.botEvent(w.ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if out := press("ou_stranger"); !strings.Contains(fmt.Sprint(out), "只有机器人的主人") {
		t.Fatalf("a stranger's press: %v", out)
	}
	out := press("ou_me")
	if !strings.Contains(fmt.Sprint(out["toast"]), "李四") || out["card"] == nil {
		t.Fatalf("press: %v", out)
	}
	select {
	case r := <-got:
		if r.Answers["发给谁？"] != "李四" {
			t.Fatalf("answers %v", r.Answers)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the question was not answered")
	}
	if out := press("ou_me"); !strings.Contains(fmt.Sprint(out), "已经处理过了") {
		t.Fatalf("a second press: %v", out)
	}
}

// Agents are off by default: the command says so, and nothing is listed.
func TestBotAgentsNeedTheirSwitch(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_a", "ou_me", "text", textContent("/agents"), nil))
	w.api.find(t, "/messages/om_a/reply", "没有打开")
	if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"agents":true}}`); code != 200 {
		t.Fatal(code)
	}
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_b", "ou_me", "text", textContent("/agents"), nil))
	w.api.find(t, "/messages/om_b/reply", "Agent")
	// /status says where messages go.
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_c", "ou_me", "text", textContent("/status"), nil))
	w.api.find(t, "/messages/om_c/reply", "主对话")
}

func TestTurnAnswerIsTheLastWords(t *testing.T) {
	items := []proto.Item{
		{Role: "user", Kind: proto.KindText, Text: "问"},
		{Role: "assistant", Kind: proto.KindText, Text: "先看看"},
		{Role: "assistant", Kind: proto.KindToolUse},
		{Role: "user", Kind: proto.KindToolResult},
		{Role: "assistant", Kind: proto.KindText, Text: "第一段"},
		{Role: "assistant", Kind: proto.KindText, Text: "第二段"},
	}
	got := channel.Answer(items)
	if len(got) != 2 || got[0].Text != "第一段" || got[1].Text != "第二段" {
		t.Fatalf("got %+v", got)
	}
	// Ending on a tool call (stopped): its last text still answers.
	if got := channel.Answer(items[:3]); len(got) != 1 || got[0].Text != "先看看" {
		t.Fatalf("ended on a tool: %+v", got)
	}
	if got := channel.Answer(nil); len(got) != 0 {
		t.Fatalf("nothing: %+v", got)
	}
}

// A message sent while a turn is going on waits behind it: the end of that turn is not its answer.
func TestBotAnswerWaitsForTheTurnOfItsOwnMessage(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_q2", "ou_me", "text", textContent("第二个问题"), nil))
	sent := w.node.sent(t, 0)
	text, _ := sent["text"].(string)
	// The turn in progress ends; the new message has not been taken yet.
	w.node.mu.Lock()
	w.node.items = []proto.Item{{Seq: 11, Role: "assistant", Kind: proto.KindText, Text: "上一个问题的回答"}}
	w.node.last = 11
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	time.Sleep(1500 * time.Millisecond)
	if n := w.api.count("上一个问题的回答"); n != 0 {
		t.Fatal("the end of the earlier turn was posted as this message's answer")
	}
	// Its own turn.
	w.node.mu.Lock()
	w.node.items = append(w.node.items,
		proto.Item{Seq: 12, Role: "user", Kind: proto.KindText, Text: text},
		proto.Item{Seq: 13, Role: "assistant", Kind: proto.KindText, Text: "第二个问题的回答"})
	w.node.last = 13
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	w.api.find(t, "第二个问题的回答")
}

// Wrong codes end the pairing after a few tries; a new code pairs.
func TestBotPairingGivesUpOnGuesses(t *testing.T) {
	w := newBotWorld(t)
	if code, _ := w.do(t, "POST", "/api/v1/bots/feishu/pair", ""); code != 200 {
		t.Fatal(code)
	}
	for i := 0; i < botPairTries; i++ {
		_, _ = w.h.botEvent(w.ctx, botMessageEvent(fmt.Sprintf("om_g%d", i), "ou_x", "text", textContent(fmt.Sprintf("%06d", i)), nil))
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, out := w.do(t, "GET", "/api/v1/bots/feishu", "")
		if out["pairing"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pairing survived %d wrong codes: %v", botPairTries, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.pair(t) // "/new <text>" taking the rest is the assistant's (TestBotCommandsPassTheRestOn there)
}

// The watch loop and a turn's end check at the same time: the answer is posted once.
func TestBotAnswerIsPostedOnceWhenChecksRace(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_r", "ou_me", "text", textContent("并发的问题"), nil))
	text, _ := w.node.sent(t, 0)["text"].(string)
	w.node.mu.Lock()
	w.node.items = []proto.Item{
		{Seq: 11, Role: "user", Kind: proto.KindText, Text: text},
		{Seq: 12, Role: "assistant", Kind: proto.KindText, Text: "只答一次"},
	}
	w.node.last = 12
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	// The turn's end told the assistant again and again at once (each runs on its own there).
	for i := 0; i < 8; i++ {
		w.h.asst().TurnEnded(context.Background(), assistantwire.SessionRef{MachineID: "box", SessionID: "main-1"})
	}
	w.api.find(t, "只答一次")
	time.Sleep(1500 * time.Millisecond)
	if n := w.api.count("只答一次"); n != 1 {
		t.Fatalf("the answer was posted %d times", n)
	}
}

// When the bot sends to a side conversation whose launcher is deleted, it falls back to the assistant's launcher (#2007).
func TestBotSideConversationLauncherFallback(t *testing.T) {
	w := newBotWorld(t)
	ctx := context.Background()
	if err := w.h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Provider: "launcher.asst-launcher"}); err != nil {
		t.Fatal(err)
	}
	w.m.mu.Lock()
	w.m.sessions = append(w.m.sessions, proto.Session{
		SessionID: "side-1",
		MachineID: "box",
		Cwd:       "/home/u/.jarvis/assistant",
		Launcher:  "deleted-launcher",
		Model:     "deleted-model",
		Assistant: true,
		Status:    proto.StatusIdle,
	})
	w.m.mu.Unlock()
	w.pair(t)
	if code, out := w.do(t, "PUT", "/api/v1/channels/feishu", `{"target":{"kind":"conversation","machine_id":"box","session_id":"side-1"}}`); code != 200 {
		t.Fatalf("set target: %d %v", code, out)
	}

	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_side", "ou_me", "text", textContent("发给旁支"), nil))
	sent := w.node.sent(t, 0)
	if sent["session_id"] != "side-1" {
		t.Fatalf("session_id %v, want side-1", sent["session_id"])
	}
	if sent["launcher"] != "asst-launcher" {
		t.Fatalf("launcher %v, want asst-launcher", sent["launcher"])
	}
	if sent["model"] != nil && sent["model"] != "" {
		t.Fatalf("model %v, want empty/dropped", sent["model"])
	}
}

// stopAssistant stops h's assistant and waits until the hub sees it gone.
func stopAssistant(t *testing.T, h *Hub) {
	t.Helper()
	a := assistantOf(t, h)
	a.stop()
	<-a.done
	eventually(t, 5*time.Second, "the assistant disconnected", func() bool { return h.busConnOf(assistantwire.AssistantID) == nil })
}

// While the assistant is not connected the bot still takes the owner's messages: each is answered that the assistant
// can't be reached, nothing goes to the conversation; a press on a card says so too. Back, it takes messages again.
func TestBotSaysTheAssistantIsAwayAndRecovers(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	shortInboundWait(t)
	stopAssistant(t, w.h)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_away", "ou_me", "text", textContent("在吗？"), nil))
	w.api.find(t, "/messages/om_away/reply", "助手暂时连不上")
	b, _ := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"event_type": "card.action.trigger"},
		"event": map[string]any{"operator": map[string]any{"open_id": "ou_me"},
			"action": map[string]any{"value": map[string]any{"a": "ix", "id": "ix_x"}, "tag": "button"}}})
	res, _ := w.h.botEvent(w.ctx, b)
	if raw, _ := json.Marshal(res); !strings.Contains(string(raw), "助手暂时连不上") {
		t.Fatalf("a press while away: %s", raw)
	}
	if code, _ := w.do(t, "GET", "/api/v1/bots/feishu", ""); code != 200 {
		t.Fatalf("the bot's own settings page stays the hub's: %d", code)
	}
	// Binding carries credentials: the hub's route, answered without the assistant.
	if code, out := w.do(t, "POST", "/api/v1/channels/feishu/bind", ""); code != 200 || out["step"] == nil && out["done"] == nil {
		t.Fatalf("bind while the assistant is away: %d %v", code, out)
	}
	if code, _ := w.do(t, "GET", "/api/v1/channels", ""); code != 503 {
		t.Fatalf("the channels' page is the assistant's: %d, want 503", code)
	}
	w.node.mu.Lock()
	n := len(w.node.sends)
	w.node.mu.Unlock()
	if n != 0 {
		t.Fatalf("a message reached the conversation while the assistant was away: %v", w.node.sends)
	}
	connectAssistant(t, w.h)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_back", "ou_me", "text", textContent("回来了吗？"), nil))
	if sent := w.node.sent(t, 0); sent["client_msg_id"] != "feishu-om_back" {
		t.Fatalf("send %v", sent)
	}
}

// shortInboundWait has an owner's message wait for the assistant only briefly (a test of it being away).
func shortInboundWait(t *testing.T) {
	old := inboundWait
	inboundWait = 300 * time.Millisecond
	t.Cleanup(func() { inboundWait = old })
}

// A message that comes while the assistant is away for a moment (a hub start, an assistant restart) waits for it and
// is answered as usual: nothing the bot took is dropped, and nobody is told the assistant is away.
func TestBotMessageWaitsForTheAssistant(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	stopAssistant(t, w.h)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_wait", "ou_me", "text", textContent("等一下再答"), nil))
	time.Sleep(300 * time.Millisecond)
	connectAssistant(t, w.h)
	if sent := w.node.sent(t, 0); sent["client_msg_id"] != "feishu-om_wait" {
		t.Fatalf("send %v", sent)
	}
	if n := w.api.count("助手暂时连不上"); n != 0 {
		t.Fatalf("the owner was told the assistant is away (%d)", n)
	}
}

// Answering a card from a channel takes the owner's action the hub handed over with that press or message: none,
// another card's, or one used already is refused, so the assistant can't approve anything by itself.
func TestChannelAnswersNeedTheOwnersAction(t *testing.T) {
	h := newTestHub(t)
	h.store = newBotStore()
	ix := proto.Interaction{ID: "ix_plan", MachineID: "box", SessionID: "s1", Kind: proto.IxPlan, ExpiresAt: time.Now().Add(time.Minute)}
	got := make(chan proto.Resolution, 1)
	go func() {
		r, _ := h.waitInteraction(context.Background(), ix)
		got <- r
	}()
	eventually(t, 5*time.Second, "the plan waits", func() bool { return len(h.pendingInteractions()) == 1 })
	resolve := func(action string) error {
		p, _ := json.Marshal(map[string]any{"interaction_id": "ix_plan", "action": action,
			"resolution": map[string]any{"approve": true, "mode": "bypassPermissions", "answered_by": "assistant"}})
		_, err := h.resolveFromChannel(context.Background(), p)
		return err
	}
	if err := resolve(""); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("no owner action: %v", err)
	}
	if err := resolve(h.issueOwnerAction(channel.Feishu, "ix_other")); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("another card's press: %v", err)
	}
	a := h.issueOwnerAction(channel.Feishu, "ix_plan")
	if err := resolve(a); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.Approve == nil || !*r.Approve || r.AnsweredBy != "" {
			t.Fatalf("resolution %+v: the owner's own answer", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not answered")
	}
	if err := resolve(a); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("an owner action used twice: %v", err)
	}
}

// Two messages sent together (a backlog handed over at once) are one run: the turn that answers the first ends
// before the second is in the conversation, and its words are not posted as the answer; the second's turn is.
func TestBotFoldedMessagesWaitForTheNewest(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_f1", "ou_me", "text", textContent("第一条"), nil))
	first, _ := w.node.sent(t, 0)["text"].(string)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_f2", "ou_me", "text", textContent("第二条"), nil))
	second, _ := w.node.sent(t, 1)["text"].(string)
	w.node.mu.Lock()
	w.node.items = []proto.Item{{Seq: 11, Role: "user", Kind: proto.KindText, Text: first, ClientMsgID: "feishu-om_f1"},
		{Seq: 12, Role: "assistant", Kind: proto.KindText, Text: "只答了第一条"}}
	w.node.last = 12
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	time.Sleep(1500 * time.Millisecond)
	if n := w.api.count("只答了第一条"); n != 0 {
		t.Fatal("the first message's turn was posted as the answer to both")
	}
	w.node.mu.Lock()
	w.node.items = append(w.node.items, proto.Item{Seq: 13, Role: "user", Kind: proto.KindText, Text: second, ClientMsgID: "feishu-om_f2"},
		proto.Item{Seq: 14, Role: "assistant", Kind: proto.KindText, Text: "两条都答了"})
	w.node.last = 14
	w.node.mu.Unlock()
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusWorking}})
	w.m.setSessions([]proto.Session{{SessionID: "main-1", Cwd: "/home/u/.jarvis/assistant", Status: proto.StatusDone}})
	w.api.find(t, "两条都答了")
	w.api.find(t, "/messages/om_f1/reactions", "DONE")
	w.api.find(t, "/messages/om_f2/reactions", "DONE")
}
