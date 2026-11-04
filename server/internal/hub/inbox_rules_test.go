package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// A thing's own rules replace inbox.rules for its item (the default criteria stay underneath); an item without any
// gets inbox.rules. The rules are kept with the item, trimmed and cut, and never listed.
func TestInboxTriageRulesPerSource(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	h.cfg.Inbox.Rules = "全局规则：老板找我的都需要我"
	model, calls := captureModel(t, `{"tier":"fyi","summary":"示例摘要","reason":"示例"}`)
	h.triageAI = model
	now := time.Now()
	own, plain := thing("a", now), thing("a", now)
	own.Thread, own.Tier, own.Context, own.Rules = "example-org/example-repo#1", "", "示例：甲来源的一件", "  示例来源的规则：机器人发的都算噪音  "
	plain.Thread, plain.Tier, plain.Context = "example-org/example-repo#2", "", "示例：乙来源的一件"
	things := []connector.InboxThing{own, plain}
	h.vetThings(&inboxConnector{}, things)
	h.inbox.mu.Lock()
	items := h.inbox.applyConnector("example", "Example", things, now)
	h.inbox.mu.Unlock()
	for _, it := range items {
		if !h.triageOne(ctx, it) || it.Tier != TierFYI {
			t.Fatalf("triage: %+v", it)
		}
	}
	if len(calls()) != 2 {
		t.Fatalf("%d triage calls", len(calls()))
	}
	for _, c := range calls() {
		switch {
		case strings.Contains(c.User, "甲来源"):
			if !strings.Contains(c.System, "用户自己定的规则（优先于上面的默认判断）：\n示例来源的规则：机器人发的都算噪音") ||
				strings.Contains(c.System, "全局规则") || !strings.Contains(c.System, "noise（噪音）") {
				t.Errorf("the source's rules should replace the global ones, over the defaults:\n%s", c.System)
			}
		case strings.Contains(c.User, "乙来源"):
			if !strings.Contains(c.System, "全局规则：老板找我的都需要我") || strings.Contains(c.System, "示例来源的规则") {
				t.Errorf("an item without rules should get the global ones:\n%s", c.System)
			}
		default:
			t.Errorf("unexpected call: %s", c.User)
		}
	}
	for _, it := range items {
		if it.Connector.Thread == own.Thread && it.Rules != "示例来源的规则：机器人发的都算噪音" {
			t.Fatalf("rules kept: %q", it.Rules)
		}
		raw, _ := json.Marshal(it.listed())
		if strings.Contains(string(raw), `"rules"`) {
			t.Fatalf("rules listed: %s", raw)
		}
	}
	// A migrated connector's things (Feishu through a plugin: they carry extra) bring theirs too, line breaks kept.
	lark := &extensionLark{}
	dm := connector.InboxThing{Thread: "chat:10", Kind: KindDM, Title: "Alice", Context: "Alice：在吗", At: now, Last: "5",
		Rules: "示例：私聊都需要我\n示例：机器人都算噪音\n", Extra: &connector.ThingExtra{Source: "lark", LegacyID: "lark:10:5",
			Lark: rawJSON(InboxLark{AccountID: 1, FeedID: "10"})}}
	migrated := []connector.InboxThing{dm}
	h.vetThings(lark, migrated)
	h.inbox.mu.Lock()
	h.inbox.applyMigrated("lark", migrated, now)
	item := h.inbox.items["lark:10:5"]
	h.inbox.mu.Unlock()
	if item == nil || item.Rules != "示例：私聊都需要我\n示例：机器人都算噪音" || !h.triageOne(ctx, item) {
		t.Fatalf("migrated item: %+v", item)
	}
	if c := calls()[len(calls())-1]; !strings.Contains(c.System, "示例：私聊都需要我\n示例：机器人都算噪音") || strings.Contains(c.System, "全局规则") {
		t.Fatalf("migrated item's prompt:\n%s", c.System)
	}
	long := []connector.InboxThing{{Rules: strings.Repeat("规", thingRulesMost+10)}}
	h.vetThings(&inboxConnector{}, long)
	if n := len([]rune(long[0].Rules)); n != thingRulesMost+1 { // cut, with an ellipsis
		t.Fatalf("rules of %d runes kept", n)
	}
}
