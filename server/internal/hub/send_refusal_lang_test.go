package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A node's coded refusal of a send is explained in the request's language and names the machine (docs/assistant/onboarding.md
// 4.6 rule 2).
func TestSendRefusalSaysWhyInTheRequestLanguage(t *testing.T) {
	h := newTestHub(t)
	pipeNode(t, h, "box", func(context.Context) error {
		return &rpc.Error{Code: proto.CodeClaudeMissing, Message: "zsh:1: command not found: claude",
			Data: json.RawMessage(`{"delivered":"none","launcher":"claude"}`)}
	})
	h.byID["box"].setName("Work Laptop")
	ctx := i18n.With(context.Background(), i18n.En)
	status, body, _ := h.deliverOnce(ctx, h.byID["box"], map[string]any{"session": "s1", "text": "hi"})
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if status != 409 || out["code"] != proto.CodeClaudeMissing || out["machine_name"] != "Work Laptop" ||
		out["error"] != "Claude Code isn't installed on Work Laptop yet." || out["action"] != "install_claude" {
		t.Fatalf("refusal: %d %v", status, out)
	}
}

func TestSendToCodexWithActiveWriterReturnsConflict(t *testing.T) {
	h := newTestHub(t)
	h.store = newSentStore()
	pipeNode(t, h, "box", func(context.Context) error {
		return &rpc.Error{Code: "codex_active_writer", Message: "app-server unavailable",
			Data: json.RawMessage(`{"delivered":"none","retryable":false}`)}
	})
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		ctx := i18n.With(context.Background(), lang)
		status, body, outcome := h.deliverOnce(ctx, h.byID["box"], map[string]any{"session_id": "s1", "text": "scratch follow-up"})
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		if status != 409 || outcome != "" || out["code"] != "codex_active_writer" ||
			out["delivered"] != "none" || out["retryable"] != false ||
			out["error"] != i18n.T(lang, "rpc-errors.codex_active_writer") {
			t.Fatalf("%s: status %d, outcome %q, body %v", lang, status, outcome, out)
		}
	}
}
