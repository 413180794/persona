package agents

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/sessionlist"
)

func ids(list []Session) map[string]bool {
	out := map[string]bool{}
	for _, x := range list {
		out[x.ID] = true
	}
	return out
}

// A removed conversation leaves the list, the pages, the counts (unread too) and stays out across a restart; a new
// message brings it back by itself, Unhide by hand; the hidden view lists what is still out.
func TestHideLeavesTheListUntilANewMessage(t *testing.T) {
	ctx := context.Background()
	old := time.Now().Add(-time.Hour).UnixMilli()
	src := &memSource{name: "gw", sessions: []AgentSession{
		{SessionKey: "a", RankTimeMs: old, UnreadCount: 3},
		{SessionKey: "b", RankTimeMs: old},
	}}
	kv := &memKV{m: map[string][]byte{}}
	var changed int
	s := NewService(kv, func([]string) { changed++ }, nil, src)
	if _, err := s.Sessions(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hide(ctx, "gw:a"); err != nil {
		t.Fatal(err)
	}
	if changed == 0 {
		t.Fatal("the apps are told to re-pull")
	}
	list, _ := s.Sessions(ctx, "", "")
	if got := ids(list); got["gw:a"] || !got["gw:b"] {
		t.Fatalf("hidden one left the list: %v", got)
	}
	page, err := s.ListPage(ctx, sessionlist.Options{})
	if err != nil || page.Total != 1 {
		t.Fatalf("and the pages: %+v %v", page, err)
	}
	counts, err := s.ListCounts(ctx, sessionlist.Options{})
	if err != nil || counts.Total != 1 || counts.State["unread"] != 0 {
		t.Fatalf("and the counts, its unread too: %+v %v", counts, err)
	}
	hidden, err := s.Hidden(ctx)
	if err != nil || len(hidden) != 1 || hidden[0].ID != "gw:a" {
		t.Fatalf("the hidden view lists it: %v %v", hidden, err)
	}

	// Kept across a restart (the shared prefs in the database), and an app saving its prefs without the field (an older
	// app, a pin toggle) does not bring it back.
	s2 := NewService(kv, nil, nil, src)
	if _, err := s2.PutPrefs(ctx, Prefs{Pins: []string{"gw:b"}}); err != nil {
		t.Fatal(err)
	}
	list, _ = s2.Sessions(ctx, "", "")
	if ids(list)["gw:a"] {
		t.Fatalf("kept across a restart and a prefs save: %v", ids(list))
	}
	if p, _ := s2.Prefs(ctx); len(p.Pins) != 1 || len(p.Hidden) != 1 {
		t.Fatalf("the save kept its pins and the hidden set: %+v", p)
	}

	// A new message (the source's rank time past the hide) brings it back.
	src.sessions[0].RankTimeMs = time.Now().Add(time.Minute).UnixMilli()
	s2.Updated("gw", []string{"a"})
	list, _ = s2.Sessions(ctx, "", "")
	if !ids(list)["gw:a"] {
		t.Fatalf("a new message brings it back: %v", ids(list))
	}
	if hidden, _ := s2.Hidden(ctx); len(hidden) != 0 {
		t.Fatalf("and it is no longer in the hidden view: %v", hidden)
	}

	// Hide again, then bring it back by hand.
	if _, err := s2.Hide(ctx, "gw:b"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s2.Sessions(ctx, "", ""); ids(list)["gw:b"] {
		t.Fatal("hidden")
	}
	if _, err := s2.Unhide(ctx, "gw:b"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s2.Sessions(ctx, "", ""); !ids(list)["gw:b"] {
		t.Fatal("unhidden")
	}
	page, err = s2.ListPage(ctx, sessionlist.Options{})
	if err != nil || page.Total != 2 {
		t.Fatalf("both back in the pages: %+v %v", page, err)
	}
}

// Only a conversation a source lists can be removed; an unknown source is refused.
func TestHideNeedsAListedConversation(t *testing.T) {
	ctx := context.Background()
	s := NewService(&memKV{m: map[string][]byte{}}, nil, nil, &memSource{name: "gw", sessions: []AgentSession{{SessionKey: "a"}}})
	if _, err := s.Hide(ctx, "gw:nope"); HTTPStatus(err) != 404 {
		t.Fatalf("unknown conversation: %v", err)
	}
	if _, err := s.Hide(ctx, "other:a"); err == nil {
		t.Fatal("unknown source")
	}
	if _, err := s.Unhide(ctx, "other:a"); err == nil {
		t.Fatal("unknown source on unhide")
	}
}

// The hidden set is bounded: past maxHidden the oldest go.
func TestHiddenIsBounded(t *testing.T) {
	h := map[string]int64{}
	for i := range maxHidden + 3 {
		h[string(rune('a'+i%26))+time.Duration(i).String()] = int64(i + 1)
	}
	trimHidden(h)
	if len(h) != maxHidden {
		t.Fatalf("%d", len(h))
	}
	for _, at := range h {
		if at <= 3 {
			t.Fatalf("an oldest entry was kept: %d", at)
		}
	}
}
