package hub

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// Machine monitoring (docs/machines.md): every node sends a sample every 10 s (sysmon); the hub keeps the latest in
// memory for the live values and every sample in Postgres for the charts: raw for the latest hour, then one row a
// minute, nothing older than a day.

const (
	metricsRawFor   = time.Hour
	metricsKeep     = 24*time.Hour + 10*time.Minute // a 24-hour chart starts full
	metricsTidyEach = time.Minute
	// metricsFresh: a latest sample older than this is not "now" any more (the node stopped sending).
	metricsFresh = 3 * proto.MetricEvery
)

type metricsStore interface {
	PutMachineSystem(ctx context.Context, machine string, s proto.System) error
	MachineSystems(ctx context.Context) (map[string]proto.System, error)
	InsertMetric(ctx context.Context, machine string, s proto.MetricSample) error
	RollupMetrics(ctx context.Context, before time.Time) (int64, error)
	PruneMetrics(ctx context.Context, before time.Time) (int64, error)
	MetricSeries(ctx context.Context, machines []string, from, to time.Time, step int) (map[string][]proto.MetricPoint, error)
}

type metricsBook struct {
	mu      sync.Mutex
	latest  map[string]proto.MetricSample
	systems map[string]proto.System
}

func (b *metricsBook) system(machine string) *proto.System {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.systems[machine]; ok {
		return &s
	}
	return nil
}

func (b *metricsBook) setSystem(machine string, s proto.System) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.systems == nil {
		b.systems = map[string]proto.System{}
	}
	b.systems[machine] = s
}

// now is a machine's latest sample if it is recent.
func (b *metricsBook) now(machine string, at time.Time) *proto.MetricSample {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.latest[machine]
	if !ok || at.Sub(time.Unix(s.T, 0)) > metricsFresh {
		return nil
	}
	return &s
}

func (h *Hub) metricsStore() metricsStore {
	if s, ok := h.store.(metricsStore); ok {
		return s
	}
	return nil
}

// onSystem keeps what a node said it is (node.info) for when it is offline.
func (h *Hub) onSystem(machine string, s *proto.System) {
	if s == nil {
		return
	}
	h.metrics.setSystem(machine, *s)
	if st := h.metricsStore(); st != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := st.PutMachineSystem(ctx, machine, *s); err != nil {
			h.logf("[%s] save system: %v", machine, err)
		}
	}
}

// onMetrics takes a node's "metrics" notification.
func (h *Hub) onMetrics(machine string, params json.RawMessage) {
	var p struct {
		Sample proto.MetricSample `json:"sample"`
	}
	if json.Unmarshal(params, &p) != nil || p.Sample.T == 0 {
		return
	}
	h.metrics.mu.Lock()
	if h.metrics.latest == nil {
		h.metrics.latest = map[string]proto.MetricSample{}
	}
	h.metrics.latest[machine] = p.Sample
	h.metrics.mu.Unlock()
	if st := h.metricsStore(); st != nil {
		go func() { // not on the node's read loop
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := st.InsertMetric(ctx, machine, p.Sample); err != nil {
				h.logf("[%s] save metrics: %v", machine, err)
			}
		}()
	}
}

// metricsLoop loads the machines' systems, then each minute rolls up the raw rows past the hour and drops what is
// older than a day.
func (h *Hub) metricsLoop(ctx context.Context) {
	st := h.metricsStore()
	if st == nil {
		return
	}
	if systems, err := st.MachineSystems(ctx); err == nil {
		for id, s := range systems {
			if h.metrics.system(id) == nil { // a node that connected meanwhile is newer
				h.metrics.setSystem(id, s)
			}
		}
		h.broadcastMachines()
	}
	t := time.NewTicker(metricsTidyEach)
	defer t.Stop()
	for {
		now := time.Now()
		if _, err := st.RollupMetrics(ctx, now.Add(-metricsRawFor)); err != nil && ctx.Err() == nil {
			h.logf("metrics rollup: %v", err)
		}
		if _, err := st.PruneMetrics(ctx, now.Add(-metricsKeep)); err != nil && ctx.Err() == nil {
			h.logf("metrics prune: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// metricsRanges: what a chart can show, and its step (about 360 points each). The 1-hour chart is raw.
var metricsRanges = map[string]struct {
	span time.Duration
	step int
}{
	"1h":  {time.Hour, db.RawMetricStep},
	"6h":  {6 * time.Hour, 60},
	"24h": {24 * time.Hour, 240},
}

// withGaps puts a break where a machine sent nothing for more than three steps (offline, the hub restarting): a
// line drawn across it would claim values nobody measured. Short holes (a missed sample) are bridged. A break also
// ends the series when the latest point is that old (the machine is offline now).
func withGaps(points []proto.MetricPoint, step int, end time.Time) []proto.MetricPoint {
	limit := int64(3 * step)
	out := make([]proto.MetricPoint, 0, len(points)+2)
	for i, p := range points {
		if i > 0 && p.T-points[i-1].T > limit {
			out = append(out, proto.MetricPoint{T: points[i-1].T + int64(step), Gap: true})
		}
		out = append(out, roundPoint(p))
	}
	if n := len(points); n > 0 && end.Unix()-points[n-1].T > limit+int64(step) {
		out = append(out, proto.MetricPoint{T: points[n-1].T + int64(step), Gap: true})
	}
	return out
}

func roundPoint(p proto.MetricPoint) proto.MetricPoint {
	r := func(v *float64, digits float64) *float64 {
		if v == nil {
			return nil
		}
		x := math.Round(*v*digits) / digits
		return &x
	}
	p.CPU, p.Load = r(p.CPU, 10), r(p.Load, 100)
	p.Mem, p.Swap, p.Rx, p.Tx, p.Disk = r(p.Mem, 1), r(p.Swap, 1), r(p.Rx, 1), r(p.Tx, 1), r(p.Disk, 1)
	return p
}

// MachineMetrics is a machine's page of history (GET /api/v1/machines/{id}/metrics) or its dashboard card.
type MachineMetrics struct {
	MachineID string              `json:"machine_id"`
	Online    bool                `json:"online"`
	System    *proto.System       `json:"system,omitempty"`
	Latest    *proto.MetricSample `json:"latest,omitempty"` // nil when the node has not sent anything lately
	Points    []proto.MetricPoint `json:"points"`
}

func (h *Hub) machineMetrics(ctx context.Context, ids []string, span time.Duration, step int, now time.Time) ([]MachineMetrics, error) {
	series := map[string][]proto.MetricPoint{}
	if st := h.metricsStore(); st != nil {
		// From a step boundary, so the first step is not a partial average.
		from := now.Add(-span).Truncate(time.Duration(step) * time.Second)
		var err error
		if series, err = st.MetricSeries(ctx, ids, from, now.Add(time.Minute), step); err != nil {
			return nil, err
		}
	}
	out := make([]MachineMetrics, 0, len(ids))
	for _, id := range ids {
		m := h.localMachine(id)
		if m == nil {
			continue
		}
		out = append(out, MachineMetrics{MachineID: id, Online: m.snapshot().Online, System: h.metrics.system(id),
			Latest: h.metrics.now(id, now), Points: withGaps(series[id], step, now)})
	}
	return out, nil
}

func (h *Hub) metricsRoutes(api *http.ServeMux) {
	// The dashboard: every machine's latest sample and its last hour by minute (the sparklines).
	api.HandleFunc("GET /api/v1/metrics", func(w http.ResponseWriter, r *http.Request) {
		machines := h.machinesNow()
		ids := make([]string, 0, len(machines))
		for _, m := range machines {
			ids = append(ids, m.cfg.ID)
		}
		list, err := h.machineMetrics(r.Context(), ids, time.Hour, 60, time.Now())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"machines": list, "step": 60})
	})
	api.HandleFunc("GET /api/v1/machines/{id}/metrics", func(w http.ResponseWriter, r *http.Request) {
		m := h.localMachine(r.PathValue("id"))
		if m == nil {
			writeErr(w, http.StatusNotFound, errors.New("no such machine"))
			return
		}
		id := m.cfg.ID
		name := r.URL.Query().Get("range")
		if name == "" {
			name = "1h"
		}
		rg, ok := metricsRanges[name]
		if !ok {
			writeErr(w, http.StatusBadRequest, errors.New("range is 1h, 6h or 24h"))
			return
		}
		list, err := h.machineMetrics(r.Context(), []string{id}, rg.span, rg.step, time.Now())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if len(list) == 0 { // removed or unpaired while the history was read
			writeErr(w, http.StatusNotFound, errors.New("no such machine"))
			return
		}
		writeJSON(w, map[string]any{"metrics": list[0], "range": name, "step": rg.step})
	})
}
