package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A message card's sent mark: set once on the cards about the item put in by then, kept, and listed again to a
// client that asks for what came since (its time does not change, its body does).
func TestFeedMarkSent(t *testing.T) {
	d, ctx := open(t), context.Background()
	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	add := func(id, kind, ref string, at time.Time) {
		t.Helper()
		if err := d.AppendFeed(ctx, FeedRow{ID: id, Conversation: "c1", At: at, Kind: kind, Ref: ref, Body: json.RawMessage(`{"draft":"在修了"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	add("f1", "message", "inbox:1", t0)
	add("f2", "task", "inbox:1", t0.Add(time.Minute))
	add("f3", "message", "inbox:2", t0.Add(2*time.Minute))
	add("f4", "message", "inbox:1", t0.Add(30*time.Minute)) // a later draft

	sent := t0.Add(10 * time.Minute)
	patch := json.RawMessage(`{"sent_at":"` + sent.Format(time.RFC3339Nano) + `","sent":"修好了"}`)
	rows, err := d.MarkFeedSent(ctx, "message", "inbox:1", sent, patch)
	if err != nil || len(rows) != 1 || rows[0].ID != "f1" {
		t.Fatalf("MarkFeedSent = %+v, %v", rows, err)
	}
	var b map[string]string
	_ = json.Unmarshal(rows[0].Body, &b)
	if b["draft"] != "在修了" || b["sent"] != "修好了" || b["sent_at"] == "" {
		t.Fatalf("body = %s", rows[0].Body)
	}
	// Marked once: a second send does not move it.
	again := json.RawMessage(`{"sent_at":"` + t0.Add(40*time.Minute).Format(time.RFC3339Nano) + `","sent":"别的"}`)
	if rows, err := d.MarkFeedSent(ctx, "message", "inbox:1", t0.Add(20*time.Minute), again); err != nil || len(rows) != 0 {
		t.Fatalf("marked again: %+v, %v", rows, err)
	}
	if r, err := d.FeedItem(ctx, "f1"); err != nil || r == nil || r.Kind != "message" || !json.Valid(r.Body) ||
		!strings.Contains(string(r.Body), `"sent": "修好了"`) {
		t.Fatalf("FeedItem = %+v (%s), %v", r, r.Body, err)
	}
	if r, err := d.FeedItem(ctx, "nope"); err != nil || r != nil {
		t.Fatalf("FeedItem(nope) = %+v, %v", r, err)
	}

	// A client that has everything up to f3 gets f4 and the card sent after f3 came.
	list, err := d.Feed(ctx, "c1", t0.Add(2*time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	if len(ids) != 2 || ids[0] != "f1" || ids[1] != "f4" {
		t.Fatalf("since f3: %v", ids)
	}
	if list, _ := d.Feed(ctx, "c1", t0.Add(30*time.Minute), 100); len(list) != 0 {
		t.Fatalf("since f4: %+v", list)
	}
}
