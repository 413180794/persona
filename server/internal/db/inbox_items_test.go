package db

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// inboxRows is n inbox items about the size of a Feishu chat's (its JSON with the last messages and the triage).
func inboxRows(n int, now time.Time) []InboxRow {
	rows := make([]InboxRow, n)
	for i := range rows {
		id := "feishu:chat-" + strconv.Itoa(i)
		rows[i] = InboxRow{ID: id, Data: []byte(fmt.Sprintf(`{"id":%q,"title":"示例群","last":"%0400d"}`, id, i)), UpdatedAt: now}
	}
	return rows
}

// PutInboxItems stores new items and updates those it has, as PutInboxItem does, and stores none when one fails.
func TestPutInboxItems(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now()
	if err := d.PutInboxItem(ctx, "feishu:chat-1", []byte(`{"id":"feishu:chat-1"}`), now, nil); err != nil {
		t.Fatal(err)
	}
	rows := inboxRows(3, now)
	rows[1].Data, rows[1].DoneAt = []byte(`{"id":"feishu:chat-1","count":2}`), &now
	if err := d.PutInboxItems(ctx, rows); err != nil {
		t.Fatal(err)
	}
	got, err := d.InboxItems(ctx, now.Add(-time.Hour))
	if err != nil || len(got) != 3 {
		t.Fatalf("rows = %d, %v", len(got), err)
	}
	var done int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM inbox_items WHERE done_at IS NOT NULL AND data->>'count' = '2'`).Scan(&done); err != nil || done != 1 {
		t.Fatalf("updated row: %d, %v", done, err)
	}
	bad := inboxRows(2, now)
	bad[0].ID, bad[1].ID, bad[1].Data = "x:1", "x:2", []byte(`not json`)
	if err := d.PutInboxItems(ctx, bad); err == nil {
		t.Fatal("a row that is not JSON was stored")
	}
	if got, _ := d.InboxItems(ctx, now.Add(-time.Hour)); len(got) != 3 {
		t.Fatalf("rows after a failed batch = %d, want the batch stored whole or not at all", len(got))
	}
	if err := d.PutInboxItems(ctx, nil); err != nil {
		t.Fatal(err)
	}
}
