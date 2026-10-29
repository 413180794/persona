package hub

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestUsageSummary(t *testing.T) {
	loc := time.FixedZone("test", 8*3600)
	now := time.Date(2026, 10, 29, 9, 0, 30, 0, loc) // 30 s into the minute
	minute := now.Unix() / 60 * 60
	var book usageBook
	report := func(machine string, full bool, buckets ...proto.UsageBucket) {
		b, _ := json.Marshal(map[string]any{"buckets": buckets, "full": full, "load": 4.0, "cpus": 8})
		book.apply(machine, b, now)
	}
	report("a", true,
		proto.UsageBucket{Minute: minute, Model: "claude-opus-5-5", Input: 1_000_000, Output: 100, Requests: 1},
		proto.UsageBucket{Minute: minute - 60, Model: "claude-opus-5-5", CacheWrite: 1_000_000, Output: 400, Requests: 2},
		proto.UsageBucket{Minute: minute - 10*3600, Model: "claude-opus-5-5", Output: 999, Requests: 9}, // yesterday here
	)
	report("b", true, proto.UsageBucket{Minute: minute, Model: "mystery", CacheRead: 50, Output: 250, Requests: 1})
	// A later report replaces a minute's total instead of adding to it.
	report("b", false, proto.UsageBucket{Minute: minute, Model: "mystery", CacheRead: 50, Output: 500, Requests: 2})

	s := book.summary(now, loc)
	if s.Requests != 5 || s.Output != 1000 || s.Tokens != 2_001_050 {
		t.Fatalf("today = %+v", s)
	}
	// 1000 output tokens over the last 2.5 minutes.
	if s.TPM != 400 || len(s.Machines) != 2 || s.Machines[0].Load != 0.5 {
		t.Fatalf("tpm / machines = %d %+v", s.TPM, s.Machines)
	}
	// A full report (the node reconnected) replaces everything.
	report("b", true)
	if s := book.summary(now, loc); s.Output != 500 {
		t.Fatalf("after a full report: %+v", s)
	}
}
