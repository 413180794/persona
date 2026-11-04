package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/agents"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/proc"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/host"
	"github.com/nwverse/persona/server/internal/rpc"
	wechat "github.com/nwverse/persona/server/internal/wechat/spec"
)

// Plugins (docs/plugins.md): one registry of every plugin — built into personad, written in hub.json, installed on
// this machine — and the plugin host running them. A plugin's contributions plug into the hub where they belong: a
// connector into the connector registry, a voice plugin into the app's voice conversation (relayed under
// /api/v1/plugins/<id>/…).

// PluginConfig is hub.json's plugins[]: a process the hub runs next to itself (docs/plugins.md「hub.json 里的插件」).
// The hub starts it, restarts it when it exits and relays /api/v1/plugins/{id}/… to the HTTP server it serves on the
// unix socket in JARVIS_PLUGIN_SOCKET (WebSockets included). Without a command the plugin runs elsewhere and only its
// socket is here (e.g. forwarded with ssh -R from a machine that reaches what the plugin needs). The registry gives it
// a manifest made from the entry; it is changed in hub.json only.
type PluginConfig struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Kind     string            `json:"kind"`
	Command  string            `json:"command,omitempty"`
	Socket   string            `json:"socket,omitempty"` // default ~/.jarvis/plugins/<id>.sock
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Dir      string            `json:"dir,omitempty"` // working directory; "~/" is the hub machine's home
	Disabled bool              `json:"disabled,omitempty"`
	// Config is the plugin's own settings, handed over as JSON in JARVIS_PLUGIN_CONFIG (the built-in voice plugin's
	// providers, docs/plugins.md「内置语音插件」).
	Config json.RawMessage `json:"config,omitempty"`
}

// PluginView is a plugin as the app sees it (GET /api/v1/plugins). id, name, kind, status and error are what it has
// always said; the rest came with the plugin base.
type PluginView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // disabled | needs_setup | incompatible | stopped | starting | running | error
	Error  string `json:"error,omitempty"`

	Version     string              `json:"version,omitempty"`
	Source      string              `json:"source,omitempty"` // builtin | hub.json | installed
	Enabled     bool                `json:"enabled"`
	Description string              `json:"description,omitempty"`
	Contributes map[string][]string `json:"contributes,omitempty"`
	NeedsSetup  []string            `json:"needs_setup,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
}

// Statuses besides the host's.
const (
	pluginDisabled     = "disabled"
	pluginNeedsSetup   = "needs_setup"
	pluginIncompatible = "incompatible"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// hubPlugin is one plugin as the hub runs it now.
type hubPlugin struct {
	id     string
	kind   string
	socket string // its HTTP socket, when it serves HTTP
	proxy  *httputil.ReverseProxy
	client *http.Client

	pending bool         // made before the plugins start (newPlugins): startPlugins runs it
	proc    *host.Proc   // nil while it does not run (disabled, needs setup, incompatible, nothing to run)
	conns   []*proc.Conn // its connectors
	links   []*proc.Link // theirs, to proc
	status  string       // when proc is nil
	err     string

	// agentSources are its agent_sources registered with the agents service (plugin_agents.go).
	agentSources []string
}

// healthy says whether its HTTP side answers /health.
func (p *hubPlugin) healthy(ctx context.Context) bool { return p.proc != nil && p.proc.Healthy(ctx) }

// plugins is the registry and what runs of it.
type plugins struct {
	dir  string // the hub's data directory
	reg  *plugin.Registry
	logf func(string, ...any)

	h    *Hub
	ctx  context.Context
	mu   sync.Mutex
	byID map[string]*hubPlugin
	// skillsSent remembers which skill digests each machine was sent (machine id → *sync.Map of name → digest), so
	// the files go once (again after the hub restarts).
	skillsSent sync.Map
	// markets are the plugin markets (plugin_market.go).
	markets markets
}

// newPlugins is the registry with hub.json's plugins: a bad entry is logged and skipped. Built-in and installed ones
// join when the hub attaches its database (attachPlugins).
func newPlugins(dir string, cfgs []PluginConfig, logf func(string, ...any)) *plugins {
	ps := &plugins{dir: dir, reg: plugin.NewRegistry(filepath.Join(dir, "plugins"), nil, nil, 0), logf: logf, byID: map[string]*hubPlugin{}}
	for _, c := range cfgs {
		if !pluginIDPattern.MatchString(c.ID) || (c.Command == "" && c.Socket == "") {
			logf("plugin %q: skipped (needs an id of a-z0-9_- and a command or a socket)", c.ID)
			continue
		}
		if c.Name == "" {
			c.Name = c.ID
		}
		if err := ps.reg.AddHubJSON(legacyManifest(c), c, !c.Disabled); err != nil {
			logf("plugin %q: skipped (duplicate id)", c.ID)
			continue
		}
		// Known before it runs: its socket (the model settings ask a voice plugin for its models through it).
		hp := &hubPlugin{id: c.ID, kind: c.Kind, socket: ps.legacySocket(c), status: host.StateStarting, pending: true}
		if c.Disabled {
			hp.status = pluginDisabled
		}
		hp.setProxy(c.ID)
		ps.byID[c.ID] = hp
	}
	return ps
}

// legacySocket is where a hub.json plugin serves HTTP.
func (ps *plugins) legacySocket(c PluginConfig) string {
	if c.Socket != "" {
		return expandHome(c.Socket)
	}
	return filepath.Join(ps.dir, "plugins", c.ID+".sock")
}

// legacyManifest is a hub.json entry as a manifest: a program serving HTTP (no protocol) contributing its kind, run
// from the start, with the hub's API (it has always had the hub's token).
func legacyManifest(c PluginConfig) *plugin.Manifest {
	kind := c.Kind
	if kind == "" {
		kind = "voice"
	}
	m := &plugin.Manifest{ManifestVersion: plugin.ManifestVersion, ID: c.ID, Name: c.Name, Version: "0.0.0",
		Runtime:          &plugin.Runtime{Command: plugin.Command{Path: c.Command}, Args: c.Args, RPC: "none", HTTP: true},
		ActivationEvents: []string{plugin.OnStartup}, Permissions: plugin.Permissions{HubAPI: true},
		Contributes: map[string]json.RawMessage{}}
	item, _ := json.Marshal([]map[string]string{{"id": c.ID, "name": c.Name}})
	m.Contributes[kind] = item
	return m
}

// list is every plugin, by id.
func (ps *plugins) list() []PluginView {
	out := []PluginView{}
	for _, p := range ps.reg.List() {
		out = append(out, ps.view(context.Background(), p))
	}
	return out
}

func (ps *plugins) view(ctx context.Context, p *plugin.Plugin) PluginView {
	m := p.Manifest
	v := PluginView{ID: p.Record.ID, Name: m.Name, Kind: m.Kind(), Version: p.Record.Version, Source: p.Record.Source,
		Enabled: p.Record.Enabled, Description: m.Description, Contributes: m.Contributions(), Warnings: p.Warnings}
	if c, ok := p.Legacy.(PluginConfig); ok {
		v.Kind, v.Version = c.Kind, ""
	}
	ps.mu.Lock()
	hp := ps.byID[p.Record.ID]
	ps.mu.Unlock()
	switch {
	case !p.Record.Enabled:
		v.Status = pluginDisabled
	case p.Problem != "":
		v.Status, v.Error = pluginIncompatible, p.Problem
	case hp == nil:
		v.Status = host.StateStarting
	case hp.proc == nil:
		v.Status, v.Error = hp.status, hp.err
	default:
		v.Status, v.Error = hp.proc.Status()
	}
	if v.Status == pluginNeedsSetup || (hp == nil && p.Record.Enabled) {
		v.NeedsSetup = ps.reg.Missing(ctx, p)
		if len(v.NeedsSetup) > 0 {
			v.Status = pluginNeedsSetup
		}
	}
	return v
}

// get is the running state of plugin id (nil when none).
func (ps *plugins) get(id string) *hubPlugin {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.byID[id]
}

// hubSealer seals plugin secrets with secret.key.
type hubSealer struct{ h *Hub }

func (s hubSealer) Seal(plain string) (string, error)  { return s.h.sealKey(plain) }
func (s hubSealer) Open(sealed string) (string, error) { return s.h.openKey(sealed) }

// attachPlugins gives the registry the database, the hub's build and the built-in plugins, reads the rows and
// registers the connectors plugins contribute (startConnectors calls it between the hub's own connectors and Start).
func (h *Hub) attachPlugins(ctx context.Context) {
	if h.plugins == nil {
		h.plugins = newPlugins(h.dataDirOrTemp(), nil, h.logf)
	}
	ps := h.plugins
	ps.h, ps.ctx = h, ctx
	var store plugin.Store
	if s, ok := h.store.(plugin.Store); ok {
		store = s
	}
	reg := plugin.NewRegistry(filepath.Join(ps.dir, "plugins"), store, hubSealer{h}, h.build.Number)
	for _, p := range ps.reg.List() { // hub.json's
		_ = reg.AddHubJSON(p.Manifest, p.Legacy, p.Record.Enabled)
	}
	reg.RemoteManifest = ps.remoteManifest
	var own []string
	for _, c := range h.connectors.All() {
		if !slices.Contains(connector.MigratableConnectors, c.ID()) { // a plugin may take these over (connectors_migrate.go)
			own = append(own, c.ID())
		}
	}
	reg.Reserve("connectors", own...)
	// The hub's own personal agents sources, but those a plugin may take over (plugin_agents.go).
	for _, id := range []string{agents.OLKBridge} {
		if !slices.Contains(agents.MigratableSources, id) {
			reg.Reserve("agent_sources", id)
		}
	}
	if path := telegramProgram(); path != "" {
		if err := reg.AddBuiltin(telegramManifest(h, path), filepath.Dir(path), true); err != nil {
			h.logf("plugin telegram: %v", err)
		}
	}
	if path := siblingProgram(wechat.Program); path != "" {
		if err := reg.AddBuiltin(wechatManifest(h, path), filepath.Dir(path), true); err != nil {
			h.logf("plugin wechat: %v", err)
		}
	}
	if err := reg.AddBuiltin(cuaManifest(h), "", false); err != nil { // off until the user turns it on (computer.go)
		h.logf("plugin cua: %v", err)
	}
	if err := reg.AddBuiltin(egoManifest(h), "", false); err != nil { // the same, for ego lite (browser.go)
		h.logf("plugin ego: %v", err)
	}
	if err := reg.Load(ctx); err != nil {
		h.logf("plugins: %v", err)
	}
	h.fixMachineSettings(ctx, reg) // machine names typed into settings become ids (plugin_machines.go)
	reg.OnChange = ps.changed
	ps.mu.Lock()
	ps.reg = reg
	ps.mu.Unlock()
	for _, p := range reg.List() {
		if len(p.Manifest.Items("connectors")) > 0 {
			ps.apply(ctx, p.Record.ID)
		}
	}
}

// dataDirOrTemp is where plugins keep their packages and sockets.
func (h *Hub) dataDirOrTemp() string {
	if h.cfg != nil && h.cfg.dir != "" {
		return h.cfg.dir
	}
	return os.TempDir()
}

// startPlugins runs every plugin that is not running yet (connectors' ones run since attachPlugins).
func (h *Hub) startPlugins(ctx context.Context) {
	if h.plugins == nil {
		return
	}
	ps := h.plugins
	if ps.h == nil {
		ps.h, ps.ctx = h, ctx
	}
	for _, p := range ps.reg.List() {
		if hp := ps.get(p.Record.ID); hp == nil || hp.pending {
			ps.apply(ctx, p.Record.ID)
		}
	}
}

// changed is the registry's news: the plugin runs again as it is now; one just upgraded that fails to come up is
// rolled back.
func (ps *plugins) changed(c plugin.Change) {
	ctx := ps.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ps.apply(ctx, c.ID)
	if c.ID == cuaPluginID && ps.h != nil {
		ps.h.computerChanged(ctx)
	}
	if c.ID == egoPluginID && ps.h != nil {
		ps.h.browserChanged(ctx)
	}
	if c.Kind == "removed" && c.Old != nil && ps.h != nil {
		for _, id := range c.Old.Manifest.Items("connectors") {
			ps.h.forgetConnector(ctx, id)
		}
	}
	if c.Kind == "upgraded" {
		go ps.watchUpgrade(ctx, c.ID)
	}
}

// watchUpgrade rolls an upgraded plugin back when it does not come up (only one that runs by itself or for a
// connector that is on: a plugin started by calls is not started for this).
func (ps *plugins) watchUpgrade(ctx context.Context, id string) {
	hp := ps.get(id)
	if hp == nil || hp.proc == nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, host.ReadyWithin+5*time.Second)
	defer cancel()
	for wctx.Err() == nil {
		state, why := hp.proc.Status()
		switch state {
		case host.StateRunning:
			return
		case host.StateError:
			ps.logf("plugin %s: the new version does not come up (%s): back to the one before", id, why)
			if _, err := ps.reg.Rollback(ctx, id, "hub"); err != nil {
				ps.logf("plugin %s: rollback: %v", id, err)
			}
			return
		}
		select {
		case <-wctx.Done():
		case <-hp.proc.Changes():
		}
	}
}

// apply makes plugin id run as the registry says: whatever ran of it stops (its connectors leave the registry), and
// it starts again if it is there, switched on, compatible and set up.
func (ps *plugins) apply(ctx context.Context, id string) {
	ps.mu.Lock()
	old := ps.byID[id]
	delete(ps.byID, id)
	ps.mu.Unlock()
	if old != nil {
		ps.removeAgentSources(old)
		for _, c := range old.conns {
			if ps.h != nil && ps.h.connectors != nil {
				ps.h.removePluginConnector(c.ID())
			}
		}
		if old.proc != nil {
			old.proc.Stop()
		}
	}
	p := ps.reg.Get(id)
	if p == nil {
		return
	}
	hp := &hubPlugin{id: id, kind: p.Manifest.Kind()}
	if c, ok := p.Legacy.(PluginConfig); ok {
		hp.kind = c.Kind
	}
	switch missing := ps.reg.Missing(ctx, p); {
	case !p.Record.Enabled:
		hp.status = pluginDisabled
	case p.Problem != "":
		hp.status, hp.err = pluginIncompatible, p.Problem
	case len(missing) > 0:
		hp.status, hp.err = pluginNeedsSetup, "还缺："+strings.Join(missing, "、")
	case p.Manifest.Runtime == nil:
		hp.status = host.StateRunning // nothing to run: its declarations are in effect
	default:
		spec, err := ps.spec(ctx, p, hp)
		if err != nil {
			hp.status, hp.err = host.StateError, err.Error()
			break
		}
		hp.proc = host.Start(ctx, spec)
		for _, l := range hp.links {
			l.Proc = hp.proc
		}
	}
	if hp.socket != "" {
		hp.setProxy(id)
	}
	ps.addAgentSources(id, p.Manifest, hp) // before hp is published: its agentsrc.updated finds them
	ps.mu.Lock()
	ps.byID[id] = hp
	ps.mu.Unlock()
	if hp.proc != nil && ps.h != nil && ps.h.connectors != nil {
		for _, c := range hp.conns {
			if err := ps.h.addPluginConnector(ctx, c, p.Record.Source == plugin.SourceBuiltin); err != nil {
				ps.logf("plugin %s: %v", id, err)
			}
		}
	}
}

// setProxy relays the app's requests to the plugin's HTTP side.
func (hp *hubPlugin) setProxy(id string) {
	socket := hp.socket
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}
	hp.client = &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 5 * time.Second}
	prefix := "/api/v1/plugins/" + id
	hp.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme, r.Out.URL.Host = "http", "plugin"
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, prefix)
			if r.Out.URL.Path == "" {
				r.Out.URL.Path = "/"
			}
			// Keep what the caller escaped (an id with "/" in it, as %2F): the plugin reads EscapedPath.
			r.Out.URL.RawPath = ""
			if raw := strings.TrimPrefix(r.In.URL.EscapedPath(), prefix); raw != "" && raw != r.Out.URL.Path {
				r.Out.URL.RawPath = raw
			}
			// The hub has already checked the caller: the plugin never sees the app's credential.
			q := r.Out.URL.Query()
			q.Del("token")
			r.Out.URL.RawQuery = q.Encode()
			r.Out.Header.Del("Authorization")
			r.Out.Host = "plugin"
		},
		Transport:     &http.Transport{DialContext: dial},
		FlushInterval: -1, // stream (audio, server-sent events) as it comes
		// The plugin says how its answers may be cached (images, emojis); the hub's no-store only when it says nothing.
		// The caller drops the hub's own Cache-Control first (servePluginHTTP): the proxy adds headers, it does not
		// replace them, and "no-store, private, max-age=3600" kept the apps from caching anything.
		ModifyResponse: func(res *http.Response) error {
			if res.Header.Get("Cache-Control") == "" {
				res.Header.Set("Cache-Control", "no-store")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Cache-Control", "no-store")
			writeErr(w, http.StatusBadGateway, fmt.Errorf("plugin %s is not answering: %w", id, err))
		},
	}
}

// servePluginHTTP hands one request to the plugin's HTTP side.
func (hp *hubPlugin) servePluginHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Del("Cache-Control") // set by the hub's auth for its own answers: the plugin's own goes instead
	hp.proxy.ServeHTTP(w, r)
}

// selfExecutable is this very binary (a built-in plugin's "personad").
func selfExecutable() string {
	if self, err := os.Executable(); err == nil {
		return self
	}
	return "personad"
}

// spec is how the host runs p; it also makes p's connectors (hp.conns) and its HTTP socket (hp.socket).
func (ps *plugins) spec(ctx context.Context, p *plugin.Plugin, hp *hubPlugin) (host.Spec, error) {
	m := p.Manifest
	id := p.Record.ID
	s := host.Spec{ID: id, Name: m.Name, Logf: ps.logf}
	hubEnv := ps.hubEnv()
	if c, ok := p.Legacy.(PluginConfig); ok { // hub.json's, exactly as it has always run
		hp.socket = ps.legacySocket(c)
		s.HTTP, s.Socket, s.Startup = true, hp.socket, true
		if c.Command != "" {
			command := expandHome(c.Command)
			if plugin.IsBuiltinCommand(command) {
				command = selfExecutable()
			}
			s.Command, s.Args, s.Dir = command, c.Args, expandHome(c.Dir)
			s.Env = append(slices.Clone(hubEnv), "PERSONA_PLUGIN_ID="+c.ID, "PERSONA_PLUGIN_SOCKET="+hp.socket)
			for k, v := range c.Env {
				s.Env = append(s.Env, k+"="+v)
			}
			if len(c.Config) > 0 {
				s.Env = append(s.Env, "PERSONA_PLUGIN_CONFIG="+string(c.Config))
			}
		}
		return s, nil
	}

	rt := m.Runtime
	activation := m.Activation()
	conns := m.Connectors()
	s.RPC = rt.SpeaksRPC()
	s.Startup = slices.Contains(activation, plugin.OnStartup)
	s.OnCall = slices.Contains(activation, plugin.OnCall) && len(conns) == 0 // a connector's switch holds it instead
	s.OnHTTP = slices.Contains(activation, plugin.OnHTTP)
	if len(conns) > 0 {
		s.CallTimeout = 2 * time.Minute // the connector protocol's
	}
	if r := p.Record.Remote; r != nil {
		s.RPCSocket, s.Socket = r.RPCSocket, r.HTTPSocket
		s.RPC, s.HTTP = r.RPCSocket != "", r.HTTPSocket != ""
		hp.socket = r.HTTPSocket
	} else {
		cmd, ok := rt.Command.For(runtime.GOOS + "-" + runtime.GOARCH)
		if !ok {
			return s, fmt.Errorf("没有 %s-%s 的程序", runtime.GOOS, runtime.GOARCH)
		}
		if plugin.IsBuiltinCommand(cmd) {
			s.Command = selfExecutable()
		} else {
			s.Command = filepath.Join(p.Dir, filepath.FromSlash(cmd))
		}
		s.Args, s.Dir = rt.Args, p.Dir
		s.CleanEnv = p.Record.Source == plugin.SourceInstalled // not the hub's own credentials
		dataDir := ps.reg.DataDir(id)
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return s, err
		}
		s.Env = []string{"PERSONA_PLUGIN_ID=" + id, "PERSONA_PLUGIN_DIR=" + p.Dir, "PERSONA_PLUGIN_DATA=" + dataDir}
		for k, v := range rt.Env {
			s.Env = append(s.Env, k+"="+v)
		}
		if rt.HTTP {
			hp.socket = filepath.Join(ps.dir, "plugins", id+".sock")
			s.HTTP, s.Socket = true, hp.socket
			s.Env = append(s.Env, "PERSONA_PLUGIN_SOCKET="+hp.socket)
		}
		if p.Record.Granted.HubAPI {
			s.Env = append(s.Env, hubEnv...)
		}
		if !s.RPC { // no initialize to carry them
			secrets, err := ps.reg.Secrets(ctx, id)
			if err != nil {
				return s, err
			}
			b, _ := json.Marshal(secrets)
			s.Env = append(s.Env, "PERSONA_PLUGIN_CONFIG="+string(ps.reg.Config(p)), "PERSONA_PLUGIN_SECRETS="+string(b))
		}
	}
	builtin := p.Record.Source == plugin.SourceBuiltin
	s.Init = func(ctx context.Context) (plugin.InitializeParams, error) {
		secrets, err := ps.reg.Secrets(ctx, id)
		if err != nil {
			return plugin.InitializeParams{}, err
		}
		info := plugin.PluginInfo{ID: id, Version: p.Record.Version, Dir: p.Dir}
		if p.Record.Remote == nil {
			info.DataDir = ps.reg.DataDir(id)
		}
		var build int64
		if ps.h != nil {
			build = ps.h.build.Number
		}
		params := plugin.InitializeParams{Hub: plugin.HubInfo{Build: build, ExtensionPoints: plugin.PointVersions()}, Plugin: info,
			Config: ps.reg.Config(p), Secrets: secrets, Granted: p.Record.Granted, Active: map[string][]string{}}
		if builtin && id == telegramPluginID && ps.h != nil {
			params.Config = ps.h.telegramSettings()
		}
		for _, c := range conns {
			if ps.h == nil || ps.h.connectors.Enabled(c.ID) {
				params.Active["connectors"] = append(params.Active["connectors"], c.ID)
			}
		}
		return params, nil
	}
	if builtin && (id == telegramPluginID || id == wechat.ID) && ps.h != nil {
		s.Check = sameBuild(ps.h, p.Manifest.Name, s.Command) // the program must be the hub's build
	}
	s.Accept = func(res plugin.InitializeResult) error {
		var said []plugin.ConnectorItem
		_ = json.Unmarshal(res.Contributes["connectors"], &said)
		for _, c := range conns {
			i := slices.IndexFunc(said, func(x plugin.ConnectorItem) bool { return x.ID == c.ID })
			switch {
			case i < 0:
				ps.logf("plugin %s: the program does not say it has connector %s (the manifest's is used)", id, c.ID)
			case !sameItem(said[i], c):
				ps.logf("plugin %s: the program declares connector %s as %+v, the manifest as %+v (the manifest's is used)", id, c.ID, said[i], c)
			}
		}
		return nil
	}
	byID := map[string]*proc.Conn{}
	s.Handle = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if rest, ok := strings.CutPrefix(method, serve.Namespace+"."); ok {
			c := byID[contributionOf(params)]
			if c == nil && len(byID) == 1 {
				for _, only := range byID {
					c = only
				}
			}
			if c == nil {
				return nil, &rpc.Error{Code: plugin.CodeNoContribution, Message: "no connector " + contributionOf(params)}
			}
			return c.Handle(ctx, rest, params)
		}
		if method == "quota.http" || method == "gateway.http" {
			return ps.quotaHTTP(ctx, id, params)
		}
		return nil, &rpc.Error{Code: plugin.CodeUnknownMethod, Message: "unknown method " + method}
	}
	s.OnEvent = func(event string, params json.RawMessage) {
		if event == "api.broadcast" {
			ps.apiBroadcast(id, params)
			return
		}
		if event == "agentsrc.updated" {
			ps.agentSourceUpdated(id, hp, params)
			return
		}
		if rest, ok := strings.CutPrefix(event, serve.Namespace+"."); ok {
			if c := byID[contributionOf(params)]; c != nil {
				c.Event(rest, params)
			} else if len(byID) == 1 {
				for _, only := range byID {
					only.Event(rest, params)
				}
			}
		}
	}
	// The connectors reach the process through their links, filled in by apply once the host has started it.
	for _, c := range conns {
		link := &proc.Link{ID: c.ID}
		hp.links = append(hp.links, link)
		var secrets connector.Secrets
		var kbase connector.KnowledgeBase
		if ps.h != nil {
			secrets, kbase = ps.h.secretsFor(c.ID), ps.h.kbFor(c.ID)
		}
		for kind, d := range c.KindDescriptions {
			pluginKinds.Store(c.ID+"/"+kind, d)
		}
		conn := proc.New(proc.Config{ID: c.ID, Name: c.Name, Manifest: connectorManifest(c), Secrets: secrets, KB: kbase, Logf: ps.logf, Link: link})
		byID[c.ID] = conn
		hp.conns = append(hp.conns, conn)
	}
	return s, nil
}

func contributionOf(params json.RawMessage) string {
	var t plugin.Target
	_ = json.Unmarshal(params, &t)
	return t.Contribution
}

func sameItem(a, b plugin.ConnectorItem) bool {
	ja, _ := json.Marshal(connectorManifest(a))
	jb, _ := json.Marshal(connectorManifest(b))
	return string(ja) == string(jb)
}

// pluginKinds are the plugin connectors' kinds of inbox things as they describe them: "<connector>/<kind>" → words.
var pluginKinds sync.Map

// connectorManifest is a contribution to connectors as the connector registry declares it.
func connectorManifest(c plugin.ConnectorItem) connector.Manifest {
	m := serve.Manifest{ID: c.ID, Name: c.Name, Capabilities: slices.Clone(c.Capabilities), Hosts: c.Hosts, AgentTools: c.AgentTools,
		Unlisted: c.Unlisted, ConfirmAlways: c.ConfirmAlways, PollSeconds: c.PollSeconds, Kinds: c.Kinds, ArchiveSources: c.ArchiveSources}.Of()
	slices.Sort(m.Capabilities)
	return m
}

// hubEnv is what a plugin granted hub_api (and every hub.json one) gets: the hub's address and token on this machine.
func (ps *plugins) hubEnv() []string {
	if ps.h == nil || ps.h.cfg == nil {
		return nil
	}
	// A plugin granted it is configured or accepted by whoever runs the hub, so it gets the hub's own credential to
	// call its API with (the knowledge base, the agents, …): like a node, it is part of the hub.
	return []string{"JARVIS_HUB_URL=" + hubLocalURL(ps.h.cfg.Listen), "JARVIS_HUB_TOKEN=" + ps.h.cfg.Token}
}

// remoteManifest asks a plugin elsewhere for its manifest: initialize over its protocol socket says who it is and
// what it contributes; one serving HTTP only answers GET /manifest with persona-plugin.json.
func (ps *plugins) remoteManifest(ctx context.Context, r plugin.Remote) (*plugin.Manifest, error) {
	if r.RPCSocket == "" {
		dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", r.HTTPSocket)
		}
		client := &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: 10 * time.Second}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://plugin/manifest", nil)
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET /manifest: HTTP %d", res.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, plugin.ManifestMost+1))
		if err != nil {
			return nil, err
		}
		return plugin.Parse(b)
	}
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	conn, err := d.DialContext(dctx, "unix", r.RPCSocket)
	cancel()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	peer := rpc.New("h", conn)
	go func() { _ = peer.Serve(ctx, bufio.NewReader(conn)) }()
	ictx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var m plugin.Manifest
	if err := peer.Call(ictx, plugin.MManifest, struct{}{}, &m); err != nil {
		return nil, fmt.Errorf("the plugin does not answer manifest: %w", err)
	}
	b, _ := json.Marshal(m)
	return plugin.Parse(b)
}

// addPluginConnector registers a plugin's connector: built-in ones start switched off until the user turns them on
// (Telegram waits for the user to sign in), installed ones on; the user's switches and settings apply.
func (h *Hub) addPluginConnector(ctx context.Context, c *proc.Conn, off bool) error {
	if off {
		if h.connectorsOff == nil {
			h.connectorsOff = map[string]bool{}
		}
		h.connectorsOff[c.ID()] = true
	}
	cs := h.connectorSettings(ctx)
	if !h.connectorOn(cs, c.ID()) {
		h.connectors.StartOff(c.ID()) // added after Start, it must not run first
	}
	h.yieldOwnConnector(c.ID())
	if err := h.addConnector(c); err != nil {
		h.restoreOwnConnector(c.ID())
		return err
	}
	h.restoreSwitchesOf(c, cs)
	return nil
}

// forgetConnector deletes what the hub kept for a connector of a removed plugin: its secret and its settings.
func (h *Hub) forgetConnector(ctx context.Context, id string) {
	if h.store == nil {
		return
	}
	_ = h.store.DeleteConnectorSecret(ctx, id)
	var cs connectorSettings
	if ok, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil || !ok {
		return
	}
	delete(cs.Switch, id)
	delete(cs.Options, id)
	for _, f := range []connector.Feed{connector.FeedArchive, connector.FeedInbox} {
		delete(*cs.feeds(f), id)
	}
	_ = h.store.PutSetting(ctx, connectorSettingsKey, cs)
}

func logLines(r io.Reader, f func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			f(line)
		}
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// hubLocalURL is how a plugin on this machine reaches the hub's API.
func hubLocalURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:18660"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
