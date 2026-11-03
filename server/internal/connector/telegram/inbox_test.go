package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

func thingOf(things []connector.InboxThing, key string) *connector.InboxThing {
	for i := range things {
		if things[i].Thread == key {
			return &things[i]
		}
	}
	return nil
}

// What needs the user: an unread private message, a group's @ of the user; muted and archived ones stay out; what
// was answered or read closes.
func TestInbox(t *testing.T) {
	c, f, ctx := archiveAccount(t)
	now := time.Now()
	f.mu.Lock()
	f.dialogs = []dialog{
		{Peer: peer{Kind: 'u', ID: 1, Hash: 11}, Name: "示例用户", Kind: "dm", Top: 3, TopAt: now, Unread: 1, TopFrom: "示例用户", TopText: "示例问题", Username: "example"},
		{Peer: peer{Kind: 'c', ID: 2, Hash: 22}, Name: "示例群", Kind: "group", Top: 12, TopAt: now, Unread: 5, Mentions: 1},
		{Peer: peer{Kind: 'c', ID: 6}, Name: "静音的示例群", Kind: "group", Top: 7, TopAt: now, Mentions: 2, Muted: true},
		{Peer: peer{Kind: 'u', ID: 7}, Name: "归档的示例用户", Kind: "dm", Top: 4, TopAt: now, Unread: 1, Archived: true},
		{Peer: peer{Kind: 'u', ID: 8}, Name: "我回过的示例用户", Kind: "dm", Top: 9, TopAt: now, TopOut: true},
		{Peer: peer{Kind: 'c', ID: 9}, Name: "没 @ 过我的示例群", Kind: "group", Top: 30, TopAt: now, Unread: 40},
		{Peer: peer{Kind: 'u', ID: 3}, Name: "示例机器人", Kind: "dm", Top: 5, TopAt: now, Unread: 1, Bot: true},
	}
	f.history["c2"][1].Mentioned = true // 12: the message that @ me
	f.mu.Unlock()
	c.refreshInbox(ctx)
	things, _ := c.Collect(ctx)

	dm := thingOf(things, "u1")
	if dm == nil || dm.Kind != "dm" || dm.Label != "私聊" || dm.Read || dm.Done != "" || dm.Tier != "action" || dm.Last != "3" ||
		dm.OpenURL != "tg://resolve?domain=example" || !strings.Contains(dm.Context, "我：示例回复") {
		t.Fatalf("dm %+v", dm)
	}
	group := thingOf(things, "c2")
	if group == nil || group.Kind != "mention" || group.Label != "@你" || group.Read || group.Last != "12" ||
		!strings.Contains(group.Context, "[图片]") || strings.Contains(group.Context, "群里的示例消息") {
		t.Fatalf("mention %+v", group)
	}
	for _, out := range []string{"c6", "u7", "c9", "u3"} {
		if thingOf(things, out) != nil {
			t.Errorf("%s is in the inbox", out)
		}
	}
	if r := thingOf(things, "u8"); r == nil || r.Done != connector.DoneReplied {
		t.Fatalf("answered %+v", r)
	}

	// Read on the phone: the dm is read; the group gets another mention later, then it is read.
	f.mu.Lock()
	f.dialogs[0].Unread = 0
	f.dialogs[1].Top, f.dialogs[1].Mentions = 15, 2
	f.mu.Unlock()
	c.refreshInbox(ctx)
	things, _ = c.Collect(ctx)
	if dm := thingOf(things, "u1"); !dm.Read {
		t.Fatalf("read dm %+v", dm)
	}
	if g := thingOf(things, "c2"); g.Last != "15" || g.Read {
		t.Fatalf("a new mention %+v", g)
	}
	f.mu.Lock()
	f.dialogs[1].Top, f.dialogs[1].Mentions = 16, 0
	f.mu.Unlock()
	c.refreshInbox(ctx)
	things, _ = c.Collect(ctx)
	if g := thingOf(things, "c2"); !g.Read || g.Last != "15" {
		t.Fatalf("read mentions %+v", g)
	}
	// Another mention after that is a new move (not the one already done).
	f.mu.Lock()
	f.dialogs[1].Top, f.dialogs[1].Mentions = 20, 1
	f.mu.Unlock()
	c.refreshInbox(ctx)
	things, _ = c.Collect(ctx)
	if g := thingOf(things, "c2"); g.Last != "20" || g.Read {
		t.Fatalf("a later mention %+v", g)
	}

	if err := c.Reply(context.Background(), connector.InboxReply{Thread: "u1", Text: "示例回复"}); err == nil {
		t.Fatal("replied: the connector only reads")
	}
}

func TestOpenURL(t *testing.T) {
	for _, c := range []struct {
		d    dialog
		want string
	}{
		{dialog{Peer: peer{Kind: 'u', ID: 1}, Username: "example"}, "tg://resolve?domain=example"},
		{dialog{Peer: peer{Kind: 'u', ID: 1}}, "tg://user?id=1"},
		{dialog{Peer: peer{Kind: 'c', ID: 2}, Top: 12}, "https://t.me/c/2/12"},
		{dialog{Peer: peer{Kind: 'g', ID: 6}}, ""},
	} {
		if got := openURL(c.d); got != c.want {
			t.Errorf("%+v: %q, want %q", c.d, got, c.want)
		}
	}
}
