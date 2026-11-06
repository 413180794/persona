package main

import (
	"context"
	"testing"

	"github.com/nwverse/persona/sdk/go/ext"
	"github.com/nwverse/persona/sdk/go/exttest"
	"github.com/nwverse/persona/sdk/go/wire"
)

func TestConformance(t *testing.T) {
	exttest.Check(t, manifest, exttest.Serve(t, program()))
}

func TestSearchAndOpen(t *testing.T) {
	a := &app{name: "示例", greeting: "你好"}
	ctx := context.Background()
	r := &ext.UIRequest{}
	r.UIVersion, r.Action, r.Values = 3, "search", map[string]string{"q": "条目 2"}
	if _, err := a.homeAction(ctx, r); err != nil {
		t.Fatal(err)
	}
	v, err := a.home(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if v.Search == nil || v.Search.Value != "条目 2" {
		t.Fatalf("search box: %+v", v.Search)
	}
	rows := v.Sections[1].Items
	if len(rows) != 2 { // items 2 and 20
		t.Fatalf("searching 条目 2 found %d", len(rows))
	}
	push := rows[1].Action.Push
	if push == nil {
		t.Fatal("a row opens its detail on top of the list")
	}
	r.State = push.State
	if v, _ = a.home(ctx, r); v.Title != "示例条目 20" {
		t.Fatalf("opened %q", v.Title)
	}
	// An app before UI version 3: a field and a button instead of the search box.
	r.UIVersion, r.State = 2, ""
	if v, _ = a.home(ctx, r); v.Search != nil || v.Sections[0].Items[1].Type != wire.ComponentField {
		t.Fatalf("for an older app: %+v", v.Sections[0])
	}
}
