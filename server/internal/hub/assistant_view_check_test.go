package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantViewCheckUsesLiveFlagAndTrustedSession(t *testing.T) {
	h, n := conversationsHub(t)
	n.m.mu.Lock()
	n.m.sessions = append(n.m.sessions, proto.Session{SessionID: "terminal-example", Cwd: convDir, PaneID: "window:pane", Assistant: true})
	n.m.mu.Unlock()
	invalid := "```persona-view\n{\"sections\":[]}\n```"
	valid := "```persona-view\n{\"fallback\":\"Complete\",\"sections\":[]}\n```"
	for _, tc := range []struct {
		name, session, message string
		enabled, block         bool
	}{
		{"disabled", "trip-0002", invalid, false, false},
		{"trusted-invalid", "trip-0002", invalid, true, true},
		{"trusted-valid", "trip-0002", valid, true, false},
		{"ordinary", "agent-0005", invalid, true, false},
		{"pane", "terminal-example", invalid, true, false},
		{"unknown", "unknown-example", invalid, true, false},
		{"plain", "trip-0002", "All complete", true, false},
		{"incomplete", "trip-0002", "```persona-view\n{\"fallback\":\"Complete\",\"sections\":[]}", true, true},
		{"bounded", "trip-0002", strings.Repeat("x", 1<<20) + "persona-view", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.cfg.Assistant.PersonaView = tc.enabled
			raw, _ := json.Marshal(assistantwire.ViewCheckRequest{SessionID: tc.session, Message: tc.message})
			out, err := n.m.onRequest(context.Background(), assistantwire.MethodViewCheck, raw)
			if err != nil {
				t.Fatal(err)
			}
			result, ok := out.(assistantwire.ViewCheckResponse)
			if !ok || (result.Reason != "") != tc.block {
				t.Fatalf("check result: %v", out)
			}
			if tc.block && (!strings.Contains(result.Reason, "这一轮") || !strings.Contains(result.Reason, "fallback") || strings.Contains(result.Reason, tc.message)) {
				t.Fatal("repair feedback did not provide the bounded template instructions")
			}
		})
	}
}
