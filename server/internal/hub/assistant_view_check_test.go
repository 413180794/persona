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
	editSessions(n.m, func(list []proto.Session) []proto.Session {
		return append(list, proto.Session{SessionID: "terminal-example", Cwd: convDir, PaneID: "window:pane", Assistant: true})
	})
	invalid := "```persona-view\n{\"sections\":[]}\n```"
	valid := "```persona-view\n{\"fallback\":\"Complete\",\"sections\":[]}\n```"
	on, off := true, false
	for _, tc := range []struct {
		name, session, message string
		enabled, block         bool
		override               *bool
	}{
		{"disabled", "trip-0002", invalid, false, false, nil},
		{"trusted-invalid", "trip-0002", invalid, true, true, nil},
		{"admin-enabled", "trip-0002", invalid, false, true, &on},
		{"admin-disabled", "trip-0002", invalid, true, false, &off},
		{"trusted-valid", "trip-0002", valid, true, false, nil},
		{"ordinary", "agent-0005", invalid, true, false, nil},
		{"pane", "terminal-example", invalid, true, false, nil},
		{"unknown", "unknown-example", invalid, true, false, nil},
		{"plain", "trip-0002", "All complete", true, false, nil},
		{"incomplete", "trip-0002", "```persona-view\n{\"fallback\":\"Complete\",\"sections\":[]}", true, true, nil},
		{"bounded", "trip-0002", strings.Repeat("x", 1<<20) + "persona-view", true, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.admin.mu.Lock()
			h.cfg.Assistant.PersonaView = tc.enabled
			h.admin.personaView = tc.override
			h.admin.mu.Unlock()
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
