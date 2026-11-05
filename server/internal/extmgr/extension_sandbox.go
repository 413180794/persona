package extmgr

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/egress"
	"github.com/nwverse/persona/server/internal/extension/host"
	"github.com/nwverse/persona/server/internal/extension/sandbox"
	"github.com/nwverse/persona/server/internal/i18n"
)

// An installed extension's program runs in a sandbox, reaching out only through the hub's egress proxy and only to the
// hosts of its permissions.network (docs/extensions.md「沙箱」「出网」). Whether it does: the owner's choice for it,
// else on unless it came from a trusted market (the official ones, and those hub.json trusts by their signing keys).
// The extensions already installed when this came keep running as they did (seeded off).

// sandboxProbe is what this machine can do (tests replace it).
var sandboxProbe = sandbox.Probe

// extensionSandboxKey is the hub setting with the owner's choices.
const extensionSandboxKey = "extension_sandbox"

// officialMarketKeys are the key ids of the official markets' signing keys (their market.pub files; not secrets).
var officialMarketKeys = []string{"32ed46cdbe349656", "6b540324aac0e252"}

type sandboxSettings struct {
	// Seeded: the extensions installed before sandboxes were recorded off, once.
	Seeded bool `json:"seeded"`
	// On is the owner's choice per extension; one not in it follows the default.
	On map[string]bool `json:"on,omitempty"`
}

// extensionSandboxes is the hub's side: the choices, the proxy.
type extensionSandboxes struct {
	mu       sync.Mutex
	settings sandboxSettings
	proxy    *egress.Proxy
	proxyErr error
	once     sync.Once
	// noDefault: nothing is sandboxed unless switched on (the package's tests, whose test programs live outside their
	// packages).
	noDefault bool
}

// SandboxView is an installed extension's sandbox as the apps and the admin UI show it.
type SandboxView struct {
	// Enabled: it runs in a sandbox (or would, when the machine has none: Active false, Why says why).
	Enabled bool `json:"enabled"`
	// Default is what it would be without the owner's choice; Chosen whether there is one.
	Default bool   `json:"default"`
	Chosen  bool   `json:"chosen"`
	Active  bool   `json:"active"`
	Why     string `json:"why,omitempty"`
	// Trusted: it came from a trusted market.
	Trusted bool          `json:"trusted,omitempty"`
	Egress  egress.Counts `json:"egress"`
}

type sandboxState struct {
	SandboxView
	applies bool
}

// loadSandboxes reads the owner's choices; the first time, the installed extensions are recorded off: they keep
// running as they did before there were sandboxes.
func (ps *extensions) loadSandboxes(ctx context.Context) {
	if ps.h == nil || ps.h.store == nil {
		return
	}
	sb := &ps.sandboxes
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if _, err := ps.h.store.Setting(ctx, extensionSandboxKey, &sb.settings); err != nil {
		ps.logf("extensions: sandbox settings: %v", err)
		return
	}
	if sb.settings.Seeded {
		return
	}
	if sb.settings.On == nil {
		sb.settings.On = map[string]bool{}
	}
	for _, p := range ps.reg.List() {
		if sandboxApplies(p) {
			if _, ok := sb.settings.On[p.Record.ID]; !ok {
				sb.settings.On[p.Record.ID] = false
				ps.logf("extension %s: installed before sandboxes, keeps running without one (personad extension sandbox %s on)", p.Record.ID, p.Record.ID)
			}
		}
	}
	sb.settings.Seeded = true
	if err := ps.h.store.PutSetting(ctx, extensionSandboxKey, sb.settings); err != nil {
		ps.logf("extensions: sandbox settings: %v", err)
	}
}

// sandboxApplies: the hub runs p's program from a package it installed (not built in, not hub.json's, not elsewhere).
func sandboxApplies(p *extension.Extension) bool {
	return p != nil && p.Record.Source == extension.SourceInstalled && p.Record.Remote == nil && p.Manifest.Runtime != nil
}

// sandboxOf is whether p runs in a sandbox, and why.
func (ps *extensions) sandboxOf(ctx context.Context, p *extension.Extension) sandboxState {
	if !sandboxApplies(p) {
		return sandboxState{}
	}
	st := sandboxState{applies: true}
	st.Trusted = ps.fromTrustedMarket(ctx, p)
	st.Default = !st.Trusted && !ps.sandboxes.noDefault
	ps.sandboxes.mu.Lock()
	on, chosen := ps.sandboxes.settings.On[p.Record.ID]
	ps.sandboxes.mu.Unlock()
	st.Enabled, st.Chosen = st.Default, chosen
	if chosen {
		st.Enabled = on
	}
	if st.Enabled {
		sup := sandboxProbe()
		st.Active = sup.Available()
		if !st.Active {
			st.Why = i18n.T(ps.h.contextLang(ctx), "hub-extension_sandbox.unavailable", i18n.Args{"why": sup.Why})
		}
	}
	if px := ps.sandboxes.proxyIfStarted(); px != nil {
		st.Egress = px.Stats(p.Record.ID)
	}
	return st
}

// fromTrustedMarket: p was installed from a market whose signing key the hub trusts.
func (ps *extensions) fromTrustedMarket(ctx context.Context, p *extension.Extension) bool {
	mid := extension.FromMarket(p.Record.Origin)
	if mid == "" || ps.h == nil || ps.h.marketsLoad(ctx) != nil {
		return false
	}
	ms := ps.h.marketsState()
	ms.mu.Lock()
	s := ms.byID[mid]
	key := ""
	if s != nil {
		key = s.rec.SigningKey
	}
	ms.mu.Unlock()
	if key == "" {
		return false
	}
	return slices.Contains(ps.trustedMarketKeys(), keyIDOf(key))
}

// trustedMarketKeys are the key ids the hub trusts: the official ones and hub.json's trusted_extension_markets (key
// ids, or ed25519:<key> lines from a market.pub).
func (ps *extensions) trustedMarketKeys() []string {
	out := slices.Clone(officialMarketKeys)
	if ps.h == nil || ps.h.cfg == nil {
		return out
	}
	for _, k := range ps.h.cfg.TrustedExtensionMarkets {
		k = strings.TrimSpace(k)
		if strings.HasPrefix(k, "ed25519:") {
			k = keyIDOf(k)
		}
		if k != "" {
			out = append(out, strings.ToLower(k))
		}
	}
	return out
}

func (sb *extensionSandboxes) proxyIfStarted() *egress.Proxy {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.proxy
}

// egress is the hub's egress proxy, started the first time an extension needs it.
func (ps *extensions) egress() (*egress.Proxy, error) {
	sb := &ps.sandboxes
	sb.once.Do(func() {
		hubAddr := "127.0.0.1:18660"
		if ps.h != nil && ps.h.cfg != nil {
			if u, err := url.Parse(hubLocalURL(ps.h.cfg.Listen)); err == nil {
				hubAddr = u.Host
			}
		}
		px := &egress.Proxy{HubAddr: hubAddr, Logf: ps.logf}
		if ps.stats != nil { // the admin UI's counts (extension_metrics.go)
			stats := ps.stats
			px.OnDecision = func(ext, _ string, allowed bool) {
				name := "egress.denied"
				if allowed {
					name = "egress.allowed"
				}
				stats.of(ext).Count(name, 1)
			}
		}
		err := px.Start()
		sb.mu.Lock()
		if err == nil {
			sb.proxy = px
		}
		sb.proxyErr = err
		sb.mu.Unlock()
	})
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.proxy, sb.proxyErr
}

// forgetEgress: p's program stopped for good this run (switched off, upgraded, removed); a new start registers anew.
func (ps *extensions) forgetEgress(id string) {
	if px := ps.sandboxes.proxyIfStarted(); px != nil {
		px.Unregister(id)
	}
}

// forgetSandbox drops the owner's choice for an extension no longer installed: installed again, it starts from the
// default.
func (ps *extensions) forgetSandbox(ctx context.Context, id string) {
	if id != "" && ps.root != "" {
		_ = os.RemoveAll(filepath.Join(ps.root, "run", id)) // its sockets
	}
	sb := &ps.sandboxes
	sb.mu.Lock()
	_, ok := sb.settings.On[id]
	if ok {
		delete(sb.settings.On, id)
	}
	settings := sb.settings
	sb.mu.Unlock()
	if ok && ps.h != nil && ps.h.store != nil {
		if err := ps.h.store.PutSetting(ctx, extensionSandboxKey, settings); err != nil {
			ps.logf("extensions: sandbox settings: %v", err)
		}
	}
}

// setSandbox records the owner's choice (nil: back to the default) and restarts it.
func (ps *extensions) setSandbox(ctx context.Context, p *extension.Extension, on *bool, who string) error {
	if !sandboxApplies(p) {
		return i18n.Err("hub-extension_sandbox.not_applicable")
	}
	if ps.h == nil || ps.h.store == nil {
		return i18n.Err("hub-extension_sandbox.no_database")
	}
	sb := &ps.sandboxes
	sb.mu.Lock()
	if sb.settings.On == nil {
		sb.settings.On = map[string]bool{}
	}
	if on == nil {
		delete(sb.settings.On, p.Record.ID)
	} else {
		sb.settings.On[p.Record.ID] = *on
	}
	sb.settings.Seeded = true
	settings := sb.settings
	sb.mu.Unlock()
	if err := ps.h.store.PutSetting(ctx, extensionSandboxKey, settings); err != nil {
		return err
	}
	st := ps.sandboxOf(ctx, p)
	ps.reg.Audit(ctx, p.Record.ID, "sandbox", who, map[string]any{"enabled": st.Enabled, "chosen": st.Chosen})
	ps.logf("extension %s: sandbox %v (by %s)", p.Record.ID, st.Enabled, who)
	run := ps.ctx // its program lives as long as the extensions do, not as long as this request
	if run == nil {
		run = context.Background()
	}
	ps.apply(run, p.Record.ID)
	return nil
}

// confine puts the sandbox and the egress proxy around s, the way p's program runs: in a sandbox, its HTTP socket
// moves to its run directory, the hub's API is HubHost through the proxy, and the program runs inside; with a sandbox
// on but none on this machine, it is at least pointed at the proxy.
func (ps *extensions) confine(ctx context.Context, p *extension.Extension, hp *hubExtension, s *host.Spec) error {
	st := ps.sandboxOf(ctx, p)
	if !st.applies || !st.Enabled {
		return nil
	}
	px, err := ps.egress()
	if err != nil {
		return err
	}
	id := p.Record.ID
	g := p.Record.Granted
	rule := egress.Rule{Hosts: slices.Clone(g.Network), Hub: g.FullHub() || len(g.Hub) > 0}
	if !st.Active {
		start := s.StartEnv
		s.StartEnv = func() []string {
			var env []string
			if start != nil {
				env = start()
			}
			return append(env, sandbox.ProxyEnv(px.Register(id, rule))...)
		}
		return nil
	}
	run := filepath.Join(ps.root, "run", id)
	if err := os.MkdirAll(run, 0o700); err != nil {
		return err
	}
	if s.HTTP {
		sock := filepath.Join(run, "http.sock")
		hp.socket, s.Socket = sock, sock
		s.Env = setEnv(s.Env, "PERSONA_PLUGIN_SOCKET", sock)
	}
	hubURL := "http://" + egress.HubHost
	s.Env = setEnv(s.Env, "JARVIS_HUB_URL", hubURL)
	if start := s.StartEnv; start != nil {
		s.StartEnv = func() []string { return setEnv(start(), "JARVIS_HUB_URL", hubURL) }
	}
	if initf := s.Init; initf != nil {
		s.Init = func(ctx context.Context) (extension.InitializeParams, error) {
			params, err := initf(ctx)
			if params.Hub.URL != "" {
				params.Hub.URL = hubURL
			}
			return params, err
		}
	}
	limits := p.Manifest.Runtime.Resources.Limits()
	sup := sandboxProbe()
	pkg, data := p.Dir, ps.reg.DataDir(id)
	s.Wrap = func(command string, args []string) (string, []string, []string, error) {
		proxyURL := px.Register(id, rule)
		plan := sandbox.Plan{ID: id, Command: command, Args: args, Pkg: pkg, Data: data, Run: run,
			MemoryMB: limits.MemoryMB, CPUPercent: limits.CPUPercent, Scope: sup.Scope, Env: os.Environ()}
		if runtime.GOOS == "darwin" {
			plan.ProxyURL = proxyURL
			name, a, env := sandbox.Darwin(plan)
			return name, a, env, nil
		}
		plan.ProxySocket, plan.Self = filepath.Join(run, "proxy.sock"), selfExecutable()
		if err := px.ListenUnix(id, plan.ProxySocket); err != nil {
			return "", nil, nil, err
		}
		name, a, env := sandbox.Linux(sup.Bwrap, plan)
		return name, a, env, nil
	}
	return nil
}

// setEnv is env with name set to value where it is there (and its PERSONA_ / JARVIS_ twin).
func setEnv(env []string, name, value string) []string {
	twin := ""
	if rest, ok := strings.CutPrefix(name, "JARVIS_"); ok {
		twin = "PERSONA_" + rest
	} else if rest, ok := strings.CutPrefix(name, "PERSONA_"); ok {
		twin = "JARVIS_" + rest
	}
	out := slices.Clone(env)
	for i, kv := range out {
		k, _, _ := strings.Cut(kv, "=")
		if k == name || k == twin {
			out[i] = k + "=" + value
		}
	}
	return out
}

// sandboxRoutes: the owner turns an extension's sandbox on or off (this machine's master token, or the admin UI).
func (h *Manager) sandboxRoutes(api *http.ServeMux) {
	api.HandleFunc("PUT /api/v1/extensions/{id}/-/sandbox", func(w http.ResponseWriter, r *http.Request) {
		if !h.fromThisMachine(r) {
			writeErrCode(w, http.StatusForbidden, "this_machine_only", i18n.Err("hub-extension_sandbox.owner_only"))
			return
		}
		h.putSandbox(w, r, actorOr(r, "personad extension"))
	})
}

func (h *Manager) putSandbox(w http.ResponseWriter, r *http.Request, who string) {
	ps := h.extensions
	var p *extension.Extension
	if ps != nil {
		p = ps.reg.Get(r.PathValue("id"))
	}
	if p == nil {
		writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such extension"))
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"` // null: back to the default
	}
	if !readExtensionJSON(w, r, &in) {
		return
	}
	if err := ps.setSandbox(r.Context(), p, in.Enabled, who); err != nil {
		writeErrCode(w, http.StatusConflict, "no_sandbox", err)
		return
	}
	writeJSON(w, detailOut(r, ps.detail(r.Context(), ps.reg.Get(p.Record.ID))))
}
