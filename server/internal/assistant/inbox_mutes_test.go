package assistant

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

func muteHub(t *testing.T) *inboxHub {
	t.Helper()
	h := newInboxHub(t)
	h.inboxReady(context.Background())
	return h
}

func mailItem(id, from, name string) *InboxItem {
	return &InboxItem{ID: id, Source: "mail", Kind: "mail", Title: "示例订单", UpdatedAt: time.Now(),
		Mail: &InboxMail{AccountID: "acc1", From: MailAddress{Name: name, Email: from}}}
}

// What an item can be muted by, for each kind of source.
func TestMutesOf(t *testing.T) {
	for name, c := range map[string]struct {
		it   *InboxItem
		want string
	}{
		"feishu dm":    {&InboxItem{Source: "lark", Title: "韩梅梅", Sender: "韩梅梅", Lark: &InboxLark{FeedID: "f1", SenderID: "u1"}}, "[{chat f1 韩梅梅}]"},
		"feishu group": {&InboxItem{Source: "lark", Title: "周末徒步群", Sender: "Tom", Lark: &InboxLark{FeedID: "f3", IsGroup: true, SenderID: "u-tom"}}, "[{chat f3 周末徒步群} {sender u-tom Tom}]"},
		"feishu doc":   {&InboxItem{Source: "lark", Lark: &InboxLark{FeedID: "f9"}, Doc: &InboxDoc{}}, "[]"},
		"mail":         {mailItem("m", "Shop@Example.com", ""), "[{sender shop@example.com shop@example.com}]"},
		"connector": {&InboxItem{Source: "github", Connector: &InboxConnector{Tags: []connector.Tag{
			{Key: connector.TagRepo, Value: "example-org/example-repo", Name: "example-org/example-repo"},
			{Key: connector.TagBranch, Value: "main"}, {Key: connector.TagSender, Value: "alice", Name: "alice"}}}},
			"[{repo example-org/example-repo example-org/example-repo} {sender alice alice}]"},
	} {
		if got := fmt.Sprint(mutesOf(c.it)); got != c.want && !(c.want == "[]" && got == "[]") {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// Muting a mail's sender files the open items it covers (that sender's, not another's), keeps the mute, lists it,
// files what comes in later at the next collection, and unmuting leaves what it covered done.
func TestMuteFromAnItem(t *testing.T) {
	h := muteHub(t)
	ctx := context.Background()
	h.inbox.items["a"] = mailItem("a", "shop@example.com", "示例商城")
	h.inbox.items["b"] = mailItem("b", "SHOP@example.com", "示例商城")
	h.inbox.items["c"] = mailItem("c", "lucy@example.com", "Lucy")
	mux := h.routes()

	_, out := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
	if !strings.Contains(fmt.Sprint(out["items"]), "mutes:[map[name:示例商城 tag:sender value:shop@example.com]]") {
		t.Fatalf("items do not say what mutes them: %v", out["items"])
	}
	if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"a","tag":"repo"}`); code != 400 {
		t.Fatalf("a mail muted by repository: %d", code)
	}
	code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"a","tag":"sender"}`)
	if code != 200 || out["settled"] != float64(2) {
		t.Fatalf("mute: %d %v", code, out)
	}
	for id, done := range map[string]bool{"a": true, "b": true, "c": false} {
		it := h.inbox.items[id]
		if it.open() == done || done && it.Reason != "已静音：示例商城" {
			t.Errorf("%s: done %v, reason %q", id, it.DoneAt, it.Reason)
		}
	}
	_, out = inboxCall(t, mux, "GET", "/api/v1/inbox/mutes", "")
	list := out["mutes"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["source_name"] != "邮件" || list[0].(map[string]any)["value"] != "shop@example.com" {
		t.Fatalf("mutes %v", out)
	}
	// Muting it again is the same mute.
	if _, out := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"b","tag":"sender"}`); out["settled"] != float64(0) {
		t.Fatalf("again: %v", out)
	}
	if len(h.listedMutes()) != 1 {
		t.Fatal("muted twice")
	}

	// Kept: the manager started again has it, and a later mail of the sender is filed at the next collection.
	h2 := New(Options{Hub: h.f, Store: func() Store { return h.st }, Logf: testLogf(t)})
	h2.inboxReady(ctx)
	h2.inbox.items["d"] = mailItem("d", "shop@example.com", "示例商城")
	h2.inbox.mu.Lock()
	h2.inbox.applyMutes(time.Now()) // as each collection does
	h2.inbox.mu.Unlock()
	if h2.inbox.items["d"].open() {
		t.Fatal("a later mail of a muted sender stayed open")
	}

	id := list[0].(map[string]any)["id"].(string)
	if code, _ := inboxCall(t, mux, "DELETE", "/api/v1/inbox/mutes/"+id, ""); code != 200 || len(h.listedMutes()) != 0 {
		t.Fatalf("unmute: %d %v", code, h.listedMutes())
	}
	if h.inbox.items["a"].open() {
		t.Fatal("unmuting reopened what it covered")
	}
}

// A source's muted repositories (GitHub's page lists and sets them as its muted_repos): set replaces them (the others
// unmuted), add only adds (the ones its options kept before, moved over), a repository needs its owner.
func TestInboxRepoMutes(t *testing.T) {
	h := muteHub(t)
	ctx := context.Background()
	h.inbox.srcMu.Lock()
	h.inbox.names["github"] = "GitHub"
	h.inbox.srcMu.Unlock()
	if repos, err := h.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github", Set: true, Add: true, Repos: []string{"Example-Org/Old-Site"}}); err != nil ||
		!slices.Equal(repos, []string{"Example-Org/Old-Site"}) {
		t.Fatalf("moved: %v %v", repos, err)
	}
	if got := h.listedMutes(); len(got) != 1 || got[0].Value != "example-org/old-site" || got[0].SourceName != "GitHub" || got[0].Tag != connector.TagRepo {
		t.Fatalf("mutes %+v", got)
	}
	if repos, _ := h.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github", Set: true, Add: true, Repos: []string{"example-org/old-site"}}); len(repos) != 1 {
		t.Fatalf("moved again: %v", repos)
	}
	if repos, err := h.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github", Set: true, Repos: []string{"example-org/new"}}); err != nil ||
		!slices.Equal(repos, []string{"example-org/new"}) {
		t.Fatalf("set: %v %v", repos, err)
	}
	if _, err := h.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github", Set: true, Repos: []string{"nope"}}); err == nil {
		t.Fatal("a repository without its owner was taken")
	}
	if repos, _ := h.InboxRepoMutes(ctx, assistantwire.InboxRepoMutes{Source: "github"}); !slices.Equal(repos, []string{"example-org/new"}) {
		t.Fatalf("listed: %v", repos)
	}
}
