package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/extension"
)

// A real running extension receives KB events only with a granted KB read scope (or the legacy full grant).
func TestKBEventsReachOnlyAuthorizedExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, permissions string
		allowed           bool
	}{
		{"reader", `{"hub":["kb.read"]}`, true},
		{"writer", `{"hub":["kb.write"]}`, false},
		{"none", `{}`, false},
		{"legacy", `{"hub_api":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := extensionTestHub(t)
			mux := h.routes()
			token := h.cfg.Token
			dir := uiPackage(t)
			path := filepath.Join(dir, extension.ManifestFile)
			raw, _ := os.ReadFile(path)
			var manifest map[string]any
			_ = json.Unmarshal(raw, &manifest)
			var permissions map[string]any
			_ = json.Unmarshal([]byte(tc.permissions), &permissions)
			permissions["launcher_keys"] = true
			manifest["permissions"] = permissions
			manifest["contributes"].(map[string]any)["events"] = []any{map[string]any{"id": "kb", "subscribe": []string{"kb.changed", "kb.forgotten", "agent.finished"}}}
			raw, _ = json.Marshal(manifest)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", token, `{"source":"dir:`+dir+`"}`)
			if code != 200 {
				t.Fatalf("preview %d %v", code, pv)
			}
			code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", token, fmt.Sprintf(`{"preview_id":%q,"permissions_digest":%q}`, pv["preview_id"], pv["permissions_digest"]))
			if code != 200 {
				t.Fatalf("install %d %v", code, out)
			}
			waitLong(t, "KB subscriber running", func() bool { p := h.extensions.get("example-ui"); return p != nil && p.proc != nil && p.proc.Ready() })
			if tc.allowed {
				a := connectAssistant(t, h)
				if err := a.bh.KBEvent(assistantwire.EventKBChanged, assistantwire.KBChanged{Kind: "people", ID: "example-person", What: "metadata"}); err != nil {
					t.Fatal(err)
				}
				if err := a.bh.KBEvent(assistantwire.EventKBForgotten, assistantwire.KBForgottenEvent{Source: "example"}); err != nil {
					t.Fatal(err)
				}
				waitLong(t, "assistant events delivered", func() bool {
					_, out := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/home", token, `{}`)
					return strings.Contains(fmt.Sprint(out["view"]), "value:2")
				})
				if err := h.extensions.reg.SetContribution(context.Background(), "example-ui", "events", "kb", false, "test"); err != nil {
					t.Fatal(err)
				}
				if ids := h.extensions.reg.EventSubscribers("kb.changed", extension.ScopeKBRead); len(ids) != 0 {
					t.Fatalf("disabled contribution still receives KB: %v", ids)
				}
				if err := h.extensions.reg.SetContribution(context.Background(), "example-ui", "events", "kb", true, "test"); err != nil {
					t.Fatal(err)
				}
				if err := h.extensions.reg.SetEnabled(context.Background(), "example-ui", false, "test"); err != nil {
					t.Fatal(err)
				}
				if ids := h.extensions.reg.EventSubscribers("kb.forgotten", extension.ScopeKBRead); len(ids) != 0 {
					t.Fatalf("disabled extension still receives KB: %v", ids)
				}
			} else {
				h.extensions.emit("kb.changed", map[string]any{"kind": "people", "id": "example-person", "what": "metadata"})
				h.extensions.emit("kb.forgotten", map[string]any{"source": "example"})
				// This event follows both KB notifications on the same process stream, proving the subscriber has read them.
				h.extensions.emit("agent.finished", map[string]any{})
				waitLong(t, "sentinel event delivered", func() bool {
					_, out := call(t, mux, "POST", "/api/v1/extensions/example-ui/-/ui/home", token, `{}`)
					return strings.Contains(fmt.Sprint(out["view"]), "value:1")
				})
			}
		})
	}
}

func TestKBEventsRejectNonAssistantAndInvalidPayloads(t *testing.T) {
	h := newTestHub(t)
	// A nil registry would panic if a rejected payload reached the worker.
	h.extensions = &extensions{}
	wrong := &busConn{ident: db.BusIdentity{ID: "other"}}
	assistant := &busConn{ident: db.BusIdentity{ID: assistantwire.AssistantID}}
	for _, raw := range []string{`{"kind":"people","id":"person","what":"metadata"}`, `{"source":"example"}`} {
		busEvents[assistantwire.EventKBChanged](h, wrong, json.RawMessage(raw))
		busEvents[assistantwire.EventKBForgotten](h, wrong, json.RawMessage(raw))
	}
	for _, raw := range []string{`{}`, `{"kind":"unknown","id":"p","what":"metadata"}`, `{"kind":"people","id":"p","what":"raw private contents"}`, `[]`, `null`} {
		busEvents[assistantwire.EventKBChanged](h, assistant, json.RawMessage(raw))
	}
	busEvents[assistantwire.EventKBForgotten](h, assistant, json.RawMessage(`{}`))
	h.extensions.kbEvents.mu.Lock()
	defer h.extensions.kbEvents.mu.Unlock()
	if h.extensions.kbEvents.running || len(h.extensions.kbEvents.pending) > 0 {
		t.Fatal("invalid event queued")
	}
}
