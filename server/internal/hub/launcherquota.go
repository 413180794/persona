package hub

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/quota"
)

// A launcher's quota at its gateway (docs/launchers.md「额度」), read by the quota plugins (docs/plugins.md「额度」). A
// plugin never sees the key: it says what to send, and whoever holds the key sends it — the hub for a launcher added in
// Persona (its sealed key), the machine for a ~/.zshrc one (node/launcherquota.go), so that key stays on that machine.
// Answers are kept a minute: the page opens often, the gateways count every read.

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
	e := quotaEntry{at: time.Now()}
	gw, err := h.launcherGateway(ctx, name, machine)
	if errors.Is(err, errOffline) || errors.Is(err, errNoLauncher) {
		return quotaEntry{}, err
	}
	e.machine = gw.machine
	base, send := gw.base, gw.send
	if err == nil {
		e.report, err = h.plugins.quota(ctx, base, h.userTZName(), send)
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
		if m := h.localMachine(id); m != nil && m.isOnline() {
			return m
		}
	}
	return nil
}

// localMachineID is the machine the hub runs on (kind local), "" when none is configured.
func (h *Hub) localMachineID() string {
	for _, m := range h.machinesNow() {
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

var errNoLauncher = errors.New("no such launcher")

// launcherGateway is where a launcher's gateway is and how to send to it with the key (quota, and plugins' launcher
// sections): the hub for a launcher added in Persona, the machine (machine, or one that has it) for a ~/.zshrc one.
type launcherGatewayInfo struct {
	base    string
	machine string
	send    quotaSender
}

func (h *Hub) launcherGateway(ctx context.Context, name, machine string) (launcherGatewayInfo, error) {
	var out launcherGatewayInfo
	var view *LauncherView
	for _, v := range h.launcherViews(ctx) {
		if v.Name == name {
			v := v
			view = &v
		}
	}
	if view == nil {
		return out, errNoLauncher
	}
	if view.Source == "jarvis" {
		for _, s := range h.storedLaunchers(ctx) {
			if s.Name != name {
				continue
			}
			key, err := h.openKey(s.KeySealed)
			if err != nil {
				return out, err
			}
			base := s.BaseURL
			out.machine, out.base = h.localMachineID(), base
			client := &http.Client{Timeout: 25 * time.Second}
			out.send = func(ctx context.Context, r quota.Request) (quota.Response, error) {
				return quota.Do(ctx, client, base, key, r)
			}
		}
		if out.base == "" {
			return out, errors.New("这个启动器没有设网关地址（用的是默认的官方接口）")
		}
		return out, nil
	}
	m := h.quotaMachine(ctx, view.Machines, machine)
	if m == nil {
		return out, errOffline
	}
	out.machine = m.cfg.ID
	var gw struct {
		BaseURL string `json:"base_url"`
	}
	if err := m.call(ctx, "launchers.http", map[string]any{"name": name}, &gw); err != nil {
		if strings.Contains(err.Error(), "unknown method") {
			return out, errors.New(m.cfg.Name + " 上的 node 太旧：更新后才能查额度")
		}
		return out, err
	}
	out.base = gw.BaseURL
	out.send = func(ctx context.Context, r quota.Request) (quota.Response, error) {
		var resp quota.Response
		err := m.call(ctx, "launchers.http", map[string]any{"name": name, "http": r}, &resp)
		return resp, err
	}
	return out, nil
}
