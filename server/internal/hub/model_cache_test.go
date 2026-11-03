package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
)

// mutableModel changes replies without changing the provider configuration, to distinguish cache hits from invalidation.
func mutableModel(t *testing.T, reply string) (*ai.Client, func(string)) {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		v := reply
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": v}}})
	}))
	t.Cleanup(srv.Close)
	return ai.New(ai.Config{BaseURL: srv.URL, APIKey: "test-key", Model: "test-model"}), func(v string) { mu.Lock(); reply = v; mu.Unlock() }
}

func TestTranslationConfigurationInvalidatesCache(t *testing.T) {
	h := newTestHub(t)
	h.triageAI = fakeModel(t, `{"texts":["旧模型"]}`)
	first, err := h.translateTexts(context.Background(), []string{"hello"}, "zh-Hans")
	if err != nil || first[0] != "旧模型" {
		t.Fatalf("%v %v", first, err)
	}
	h.triageAI = fakeModel(t, `{"texts":["新模型"]}`)
	second, err := h.translateTexts(context.Background(), []string{"hello"}, "zh-Hans")
	if err != nil || second[0] != "新模型" {
		t.Fatalf("stale cache: %v %v", second, err)
	}
}
