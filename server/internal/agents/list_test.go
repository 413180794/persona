package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/sessionlist"
)

func TestPagedSourceFailureDropsStaleRows(t *testing.T) {
	a := &memSource{name: "first", sessions: []AgentSession{{SessionKey: "a"}}}
	b := &memSource{name: "second", sessions: []AgentSession{{SessionKey: "b"}}}
	s := NewService(nil, nil, nil, a, b)
	first, err := s.ListPage(context.Background(), sessionlist.Options{})
	if err != nil || first.Total != 2 {
		t.Fatalf("baseline: %+v %v", first, err)
	}
	b.fail = errors.New("demo source unavailable")
	s.Updated("second", nil)
	page, err := s.ListPage(context.Background(), sessionlist.Options{Since: first.Revision})
	if err != nil || page.Total != 1 || len(page.Remove) != 1 || page.Remove[0] != "second:b" {
		t.Fatalf("failed source: %+v %v", page, err)
	}
}

func TestPagedLateInvalidatedSourceDoesNotResurrect(t *testing.T) {
	source := &memSource{name: "demo", sessions: []AgentSession{{SessionKey: "stale"}}}
	s := NewService(nil, nil, nil, source)
	source.during = func() { s.Updated("demo", nil) }
	page, err := s.ListPage(context.Background(), sessionlist.Options{})
	if err != nil || page.Total != 0 {
		t.Fatalf("stale list published: %+v %v", page, err)
	}
	source.during = nil
	source.sessions = []AgentSession{{SessionKey: "fresh"}}
	page, err = s.ListPage(context.Background(), sessionlist.Options{})
	if err != nil || page.Total != 1 {
		t.Fatalf("fresh list missing: %+v %v", page, err)
	}
}
