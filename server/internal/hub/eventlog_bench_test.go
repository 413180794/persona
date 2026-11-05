package hub

import (
	"strings"
	"testing"
	"time"
)

// BenchmarkEventLogAdd measures one broadcast's turn in a full event log, the
// hub's steady state: every add drops the oldest event. It runs under Hub.mu,
// which every broadcast and every connecting app waits on.
func BenchmarkEventLogAdd(b *testing.B) {
	body := []byte(`{"type":"sessions","machine_id":"m1","sessions":[{"id":"s1","title":"` + strings.Repeat("t", 300) + `"}]}`)
	l := newEventLog()
	now := time.Now()
	for range eventLogMax {
		l.add(body, false, now)
	}
	b.ReportAllocs()
	for b.Loop() {
		l.add(body, false, now)
	}
	if len(l.events) != eventLogMax {
		b.Fatalf("log holds %d events", len(l.events))
	}
}
