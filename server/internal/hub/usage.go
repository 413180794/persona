package hub

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Token usage across the machines, for the Dynamic Island (docs/live-activity.md): every node meters its
// transcripts per minute and model (node/usage.go) and reports changed minutes; the hub keeps the last day of them,
// in memory only (a new node connection resends its whole window), and sums them up.

type usageKey struct {
	minute int64
	model  string
}

type machineUsage struct {
	buckets map[usageKey]proto.UsageBucket
	load    float64
	cpus    int
	at      time.Time // last report
}

type usageBook struct {
	mu       sync.Mutex
	machines map[string]*machineUsage
}

// apply takes a node's "usage" notification.
func (u *usageBook) apply(machine string, params json.RawMessage, now time.Time) bool {
	var p struct {
		Buckets []proto.UsageBucket `json:"buckets"`
		Full    bool                `json:"full"`
		Load    float64             `json:"load"`
		CPUs    int                 `json:"cpus"`
	}
	if json.Unmarshal(params, &p) != nil {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.machines == nil {
		u.machines = map[string]*machineUsage{}
	}
	m := u.machines[machine]
	if m == nil || p.Full {
		m = &machineUsage{buckets: map[usageKey]proto.UsageBucket{}}
		u.machines[machine] = m
	}
	for _, b := range p.Buckets {
		m.buckets[usageKey{b.Minute, b.Model}] = b
	}
	m.load, m.cpus, m.at = p.Load, p.CPUs, now
	cutoff := now.Add(-26*time.Hour).Unix() / 60 * 60
	for k := range m.buckets {
		if k.minute < cutoff {
			delete(m.buckets, k)
		}
	}
	return true
}

// UsageSummary is what the island and the app show.
type UsageSummary struct {
	TPM      int64          `json:"tpm"`      // output tokens per minute, over the last two to three minutes
	Tokens   int64          `json:"tokens"`   // today: all tokens, cache reads included
	Output   int64          `json:"output"`   // today
	Requests int            `json:"requests"` // today
	Machines []MachineUsage `json:"machines"`
}

// MachineUsage is one machine's share and load; Load is the 1-minute load average over its cores, 0 when unknown.
type MachineUsage struct {
	ID     string  `json:"id"`
	TPM    int64   `json:"tpm"`
	Tokens int64   `json:"tokens"`
	Load   float64 `json:"load"`
}

// tpmSpan: TPM is averaged from the start of the minute two minutes ago until now (2–3 minutes), so it neither
// drops to 0 between replies nor lags much behind.
const tpmSpan = 2

func (u *usageBook) summary(now time.Time, loc *time.Location) UsageSummary {
	y, mo, d := now.In(loc).Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, loc).Unix()
	from := now.Unix()/60*60 - tpmSpan*60
	elapsed := float64(now.Unix()-from) / 60
	out := UsageSummary{Machines: []MachineUsage{}}
	u.mu.Lock()
	defer u.mu.Unlock()
	for id, m := range u.machines {
		mu := MachineUsage{ID: id}
		if now.Sub(m.at) < 2*time.Minute && m.cpus > 0 {
			mu.Load = m.load / float64(m.cpus)
		}
		var recent int64
		for k, b := range m.buckets {
			if k.minute >= from {
				recent += b.Output
			}
			if k.minute < today {
				continue
			}
			tokens := b.Input + b.CacheWrite + b.CacheRead + b.Output
			mu.Tokens += tokens
			out.Tokens += tokens
			out.Output += b.Output
			out.Requests += b.Requests
		}
		mu.TPM = int64(float64(recent) / elapsed)
		out.TPM += mu.TPM
		out.Machines = append(out.Machines, mu)
	}
	sort.Slice(out.Machines, func(i, j int) bool { return out.Machines[i].ID < out.Machines[j].ID })
	return out
}

func (h *Hub) usageSummary() UsageSummary {
	return h.usage.summary(time.Now(), h.userLocation())
}

func (h *Hub) usageRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"usage": h.usageSummary()})
	})
}
