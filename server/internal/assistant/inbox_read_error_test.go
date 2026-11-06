package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

// A source the round could not read keeps what it listed, and the inbox status says which one is behind and why, so
// the owner does not take the list for complete; the next round that reads it clears that (#4801).
func TestInboxSaysWhichSourceCouldNotBeRead(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	names := map[string]string{"example": "Example"}
	ok := assistantwire.InboxRound{Round: roundPoll, Names: names,
		Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example", Things: []connector.InboxThing{thing("a", time.Now())}}}}
	if err := h.InboxCollected(ctx, ok); err != nil {
		t.Fatal(err)
	}
	before := len(openItems(h))
	if before != 1 {
		t.Fatalf("%d items after the first round", before)
	}
	failed := assistantwire.InboxRound{Round: roundPoll, Names: names,
		Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example", Things: []connector.InboxThing{}, ReadError: assistantwire.ReadFailed}}}
	if err := h.InboxCollected(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if n := len(openItems(h)); n != before {
		t.Fatalf("a source that could not be read lost its items: %d, had %d", n, before)
	}
	tr := h.inboxTriage()
	if len(tr.Sources) != 1 || tr.Sources[0].Source != "example" || tr.Sources[0].ReadError != assistantwire.ReadFailed ||
		tr.Sources[0].ReadReason == "" || tr.Sources[0].Name != "Example" || tr.Waiting != 0 || tr.State != "ok" {
		t.Fatalf("triage after a failed read: %+v", tr)
	}
	if err := h.InboxCollected(ctx, ok); err != nil {
		t.Fatal(err)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 0 {
		t.Fatalf("a source read again still says it failed: %+v", tr)
	}
}

// Review of #4825: a held round over a failed read carries the earlier things together with read_error (mergeRounds).
// Those things are applied, so new items and Done markers are not lost when the hub then clears what it held; the
// failure still shows. An unknown code reads as failed, so no raw i18n key reaches the apps.
func TestInboxAppliesThingsAFailedSourceCarries(t *testing.T) {
	h := newInboxHub(t)
	ctx := context.Background()
	names := map[string]string{"example": "Example"}
	r := assistantwire.InboxRound{Round: roundPoll, Names: names, Sources: []assistantwire.InboxSource{{ID: "example", Name: "Example",
		Things: []connector.InboxThing{thing("a", time.Now())}, ReadError: assistantwire.ReadFailed}}}
	if err := h.InboxCollected(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := len(openItems(h)); n != 1 {
		t.Fatalf("the things a failed source carried were dropped: %d items", n)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 1 || tr.Sources[0].ReadError != assistantwire.ReadFailed {
		t.Fatalf("the failure does not show: %+v", tr)
	}
	r.Sources[0].Things, r.Sources[0].ReadError = []connector.InboxThing{}, "a_code_from_a_newer_hub"
	if err := h.InboxCollected(ctx, r); err != nil {
		t.Fatal(err)
	}
	if tr := h.inboxTriage(); len(tr.Sources) != 1 || tr.Sources[0].ReadError != assistantwire.ReadFailed ||
		tr.Sources[0].ReadReason == "" || tr.Sources[0].ReadReason == "assistant-inbox_admit.read_a_code_from_a_newer_hub" {
		t.Fatalf("an unknown code: %+v", tr.Sources)
	}
}
