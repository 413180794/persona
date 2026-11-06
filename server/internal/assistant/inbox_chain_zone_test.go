package assistant

import (
	"strings"
	"testing"
	"time"
)

// A chain entry's time reads in the user's zone, not the machine's (the hub runs in UTC, #5235).
func TestChainLineInTheUsersZone(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	e := InboxChainEntry{At: time.Date(2026, 11, 6, 7, 30, 0, 0, time.UTC), Text: "replied"}
	if got := chainLine(e, loc); !strings.HasPrefix(got, "11-06 15:30 ") {
		t.Fatalf("chainLine = %q, want the time in UTC+8", got)
	}
}
