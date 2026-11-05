package db

import (
	"context"
	"testing"
	"time"
)

func TestLockTestNextDue(t *testing.T) {
	d, ctx := open(t), context.Background()
	if next, err := d.NextLockTestDue(ctx); err != nil || next != nil {
		t.Fatalf("empty table: %v, %v", next, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i, due := range []time.Time{now.Add(time.Hour), now.Add(time.Minute), now.Add(-time.Minute)} {
		test := LockTest{TestID: "lt_next_" + string(rune('a'+i)), DeviceID: "dev", CreatedAt: now.Add(-time.Hour), DueAt: due}
		if err := d.CreateLockTest(ctx, test); err != nil {
			t.Fatal(err)
		}
	}
	// The overdue test is no longer scheduled once it has been attempted.
	if ok, err := d.AttemptLockTest(ctx, "lt_next_c", 0, now); err != nil || !ok {
		t.Fatalf("attempt: %v, %v", ok, err)
	}
	if next, err := d.NextLockTestDue(ctx); err != nil || next == nil || !next.Equal(now.Add(time.Minute)) {
		t.Fatalf("next = %v, %v; want %v", next, err, now.Add(time.Minute))
	}
}
