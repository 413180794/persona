package extmgr

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

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The Feishu bot is the manager's (bots.go, bots_chat.go): its long connection, pairing, who may talk to it, and the
// transport the assistant asks for (app.call feishu.*). What a message does in a conversation — which one, the turn,
// the answer card, the commands — is the assistant's (internal/assistant feishu*.go, channels*.go), in the same
// process: here it is a fake (withAssistant) that records what the bot hands it.

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
	// rejectInteractive, when set, is the business code returned for interactive message sends (card rejection).
	rejectInteractive int
	rejectReply       int // business code for /reply, e.g. 230011 parent withdrawn
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
			code := 0
			f.mu.Lock()
			reject := f.rejectInteractive
			replyGone := f.rejectReply
			f.mu.Unlock()
			switch {
			case strings.HasSuffix(p, "/reply") && replyGone != 0:
				code = replyGone
			case reject != 0 && strings.Contains(string(body), `"msg_type":"interactive"`):
				code = reject
			}
			reply(map[string]any{"code": code, "data": map[string]any{"message_id": fmt.Sprintf("om_bot%d", n)}})
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

type botWorld struct {
	h   *Manager
	api *fakeOpenAPI
	a   *fakeAssistant
	mux http.Handler
	ctx context.Context
}

// newBotWorld is a manager with the Feishu bot on (credentials typed in the app, the fake open platform), and the
// assistant in its process.
func newBotWorld(t *testing.T) *botWorld {
	t.Helper()
	h := newTestManager(t)
	st := newBotStore()
	h.store = st
	a := withAssistant(h)
	api := newFakeOpenAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.startBots(ctx)
	mux := http.NewServeMux()
	h.botRoutes(mux)
	w := &botWorld{h: h, api: api, a: a, mux: mux, ctx: ctx}
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

// feishu is one of the assistant's app.call feishu.* (the transport).
func (w *botWorld) feishu(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, ok, err := w.h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppFeishu, Method: method, Params: raw})
	if !ok || err != nil {
		t.Fatalf("%s: handled %v, %v", method, ok, err)
	}
	res := map[string]any{}
	_ = json.Unmarshal(out, &res)
	return res
}

// handed are the owner's messages the bot handed the assistant (feishu.message), their message ids.
func (w *botWorld) handed() []string {
	var ids []string
	for _, p := range w.a.got(assistantwire.MethodFeishuMessage) {
		var m struct {
			Event struct {
				Message struct {
					ID string `json:"message_id"`
				} `json:"message"`
			} `json:"event"`
		}
		_ = json.Unmarshal(p, &m)
		ids = append(ids, m.Event.Message.ID)
	}
	return ids
}

// told says whether the assistant was told event.
func (a *fakeAssistant) wasTold(event string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.told {
		if e == event {
			return true
		}
	}
	return false
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

// pair pairs ou_me with the code the app shows; the assistant is told (it greets the owner: its card).
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
	eventually(t, 8*time.Second, "the assistant told of the pairing", func() bool { return w.a.wasTold(assistantwire.EventFeishuOwner) })
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
	if got := w.handed(); len(got) != 0 {
		t.Fatalf("a stranger's message reached the assistant: %v", got)
	}
	// A message in a group is never taken, even from the owner.
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_group", "ou_me", "text", textContent("hi"), map[string]any{"chat_type": "group"}))
	time.Sleep(200 * time.Millisecond)
	if got := w.handed(); len(got) != 0 {
		t.Fatalf("a group message reached the assistant: %v", got)
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

// Paired while the assistant is not here: the manager says hello itself.
func TestBotPairedNoteWithoutTheAssistant(t *testing.T) {
	w := newBotWorld(t)
	w.h.opts.AssistantEvent = nil // no assistant in this process to tell
	_, out := w.do(t, "POST", "/api/v1/bots/feishu/pair", "")
	pairing := out["pairing"].(map[string]any)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_pair", "ou_me", "text", textContent(pairing["code"].(string)), nil))
	w.api.find(t, "已配对")
}

// The owner's message goes to the assistant once (a redelivered event is not handed again), as Feishu sent it; the
// answer comes back through the transport: the reaction, the card replying to the message, the reaction's end.
func TestBotMessageGoesToTheAssistantAndTheAnswerComesBack(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	ev := botMessageEvent("om_q1", "ou_me", "text", textContent("今天有什么会？"), nil)
	_, _ = w.h.botEvent(w.ctx, ev)
	_, _ = w.h.botEvent(w.ctx, ev) // Feishu delivers again
	eventually(t, 5*time.Second, "the message handed to the assistant", func() bool { return len(w.handed()) == 1 })
	time.Sleep(200 * time.Millisecond)
	if got := w.handed(); len(got) != 1 || got[0] != "om_q1" {
		t.Fatalf("handed %v", got)
	}
	if p := w.a.got(assistantwire.MethodFeishuMessage)[0]; !strings.Contains(string(p), "今天有什么会？") {
		t.Fatalf("the event as Feishu sent it: %s", p)
	}

	// The assistant's turn, through the transport.
	st := w.feishu(t, assistantwire.FeishuStatusGet, nil)
	if st["ready"] != true || st["owner"] != "ou_me" || st["enabled"] != true {
		t.Fatalf("status %v", st)
	}
	r := w.feishu(t, assistantwire.FeishuReact, map[string]any{"message_id": "om_q1", "emoji": "OnIt"})
	w.api.find(t, "/messages/om_q1/reactions", "OnIt")
	card := `{"schema":"2.0","body":{"elements":[{"tag":"markdown","content":"今天 **两个会**：10:00 周会，15:00 评审。"}]}}`
	if out := w.feishu(t, assistantwire.FeishuPost, map[string]any{"reply_to": "om_q1", "card": card}); out["message_id"] == "" {
		t.Fatalf("post %v", out)
	}
	w.api.find(t, "/messages/om_q1/reply", "两个会")
	w.feishu(t, assistantwire.FeishuUnreact, map[string]any{"message_id": "om_q1", "reaction_id": r["reaction_id"]})
	w.api.find(t, "DELETE", "/messages/om_q1/reactions/")
	if out := w.feishu(t, assistantwire.FeishuUserName, map[string]any{"open_id": "ou_me"}); out["name"] != "示例用户" {
		t.Fatalf("user name %v", out)
	}
}

// A press on a card: only the owner's counts; the assistant gets it with a single-use owner action for that card, and
// answers the card with it (interactions.resolve) once, as the user's own answer; another card's action, none, or
// one used already is refused.
func TestBotCardPressGoesToTheAssistantWithTheOwnersAction(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.a.on(assistantwire.MethodFeishuAction, func(json.RawMessage) (any, error) {
		return map[string]any{"toast": map[string]any{"type": "success", "content": "已选：李四"}}, nil
	})
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
	if out := press("ou_stranger"); !strings.Contains(fmt.Sprint(out), "只有机器人的主人") || len(w.a.got(assistantwire.MethodFeishuAction)) != 0 {
		t.Fatalf("a stranger's press: %v", out)
	}
	if out := press("ou_me"); !strings.Contains(fmt.Sprint(out["toast"]), "李四") {
		t.Fatalf("press: %v", out)
	}
	var a assistantwire.FeishuAction
	_ = json.Unmarshal(w.a.got(assistantwire.MethodFeishuAction)[0], &a)
	if a.Action == "" || !strings.Contains(string(a.Event), "om_card") {
		t.Fatalf("handed %+v", a)
	}
	resolve := func(action string) (map[string]any, error) {
		raw, _ := json.Marshal(map[string]any{"interaction_id": "ix_q", "action": action,
			"resolution": map[string]any{"answers": map[string]string{"发给谁？": "李四"}, "answered_by": "assistant"}})
		out, _, err := w.h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppInteractions,
			Method: assistantwire.InteractionsOwnerAction, Params: raw})
		res := map[string]any{}
		_ = json.Unmarshal(out, &res)
		return res, err
	}
	if _, err := resolve(""); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("no owner action: %v", err)
	}
	// The press's own action is taken once (the assistant answers the card in its queue then, as the owner).
	if _, err := resolve(a.Action); err != nil {
		t.Fatalf("the press's action: %v", err)
	}
	if _, err := resolve(a.Action); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("used twice: %v", err)
	}
	if _, err := resolve(a.Action); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("an owner action used twice: %v", err)
	}
}

// The switches (Agents off by default) are the manager's to keep and say: feishu.status has them for the assistant.
func TestBotFeatureSwitches(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	feats := func() map[string]any {
		f, _ := w.feishu(t, assistantwire.FeishuStatusGet, nil)["features"].(map[string]any)
		return f
	}
	if f := feats(); f["agents"] == true || f["main"] != true {
		t.Fatalf("default features %v", f)
	}
	if code, _ := w.do(t, "PUT", "/api/v1/bots/feishu", `{"features":{"main":true,"cards":true,"agents":true}}`); code != 200 {
		t.Fatal(code)
	}
	if f := feats(); f["agents"] != true {
		t.Fatalf("agents on: %v", f)
	}
	// And the assistant's own switch call (feishu.configure).
	w.feishu(t, assistantwire.FeishuConfigure, map[string]any{"features": map[string]bool{"agents": false}}) // the given ones change
	if f := feats(); f["agents"] == true {
		t.Fatalf("agents off: %v", f)
	}
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

// away has the assistant not take the owner's messages and presses (it is restarting); back undoes it.
func (w *botWorld) away(on bool) {
	f := func(json.RawMessage) (any, error) { return nil, errAssistantOffline }
	if !on {
		f = nil
	}
	w.a.on(assistantwire.MethodFeishuMessage, f)
	w.a.on(assistantwire.MethodFeishuAction, f)
	w.a.on(assistantwire.MethodChannelReceive, f)
}

// While the assistant can't take them the bot still takes the owner's messages: each is answered that the assistant
// can't be reached; a press on a card says so too. Back, it takes messages again.
func TestBotSaysTheAssistantIsAwayAndRecovers(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	shortInboundWait(t)
	w.away(true)
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
		t.Fatalf("the bot's own settings page works: %d", code)
	}
	// Binding carries credentials: the manager's route, answered without the assistant.
	if code, out := w.do(t, "POST", "/api/v1/channels/feishu/bind", ""); code != 200 || out["step"] == nil && out["done"] == nil {
		t.Fatalf("bind while the assistant is away: %d %v", code, out)
	}
	w.away(false)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_back", "ou_me", "text", textContent("回来了吗？"), nil))
	eventually(t, 5*time.Second, "the message handed once it is back", func() bool {
		got := w.handed()
		return len(got) > 0 && got[len(got)-1] == "om_back"
	})
}

// shortInboundWait has an owner's message wait for the assistant only briefly (a test of it being away).
func shortInboundWait(t *testing.T) {
	old := inboundWait
	inboundWait = 300 * time.Millisecond
	t.Cleanup(func() { inboundWait = old })
}

// A message that comes while the assistant is away for a moment (an assistant restart) waits for it and is handed as
// usual: nothing the bot took is dropped, and nobody is told the assistant is away.
func TestBotMessageWaitsForTheAssistant(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.away(true)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_wait", "ou_me", "text", textContent("等一下再答"), nil))
	time.Sleep(300 * time.Millisecond)
	w.away(false)
	eventually(t, 8*time.Second, "the message handed once it is back", func() bool {
		got := w.handed()
		return len(got) > 0 && got[len(got)-1] == "om_wait"
	})
	if n := w.api.count("助手暂时连不上"); n != 0 {
		t.Fatalf("the owner was told the assistant is away (%d)", n)
	}
}

// Answering a card from a channel takes the owner's action the manager handed over with that press or message: none,
// another card's, or one used already is refused, so the assistant can't approve anything by itself (the assistant
// takes it, interactions.owner_action, before it answers the card in its queue).
func TestChannelAnswersNeedTheOwnersAction(t *testing.T) {
	h := newTestManager(t)
	h.store = newBotStore()
	resolve := func(action string) error {
		p, _ := json.Marshal(map[string]any{"interaction_id": "ix_plan", "action": action,
			"resolution": map[string]any{"approve": true, "mode": "bypassPermissions", "answered_by": "assistant"}})
		_, _, err := h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppInteractions,
			Method: assistantwire.InteractionsOwnerAction, Params: p})
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
	if err := resolve(a); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("an owner action used twice: %v", err)
	}
}

// The bot writes in the user's language: the pairing hint to someone not paired yet, the owner-only line.
func TestBotRepliesInTheUsersLanguage(t *testing.T) {
	w := newBotWorld(t)
	w.h.opts.Lang = func() i18n.Lang { return i18n.Ja } // the hub's hello says the user's language
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_early", "ou_me", "text", textContent("こんにちは"), nil))
	w.api.find(t, "/messages", "まだペアリングされていません")
	w.pair(t)
	_, _ = w.h.botEvent(w.ctx, botMessageEvent("om_other", "ou_stranger", "text", textContent("hi"), nil))
	w.api.find(t, "/messages", "このボットは所有者専用です")
}

// The owner's messages are handed to the assistant in the order they came, one after another: one the assistant is
// slow to take does not let the next one overtake it.
func TestBotMessagesGoToTheAssistantInOrder(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	slow := make(chan struct{})
	w.a.on(assistantwire.MethodFeishuMessage, func(p json.RawMessage) (any, error) {
		if strings.Contains(string(p), "om_o0") {
			<-slow
		}
		return map[string]any{}, nil
	})
	for i := range 4 {
		if _, err := w.h.botEvent(w.ctx, botMessageEvent(fmt.Sprintf("om_o%d", i), "ou_me", "text", textContent(fmt.Sprint(i)), nil)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if got := w.handed(); len(got) != 1 || got[0] != "om_o0" {
		t.Fatalf("handed while the first waits: %v", got)
	}
	close(slow)
	eventually(t, 5*time.Second, "all four handed", func() bool { return len(w.handed()) == 4 })
	if got := strings.Join(w.handed(), " "); got != "om_o0 om_o1 om_o2 om_o3" {
		t.Fatalf("handed as %s", got)
	}
}
