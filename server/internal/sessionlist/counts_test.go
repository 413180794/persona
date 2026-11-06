package sessionlist

import (
	"fmt"
	"testing"
)

func TestDeltaCountsDoNotBuildFullOrderedView(t *testing.T) {
	var index Index
	items := make([]Item, 8000)
	for n := range items {
		items[n] = testItem(fmt.Sprintf("row-%04d", n), n)
	}
	index.Replace("demo", items)
	baseline := index.Revision()
	items[7999].Full = testPayload{ID: items[7999].ID, Title: "Changed without a timestamp change"}
	items[7999].State = "needs_you"
	index.Replace("demo", items)
	page := mustQuery(t, &index, Options{Since: baseline})
	if len(page.Sessions) != 1 || page.Total != 8000 {
		t.Fatalf("delta: %+v", page)
	}
	view := index.cache[cacheKey{}].result
	if view.ordered || len(view.events) != 0 {
		t.Fatal("delta unnecessarily built the whole ordered list")
	}
	counts, err := index.Counts(Options{})
	if err != nil || counts.State["working"] != 7999 || counts.State["needs_you"] != 1 {
		t.Fatalf("counts: %+v %v", counts, err)
	}
	// A later full page upgrades the counts-only cache without losing members.
	full := mustQuery(t, &index, Options{Limit: 50})
	if len(full.Sessions) != 50 || full.Total != 8000 || !index.cache[cacheKey{}].result.ordered {
		t.Fatalf("full after counts: %+v", full)
	}
	index.Replace("demo", items[:7999])
	counts, err = index.Counts(Options{})
	if err != nil || counts.Total != 7999 || counts.State["working"] != 7999 {
		t.Fatalf("removed counts: %+v %v", counts, err)
	}
	if _, exists := counts.State["needs_you"]; exists {
		t.Fatal("zero-count group retained")
	}
}

func TestFrozenCountsDetachedFromCurrentCounters(t *testing.T) {
	var index Index
	index.Replace("demo", []Item{testItem("a", 1), testItem("b", 2)})
	page := mustQuery(t, &index, Options{Limit: 1})
	index.RemoveSource("demo")
	frozen, err := index.Counts(Options{Cursor: page.NextCursor})
	if err != nil || frozen.Total != 2 || frozen.State["working"] != 2 {
		t.Fatalf("snapshot mutated: %+v %v", frozen, err)
	}
	current, _ := index.Counts(Options{})
	if current.Total != 0 || len(current.State) != 0 {
		t.Fatalf("current: %+v", current)
	}
}

// Rows waiting in the background (#6045) stay in the working count, for older apps, and are counted apart too; the
// count follows a row that starts or stops waiting, and filtered counts have it as well.
func TestBackgroundWaitingCount(t *testing.T) {
	var index Index
	items := make([]Item, 4)
	for n := range items {
		items[n] = testItem(fmt.Sprintf("row-%d", n), n)
	}
	items[1].BackgroundWaiting, items[2].BackgroundWaiting = true, true
	index.Replace("demo", items)
	counts, err := index.Counts(Options{})
	if err != nil || counts.State["working"] != 4 || counts.BackgroundWaiting != 2 {
		t.Fatalf("counts: %+v %v", counts, err)
	}
	items[2].BackgroundWaiting = false
	items[2].Full = testPayload{ID: items[2].ID, Title: "Its build started"}
	index.Replace("demo", items)
	if counts, _ = index.Counts(Options{}); counts.BackgroundWaiting != 1 {
		t.Fatalf("after one started working: %+v", counts)
	}
	if counts, _ = index.Counts(Options{State: "working"}); counts.BackgroundWaiting != 1 || counts.Total != 4 {
		t.Fatalf("filtered: %+v", counts)
	}
}
