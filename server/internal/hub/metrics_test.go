package hub

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
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
	h := &Hub{}
	now := time.Unix(10_000, 0)
	cpu := 42.0
	b, _ := json.Marshal(map[string]any{"sample": proto.MetricSample{T: now.Unix() - 5, CPU: &cpu, MemTotal: 8}})
	h.onMetrics("m1", b)
	if s := h.metrics.now("m1", now); s == nil || *s.CPU != 42 {
		t.Fatalf("latest = %+v", s)
	}
	if s := h.metrics.now("m1", now.Add(time.Minute)); s != nil {
		t.Fatalf("a minute-old sample is not now: %+v", s)
	}
	h.onMetrics("m1", []byte(`{"sample":{}}`)) // no time: ignored
	if s := h.metrics.now("m1", now); s == nil {
		t.Fatal("an empty sample replaced the latest")
	}
	h.onSystem("m1", &proto.System{OS: "linux", Distro: "ubuntu"})
	if s := h.metrics.system("m1"); s == nil || s.Distro != "ubuntu" {
		t.Fatalf("system = %+v", s)
	}
}
