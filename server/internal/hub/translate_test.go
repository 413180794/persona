package hub

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
)

func TestTranslateTextsKeepsOrderAndCaches(t *testing.T) {
	h := newTestHub(t)
	model, setReply := mutableModel(t, `{"texts":["你好","明天见"]}`)
	h.triageAI = model
	out, err := h.translateTexts(context.Background(), []string{"Hello", "", "See you tomorrow"}, "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out, "|") != "你好||明天见" {
		t.Fatalf("%q", out)
	}
	// Cached now: a model that would answer wrongly is not asked.
	setReply(`{"texts":["错"]}`)
	out, err = h.translateTexts(context.Background(), []string{"See you tomorrow"}, "zh-Hans")
	if err != nil || out[0] != "明天见" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := h.translateTexts(context.Background(), []string{"x"}, "klingon"); err == nil {
		t.Fatal("unknown language accepted")
	}
	// A count that does not line up is an error, not a shifted list.
	h.triageAI = fakeModel(t, `{"texts":["一"]}`)
	if _, err := h.translateTexts(context.Background(), []string{"one", "two"}, "zh-Hans"); err == nil {
		t.Fatal("misaligned answer accepted")
	}
}

// JARVIS_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run TranslateLive -v ./internal/hub
func TestTranslateLive(t *testing.T) {
	raw := os.Getenv("JARVIS_LIVE_AI")
	if raw == "" {
		t.Skip("JARVIS_LIVE_AI not set")
	}
	var cfg ai.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	h := newTestHub(t)
	h.triageAI = ai.New(cfg)
	in := []string{"Can we move the review to Tuesday afternoon? I'm travelling on Thursday.",
		"Run `go test ./...` before you push, and check https://example.com/build/42."}
	out, err := h.translateTexts(context.Background(), in, "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%q", out)
	if !strings.Contains(out[0], "周二") || !strings.Contains(out[1], "go test ./...") || !strings.Contains(out[1], "https://example.com/build/42") {
		t.Fatalf("translation lost something: %q", out)
	}
}
