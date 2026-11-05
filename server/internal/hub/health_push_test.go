package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A health conversation's card says on the lock screen that the health space wants you, not what it is about nor the
// conversation's title (docs/health.md「外发」); other cards are as before.
func TestHealthCardPushHidesContent(t *testing.T) {
	alert := func(ix proto.Interaction) map[string]any {
		return interactionPayload(ix, "server")["aps"].(map[string]any)["alert"].(map[string]any)
	}
	a := alert(proto.Interaction{Kind: proto.IxPermission, ToolName: "mail_send", SessionTitle: "血压", Cwd: "/home/me/.persona/health",
		Summary: "发给 doctor@example.com：最近一周血压 150/95"})
	if body, sub := a["body"].(string), a["subtitle"].(string); strings.Contains(body, "150") || strings.Contains(sub, "血压") || !strings.Contains(sub, "健康") {
		t.Fatalf("health card: %v", a)
	}
	q := alert(proto.Interaction{Kind: proto.IxQuestion, ToolName: "ask_user", SessionTitle: "睡眠", Cwd: "/home/me/.jarvis/health",
		Questions: []proto.Question{{Question: "昨晚几点睡的？"}}})
	if strings.Contains(q["body"].(string), "几点") {
		t.Fatalf("health question: %v", q)
	}
	if o := alert(proto.Interaction{Kind: proto.IxPermission, SessionTitle: "Persona", Cwd: "/home/me/.persona/assistant", Summary: "发周报"}); o["body"] != "发周报" {
		t.Fatalf("assistant's own card: %v", o)
	}
}

// A health conversation's finished turn is pushed without its words or title; another's says them as before.
func TestHealthDonePushHidesContent(t *testing.T) {
	head, text := doneAlert(i18n.ZhHans, proto.Session{Title: "血压", Cwd: "/home/me/.persona/health", LastText: "这周平均 150/95"})
	if strings.Contains(head, "血压") || strings.Contains(text, "150") || !strings.Contains(head, "健康") {
		t.Fatalf("health done: %q %q", head, text)
	}
	if head, text := doneAlert(i18n.ZhHans, proto.Session{Title: "周报", Cwd: "/home/me/.persona/assistant", LastText: "写好了"}); head != "✅ 周报" || text != "写好了" {
		t.Fatalf("assistant's done: %q %q", head, text)
	}
}
