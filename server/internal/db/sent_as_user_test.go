package db

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A row is read back as it was written, newest first; empty JSON fields come back as empty objects.
func TestSentAsUserRecord(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	id, err := d.RecordSentAsUser(ctx, SentAsUser{At: at, Connector: "github", Action: "github_comment",
		Target: json.RawMessage(`{"ref":"example-org/example-repo#12"}`), Text: "Looks good.", Summary: "comment on example-org/example-repo#12",
		Origin:     json.RawMessage(`{"machine_id":"mac","session_id":"s1"}`),
		LetThrough: json.RawMessage(`{"kind":"card","interaction_id":"ix_1"}`)})
	if err != nil || id == 0 {
		t.Fatalf("record: %d %v", id, err)
	}
	if _, err := d.RecordSentAsUser(ctx, SentAsUser{At: at.Add(time.Minute), Connector: "mail", Action: "mail_send"}); err != nil {
		t.Fatal(err)
	}
	rows, next, err := d.SentAsUsers(ctx, 0, 0, "")
	if err != nil || next != 0 || len(rows) != 2 || rows[0].Connector != "mail" || rows[1].ID != id {
		t.Fatalf("rows %+v next %d %v", rows, next, err)
	}
	if string(rows[0].Target) != `{}` || string(rows[0].Origin) != `{}` || string(rows[0].LetThrough) != `{}` {
		t.Fatalf("empty fields: %s %s %s", rows[0].Target, rows[0].Origin, rows[0].LetThrough)
	}
	got := rows[1]
	var target struct{ Ref string }
	_ = json.Unmarshal(got.Target, &target)
	var through map[string]string
	_ = json.Unmarshal(got.LetThrough, &through)
	if !got.At.Equal(at) || got.Text != "Looks good." || got.Summary == "" || target.Ref != "example-org/example-repo#12" ||
		through["kind"] != "card" || through["interaction_id"] != "ix_1" || got.RecalledAt != nil || got.MessageRef != "" {
		t.Fatalf("read back %+v", got)
	}
}

// The ledger pages back by id, at most 200 rows at a time, and can be read for one connector.
func TestSentAsUserPaging(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	for i := range 5 {
		c := "github"
		if i%2 == 1 {
			c = "mail"
		}
		if _, err := d.RecordSentAsUser(ctx, SentAsUser{Connector: c, Action: c + "_x", Text: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	var texts []string
	var before int64
	for pages := 0; ; pages++ {
		rows, next, err := d.SentAsUsers(ctx, before, 2, "")
		if err != nil || len(rows) == 0 || len(rows) > 2 || pages > 2 {
			t.Fatalf("page %d: %+v %v", pages, rows, err)
		}
		for _, r := range rows {
			texts = append(texts, r.Text)
		}
		if next == 0 {
			break
		}
		if next != rows[len(rows)-1].ID {
			t.Fatalf("next %d is not the last id of %+v", next, rows)
		}
		before = next
	}
	if fmt.Sprint(texts) != "[4 3 2 1 0]" {
		t.Fatalf("paged: %v", texts)
	}
	rows, next, err := d.SentAsUsers(ctx, 0, 2, "mail")
	if err != nil || next != 0 || len(rows) != 2 || rows[0].Text != "3" || rows[1].Text != "1" {
		t.Fatalf("mail's: %+v next %d %v", rows, next, err)
	}
	// An exact fit has no next page.
	if rows, next, err = d.SentAsUsers(ctx, 0, 5, ""); err != nil || len(rows) != 5 || next != 0 {
		t.Fatalf("exact page: %d rows, next %d, %v", len(rows), next, err)
	}
	for i := range SentAsUserMost {
		if _, err := d.RecordSentAsUser(ctx, SentAsUser{Connector: "github", Action: "github_x", Text: fmt.Sprint("more ", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if rows, next, err = d.SentAsUsers(ctx, 0, 1000, ""); err != nil || len(rows) != SentAsUserMost || next == 0 {
		t.Fatalf("too many asked for: %d rows, next %d, %v", len(rows), next, err)
	}
}

// Prune deletes what is older than the cut and nothing else.
func TestSentAsUserPrune(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	now := time.Now()
	for _, age := range []time.Duration{100 * 24 * time.Hour, 91 * 24 * time.Hour, 89 * 24 * time.Hour, time.Hour} {
		if _, err := d.RecordSentAsUser(ctx, SentAsUser{At: now.Add(-age), Connector: "github", Action: "github_comment", Text: age.String()}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := d.PruneSentAsUser(ctx, now.Add(-90*24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("pruned %d %v", n, err)
	}
	rows, _, _ := d.SentAsUsers(ctx, 0, 10, "")
	if len(rows) != 2 {
		t.Fatalf("left %+v", rows)
	}
}
