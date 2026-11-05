package devices

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func pts(ts ...int64) []proto.MetricPoint {
	out := make([]proto.MetricPoint, len(ts))
	for i, t := range ts {
		v := float64(i)
		out[i] = proto.MetricPoint{T: t, CPU: &v}
	}
	return out
}

func gapsAt(points []proto.MetricPoint) []int64 {
	var out []int64
	for _, p := range points {
		if p.Gap {
			out = append(out, p.T)
		}
	}
	return out
}

func TestMetricGaps(t *testing.T) {
	end := time.Unix(1000, 0)
	// One missed sample (a 20 s hole) is bridged; the node away for a minute is a break right after its last point.
	got := withGaps(pts(800, 810, 830, 900, 910, 1000), 10, end)
	if g := gapsAt(got); len(g) != 2 || g[0] != 840 || g[1] != 920 {
		t.Fatalf("gaps at %v in %+v", g, got)
	}
	if len(got) != 8 {
		t.Fatalf("%d points", len(got))
	}
	// Offline now: the series ends with a break, so the chart does not run the last value up to now.
	if g := gapsAt(withGaps(pts(700, 710), 10, end)); len(g) != 1 || g[0] != 720 {
		t.Fatalf("trailing gap %v", g)
	}
	// Still sending: no trailing break for the step in progress.
	if g := gapsAt(withGaps(pts(960, 970, 980), 10, end)); len(g) != 0 {
		t.Fatalf("break while online: %v", g)
	}
	if got := withGaps(nil, 60, end); len(got) != 0 {
		t.Fatalf("empty = %+v", got)
	}
}

func TestMetricsBook(t *testing.T) {
	d := New(Options{})
	now := time.Unix(10_000, 0)
	cpu := 42.0
	b, _ := json.Marshal(map[string]any{"sample": proto.MetricSample{T: now.Unix() - 5, CPU: &cpu, MemTotal: 8}})
	d.onMetrics("m1", b)
	if s := d.metrics.now("m1", now); s == nil || *s.CPU != 42 {
		t.Fatalf("latest = %+v", s)
	}
	if s := d.metrics.now("m1", now.Add(time.Minute)); s != nil {
		t.Fatalf("a minute-old sample is not now: %+v", s)
	}
	d.onMetrics("m1", []byte(`{"sample":{}}`)) // no time: ignored
	if s := d.metrics.now("m1", now); s == nil {
		t.Fatal("an empty sample replaced the latest")
	}
	d.onSystem("m1", &proto.System{OS: "linux", Distro: "ubuntu"})
	if s := d.metrics.system("m1"); s == nil || s.Distro != "ubuntu" {
		t.Fatalf("system = %+v", s)
	}
}
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
