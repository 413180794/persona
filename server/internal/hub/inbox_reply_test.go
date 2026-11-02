package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// replyHub is a hub with one open Feishu inbox item and a fake lark-connector that records the clientId of each send.
func replyHub(t *testing.T) (*Hub, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var sends []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/agent/v1/conversations/feed-1/messages" {
			w.WriteHeader(404)
			return
		}
		var body struct {
			ClientID string `json:"clientId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		time.Sleep(20 * time.Millisecond) // long enough for a concurrent reply to arrive meanwhile
		mu.Lock()
		sends = append(sends, body.ClientID)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"messageId":"7000000000000000003"}`)
	}))
	t.Cleanup(srv.Close)
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	h.inbox.init()
	h.inbox.items["lark:feed-1:100"] = &InboxItem{ID: "lark:feed-1:100", Source: "lark", UpdatedAt: time.Now(),
		Lark: &InboxLark{AccountID: 1, FeedID: "feed-1", StartPosition: "100"}}
	return h, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sends...)
	}
}

func TestInboxReplySendsOnce(t *testing.T) {
	h, sends := replyHub(t)
	ctx := context.Background()
	for range 2 {
		it, err := h.replyInbox(ctx, "lark:feed-1:100", "好的，我看一下")
		if err != nil || it.DoneBy != "replied" {
			t.Fatalf("reply: %+v %v", it, err)
		}
	}
	got := sends()
	if len(got) != 1 || got[0] != lark.NamedClientID("lark:feed-1:100\n好的，我看一下") {
		t.Fatalf("sends = %q, want one with the item's named clientId", got)
	}
	// Different text is a second message the user means to send.
	if _, err := h.replyInbox(ctx, "lark:feed-1:100", "补充一句"); err != nil {
		t.Fatal(err)
	}
	if got := sends(); len(got) != 2 || got[1] == got[0] {
		t.Fatalf("sends = %q", got)
	}
}

func TestInboxReplyConcurrentSendsOnce(t *testing.T) {
	h, sends := replyHub(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.replyInbox(context.Background(), "lark:feed-1:100", "收到")
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	if got := sends(); len(got) != 1 {
		t.Fatalf("sends = %q, want one", got)
	}
}

func TestInboxReplyAfterRestartReusesClientID(t *testing.T) {
	// The in-memory record is gone after a restart; the retry still carries the same clientId, which lark-connector
	// answers with the message it already sent.
	h, sends := replyHub(t)
	ctx := context.Background()
	if _, err := h.replyInbox(ctx, "lark:feed-1:100", "收到"); err != nil {
		t.Fatal(err)
	}
	h.inbox.replies.Delete("lark:feed-1:100")
	if _, err := h.replyInbox(ctx, "lark:feed-1:100", "收到"); err != nil {
		t.Fatal(err)
	}
	if got := sends(); len(got) != 2 || got[0] != got[1] {
		t.Fatalf("sends = %q, want the same clientId twice", got)
	}
}
