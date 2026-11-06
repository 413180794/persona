//go:build !windows

package hub

import (
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// syntheticAgentHub uses only demo data, but includes the preview and nested task
// metadata that make production session lists expensive to encode and decode.
func syntheticAgentHub(t *testing.T) *Hub {
	h := newTestHub(t)
	for machine := range 4 {
		mid := fmt.Sprintf("demo-%d", machine)
		m := newMachine(h, MachineConfig{ID: mid, Kind: "local"})
		h.machines, h.byID[mid] = append(h.machines, m), m
		for i := range 2000 {
			state := []string{"needs_you", "working", "unread", "idle"}[i%4]
			m.sessions = append(m.sessions, proto.Session{
				MachineID: mid, SessionID: fmt.Sprintf("session-%04d", i), Title: fmt.Sprintf("Demo task %04d", i),
				Cwd: fmt.Sprintf("/demo/project-%02d", i%20), RepoRoot: fmt.Sprintf("/demo/project-%02d", i%20),
				SpaceID: fmt.Sprintf("space-%02d", i%10), SpaceName: fmt.Sprintf("Demo space %02d", i%10),
				Agent: []string{"claude", "codex"}[i%2], State: state, Status: proto.StatusIdle,
				UpdatedAt: time.Unix(1700000000+int64(i), 0), LastText: strings.Repeat("demo preview ", 50),
				Background: []proto.BackgroundTask{{ID: "demo-task", Kind: "shell", Description: "Demo test run"}},
			})
		}
	}
	return h
}

func BenchmarkAgents8000Legacy(b *testing.B) {
	h := syntheticAgentHub(&testing.T{})
	r := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.listSessions(w, r)
	b.ReportMetric(float64(w.Body.Len()), "JSON-bytes")
	b.ReportAllocs()
	for b.Loop() {
		w := httptest.NewRecorder()
		h.listSessions(w, r)
	}
}

// TestAgents8000Measurement reports actual handler time, uncompressed JSON bytes,
// process CPU and heap for a repeatable synthetic workload, without a live node.
func TestAgents8000Measurement(t *testing.T) {
	h := syntheticAgentHub(t)
	for _, query := range []string{"", "?summary=1&limit=50", "?counts=1"} {
		r := httptest.NewRequest("GET", "/api/v1/sessions"+query, nil)
		w := httptest.NewRecorder()
		coldStart := time.Now()
		h.listSessions(w, r)
		firstRequest := time.Since(coldStart)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
		bytes := w.Body.Len()
		runtime.GC()
		var before, after runtime.MemStats
		var cpuBefore, cpuAfter syscall.Rusage
		runtime.ReadMemStats(&before)
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuBefore)
		start := time.Now()
		const requests = 100
		for range requests {
			w := httptest.NewRecorder()
			h.listSessions(w, r)
		}
		elapsed := time.Since(start)
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &cpuAfter)
		runtime.ReadMemStats(&after)
		cpu := func(r syscall.Rusage) time.Duration {
			return time.Duration(r.Utime.Sec+r.Stime.Sec)*time.Second + time.Duration(r.Utime.Usec+r.Stime.Usec)*time.Microsecond
		}
		runtime.GC()
		var retained runtime.MemStats
		runtime.ReadMemStats(&retained)
		rss := cpuAfter.Maxrss * 1024 // Linux reports KiB; Darwin reports bytes.
		if runtime.GOOS == "darwin" {
			rss = cpuAfter.Maxrss
		}
		t.Logf("GET /api/v1/sessions%s: sessions=8000 requests=%d first_request=%s mean=%s JSON=%d bytes CPU/request=%s allocated/request=%d bytes retained_heap=%d bytes RSS_peak=%d bytes",
			query, requests, firstRequest, elapsed/requests, bytes, (cpu(cpuAfter)-cpu(cpuBefore))/requests, (after.TotalAlloc-before.TotalAlloc)/requests, retained.HeapAlloc, rss)
	}
}
