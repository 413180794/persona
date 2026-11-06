package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
)

// Key construction: deterministic for replies (parent + purpose + content), random for unanchored sends, always within
// the gateway's 50-char cap.
func TestFeishuUUIDKeys(t *testing.T) {
	if got := feishuReplyUUID("om_1", "c", "card-a"); got != feishuReplyUUID("om_1", "c", "card-a") {
		t.Fatalf("reply uuid is not deterministic: %q", got)
	}
	distinct := []string{
		feishuReplyUUID("om_1", "c", "a"),
		feishuReplyUUID("om_2", "c", "a"),
		feishuReplyUUID("om_1", "f", "a"),
		feishuReplyUUID("om_1", "c", "b"),
		feishuSendUUID(),
		feishuSendUUID(),
	}
	seen := map[string]bool{}
	for _, u := range distinct {
		if len(u) > feishuMaxUUID || seen[u] {
			t.Fatalf("bad or colliding uuid %q (len %d)", u, len(u))
		}
		seen[u] = true
	}
	longID := strings.Repeat("om_long", 20)
	long := feishuReplyUUID(longID, "c", "x")
	if len(long) != feishuMaxUUID || !strings.HasPrefix(long, "r:"+longID[:feishuMaxUUID-2-2-12]) {
		t.Fatalf("long parent uuid = %q (len %d)", long, len(long))
	}
}

type fakeUUIDAPI struct {
	srv         *httptest.Server
	mu          sync.Mutex
	posts       []map[string]any
	rejectCardN int
}

func newFakeUUIDAPI(t *testing.T) *fakeUUIDAPI {
	f := &fakeUUIDAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal") {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "tok", "expire": 7200})
			return
		}
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		f.mu.Lock()
		reject := in["msg_type"] == feishubot.TypeInteractive && strings.HasSuffix(r.URL.Path, "/reply") && f.rejectCardN > 0
		if reject {
			f.rejectCardN--
		}
		f.posts = append(f.posts, in)
		f.mu.Unlock()
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 230099, "msg": "invalid card"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"message_id": "om_sent"}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUUIDAPI) uuids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.posts))
	for _, p := range f.posts {
		if u, _ := p["uuid"].(string); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// Every send/reply the bot's chat path makes carries a stable key: the reply card is deterministic per content, two
// cards of one turn get two keys, the rich-text fallback gets its own key, and unanchored sends never repeat a key.
func TestBotPostJSONUUIDs(t *testing.T) {
	cardA := feishubot.NewCard("t", "green", "a").Markdown("card-a").JSON()
	cardB := feishubot.NewCard("t", "green", "b").Markdown("card-b").JSON()

	t.Run("reply cards are deterministic and content-distinct", func(t *testing.T) {
		f := newFakeUUIDAPI(t)
		h := newTestManager(t)
		api := feishubot.New("cli", "sec", f.srv.URL)
		ctx := context.Background()
		for _, card := range []string{cardA, cardA, cardB} { // a replayed, then the second card of the turn
			if _, err := h.botPostJSON(ctx, api, "chat", "om_p", card, "fb"); err != nil {
				t.Fatal(err)
			}
		}
		got := f.uuids()
		wantA := feishuReplyUUID("om_p", "c", cardA)
		wantB := feishuReplyUUID("om_p", "c", cardB)
		if len(got) != 3 || got[0] != wantA || got[1] != wantA || got[2] != wantB || got[0] == got[2] {
			t.Fatalf("uuids %v want [%s %s %s]", got, wantA, wantA, wantB)
		}
	})

	t.Run("rejected card falls back with its own stable key", func(t *testing.T) {
		f := newFakeUUIDAPI(t)
		f.rejectCardN = 1 << 30 // every card reply is rejected
		h := newTestManager(t)
		api := feishubot.New("cli", "sec", f.srv.URL)
		for range [2]int{} {
			if _, err := h.botPostJSON(context.Background(), api, "chat", "om_p", cardA, "fallback text"); err != nil {
				t.Fatal(err)
			}
		}
		got := f.uuids()
		cardKey := feishuReplyUUID("om_p", "c", cardA)
		fbKey := feishuReplyUUID("om_p", "f", "fallback text")
		if len(got) != 4 || got[0] != cardKey || got[1] != fbKey || got[2] != cardKey || got[3] != fbKey {
			t.Fatalf("uuids %v, want card/fallback keys alternating", got)
		}
	})

	t.Run("empty fallback resolves to the i18n default before the key is made", func(t *testing.T) {
		f := newFakeUUIDAPI(t)
		f.rejectCardN = 1
		h := newTestManager(t)
		api := feishubot.New("cli", "sec", f.srv.URL)
		if _, err := h.botPostJSON(context.Background(), api, "chat", "om_p", cardA, ""); err != nil {
			t.Fatal(err)
		}
		resolved := i18n.T(h.botLang(context.Background()), "hub-bots_chat.card_refused")
		got := f.uuids()
		want := feishuReplyUUID("om_p", "f", resolved)
		if len(got) != 2 || got[1] != want {
			t.Fatalf("uuids %v, last key must hash the resolved fallback %q", got, want)
		}
	})

	t.Run("send into the chat uses a fresh random key each time", func(t *testing.T) {
		f := newFakeUUIDAPI(t)
		h := newTestManager(t)
		api := feishubot.New("cli", "sec", f.srv.URL)
		for range [2]int{} {
			if _, err := h.botPostJSON(context.Background(), api, "chat", "", cardA, "fb"); err != nil {
				t.Fatal(err)
			}
		}
		got := f.uuids()
		if len(got) != 2 || got[0] == got[1] || !strings.HasPrefix(got[0], "s:") {
			t.Fatalf("uuids %v", got)
		}
	})
}

// botNote's reply key is the deterministic note key; two calls with the same message and text keep it.
func TestBotNoteReplyUUID(t *testing.T) {
	f := newFakeUUIDAPI(t)
	h := newTestManager(t)
	api := feishubot.New("cli", "sec", f.srv.URL)
	h.botNote(context.Background(), api, "chat", "om_n", "cannot send")
	h.botNote(context.Background(), api, "chat", "om_n", "cannot send")
	got := f.uuids()
	want := feishuReplyUUID("om_n", "n", "cannot send")
	if len(got) != 2 || got[0] != want || got[1] != want {
		t.Fatalf("uuids %v want %q twice", got, want)
	}
}

var uuidInCall = regexp.MustCompile(`"uuid":"([^"]+)"`)

// The assistant's feishu.text transport: replies keep one key, unanchored texts vary.
func TestChannelFeishuTextUUID(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	const text = "uuid probe answer 4077"
	call := func(replyTo string) {
		params := map[string]any{"text": text}
		if replyTo != "" {
			params["reply_to"] = replyTo
		}
		w.feishu(t, assistantwire.FeishuText, params)
	}
	call("om_4077")
	call("om_4077")
	call("")
	call("")

	var replyKeys, sendKeys []string
	w.api.mu.Lock()
	for _, c := range w.api.calls {
		if !strings.Contains(c, text) {
			continue
		}
		m := uuidInCall.FindStringSubmatch(c)
		if m == nil {
			continue
		}
		switch {
		case strings.Contains(c, "/messages/om_4077/reply"):
			replyKeys = append(replyKeys, m[1])
		case strings.Contains(c, "POST /open-apis/im/v1/messages "):
			sendKeys = append(sendKeys, m[1])
		}
	}
	w.api.mu.Unlock()
	want := feishuReplyUUID("om_4077", "t", text)
	if len(replyKeys) != 2 || replyKeys[0] != want || replyKeys[1] != want {
		t.Fatalf("reply keys %v want %q twice", replyKeys, want)
	}
	if len(sendKeys) != 2 || sendKeys[0] == sendKeys[1] || !strings.HasPrefix(sendKeys[0], "s:") {
		t.Fatalf("send keys %v want two distinct random keys", sendKeys)
	}
}

// A text reply whose parent was withdrawn (230011) is rerouted into the chat with a fresh one-shot key; 230110 takes
// the same helper branch and is covered for both codes by TestBotPostDetoursWithdrawnReplyToSend.
func TestChannelFeishuTextDetoursWithdrawnReplyToSend(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.api.rejectReply = 230011
	const text = "gone parent text 4457"
	w.feishu(t, assistantwire.FeishuText, map[string]any{"text": text, "reply_to": "om_gone"})
	w.api.find(t, "/messages/om_gone/reply", text) // the anchored reply was attempted first
	sendCall := w.api.find(t, "POST /open-apis/im/v1/messages ", text)
	if m := uuidInCall.FindStringSubmatch(sendCall); m == nil || !strings.HasPrefix(m[1], "s:") {
		t.Fatalf("detoured send %q key %v, want a fresh one-shot key", sendCall, m)
	}
}
