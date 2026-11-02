package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
)

// An upstream out of quota (docs/model-config.md「上游限流」): the first triage pass sends a few requests at most and
// logs nothing; every item stays untriaged (sorted by rule meanwhile) and 设置 › 模型配置 says why; later passes in
// the pause send nothing. Once the quota is back every item is triaged, each asked once.
func TestInboxTriageWaitsOutAnExhaustedQuota(t *testing.T) {
	ctx := context.Background()
	var sent atomic.Int32
	var back atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		if !back.Load() {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","code":"account_quota_exhausted","message":"Account usage limit reached"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text",
			"text": `{"tier":"fyi","summary":"周五灰度","reason":"群里同步"}`}}})
	}))
	t.Cleanup(srv.Close)

	h, mux := modelTestHub(t)
	h.store = inboxTestStore{h.store.(*modelTestStore)}
	h.inbox.init()
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	h.cfg.Inbox.TriageAI = &ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	const items = 20
	for i := range items {
		id := fmt.Sprintf("lark:g%d:1", i)
		h.inbox.items[id] = &InboxItem{ID: id, Kind: KindGroup, Last: "1", Context: "张三：周五灰度", UpdatedAt: time.Now()}
	}
	pass := func() {
		h.triageInbox(ctx)
		h.inbox.triaging.Lock() // the pass runs in the background: wait for it
		h.inbox.triaging.Unlock()
	}
	triaged := func() (n int) {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		for _, it := range h.inbox.items {
			if it.TriagedAt == it.Last && it.Summary == "周五灰度" {
				n++
			}
		}
		return n
	}

	pass()
	if n := sent.Load(); n < 1 || n > triageConcurrency {
		t.Fatalf("sent %d request(s) in the first pass", n)
	}
	h.inbox.mu.Lock()
	for _, it := range h.inbox.items {
		if it.TriagedAt != "" || it.Tier != TierFYI || it.Reason != "按规则" {
			t.Errorf("item %s: tier %q reason %q triaged %q", it.ID, it.Tier, it.Reason, it.TriagedAt)
		}
	}
	h.inbox.mu.Unlock()
	if strings.Contains(logs.String(), "triage") {
		t.Fatalf("logged:\n%s", logs.String())
	}
	r := modelRequest(mux, "GET", "inbox.triage", "")
	var detail struct {
		Purpose ModelPurpose `json:"purpose"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &detail); err != nil || detail.Purpose.Limit == nil ||
		detail.Purpose.Limit.Reason != ai.ReasonQuota || !strings.HasPrefix(detail.Purpose.Status, "额度用完，") {
		t.Fatalf("purpose = %s (%v)", r.Body.String(), err)
	}
	first := sent.Load()
	pass()
	if sent.Load() != first {
		t.Fatalf("a pass during the pause sent %d request(s)", sent.Load()-first)
	}

	back.Store(true)
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1
	for range 3 {
		if pass(); triaged() == items {
			break
		}
	}
	if n := triaged(); n != items || sent.Load()-first != items {
		t.Fatalf("%d of %d triaged with %d request(s) after the pause", n, items, sent.Load()-first)
	}
	r = modelRequest(mux, "GET", "inbox.triage", "")
	if strings.Contains(r.Body.String(), `"limit"`) {
		t.Fatalf("still held: %s", r.Body.String())
	}
}

// inboxTestStore is the model configuration's store that also takes inbox items.
type inboxTestStore struct{ *modelTestStore }

func (inboxTestStore) PutInboxItem(context.Context, string, []byte, time.Time, *time.Time) error {
	return nil
}

func (inboxTestStore) DeleteInboxItems(context.Context, []string) error { return nil }

// A judge call put off by a held connection leaves the item to be judged later and says nothing in the log.
func TestInboxJudgePutOffQuietly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"service_unavailable","code":"account_concurrency_full","message":"account concurrency is full"}}`))
	}))
	t.Cleanup(srv.Close)
	h := agentHub(t)
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	h.judgeAI = ai.New(ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	for _, id := range []string{"x", "y"} {
		it := actionItem(id, time.Now())
		h.inbox.items[id] = it
		if h.judgeOne(context.Background(), it) || it.JudgedAt != "" || it.Agent != nil {
			t.Fatalf("judged on a put-off call: %+v", it.Agent)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("logged:\n%s", logs.String())
	}
}
