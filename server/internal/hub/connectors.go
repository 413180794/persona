package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/connector/proc"
	"github.com/nwverse/persona/server/internal/connector/telegram/spec"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Connectors: each source of the user's information plugs in as a connector, and the hub's pipes pick up what it
// implements — the assistant's tools (writes behind the gate), 设置 › 连接器's status, realtime kicks
// (internal/connector, docs/plan-connectors.md).

// addConnector registers c, refusing one that offers a tool the hub has itself ("kb_search", "phone_…"): calls go
// to the connector first. A connector may share a prefix with the hub's tools ("lark") as long as it does not offer
// them; the hub's own then move to it (docs/plan-connectors.md M4).
func (h *Hub) addConnector(c connector.Connector) error {
	for _, t := range connector.ToolNames(c) {
		for _, b := range h.builtinTools() {
			if t == b || strings.HasSuffix(b, "_") && strings.HasPrefix(t, b) {
				return fmt.Errorf("connector %q: %s would shadow the hub's own", c.ID(), t)
			}
		}
	}
	return h.connectors.Add(c)
}

// builtinTools are the names of every tool the hub offers besides connectors', whether or not it is set up to offer
// them now (the phone's by their prefix: they come and go with the app), the assistant's as it last listed them (none
// while it has not connected yet: connectors register as the hub starts, and do not wait for it).
func (h *Hub) builtinTools() []string {
	names := []string{"phone_"}
	for _, t := range h.assistantToolsNow() {
		names = append(names, t.Name)
	}
	return names
}

// connectorChanged is a connector's realtime event: collect the inbox or archive soon.
func (h *Hub) connectorChanged(id string, scope connector.Scope) {
	switch scope {
	case connector.ScopeInbox:
		h.kickInbox()
	case connector.ScopeChats, connector.ScopeItems:
		h.kbKick()
	}
}

// callConnector runs a connector's tool for machineID's assistant; a write the gate leaves to the user is asked on
// the phone like mail_send.
func (h *Hub) callConnector(ctx context.Context, machineID, name string, args json.RawMessage) proto.ToolResult {
	return h.connectors.Call(ctx, name, args, func(ctx context.Context, summary string, input json.RawMessage) bool {
		return h.approve(ctx, machineID, name, summary, input)
	})
}

func (h *Hub) connectorRoutes(api *http.ServeMux) {
	h.githubAuthRoutes(api)
	h.connectorThreadRoutes(api)
	api.HandleFunc("GET /api/v1/connectors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"connectors": h.listedConnectors(r.Context(), r.URL.Query().Get("all") == "1")})
	})
	// Signing in, a step at a time (connector.Login): the app shows each Prompt's fields and sends them back. The
	// values (a phone number, a code, a password) go to the connector only: never into a log or a response.
	api.HandleFunc("POST /api/v1/connectors/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		l, ok := h.connectorLogin(w, r.PathValue("id"))
		if !ok {
			return
		}
		var body struct {
			Step   string            `json:"step"`
			Values map[string]string `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		p, err := l.LoginStep(r.Context(), body.Step, body.Values)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		if err := withQR(&p); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, p)
	})
	api.HandleFunc("POST /api/v1/connectors/{id}/logout", func(w http.ResponseWriter, r *http.Request) {
		l, ok := h.connectorLogin(w, r.PathValue("id"))
		if !ok {
			return
		}
		if err := l.Logout(r.Context()); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	// 设置 › 连接器's switch: off, the connector stops and brings nothing in (Registry.SetEnabled); kept in settings.
	api.HandleFunc("PUT /api/v1/connectors/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if h.connectors.Get(id) == nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
			return
		}
		var body struct {
			Enabled *bool           `json:"enabled"`
			Options json.RawMessage `json:"options"` // the connector's own settings (connector.Options)
			Archive *bool           `json:"archive"` // 进知识库
			Inbox   *bool           `json:"inbox"`   // 进收件箱
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if len(body.Options) > 0 && string(body.Options) != "null" {
			if err := h.setConnectorOptions(r.Context(), id, body.Options); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		for f, on := range map[connector.Feed]*bool{connector.FeedArchive: body.Archive, connector.FeedInbox: body.Inbox} {
			if on == nil {
				continue
			}
			if err := h.setConnectorFeed(r.Context(), id, f, *on); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if body.Enabled != nil {
			if err := h.setConnectorEnabled(r.Context(), id, *body.Enabled); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		for _, info := range h.listedConnectors(r.Context(), true) {
			if info.ID == id {
				writeJSON(w, info)
				return
			}
		}
	})
	// 删除已存的数据 for any connector: switched off first (and kept off: on, the next sync would bring it back), then
	// what the hub kept of it goes (forgetSource, as for iMessage). Signing out is apart (/logout).
	api.HandleFunc("DELETE /api/v1/connectors/{id}/data", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if h.connectors.Get(id) == nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
			return
		}
		if err := h.setConnectorEnabled(r.Context(), id, false); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		res, err := h.forgetSource(r.Context(), id)
		if err != nil {
			h.logf("connector %s: delete: %v", id, err)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("connector %s: deleted %d chat(s), %d person(s), %d inbox item(s)", id, res.Chats, res.People, res.Inbox)
		writeJSON(w, res)
	})
	// 删除已存的知识库内容: 进知识库 turned off first (and kept off: on, the next round would bring it back), then the
	// knowledge base's chats and people of it go. The connector keeps running: the inbox and the tools are untouched.
	api.HandleFunc("DELETE /api/v1/connectors/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if h.connectors.Get(id) == nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
			return
		}
		if err := h.setConnectorFeed(r.Context(), id, connector.FeedArchive, false); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		var res sourceForgotten
		var err error
		if res.Chats, res.People, err = h.forgetArchive(r.Context(), id); err != nil {
			h.logf("connector %s: delete the archive: %v", id, err)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("connector %s: deleted %d chat(s), %d person(s) from the knowledge base", id, res.Chats, res.People)
		writeJSON(w, res)
	})
	h.imessageRoutes(api)
}

// sourceForgotten is what deleting a source's data removed (DELETE /api/v1/connectors/{id}/data,
// DELETE /api/v1/imessage/data).
type sourceForgotten struct {
	Chats  int `json:"chats"`
	People int `json:"people"`
	Inbox  int `json:"inbox"`
}

// forgetSource removes what the hub kept of a source (a connector's id): the knowledge base's chats and people (and
// their index rows, their places in projects, the cursor) and the inbox's items. The caller stops the source first.
func (h *Hub) forgetSource(ctx context.Context, source string) (sourceForgotten, error) {
	var out sourceForgotten
	var err error
	if out.Chats, out.People, err = h.forgetArchive(ctx, source); err != nil {
		return out, err
	}
	out.Inbox, err = h.forgetInbox(ctx, source)
	return out, err
}

// forgetArchive removes a source's chats and people from the knowledge base, with their index rows, their places in
// projects and the cursor; it says how many chats and people went.
func (h *Hub) forgetArchive(ctx context.Context, source string) (int, int, error) {
	if h.kb.eng == nil {
		return 0, 0, nil
	}
	f, err := h.kb.eng.ForgetSource(source)
	if err != nil {
		return 0, 0, err
	}
	if idx, ok := h.store.(interface {
		DeleteKBEntities(ctx context.Context, kind string, ids []string) error
	}); ok {
		if err := idx.DeleteKBEntities(ctx, kb.KindChat, f.Chats); err != nil {
			return 0, 0, err
		}
		if err := idx.DeleteKBEntities(ctx, kb.KindPerson, f.People); err != nil {
			return 0, 0, err
		}
	}
	return len(f.Chats), len(f.People), nil
}

// forgetInbox removes a source's inbox items; it says how many went.
func (h *Hub) forgetInbox(ctx context.Context, source string) (int, error) {
	h.inbox.mu.Lock()
	var ids []string
	for id, it := range h.inbox.items {
		if it.Source == source {
			ids = append(ids, id)
			delete(h.inbox.items, id)
			h.inbox.replies.Delete(id)
		}
	}
	h.inbox.mu.Unlock()
	if del, ok := h.store.(interface {
		DeleteInboxItems(ctx context.Context, ids []string) error
	}); ok && len(ids) > 0 {
		if err := del.DeleteInboxItems(ctx, ids); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		h.broadcast(map[string]any{"type": "inbox"})
	}
	return len(ids), nil
}

// withQR draws a QR step's code for the app (whatever the connector sent as QR is replaced: the code is always URL's).
func withQR(p *connector.Prompt) error {
	p.QR = nil
	if p.Kind != connector.PromptQRCode {
		return nil
	}
	if p.URL == "" {
		return errors.New("a QR sign-in step without a URL")
	}
	rows, size, err := qrRows(p.URL)
	if err != nil {
		return err
	}
	p.QR = &connector.QRCode{Size: size, Rows: rows}
	return nil
}

// connectorLogin is connector id's sign-in, or writes why there is none.
func (h *Hub) connectorLogin(w http.ResponseWriter, id string) (connector.Login, bool) {
	c := h.connectors.Get(id)
	if c == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
		return nil, false
	}
	l, ok := connector.As[connector.Login](c)
	if !ok {
		writeErr(w, http.StatusConflict, fmt.Errorf("%s has no sign-in", c.Name()))
		return nil, false
	}
	return l, true
}

// connectorSettingsKey holds the user's switch per connector, their own settings and the feeds turned off:
// {"switch": {"telegram": true}, "options": {"github": {"ci": "mine"}}, "archive": {"telegram": false}}.
const connectorSettingsKey = "connectors"

type connectorSettings struct {
	Switch  map[string]bool            `json:"switch,omitempty"`
	Options map[string]json.RawMessage `json:"options,omitempty"`
	// Archive and Inbox are its feeds the user turned on or off (connector.SetFeed); one not here is on.
	Archive map[string]bool `json:"archive,omitempty"`
	Inbox   map[string]bool `json:"inbox,omitempty"`
}

// feeds is the settings' map for feed f.
func (cs *connectorSettings) feeds(f connector.Feed) *map[string]bool {
	if f == connector.FeedInbox {
		return &cs.Inbox
	}
	return &cs.Archive
}

// setConnectorOptions gives a connector the user's settings and keeps them for the next start; the inbox is
// collected again under them. GitHub's muted repositories go to the inbox's mutes (inbox_mutes.go).
func (h *Hub) setConnectorOptions(ctx context.Context, id string, raw json.RawMessage) error {
	if id == "github" {
		var err error
		if raw, err = h.setGitHubMutes(ctx, raw); err != nil {
			return err
		}
	}
	return h.applyConnectorOptions(ctx, id, raw)
}

// applyConnectorOptions is setConnectorOptions without the mutes.
func (h *Hub) applyConnectorOptions(ctx context.Context, id string, raw json.RawMessage) error {
	o, ok := connector.As[connector.Options](h.connectors.Get(id))
	if !ok {
		return fmt.Errorf("%s has no settings", id)
	}
	if err := o.SetOptions(raw); err != nil {
		return err
	}
	if h.store != nil {
		var cs connectorSettings
		if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
			return err
		}
		if cs.Options == nil {
			cs.Options = map[string]json.RawMessage{}
		}
		cs.Options[id] = o.Options()
		if err := h.store.PutSetting(ctx, connectorSettingsKey, cs); err != nil {
			return err
		}
	}
	h.kickInbox()
	return nil
}

// setConnectorEnabled switches a connector and keeps the choice for the next start.
func (h *Hub) setConnectorEnabled(ctx context.Context, id string, on bool) error {
	if err := h.connectors.SetEnabled(id, on); err != nil {
		return err
	}
	if h.store == nil {
		return nil
	}
	var cs connectorSettings
	if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
		return err
	}
	if cs.Switch == nil {
		cs.Switch = map[string]bool{}
	}
	cs.Switch[id] = on
	return h.store.PutSetting(ctx, connectorSettingsKey, cs)
}

// setConnectorFeed turns one of a connector's feeds on or off and keeps the choice for the next start; turned on, the
// inbox or the archive goes again soon.
func (h *Hub) setConnectorFeed(ctx context.Context, id string, f connector.Feed, on bool) error {
	if err := h.connectors.SetFeed(id, f, on); err != nil {
		return err
	}
	if h.store != nil {
		var cs connectorSettings
		if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
			return err
		}
		m := cs.feeds(f)
		if *m == nil {
			*m = map[string]bool{}
		}
		(*m)[id] = on
		if err := h.store.PutSetting(ctx, connectorSettingsKey, cs); err != nil {
			return err
		}
	}
	if on && f == connector.FeedInbox {
		h.kickInbox()
	} else if on {
		h.kbKick()
	}
	return nil
}

// restoreConnectorSwitches applies the user's switches before the connectors start. One the user never switched is
// on, unless it was added to start off (addConnectorOff: Telegram waits for the user to turn it on and sign in).
func (h *Hub) restoreConnectorSwitches(ctx context.Context) {
	cs := h.connectorSettings(ctx)
	for _, c := range h.connectors.All() {
		h.restoreSwitchesOf(c, cs)
	}
}

// connectorSettings are the user's connector switches and settings.
func (h *Hub) connectorSettings(ctx context.Context) connectorSettings {
	var cs connectorSettings
	if h.store != nil {
		if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
			h.logf("connectors: settings: %v", err)
		}
	}
	return cs
}

// connectorOn says whether the user wants connector id on (their switch, or its default).
func (h *Hub) connectorOn(cs connectorSettings, id string) bool {
	on, chosen := cs.Switch[id]
	if !chosen {
		on = !h.connectorsOff[id]
	}
	return on
}

func (h *Hub) restoreSwitchesOf(c connector.Connector, cs connectorSettings) {
	if raw, ok := cs.Options[c.ID()]; ok {
		if o, ok := connector.As[connector.Options](c); ok {
			if err := o.SetOptions(raw); err != nil {
				h.logf("connectors: %s settings: %v", c.ID(), err)
			}
		}
	}
	for _, f := range []connector.Feed{connector.FeedArchive, connector.FeedInbox} {
		if on, ok := (*cs.feeds(f))[c.ID()]; ok && !on {
			if err := h.connectors.SetFeed(c.ID(), f, false); err != nil {
				h.logf("connectors: %s %s: %v", c.ID(), f, err)
			}
		}
	}
	if !h.connectorOn(cs, c.ID()) {
		_ = h.connectors.SetEnabled(c.ID(), false)
	}
}

// addConnectorOff registers a connector that starts switched off until the user turns it on.
func (h *Hub) addConnectorOff(c connector.Connector) error {
	if err := h.addConnector(c); err != nil {
		return err
	}
	if h.connectorsOff == nil {
		h.connectorsOff = map[string]bool{}
	}
	h.connectorsOff[c.ID()] = true
	return nil
}

// connectorSecrets is where connector id keeps its secret (connector.Secrets): one row of connector_secrets, sealed
// with secret.key like launchers' keys and mail passwords. Each connector is handed its own; the plain text is only
// ever in memory.
type connectorSecrets struct {
	h  *Hub
	id string
}

func (h *Hub) secretsFor(id string) connector.Secrets { return connectorSecrets{h: h, id: id} }

var errNoDatabase = errors.New("no database: nothing can be kept")

func (s connectorSecrets) Load(ctx context.Context) ([]byte, error) {
	if s.h.store == nil {
		return nil, errNoDatabase
	}
	sealed, ok, err := s.h.store.ConnectorSecret(ctx, s.id)
	if err != nil || !ok {
		return nil, err
	}
	plain, err := s.h.openKey(sealed)
	if err != nil {
		return nil, fmt.Errorf("%s: the stored secret does not open (secret.key replaced?)", s.id)
	}
	return []byte(plain), nil
}

func (s connectorSecrets) Save(ctx context.Context, data []byte) error {
	if s.h.store == nil {
		return errNoDatabase
	}
	sealed, err := s.h.sealKey(string(data))
	if err != nil {
		return err
	}
	return s.h.store.PutConnectorSecret(ctx, s.id, sealed)
}

func (s connectorSecrets) Delete(ctx context.Context) error {
	if s.h.store == nil {
		return nil
	}
	return s.h.store.DeleteConnectorSecret(ctx, s.id)
}

// startConnectors registers the connectors, applies the user's switches and runs them.
func (h *Hub) startConnectors(ctx context.Context) {
	h.registerConnectors()
	h.attachPlugins(ctx) // the plugins' connectors (Telegram's), after the hub's own: those ids are taken
	h.restoreConnectorSwitches(ctx)
	h.restoreGitHubSource(ctx)
	h.githubMutes(ctx)
	h.connectors.Start(ctx, h.connectorChanged, h.logf)
}

// registerConnectors registers the connectors hub.json sets up.
func (h *Hub) registerConnectors() {
	if h.mail != nil {
		if err := h.addConnector(mailConnector{h}); err != nil {
			h.logf("mail connector: %v", err)
		}
	}
	// GitHub on every hub: hub.json's github.auth.from_machine reads that computer's gh login; without it (or
	// before it) the user signs in from the app with the device flow (connector/github/login.go).
	gh := github.Config{}
	if h.cfg.GitHub != nil {
		gh = *h.cfg.GitHub
	}
	if _, err := gh.HTTPClient(0); err != nil {
		h.logf("github: %v (every GitHub request fails until it is fixed)", err)
	}
	var ghc *github.Connector
	if gh.Configured() {
		name := gh.Auth.FromMachine
		if m := h.localMachine(name); m != nil {
			name = m.cfg.Name
		}
		ghc = github.New(gh, name, h.ghToken(gh.Auth.FromMachine))
	} else {
		ghc = github.New(gh, "", nil)
	}
	ghc.UseSecrets(h.secretsFor("github"))
	if err := h.addConnector(ghc); err != nil {
		h.logf("github connector: %v", err)
	}
	if im := h.cfg.IMessage; im.Configured() {
		if err := h.addIMessage(*im); err != nil {
			h.logf("imessage connector: %v", err)
		}
	}
}

// telegramProgram is jarvisd-telegram next to the running jarvisd ("" when there is none).
func telegramProgram() string { return siblingProgram("jarvisd-telegram") }

// siblingProgram is a program released with jarvisd (update.ProgramNames) next to the running jarvisd ("" when there
// is none).
func siblingProgram(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	p := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// telegramPluginID is the built-in plugin the Telegram connector runs in.
const telegramPluginID = "telegram"

// telegramManifest is the Telegram connector as a built-in plugin (docs/plan-telegram.md, docs/plugins.md): the
// program at path, contributing the connector with what it declares (spec), known before it ever runs: switched
// off, it still lists its capabilities and signs in and out. It runs while the connector is on.
func telegramManifest(h *Hub, path string) *plugin.Manifest {
	m := spec.Manifest()
	item := plugin.ConnectorItem{ID: spec.ID, Name: spec.Name, Hosts: m.Hosts, AgentTools: m.AgentTools, Unlisted: m.Unlisted,
		ConfirmAlways: m.ConfirmAlways, PollSeconds: int(m.Poll / time.Second), Kinds: m.Kinds}
	for _, c := range m.Capabilities {
		item.Capabilities = append(item.Capabilities, string(c))
	}
	items, _ := json.Marshal([]plugin.ConnectorItem{item})
	return &plugin.Manifest{ManifestVersion: plugin.ManifestVersion, ID: telegramPluginID, Name: spec.Name, Version: h.version,
		Description: "Telegram 连接器（personad 自带）", Engines: plugin.Engines{PluginProtocol: plugin.Protocol},
		Runtime: &plugin.Runtime{Command: plugin.Command{Path: filepath.Base(path)}}, Contributes: map[string]json.RawMessage{"connectors": items}}
}

// sameBuild holds a built-in plugin's program (named name, for people) to the hub's build: the two come from one
// build (`version -v` says the same).
func sameBuild(h *Hub, name, path string) func(context.Context) error {
	want := fmt.Sprintf("%s\nchannel %s, build %d", h.version, h.build.Channel, h.build.Number)
	return func(ctx context.Context) error {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("没有找到 %s 的程序（%s）", name, path)
		}
		vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(vctx, path, "version", "-v").Output()
		if err != nil {
			return fmt.Errorf("%s 的程序运行不了：%v", name, err)
		}
		if got := strings.TrimSpace(string(out)); got != strings.TrimSpace(want) {
			first := func(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }
			return fmt.Errorf("%s 的程序和 hub 不是同一个版本（%s / %s）", name, first(got), first(want))
		}
		return nil
	}
}

// telegramConnector is the program at path as the hub's connector, run as the built-in plugin: what the plugin
// registry does for it, for one that is not registered (tests).
func (h *Hub) telegramConnector(path string) *proc.Conn {
	ps := newPlugins(h.dataDirOrTemp(), nil, h.logf)
	ps.h, ps.ctx = h, context.Background()
	if err := ps.reg.AddBuiltin(telegramManifest(h, path), filepath.Dir(path), true); err != nil {
		h.logf("telegram: %v", err)
	}
	p := ps.reg.Get(telegramPluginID)
	hp := &hubPlugin{id: telegramPluginID}
	s, err := ps.spec(context.Background(), p, hp)
	if err != nil {
		h.logf("telegram: %v", err)
	}
	pr := host.Start(context.Background(), s)
	for _, l := range hp.links {
		l.Proc = pr
	}
	return hp.conns[0]
}

// telegramSettings is what jarvisd-telegram gets at each start: the user's time zone by name, for the times the
// assistant's tools show (the program runs on the hub's clock, in UTC); "" while only the hub's own is known.
func (h *Hub) telegramSettings() json.RawMessage {
	b, _ := json.Marshal(map[string]string{"zone": h.userTZName()})
	return b
}

// ghToken reads the token of machine's gh login (credential.gh_token); it never goes to a log.
func (h *Hub) ghToken(machine string) github.TokenSource {
	return func(ctx context.Context, host string) (string, error) {
		m := h.localMachine(machine)
		if m == nil {
			return "", github.ErrNoMachine
		}
		var out struct {
			Token string `json:"token"`
		}
		err := m.call(ctx, "credential.gh_token", map[string]any{"host": host}, &out)
		var re *rpc.Error
		switch {
		case errors.Is(err, errOffline):
			return "", github.ErrOffline
		case errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method"):
			return "", github.ErrNodeTooOld
		case errors.As(err, &re) && re.Code == proto.CodeNoGh:
			return "", github.ErrNoGh
		case errors.As(err, &re) && re.Code == proto.CodeNotSignedIn:
			return "", github.ErrNotSignedIn
		case err != nil:
			return "", fmt.Errorf("%s: %s", m.cfg.Name, github.Scrub(err.Error()))
		case out.Token == "":
			return "", github.ErrNotSignedIn
		}
		return out.Token, nil
	}
}
