package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/github"
	"github.com/chyroc/jarvis/server/internal/mail"
)

// muteStore keeps settings in memory and takes inbox items without keeping them.
type muteStore struct{ *keepStore }

func (muteStore) PutInboxItem(context.Context, string, []byte, time.Time, *time.Time) error {
	return nil
}

func muteHub(t *testing.T) *Hub {
	t.Helper()
	h := newTestHub(t)
	h.store = muteStore{newKeepStore()}
	h.connectors = connector.NewRegistry(nil)
	h.inbox.init()
	return h
}

func mailItem(id, from, name string) *InboxItem {
	return &InboxItem{ID: id, Source: "mail", Kind: "mail", Title: "示例订单", UpdatedAt: time.Now(),
		Mail: &InboxMail{AccountID: "acc1", From: mail.Address{Name: name, Email: from}}}
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

	_, out := call(t, mux, "GET", "/api/v1/inbox", h.cfg.Token, "")
	if !strings.Contains(fmt.Sprint(out["items"]), "mutes:[map[name:示例商城 tag:sender value:shop@example.com]]") {
		t.Fatalf("items do not say what mutes them: %v", out["items"])
	}
	if code, _ := call(t, mux, "POST", "/api/v1/inbox/mutes", h.cfg.Token, `{"item_id":"a","tag":"repo"}`); code != 400 {
		t.Fatalf("a mail muted by repository: %d", code)
	}
	code, out := call(t, mux, "POST", "/api/v1/inbox/mutes", h.cfg.Token, `{"item_id":"a","tag":"sender"}`)
	if code != 200 || out["settled"] != float64(2) {
		t.Fatalf("mute: %d %v", code, out)
	}
	for id, done := range map[string]bool{"a": true, "b": true, "c": false} {
		it := h.inbox.items[id]
		if it.open() == done || done && it.Reason != "已静音：示例商城" {
			t.Errorf("%s: done %v, reason %q", id, it.DoneAt, it.Reason)
		}
	}
	_, out = call(t, mux, "GET", "/api/v1/inbox/mutes", h.cfg.Token, "")
	list := out["mutes"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["source_name"] != "邮件" || list[0].(map[string]any)["value"] != "shop@example.com" {
		t.Fatalf("mutes %v", out)
	}
	// Muting it again is the same mute.
	if _, out := call(t, mux, "POST", "/api/v1/inbox/mutes", h.cfg.Token, `{"item_id":"b","tag":"sender"}`); out["settled"] != float64(0) {
		t.Fatalf("again: %v", out)
	}
	if len(h.listedMutes()) != 1 {
		t.Fatal("muted twice")
	}

	// Kept: a hub started again has it, and a later mail of the sender is filed at the next collection.
	h2 := newTestHub(t)
	h2.store, h2.connectors = h.store, connector.NewRegistry(nil)
	h2.inbox.init()
	h2.loadInboxMutes(ctx)
	h2.inbox.items["d"] = mailItem("d", "shop@example.com", "示例商城")
	h2.inbox.mu.Lock()
	h2.inbox.applyMutes(time.Now()) // as each collection does
	h2.inbox.mu.Unlock()
	if h2.inbox.items["d"].open() {
		t.Fatal("a later mail of a muted sender stayed open")
	}

	id := list[0].(map[string]any)["id"].(string)
	if code, _ := call(t, mux, "DELETE", "/api/v1/inbox/mutes/"+id, h.cfg.Token, ""); code != 200 || len(h.listedMutes()) != 0 {
		t.Fatalf("unmute: %d %v", code, h.listedMutes())
	}
	if h.inbox.items["a"].open() {
		t.Fatal("unmuting reopened what it covered")
	}
}

// GitHub's muted repositories: the ones its options kept are moved into the mutes; its settings list them from the
// mutes and set them into the mutes (older apps and GitHub's page work as before).
func TestGitHubMutedReposAreMutes(t *testing.T) {
	h := muteHub(t)
	ctx := context.Background()
	gh := github.New(github.Config{Inbox: github.InboxOptions{MutedRepos: []string{"Example-Org/Old-Site"}}}, "Mac", nil)
	if err := h.addConnector(gh); err != nil {
		t.Fatal(err)
	}
	h.loadInboxMutes(ctx)
	h.githubMutes(ctx)
	if got := h.listedMutes(); len(got) != 1 || got[0].Value != "example-org/old-site" || got[0].Name != "Example-Org/Old-Site" || got[0].SourceName != "GitHub" {
		t.Fatalf("not moved: %+v", got)
	}
	var kept struct {
		MutedRepos []string `json:"muted_repos"`
	}
	_ = json.Unmarshal(gh.Options(), &kept)
	if len(kept.MutedRepos) != 0 {
		t.Fatalf("still in GitHub's options: %v", kept.MutedRepos)
	}
	listed := func() []string {
		for _, info := range h.listedConnectors(ctx, true) {
			if info.ID == "github" {
				var o struct {
					MutedRepos []string `json:"muted_repos"`
				}
				_ = json.Unmarshal(info.Options, &o)
				return o.MutedRepos
			}
		}
		return nil
	}
	if got := listed(); !slices.Equal(got, []string{"Example-Org/Old-Site"}) {
		t.Fatalf("GitHub's page lists %v", got)
	}
	// The page sets the list: one added, the old one removed; the CI choice goes to GitHub.
	if err := h.setConnectorOptions(ctx, "github", json.RawMessage(`{"ci":"all","muted_repos":["example-org/new"],"ignore_branches":["train/*"]}`)); err != nil {
		t.Fatal(err)
	}
	if got := listed(); !slices.Equal(got, []string{"example-org/new"}) {
		t.Fatalf("after setting: %v", got)
	}
	var o struct {
		CI string `json:"ci"`
	}
	_ = json.Unmarshal(gh.Options(), &o)
	if o.CI != "all" {
		t.Fatalf("the CI choice did not reach GitHub: %s", gh.Options())
	}
	if err := h.setConnectorOptions(ctx, "github", json.RawMessage(`{"muted_repos":["nope"]}`)); err == nil {
		t.Fatal("a repository without its owner was taken")
	}
	// Moving again (the next start) changes nothing.
	h.githubMutes(ctx)
	if len(h.listedMutes()) != 1 {
		t.Fatalf("mutes %+v", h.listedMutes())
	}
}
