package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
)

// An upstream out of quota (docs/model-config.md「上游限流」): 设置 › 模型配置 says why while the connection is held,
// and nothing once it is back. (The inbox's triage waiting it out is the manager's: assistant's
// TestInboxTriageWaitsOutAnExhaustedQuota.)
func TestModelPurposeSaysTheQuota(t *testing.T) {
	ctx := context.Background()
	var back atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !back.Load() {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","code":"account_quota_exhausted","message":"Account usage limit reached"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"tier":"fyi"}`}}})
	}))
	t.Cleanup(srv.Close)

	h, mux := modelTestHub(t)
	h.cfg.Inbox.TriageAI = &ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	var out map[string]any
	if err := h.purposeModel("inbox.triage").JSON(ctx, "s", "u", &out); !ai.IsPaused(err) {
		t.Fatalf("out of quota: %v", err)
	}
	r := modelRequest(mux, "GET", "inbox.triage", "")
	var detail struct {
		Purpose ModelPurpose `json:"purpose"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &detail); err != nil || detail.Purpose.Limit == nil ||
		detail.Purpose.Limit.Reason != ai.ReasonQuota || !strings.HasPrefix(detail.Purpose.Status, "额度用完，") {
		t.Fatalf("purpose = %s (%v)", r.Body.String(), err)
	}
	back.Store(true)
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1
	if err := h.purposeModel("inbox.triage").JSON(ctx, "s", "u", &out); err != nil {
		t.Fatalf("back: %v", err)
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
