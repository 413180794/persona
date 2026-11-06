package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// memSource is an agents.Source held in memory, as a plugin's would be.
type memSource struct {
	name     string
	sessions []AgentSession
	listN    int
	fail     error
	during   func() // runs while Sessions is being answered
}

func (m *memSource) Name() string { return m.name }
func (m *memSource) Status(context.Context) (Status, error) {
	return Status{Source: m.name, Enabled: true}, nil
}
func (m *memSource) Sessions(context.Context) ([]Session, error) {
	m.listN++
	if m.during != nil {
		m.during()
	}
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]Session, len(m.sessions))
	for i, s := range m.sessions {
		out[i] = Wrap(m.name, s, "")
	}
	return out, nil
}
func (m *memSource) Search(context.Context, string) ([]SearchHit, error) { return nil, m.fail }
func (m *memSource) Messages(context.Context, string, string, int) (*MessagePage, error) {
	return &MessagePage{Messages: []Message{}}, nil
}
func (m *memSource) Send(context.Context, string, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (m *memSource) DeleteMessage(context.Context, string, string) error { return nil }
func (m *memSource) MarkRead(context.Context, string) error              { return nil }

// A source added while the hub runs is listed after the hub's own and reached by its ids; one by a taken name is
// refused; a removed one is gone with what was cached of it, and the apps are told.
func TestAddAndRemoveSources(t *testing.T) {
	f := &fakeBridge{sessions: []AgentSession{{SessionKey: key, Provider: "grok_bot", CanSend: true}}}
	s, changes := newSvc(f, nil)
	ctx := context.Background()
	plug := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "s1", Provider: "grok_bot", CanSend: true}}}

	if !s.Add(plug) {
		t.Fatal("a new source is added")
	}
	if s.Add(&memSource{name: testSource}) || s.Add(&memSource{name: "grokbot"}) {
		t.Fatal("a name that is taken is refused")
	}
	if st := s.Statuses(ctx); len(st) != 2 || st[0].Source != testSource || st[1].Source != "grokbot" {
		t.Fatalf("statuses %+v", st)
	}
	list, err := s.Sessions(ctx, "", "grok_bot")
	if err != nil || len(list) != 2 || list[1].ID != "grokbot:s1" || list[1].Source != "grokbot" {
		t.Fatalf("sessions %+v %v", list, err)
	}
	if _, err := s.Send(ctx, "grokbot:s1", "", "hi", "test"); err != nil {
		t.Fatalf("send to the added source: %v", err)
	}
	if _, err := s.Messages(ctx, "grokbot:s1", "", 20); err != nil {
		t.Fatal(err)
	}

	n := len(*changes)
	s.Remove("grokbot")
	if len(*changes) != n+1 {
		t.Fatal("the apps are told when a source goes")
	}
	if list, _ := s.Sessions(ctx, "", ""); len(list) != 1 || list[0].Source != testSource {
		t.Fatalf("after remove %+v", list)
	}
	if _, ok := s.cache.getSessions("grokbot"); ok {
		t.Fatal("its cached list is dropped")
	}
	if _, ok := s.cache.getLatest("grokbot:s1", 20); ok {
		t.Fatal("its cached pages are dropped")
	}
	if _, err := s.Messages(ctx, "grokbot:s1", "", 20); status(err) != http.StatusNotFound {
		t.Fatalf("a removed source's session: %v", err)
	}
	s.Remove("grokbot") // twice is nothing
	if !s.Add(plug) {
		t.Fatal("added again after it went")
	}
}

// A source that fails (its plugin crashed, timed out) is left out of the lists of all of them so the others are still
// there; asked for by name, or when it is the only one, its error is the answer.
func TestFailingSourceDoesNotHideOthers(t *testing.T) {
	f := &fakeBridge{sessions: []AgentSession{{SessionKey: key, Provider: "grok_bot", CanSend: true}}}
	s, _ := newSvc(f, nil)
	ctx := context.Background()
	down := errors.New("plugin not running")
	s.Add(&memSource{name: "broken", fail: down})
	if list, err := s.Sessions(ctx, "", ""); err != nil || len(list) != 1 || list[0].Source != testSource {
		t.Fatalf("the others' sessions: %+v %v", list, err)
	}
	if _, err := s.Search(ctx, "", "x"); err != nil {
		t.Fatalf("the others' search: %v", err)
	}
	if _, err := s.Sessions(ctx, "broken", ""); !errors.Is(err, down) {
		t.Fatalf("asked by name: %v", err)
	}
	s.Remove(testSource)
	if _, err := s.Sessions(ctx, "", ""); !errors.Is(err, down) {
		t.Fatalf("the only source: %v", err)
	}
}

// What a removed source answers late is not cached: a source by that name added again (its plugin upgraded) is asked
// itself. A send's follow-up for a removed source tells the apps nothing.
func TestRemovedSourceAnswerNotCached(t *testing.T) {
	s, changes := newSvc(&fakeBridge{}, nil)
	ctx := context.Background()
	old := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "old"}}}
	old.during = func() { s.Remove("grokbot") } // removed while its list is being answered
	s.Add(old)
	if _, err := s.Sessions(ctx, "grokbot", ""); err != nil {
		t.Fatal(err)
	}
	fresh := &memSource{name: "grokbot", sessions: []AgentSession{{SessionKey: "new"}}}
	s.Add(fresh)
	list, err := s.Sessions(ctx, "grokbot", "")
	if err != nil || len(list) != 1 || list[0].SessionKey != "new" || fresh.listN != 1 {
		t.Fatalf("the old answer came back: %+v %v (listed %d)", list, err, fresh.listN)
	}
	s.Remove("grokbot")
	n := len(*changes)
	s.Updated("grokbot", []string{"new"})
	if len(*changes) != n {
		t.Fatal("an update for a removed source was pushed")
	}
}
