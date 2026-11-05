package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

const injectedTitle = `<jarvis-context kind="context" title="快照">` + "\n机器：a\n</jarvis-context>\n现在把 hub 部署到最新 master"

func alertOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	return payload["aps"].(map[string]any)["alert"].(map[string]any)
}

// An ask_user card of a conversation titled by its first message: the subtitle is what the user wrote, not the
// <jarvis-context> block it starts with, through APNs and the WebSocket alike.
func TestInteractionPushStripsContextBlocks(t *testing.T) {
	ix := proto.Interaction{ID: "ix_1", Kind: proto.IxQuestion, ToolName: "ask_user", SessionTitle: injectedTitle,
		Questions: []proto.Question{{Question: "部署吗？"}}}
	payload := cleanAlert(interactionPayloadIn(i18n.ZhHans, ix, "m1"))
	if got := alertOf(t, payload)["subtitle"]; got != "现在把 hub 部署到最新 master · m1" {
		t.Fatalf("subtitle = %q", got)
	}
	ix.SessionTitle = `<jarvis-context kind="context" title="快照">` + "\n截断了" // only a block: just the machine
	if got := notifyMessage("interaction", interactionPayloadIn(i18n.ZhHans, ix, "m1"))["subtitle"]; got != "m1" {
		t.Fatalf("ws subtitle = %q", got)
	}
}

// cleanAlert drops a line that is nothing but a block and leaves the caller's payload as it was.
func TestCleanAlertDropsEmptyLines(t *testing.T) {
	orig := map[string]any{"aps": map[string]any{"alert": map[string]any{
		"title": "Persona 在问你", "subtitle": `<jarvis-context kind="context" t`, "body": injectedTitle}}}
	alert := alertOf(t, cleanAlert(orig))
	if _, ok := alert["subtitle"]; ok {
		t.Fatalf("subtitle kept: %q", alert["subtitle"])
	}
	if alert["body"] != "现在把 hub 部署到最新 master" || alert["title"] != "Persona 在问你" {
		t.Fatalf("alert = %v", alert)
	}
	if alertOf(t, orig)["body"] != injectedTitle {
		t.Fatal("cleanAlert changed the caller's payload")
	}
	if p := map[string]any{"aps": map[string]any{"content-state": 1}}; cleanAlert(p)["aps"] == nil {
		t.Fatal("payload without an alert lost its aps")
	}
}
