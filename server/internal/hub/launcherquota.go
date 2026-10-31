package hub

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/quota"
)

// A launcher's quota at its gateway (docs/launchers.md「额度」): relay-history and FAKEMODEL keys. A launcher added in
// Jarvis is read here with its sealed key; a ~/.zshrc one by the machine that has it (node/launcherquota.go), so the
// key stays on that machine. Answers are kept a minute: the page opens often, the gateways count every read.

const quotaFresh = time.Minute

type quotaEntry struct {
	report  quota.Report
	machine string
	err     string
	at      time.Time
}

type quotaCache struct {
	mu sync.Mutex
	m  map[string]quotaEntry
}

func (c *quotaCache) get(name string) (quotaEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[name]
	return e, ok && time.Since(e.at) < quotaFresh
}

func (c *quotaCache) put(name string, e quotaEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]quotaEntry{}
	}
	c.m[name] = e
}

// launcherQuota reads (or recalls) one launcher's quota; machine picks which machine reads a ~/.zshrc launcher.
func (h *Hub) launcherQuota(ctx context.Context, name, machine string, refresh bool) (quotaEntry, error) {
	if e, ok := h.quotas.get(name); ok && !refresh && (machine == "" || machine == e.machine) {
		return e, nil
	}
	var view *LauncherView
	for _, v := range h.launcherViews(ctx) {
		if v.Name == name {
			v := v
			view = &v
		}
	}
	if view == nil {
		return quotaEntry{}, errors.New("no such launcher")
	}
	e := quotaEntry{at: time.Now()}
	var err error
	if view.Source == "jarvis" {
		for _, s := range h.storedLaunchers(ctx) {
			if s.Name != name {
				continue
			}
			key, kerr := h.openKey(s.KeySealed)
			if kerr != nil {
				return quotaEntry{}, kerr
			}
			e.machine = h.localMachineID()
			e.report, err = quota.Fetch(ctx, &http.Client{Timeout: 25 * time.Second}, s.BaseURL, key)
		}
	} else {
		m := h.quotaMachine(ctx, view.Machines, machine)
		if m == nil {
			return quotaEntry{}, errOffline
		}
		e.machine = m.cfg.ID
		var res struct {
			Quota quota.Report `json:"quota"`
		}
		err = m.call(ctx, "launchers.quota", map[string]any{"name": name}, &res)
		e.report = res.Quota
	}
	if err != nil {
		e.err = err.Error()
		if !strings.Contains(e.err, quota.ErrUnsupported.Error()) && ctx.Err() != nil {
			return quotaEntry{}, err // cancelled: nothing to remember
		}
	}
	h.quotas.put(name, e)
	return e, nil
}

// quotaMachine: the one asked for when it has the launcher, else the hub's own machine, else the first online one.
func (h *Hub) quotaMachine(ctx context.Context, has []string, want string) *Machine {
	order := []string{want, h.assistantConfig(ctx).MachineID, h.localMachineID()}
	order = append(order, has...)
	for _, id := range order {
		if id == "" || !slices.Contains(has, id) {
			continue
		}
		if m := h.byID[id]; m != nil && m.isOnline() {
			return m
		}
	}
	return nil
}

// localMachineID is the machine the hub runs on (kind local), "" when none is configured.
func (h *Hub) localMachineID() string {
	for _, m := range h.machines {
		if m.cfg.Kind == "local" {
			return m.cfg.ID
		}
	}
	return ""
}

func (m *Machine) isOnline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online
}

func (h *Hub) launcherQuotaRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/launchers/{name}/quota", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		e, err := h.launcherQuota(ctx, r.PathValue("name"), r.URL.Query().Get("machine"), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			callErr(w, err)
			return
		}
		out := map[string]any{"machine": e.machine, "checked_at": e.at.UTC()}
		if e.err != "" {
			// Not a failure of this request: the gateway has nothing to read, or said no; the page shows why.
			out["error"] = e.err
			out["unsupported"] = strings.Contains(e.err, quota.ErrUnsupported.Error()) || strings.Contains(e.err, "没有设网关地址")
		} else {
			out["quota"] = e.report
		}
		writeJSON(w, out)
	})
}
