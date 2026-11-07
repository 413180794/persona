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

// A row is read by its id, with every column; an id that is not there is not an error.
func TestSentAsUserByID(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	id, err := d.RecordSentAsUser(ctx, SentAsUser{At: at, Connector: "github", Action: "github_comment", MessageRef: "3000000001",
		Target: json.RawMessage(`{"ref":"example-org/example-repo#12"}`), Text: "Looks good.", Summary: "comment on example-org/example-repo#12",
		Origin: json.RawMessage(`{"machine_id":"mac"}`), LetThrough: json.RawMessage(`{"kind":"card","interaction_id":"ix_1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordSentAsUser(ctx, SentAsUser{Connector: "mail", Action: "mail_send"}); err != nil {
		t.Fatal(err)
	}
	row, ok, err := d.SentAsUserByID(ctx, id)
	if err != nil || !ok || row.ID != id || !row.At.Equal(at) || row.Connector != "github" || row.Action != "github_comment" ||
		row.MessageRef != "3000000001" || row.Text != "Looks good." || row.Summary != "comment on example-org/example-repo#12" ||
		row.RecalledAt != nil || string(row.Target) != `{"ref": "example-org/example-repo#12"}` ||
		string(row.Origin) != `{"machine_id": "mac"}` || string(row.LetThrough) != `{"kind": "card", "interaction_id": "ix_1"}` {
		t.Fatalf("%+v %v %v", row, ok, err)
	}
	if row, ok, err := d.SentAsUserByID(ctx, id+100); err != nil || ok || row.ID != 0 {
		t.Fatalf("a row that is not there: %+v %v %v", row, ok, err)
	}
}

// Marking a row recalled sets its recalled_at once and touches nothing else: not another column, not another row, and
// not the time of the first mark when it is asked again.
func TestSentAsUserMarkRecalled(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	record := func(text string) int64 {
		t.Helper()
		id, err := d.RecordSentAsUser(ctx, SentAsUser{Connector: "github", Action: "github_comment", MessageRef: "7", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, other := record("first"), record("other")
	before, _, _ := d.SentAsUserByID(ctx, first)

	at := time.Date(2026, 11, 7, 9, 30, 0, 0, time.UTC)
	if done, err := d.MarkSentAsUserRecalled(ctx, first, at); err != nil || !done {
		t.Fatalf("first mark: %v %v", done, err)
	}
	row, _, _ := d.SentAsUserByID(ctx, first)
	if row.RecalledAt == nil || !row.RecalledAt.Equal(at) {
		t.Fatalf("recalled_at %v, want %v", row.RecalledAt, at)
	}
	row.RecalledAt = nil
	if gotJSON, _ := json.Marshal(row); string(gotJSON) != mustJSON(t, before) {
		t.Fatalf("another column changed:\n%s\n%s", gotJSON, mustJSON(t, before))
	}
	if done, err := d.MarkSentAsUserRecalled(ctx, first, at.Add(time.Hour)); err != nil || done {
		t.Fatalf("second mark: %v %v", done, err)
	}
	if row, _, _ := d.SentAsUserByID(ctx, first); row.RecalledAt == nil || !row.RecalledAt.Equal(at) {
		t.Fatalf("the first mark was moved to %v", row.RecalledAt)
	}
	if done, err := d.MarkSentAsUserRecalled(ctx, other+100, at); err != nil || done {
		t.Fatalf("a row that is not there: %v %v", done, err)
	}
	if row, _, _ := d.SentAsUserByID(ctx, other); row.RecalledAt != nil {
		t.Fatalf("another row was marked: %v", row.RecalledAt)
	}
	rows, _, _ := d.SentAsUsers(ctx, 0, 10, "")
	if len(rows) != 2 || rows[1].ID != first || rows[1].RecalledAt == nil || rows[0].RecalledAt != nil {
		t.Fatalf("the list: %+v", rows)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
