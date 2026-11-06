package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

const injectedTitle = `<persona-context kind="context" title="快照">` + "\n机器：a\n</persona-context>\n现在把 hub 部署到最新 master"

func alertOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	return payload["aps"].(map[string]any)["alert"].(map[string]any)
}

// An ask_user card of a conversation titled by its first message: the subtitle is what the user wrote, not the
// <persona-context> block it starts with, through APNs and the WebSocket alike.
func TestInteractionPushStripsContextBlocks(t *testing.T) {
	ix := proto.Interaction{ID: "ix_1", Kind: proto.IxQuestion, ToolName: "ask_user", SessionTitle: injectedTitle,
		Questions: []proto.Question{{Question: "部署吗？"}}}
	payload := cleanAlert(interactionPayloadIn(i18n.ZhHans, ix, "m1"))
	if got := alertOf(t, payload)["subtitle"]; got != "现在把 hub 部署到最新 master · m1" {
		t.Fatalf("subtitle = %q", got)
	}
	ix.SessionTitle = `<persona-context kind="context" title="快照">` + "\n截断了" // only a block: its title
	if got := notifyMessage("interaction", interactionPayloadIn(i18n.ZhHans, ix, "m1"))["subtitle"]; got != "快照 · m1" {
		t.Fatalf("ws subtitle = %q", got)
	}
	ix.SessionTitle = `<persona-context kind="context" title="接上…` // clipped inside the tag: just the machine
	if got := notifyMessage("interaction", interactionPayloadIn(i18n.ZhHans, ix, "m1"))["subtitle"]; got != "m1" {
		t.Fatalf("ws subtitle = %q", got)
	}
}

// cleanAlert drops a line that is nothing but a block and leaves the caller's payload as it was.
func TestCleanAlertDropsEmptyLines(t *testing.T) {
	orig := map[string]any{"aps": map[string]any{"alert": map[string]any{
		"title": "Persona 在问你", "subtitle": `<persona-context kind="context" t`, "body": injectedTitle}}}
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
	// The new name (#3182): a block, a bare snapshot, a clipped one.
	alert = alertOf(t, cleanAlert(map[string]any{"aps": map[string]any{"alert": map[string]any{
		"title": "<persona-context>\n快照\n</persona-context>\n约周五吃饭", "subtitle": `<persona-context kind="context" t`,
		"body": `<persona-context kind="context" title="快照">` + "\n机器：a\n</persona-context>\n现在把 hub 部署到最新 master"}}}))
	if _, ok := alert["subtitle"]; ok || alert["title"] != "约周五吃饭" || alert["body"] != "现在把 hub 部署到最新 master" {
		t.Fatalf("persona-context alert = %v", alert)
	}
	if p := map[string]any{"aps": map[string]any{"content-state": 1}}; cleanAlert(p)["aps"] == nil {
		t.Fatal("payload without an alert lost its aps")
	}
}
