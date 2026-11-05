package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/textfmt"
)

// 设置 › 模型供应商 (docs/launchers.md): every launcher in one list. Those in each machine's ~/.zshrc are found by its
// node and remembered by the hub (it hands them over: launchers.zshrc); those added in the app are the assistant's —
// base URL, model, and the API key AES-GCM sealed with secret.key (the hub's data directory's, which the assistant
// reads: docs/assistant.md「进程」) — and given to every node, which runs them as `personad launch <name>`. Deleting a
// ~/.zshrc launcher only hides it in Persona.

const (
	launchersManagedKey = "launchers.managed" // []storedLauncher
	launchersHiddenKey  = "launchers.hidden"  // []string: ~/.zshrc launcher names hidden in Persona
	launchersZshrcKey   = "launchers.zshrc"   // map machine id → []proto.Launcher, the last list seen
)

// storedLauncher is a provider added in Persona as kept in settings: the key sealed.
type storedLauncher struct {
	proto.ManagedLauncher
	KeySealed string `json:"key_sealed,omitempty"`
	// HeadersSealed: Headers (ANTHROPIC_CUSTOM_HEADERS) sealed like the key; the embedded Headers is never stored.
	HeadersSealed string    `json:"headers_sealed,omitempty"`
	Preset        string    `json:"preset,omitempty"` // the plugin's provider it was added from
	Created       time.Time `json:"created"`
	Updated       time.Time `json:"updated"`
}

// LauncherView is one launcher for the app: where it comes from, which machines have it; never the key.
type LauncherView struct {
	Kind                   string   `json:"kind,omitempty"`
	API                    string   `json:"api,omitempty"`
	Name                   string   `json:"name"`
	Source                 string   `json:"source"` // zshrc | jarvis
	BaseURL                string   `json:"base_url,omitempty"`
	Model                  string   `json:"model,omitempty"`
	SmallModel             string   `json:"small_model,omitempty"`
	SubagentModel          string   `json:"subagent_model,omitempty"`
	CustomModel            string   `json:"custom_model,omitempty"`
	CustomModelName        string   `json:"custom_model_name,omitempty"`
	CustomModelDescription string   `json:"custom_model_description,omitempty"`
	Args                   []string `json:"args,omitempty"`
	HasKey                 bool     `json:"has_key,omitempty"`
	Auth                   string   `json:"auth,omitempty"`     // managed: api_key (x-api-key) or bearer
	Machines               []string `json:"machines,omitempty"` // zshrc: the machines whose ~/.zshrc defines it
	Hidden                 bool     `json:"hidden,omitempty"`
	// Managed only (#2455, proto.ManagedLauncher's fields): never the headers themselves, only has_headers.
	Models        []proto.ProviderModel `json:"models,omitempty"`
	OpusModel     string                `json:"opus_model,omitempty"`
	SonnetModel   string                `json:"sonnet_model,omitempty"`
	HaikuModel    string                `json:"haiku_model,omitempty"`
	FableModel    string                `json:"fable_model,omitempty"`
	SubagentForce bool                  `json:"subagent_force,omitempty"`
	HasHeaders    bool                  `json:"has_headers,omitempty"`
	Proxy         string                `json:"proxy,omitempty"`
	NoProxy       string                `json:"no_proxy,omitempty"`
	TimeoutMS     int                   `json:"timeout_ms,omitempty"`
	CompactWindow int                   `json:"compact_window,omitempty"`
	MaxContext    int                   `json:"max_context,omitempty"`
	Effort        string                `json:"effort,omitempty"`
	// Origin is ManagedLauncher.Source ("" made by hand, "zshrc:<alias>@<machine>", "preset:<id>"); "source" is taken.
	Origin string `json:"origin,omitempty"`
	// Definition per machine (zshrc launchers, detail only): the function body, secrets masked.
	Definitions map[string]string `json:"definitions,omitempty"`
	// Details per machine (zshrc launchers, detail only): what the launcher really sets when run (node/launcherprobe.go).
	Details map[string]proto.LauncherDetail `json:"details,omitempty"`
}

// launcherView is a managed launcher for the app: never the key or the headers.
func launcherView(l storedLauncher) LauncherView {
	return LauncherView{Kind: l.Kind, API: l.API, Name: l.Name, Source: "jarvis", BaseURL: l.BaseURL, Model: l.Model, SmallModel: l.SmallModel,
		SubagentModel: l.SubagentModel, CustomModel: l.CustomModel, CustomModelName: l.CustomModelName,
		CustomModelDescription: l.CustomModelDescription, Args: l.Args, HasKey: l.KeySealed != "", Auth: l.Auth,
		Models: l.Models, OpusModel: l.OpusModel, SonnetModel: l.SonnetModel, HaikuModel: l.HaikuModel, FableModel: l.FableModel,
		SubagentForce: l.SubagentForce, HasHeaders: l.HeadersSealed != "", Proxy: l.Proxy, NoProxy: l.NoProxy, TimeoutMS: l.TimeoutMS,
		CompactWindow: l.CompactWindow, MaxContext: l.MaxContext, Effort: l.Effort, Origin: l.Source}
}

var managedLauncherName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// reservedLauncherNames are taken by claude itself and the /api/v1/launchers/<word> routes.
var reservedLauncherNames = []string{"claude", "presets", "test", "discover", "import"}

// ---- the key ----

// The providers' keys are sealed with secret.key in the hub's data directory (internal/secretkey), the file the hub
// sealed them with since before they were the assistant's, and its own secrets still are (bots, connectors).

func (s *Service) sealKey(plain string) (string, error) { return s.models.secret.Seal(plain) }

func (s *Service) openKey(sealed string) (string, error) { return s.models.secret.Open(sealed) }

// ---- what is stored ----

func (s *Service) storedLaunchers(ctx context.Context) []storedLauncher {
	var list []storedLauncher
	if st := s.store(); st != nil {
		_, _ = st.Setting(ctx, launchersManagedKey, &list)
	}
	return list
}

func (s *Service) hiddenLaunchers(ctx context.Context) []string {
	var list []string
	if st := s.store(); st != nil {
		_, _ = st.Setting(ctx, launchersHiddenKey, &list)
	}
	return list
}

func (s *Service) putSetting(ctx context.Context, key string, v any) error {
	st := s.store()
	if st == nil {
		return errors.New("no database")
	}
	return st.PutSetting(ctx, key, v)
}

// ---- the nodes ----

// managedForNodes: the launchers added in Persona with their keys and headers, for the nodes (not the Responses ones).
func (s *Service) managedForNodes(ctx context.Context) []proto.ManagedLauncher {
	out := []proto.ManagedLauncher{}
	for _, l := range s.storedLaunchers(ctx) {
		if l.Kind == "api" && l.API == "responses" {
			continue // a Messages gateway (launcher or kind:api) can run claude on a node; a Responses one cannot
		}
		m := l.ManagedLauncher
		if k, err := s.openKey(l.KeySealed); err == nil {
			m.APIKey = k
		} else {
			s.logf("launchers: open key of %s: %v", l.Name, err)
		}
		if hd, err := s.openKey(l.HeadersSealed); err == nil {
			m.Headers, m.HasHeaders = hd, hd != ""
		} else {
			s.logf("launchers: open headers of %s: %v", l.Name, err)
		}
		out = append(out, m)
	}
	return out
}

// pushManaged gives the online nodes (one machine when machine is not "") the launchers added in Persona; they keep
// them in memory. After a change; a node's new connection gets them right after it is configured (main.go's
// devices NodeCalls).
func (s *Service) pushManaged(ctx context.Context, machine string) {
	list := s.managedForNodes(ctx)
	for _, d := range s.devices(ctx) {
		if !d.Online || (machine != "" && d.ID != machine) {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := s.call(cctx, d.ID, assistantwire.DeviceLaunchersManaged, map[string]any{"launchers": list}, nil); err != nil {
			s.logf("launchers: give %s the managed launchers: %v", d.ID, err)
		}
		cancel()
	}
}

// zshrcLaunchers is one machine's ~/.zshrc launchers.
type zshrcLaunchers struct {
	MachineID string
	Launchers []proto.Launcher
}

// zshrcLaunchers is every machine's ~/.zshrc launchers in the machines' order: as last seen (an offline one's from
// the last time, kept in settings launchers.zshrc), or asked of the online nodes again first (refresh, or nothing
// seen since the assistant started; the node caches a minute).
func (s *Service) zshrcLaunchers(ctx context.Context, refresh bool) []zshrcLaunchers {
	st := &s.models
	st.zshrcMu.Lock()
	seen := st.zshrc
	st.zshrcMu.Unlock()
	devices := s.devices(ctx)
	if seen == nil || refresh {
		seen = map[string][]proto.Launcher{}
		if store := s.store(); store != nil {
			_, _ = store.Setting(ctx, launchersZshrcKey, &seen)
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		changed := false
		for _, d := range devices {
			if !d.Online {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
				defer cancel()
				var res struct {
					Launchers []proto.Launcher `json:"launchers"`
				}
				if err := s.call(cctx, d.ID, DeviceLaunchersList, map[string]any{}, &res); err != nil {
					return
				}
				mu.Lock()
				if !slices.Equal(seen[d.ID], res.Launchers) {
					seen[d.ID], changed = res.Launchers, true
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		if store := s.store(); changed && store != nil {
			_ = store.PutSetting(ctx, launchersZshrcKey, seen)
		}
		st.zshrcMu.Lock()
		st.zshrc = seen
		st.zshrcMu.Unlock()
	}
	out := make([]zshrcLaunchers, 0, len(devices))
	for _, d := range devices {
		out = append(out, zshrcLaunchers{MachineID: d.ID, Launchers: seen[d.ID]})
	}
	return out
}

// configuredLauncher is machine's hub.json launcher name (machines[].launchers, in the hello): a wrapper the node may
// not list, the agent the user said it runs. The node runs a launcher it does not list only when it is vouched for
// like this (configured, node runnableLauncher).
func (s *Service) configuredLauncher(ctx context.Context, machine, name string) (ConfiguredLauncher, bool) {
	for _, l := range s.hello(ctx).Models.Launchers[machine] {
		if l.Name == name {
			return l, true
		}
	}
	return ConfiguredLauncher{}, false
}

// launcherParams are a node launcher method's params for name on machine: configured (and its agent_type) when
// hub.json names it there.
func (s *Service) launcherParams(ctx context.Context, machine, name string, params map[string]any) map[string]any {
	params["name"] = name
	if l, ok := s.configuredLauncher(ctx, machine, name); ok {
		params["configured"] = true
		if l.AgentType != "" {
			params["agent_type"] = l.AgentType
		}
	}
	return params
}

// launcherViews is the whole list: managed first (newest last), then ~/.zshrc ones by name; hidden ones flagged.
func (s *Service) launcherViews(ctx context.Context) []LauncherView {
	return s.launcherViewsOf(ctx, s.zshrcLaunchers(ctx, false))
}

func (s *Service) launcherViewsOf(ctx context.Context, zshrc []zshrcLaunchers) []LauncherView {
	var out []LauncherView
	managed := map[string]bool{}
	for _, l := range s.storedLaunchers(ctx) {
		managed[l.Name] = true
		out = append(out, launcherView(l))
	}
	hidden := s.hiddenLaunchers(ctx)
	byName := map[string]*LauncherView{}
	var names []string
	for _, m := range zshrc {
		for _, l := range m.Launchers {
			if managed[l.Name] || l.Name == "claude" || (l.Source != "" && l.Source != "zshrc") {
				continue // a runtime's launcher (DSH, node/runtimes.go) is not a model to configure here
			}
			v := byName[l.Name]
			if v == nil {
				v = &LauncherView{Name: l.Name, Source: "zshrc", BaseURL: l.BaseURL, Model: l.Model, Hidden: slices.Contains(hidden, l.Name)}
				byName[l.Name] = v
				names = append(names, l.Name)
			}
			v.Machines = append(v.Machines, m.MachineID)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, *byName[n])
	}
	return out
}

// machineLaunchers is what a machine can start (the pickers: new chat, the assistant's model): its own launchers
// (the node's list) but the hidden ones, then the managed ones.
func (s *Service) machineLaunchers(ctx context.Context, machine string) ([]proto.Launcher, error) {
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	if err := s.call(ctx, machine, DeviceLaunchersList, map[string]any{}, &res); err != nil {
		return nil, err
	}
	hidden := s.hiddenLaunchers(ctx)
	managed := s.storedLaunchers(ctx)
	out := make([]proto.Launcher, 0, len(res.Launchers)+len(managed))
	for _, l := range res.Launchers {
		if slices.Contains(hidden, l.Name) || slices.ContainsFunc(managed, func(m storedLauncher) bool { return m.Name == l.Name }) {
			continue
		}
		if l.Source == "" { // a runtime's launcher (node/runtimes.go) keeps its own source, e.g. "dsh"
			l.Source = "zshrc"
		}
		out = append(out, l)
	}
	for _, m := range managed {
		if m.Kind == "api" {
			continue
		}
		out = append(out, proto.Launcher{Name: m.Name, BaseURL: m.BaseURL, Model: m.Model, Source: "jarvis"})
	}
	return out, nil
}

// launcherAvailable: whether the machine can still run this launcher name (plain claude always; a deleted managed
// launcher or a renamed ~/.zshrc one does not). A failed list counts as available.
func (s *Service) launcherAvailable(ctx context.Context, machine, name string) bool {
	if name == "" || name == "claude" {
		return true
	}
	if slices.ContainsFunc(s.storedLaunchers(ctx), func(l storedLauncher) bool { return l.Name == name }) {
		return true
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := s.machineLaunchers(cctx, machine)
	if err != nil {
		return true
	}
	return slices.ContainsFunc(list, func(l proto.Launcher) bool { return l.Name == name })
}

// launcherHasModel: whether the launcher offers model (its id or what an alias resolves to), from its list as it was
// a few minutes ago at most. Unknown (an agent with no list, a failed call) counts as yes.
func (s *Service) launcherHasModel(ctx context.Context, machine, launcher, model string) bool {
	key := machine + "\x00" + launcher
	c := &s.models
	c.cacheMu.Lock()
	if c.cacheAt != nil && time.Since(c.cacheAt[key]) < 5*time.Minute {
		set := c.have[key]
		c.cacheMu.Unlock()
		return set == nil || set[model]
	}
	c.cacheMu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := s.askLauncherModels(cctx, machine, launcher, false)
	if err != nil {
		return true
	}
	set := map[string]bool{}
	for _, x := range res.Models {
		set[x.ID] = true
		if x.Resolved != "" {
			set[x.Resolved] = true
		}
	}
	if len(set) == 0 {
		return true
	}
	c.cacheMu.Lock()
	if c.cacheAt == nil {
		c.cacheAt, c.have = map[string]time.Time{}, map[string]map[string]bool{}
	}
	c.cacheAt[key], c.have[key] = time.Now(), set
	c.cacheMu.Unlock()
	return set[model]
}

// askLauncherModels is machine's node's /model list of launcher name (node/launcher_models.go), with the models of
// the plugin gateway behind it.
func (s *Service) askLauncherModels(ctx context.Context, machine, name string, refresh bool) (proto.LauncherModels, error) {
	var res proto.LauncherModels
	if err := s.call(ctx, machine, assistantwire.DeviceLaunchersModels, s.launcherParams(ctx, machine, name, map[string]any{"refresh": refresh}), &res); err != nil {
		return res, err
	}
	if res.Models == nil {
		res.Models = []proto.AgentModel{}
	}
	return s.presetModels(ctx, machine, name, res), nil
}

// ---- the app's API ----

type launcherInput struct {
	Kind                   string   `json:"kind"`
	API                    string   `json:"api"`
	Name                   string   `json:"name"`
	BaseURL                string   `json:"base_url"`
	Model                  string   `json:"model"`
	SmallModel             string   `json:"small_model"`
	SubagentModel          string   `json:"subagent_model"`
	CustomModel            string   `json:"custom_model"`
	CustomModelName        string   `json:"custom_model_name"`
	CustomModelDescription string   `json:"custom_model_description"`
	Args                   []string `json:"args"`
	APIKey                 *string  `json:"api_key"` // nil or "" on an update keeps the key
	Auth                   string   `json:"auth"`    // api_key (x-api-key, the default) or bearer (Authorization: Bearer)
	// Preset is a plugin's provider it is added from (GET /api/v1/launchers/presets): what is left empty is filled in.
	Preset string `json:"preset,omitempty"`

	// #2455's fields (proto.ManagedLauncher). Each is a pointer: left out (an app from before them) keeps what is
	// stored on an update. Headers "" clears them; like the key they are sealed and never sent back.
	Models        *[]proto.ProviderModel `json:"models"`
	OpusModel     *string                `json:"opus_model"`
	SonnetModel   *string                `json:"sonnet_model"`
	HaikuModel    *string                `json:"haiku_model"`
	FableModel    *string                `json:"fable_model"`
	SubagentForce *bool                  `json:"subagent_force"`
	Headers       *string                `json:"headers"`
	Proxy         *string                `json:"proxy"`
	NoProxy       *string                `json:"no_proxy"`
	TimeoutMS     *int                   `json:"timeout_ms"`
	CompactWindow *int                   `json:"compact_window"`
	MaxContext    *int                   `json:"max_context"`
	Effort        *string                `json:"effort"`
}

// checkEffort: a thinking level Claude Code takes (CLAUDE_CODE_EFFORT_LEVEL), or none. whose is the error's key
// (err_effort_<whose>: default, model, model_default), model the model it names.
func checkEffort(whose, model, e string) error {
	if e == "" || slices.Contains(proto.EffortLevels, e) {
		return nil
	}
	return i18n.Err("assistant-models_launchers.err_effort_"+whose, i18n.Args{"model": model, "effort": strconv.Quote(e), "levels": strings.Join(proto.EffortLevels, " / ")})
}

// checkGateway is a gateway's base URL: http(s), a host, nothing that carries a credential.
func checkGateway(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return i18n.Err("assistant-models_launchers.err_base_url")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return i18n.Err("assistant-models_launchers.err_base_url_parts")
	}
	return nil
}

// checkHeaders: one "Name: value" a line (ANTHROPIC_CUSTOM_HEADERS); the error never quotes a value.
func checkHeaders(raw string) error {
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(strings.TrimSpace(name), " \t") {
			return i18n.Err("assistant-models_launchers.err_header_line", i18n.Args{"line": i + 1})
		}
	}
	return nil
}

func (in launcherInput) checkProvider() error {
	if in.Models != nil {
		seen := map[string]bool{}
		for _, m := range *in.Models {
			id := strings.TrimSpace(m.ID)
			if id == "" {
				return i18n.Err("assistant-models_launchers.err_model_no_id")
			}
			if seen[id] {
				return i18n.Err("assistant-models_launchers.err_model_duplicate", i18n.Args{"model": id})
			}
			seen[id] = true
			if m.ContextWindow < 0 {
				return i18n.Err("assistant-models_launchers.err_model_context_negative", i18n.Args{"model": id})
			}
			for _, e := range m.Efforts {
				if e == "" {
					return i18n.Err("assistant-models_launchers.err_model_effort_empty", i18n.Args{"model": id})
				}
				if err := checkEffort("model", id, e); err != nil {
					return err
				}
			}
			if err := checkEffort("model_default", id, m.DefaultEffort); err != nil {
				return err
			}
			if m.DefaultEffort != "" && len(m.Efforts) > 0 && !slices.Contains(m.Efforts, m.DefaultEffort) {
				return i18n.Err("assistant-models_launchers.err_model_default_effort_unlisted", i18n.Args{"model": id})
			}
		}
	}
	if in.Effort != nil {
		if err := checkEffort("default", "", *in.Effort); err != nil {
			return err
		}
	}
	for _, v := range []struct {
		key   string
		value *int
	}{{"err_timeout_negative", in.TimeoutMS}, {"err_compact_window_negative", in.CompactWindow}, {"err_max_context_negative", in.MaxContext}} {
		if v.value != nil && *v.value < 0 {
			return i18n.Err("assistant-models_launchers." + v.key)
		}
	}
	if in.Proxy != nil && strings.TrimSpace(*in.Proxy) != "" {
		u, err := url.Parse(strings.TrimSpace(*in.Proxy))
		if err != nil || u.Host == "" || !slices.Contains([]string{"http", "https", "socks5", "socks5h"}, u.Scheme) {
			return i18n.Err("assistant-models_launchers.err_proxy")
		}
		// The proxy is stored and shown as is (not sealed): a password in it would go back to every client.
		if u.User != nil {
			return i18n.Err("assistant-models_launchers.err_proxy_credentials")
		}
	}
	if in.Headers != nil {
		if err := checkHeaders(*in.Headers); err != nil {
			return err
		}
	}
	for _, v := range []*string{in.OpusModel, in.SonnetModel, in.HaikuModel, in.FableModel} {
		if v != nil && len(strings.TrimSpace(*v)) > 200 {
			return i18n.Err("assistant-models_launchers.err_tier_model_long")
		}
	}
	return nil
}

// applyProvider writes the #2455 fields given into s (headers apart: they are sealed by the caller).
func (in launcherInput) applyProvider(s *storedLauncher) {
	str := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	num := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	if in.Models != nil {
		var models []proto.ProviderModel
		for _, m := range *in.Models {
			m.ID, m.Name = strings.TrimSpace(m.ID), strings.TrimSpace(m.Name)
			models = append(models, m)
		}
		s.Models = models
	}
	str(&s.OpusModel, in.OpusModel)
	str(&s.SonnetModel, in.SonnetModel)
	str(&s.HaikuModel, in.HaikuModel)
	str(&s.FableModel, in.FableModel)
	str(&s.Proxy, in.Proxy)
	str(&s.NoProxy, in.NoProxy)
	str(&s.Effort, in.Effort)
	num(&s.TimeoutMS, in.TimeoutMS)
	num(&s.CompactWindow, in.CompactWindow)
	num(&s.MaxContext, in.MaxContext)
	if in.SubagentForce != nil {
		s.SubagentForce = *in.SubagentForce
	}
}

var errNoPreset = i18n.Err("assistant-models_launchers.err_no_preset")

func (in launcherInput) check() error {
	if in.Kind != "" && in.Kind != "api" && in.Kind != "launcher" {
		return i18n.Err("assistant-models_launchers.err_kind")
	}
	if in.API != "" && in.API != "messages" && in.API != "responses" {
		return i18n.Err("assistant-models_launchers.err_api")
	}
	if in.Kind != "api" && in.API == "responses" {
		return i18n.Err("assistant-models_launchers.err_claude_messages")
	}
	if in.Auth != "" && in.Auth != "api_key" && in.Auth != "bearer" {
		return i18n.Err("assistant-models_launchers.err_auth")
	}
	if in.Kind == "api" && (len(in.Args) != 0 || in.SmallModel != "" || in.SubagentModel != "" || in.CustomModel != "") {
		return i18n.Err("assistant-models_launchers.err_api_launch_fields")
	}
	for _, v := range []struct{ name, value string }{{"err_subagent_model_long", in.SubagentModel}, {"err_custom_model_long", in.CustomModel}} {
		if len(strings.TrimSpace(v.value)) > 200 {
			return i18n.Err("assistant-models_launchers." + v.name)
		}
	}
	if in.CustomModelName == "" && in.CustomModelDescription != "" {
		return i18n.Err("assistant-models_launchers.err_custom_model_name")
	}
	if !managedLauncherName.MatchString(in.Name) || slices.Contains(reservedLauncherNames, in.Name) {
		return i18n.Err("assistant-models_launchers.err_name")
	}
	if err := checkGateway(in.BaseURL); err != nil {
		return err
	}
	if err := in.checkProvider(); err != nil {
		return err
	}
	if strings.TrimSpace(in.Model) == "" {
		return i18n.Err("assistant-models_launchers.err_model_required")
	}
	for _, a := range in.Args {
		if !strings.HasPrefix(a, "-") {
			return i18n.Err("assistant-models_launchers.err_arg_dash", i18n.Args{"arg": strconv.Quote(a)})
		}
	}
	return nil
}

func (s *Service) launcherRoutes() {
	s.handle("GET /api/v1/launchers", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		// A launcher just added to a ~/.zshrc shows up (the node caches a minute).
		apierr.WriteJSON(w, map[string]any{"launchers": s.launcherViewsOf(ctx, s.zshrcLaunchers(ctx, true))})
	})
	s.handle("GET /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		name := r.PathValue("name")
		for _, v := range s.launcherViews(ctx) {
			if v.Name != name {
				continue
			}
			if v.Source == "zshrc" {
				// Every machine at once, the text and the probe apart: a launcher that fetches a token first is slow to
				// probe (the node allows 15 s), and its text should still show.
				v.Definitions, v.Details = map[string]string{}, map[string]proto.LauncherDetail{}
				var mu sync.Mutex
				var wg sync.WaitGroup
				for _, id := range v.Machines {
					wg.Add(2)
					go func() {
						defer wg.Done()
						var res struct {
							Definition string `json:"definition"`
						}
						if err := s.call(ctx, id, assistantwire.DeviceLaunchersShow, map[string]any{"name": name}, &res); err == nil {
							mu.Lock()
							v.Definitions[id] = res.Definition
							mu.Unlock()
						}
					}()
					go func() {
						defer wg.Done()
						var res struct {
							Detail proto.LauncherDetail `json:"detail"`
						}
						if err := s.call(ctx, id, DeviceLaunchersProbe, s.launcherParams(ctx, id, name, map[string]any{}), &res); err == nil {
							mu.Lock()
							v.Details[id] = res.Detail
							mu.Unlock()
						}
					}()
				}
				wg.Wait()
			}
			apierr.WriteJSON(w, map[string]any{"launcher": v})
			return
		}
		apierr.Write(w, http.StatusNotFound, errors.New("no such launcher"))
	})
	s.handle("POST /api/v1/launchers", func(w http.ResponseWriter, r *http.Request) {
		var in launcherInput
		if err := decodeBody(w, r, &in, 1<<20); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		if err := s.presetFill(r.Context(), &in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		if err := in.check(); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		for _, v := range s.launcherViews(r.Context()) {
			if v.Name == in.Name {
				apierr.Write(w, http.StatusConflict, i18n.Err("assistant-models_launchers.err_name_taken", i18n.Args{"name": in.Name}))
				return
			}
		}
		s.saveLauncher(w, r, in, nil)
	})
	s.handle("PUT /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in launcherInput
		if err := decodeBody(w, r, &in, 1<<20); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		in.Name = r.PathValue("name")
		if err := in.check(); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		for _, l := range s.storedLaunchers(r.Context()) {
			if l.Name == in.Name {
				s.saveLauncher(w, r, in, &l)
				return
			}
		}
		apierr.Write(w, http.StatusNotFound, i18n.Err("assistant-models_launchers.err_not_managed"))
	})
	s.handle("DELETE /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx, name := r.Context(), r.PathValue("name")
		list := s.storedLaunchers(ctx)
		if i := slices.IndexFunc(list, func(l storedLauncher) bool { return l.Name == name }); i >= 0 {
			// Not under a category that runs on it (checked and deleted under the configuration's lock; the nodes are
			// told after it).
			s.models.mu.Lock()
			uses, err := s.providerModelUses(ctx, launcherProvider(name))
			if err == nil && len(uses) == 0 {
				err = s.putSetting(ctx, launchersManagedKey, slices.Delete(list, i, i+1))
			}
			s.models.mu.Unlock()
			if err != nil {
				apierr.Write(w, 500, err)
				return
			}
			if len(uses) > 0 {
				apierr.Write(w, 409, i18n.Err("assistant-models_launchers.err_in_use", i18n.Args{"uses": strings.Join(uses, i18n.T(s.requestCtxLang(r.Context()), "assistant-models_launchers.list_sep"))}))
				return
			}
			s.pushManaged(ctx, "")
			apierr.WriteJSON(w, map[string]any{"ok": true, "deleted": true})
			return
		}
		hidden := s.hiddenLaunchers(ctx) // a ~/.zshrc launcher: hidden in Persona, the file is not touched
		if !slices.Contains(hidden, name) {
			hidden = append(hidden, name)
		}
		if err := s.putSetting(ctx, launchersHiddenKey, hidden); err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true, "hidden": true})
	})
	// 测试可用性: one real request through the launcher on a machine (default: the assistant's).
	s.handle("POST /api/v1/launchers/{name}/check", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
		defer cancel()
		for _, l := range s.storedLaunchers(ctx) {
			if l.Name == r.PathValue("name") && l.Kind == "api" {
				started := time.Now()
				config, err := s.apiConfig(ctx, ModelBinding{Provider: launcherProvider(l.Name), Model: l.Model})
				answer := ""
				if err == nil {
					answer, err = ai.New(config).Complete(ctx, "", prompts.RenderLauncherCheck(), 128)
				}
				result := proto.LauncherCheck{Agent: "api", Model: config.Model, Answer: textfmt.Clip(textfmt.OneLine(answer), 200), OK: err == nil,
					Millis: int(time.Since(started).Milliseconds())}
				if err != nil {
					result.Error = textfmt.Clip(textfmt.OneLine(err.Error()), 400)
				}
				apierr.WriteJSON(w, map[string]any{"check": result})
				return
			}
		}
		machine := r.URL.Query().Get("machine") // the id the app knows
		if _, ok := s.device(ctx, machine); !ok {
			machine = s.settings(ctx).MachineID
		}
		if _, ok := s.device(ctx, machine); !ok {
			apierr.Write(w, http.StatusBadRequest, errors.New("no machine to test on"))
			return
		}
		var res proto.LauncherCheck
		if err := s.call(ctx, machine, assistantwire.DeviceLaunchersCheck, map[string]any{"name": r.PathValue("name")}, &res); err != nil {
			apierr.Call(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"machine": machine, "check": res})
	})
	s.handle("POST /api/v1/launchers/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		ctx, name := r.Context(), r.PathValue("name")
		hidden := slices.DeleteFunc(s.hiddenLaunchers(ctx), func(n string) bool { return n == name })
		if err := s.putSetting(ctx, launchersHiddenKey, hidden); err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
}

// sessionLaunchers is the machine's launchers with the configured ones (hub.json machines[].launchers) applied: a
// configured launcher overrides the type, base URL and model of the one with its name, or adds a wrapper the node
// does not list. A launcher without a type gets the agent it was found to start, else one by its name.
func sessionLaunchers(discovered, configured []ConfiguredLauncher) []ConfiguredLauncher {
	list := append([]ConfiguredLauncher{}, discovered...)
	for _, override := range configured {
		found := false
		for i := range list {
			if list[i].Name == override.Name {
				if override.AgentType != "" {
					list[i].AgentType = override.AgentType
				}
				if override.BaseURL != "" {
					list[i].BaseURL = override.BaseURL
				}
				if override.Model != "" {
					list[i].Model = override.Model
				}
				found = true
				break
			}
		}
		if !found && override.Name != "" {
			list = append(list, override)
		}
	}
	for i := range list {
		if list[i].AgentType == "" {
			switch {
			case list[i].Agent != "":
				list[i].AgentType = list[i].Agent
			case list[i].Name == "codex" || list[i].Name == "traex":
				list[i].AgentType = list[i].Name
			default:
				list[i].AgentType = "claude"
			}
		}
	}
	return list
}

var errUnknownMachine = errors.New("unknown machine")

// machineLauncherRoutes are 新建 Agent's pickers on one machine: its launchers with the agent each starts
// (sessionLaunchers), and a launcher's /model list (node/launcher_models.go; the node keeps a list 10 minutes,
// ?refresh=1 asks the CLI again).
func (s *Service) machineLauncherRoutes() {
	s.handle("GET /api/v1/machines/{mid}/launchers", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		mid := r.PathValue("mid")
		if _, ok := s.device(ctx, mid); !ok {
			apierr.Write(w, http.StatusNotFound, errUnknownMachine)
			return
		}
		ls, err := s.machineLaunchers(ctx, mid)
		if err != nil {
			apierr.Call(w, err)
			return
		}
		discovered := make([]ConfiguredLauncher, 0, len(ls))
		for _, l := range ls {
			discovered = append(discovered, ConfiguredLauncher{Launcher: l})
		}
		apierr.WriteJSON(w, map[string]any{"launchers": sessionLaunchers(discovered, s.hello(ctx).Models.Launchers[mid]), "default": s.pickerDefault(ctx, r)})
	})
	s.handle("GET /api/v1/machines/{mid}/launchers/{name}/models", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
		defer cancel()
		mid := r.PathValue("mid")
		if _, ok := s.device(ctx, mid); !ok {
			apierr.Write(w, http.StatusNotFound, errUnknownMachine)
			return
		}
		res, err := s.askLauncherModels(ctx, mid, r.PathValue("name"), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			writeLauncherModelsErr(w, err)
			return
		}
		apierr.WriteJSON(w, res)
	})
	s.handle("GET /api/v1/launchers/presets", func(w http.ResponseWriter, r *http.Request) {
		apierr.WriteJSON(w, map[string]any{"presets": s.launcherPresets(r.Context())})
	})
}

func (s *Service) saveLauncher(w http.ResponseWriter, r *http.Request, in launcherInput, old *storedLauncher) {
	ctx := r.Context()
	now := time.Now().UTC()
	l := storedLauncher{Created: now, Updated: now}
	if old != nil {
		l = *old
		l.Updated = now
	}
	l.Name, l.BaseURL, l.Model, l.SmallModel = in.Name, strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		strings.TrimSpace(in.Model), strings.TrimSpace(in.SmallModel)
	l.SubagentModel, l.CustomModel = strings.TrimSpace(in.SubagentModel), strings.TrimSpace(in.CustomModel)
	l.CustomModelName, l.CustomModelDescription = strings.TrimSpace(in.CustomModelName), strings.TrimSpace(in.CustomModelDescription)
	l.Kind, l.API = in.Kind, in.API
	l.Args = in.Args
	if in.Preset != "" {
		l.Preset = in.Preset
	}
	switch {
	case in.API == "responses": // always goes as Bearer: nothing to keep
		l.Auth = ""
	case in.Auth == "bearer":
		l.Auth = in.Auth
	case in.Auth == "api_key":
		l.Auth = ""
	} // no auth in the request (an app from before it): a change keeps what is stored
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		sealed, err := s.sealKey(strings.TrimSpace(*in.APIKey))
		if err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		l.KeySealed = sealed
	}
	in.applyProvider(&l)
	if in.Headers != nil { // left out keeps them, "" clears them
		l.HeadersSealed = ""
		if hd := strings.TrimSpace(*in.Headers); hd != "" {
			sealed, err := s.sealKey(hd)
			if err != nil {
				apierr.Write(w, http.StatusInternalServerError, err)
				return
			}
			l.HeadersSealed = sealed
		}
	}
	l.APIKey, l.Headers, l.HasHeaders = "", "", false // only ever stored sealed
	list := s.storedLaunchers(ctx)
	if i := slices.IndexFunc(list, func(x storedLauncher) bool { return x.Name == l.Name }); i >= 0 {
		list[i] = l
	} else {
		list = append(list, l)
	}
	if err := s.putSetting(ctx, launchersManagedKey, list); err != nil {
		apierr.Write(w, http.StatusInternalServerError, err)
		return
	}
	s.pushManaged(ctx, "")
	apierr.WriteJSON(w, map[string]any{"launcher": launcherView(l)})
}

// pickMachine is the machine to ask about a launcher the machines in has have: want when it has it, else the
// assistant's, the hub's own, then the others; the first of them online. false when none is.
func (s *Service) pickMachine(ctx context.Context, has []string, want string) (string, bool) {
	devices := s.devices(ctx)
	local := ""
	for _, d := range devices {
		if d.Kind == "local" {
			local = d.ID
			break
		}
	}
	order := append([]string{want, s.settings(ctx).MachineID, local}, has...)
	for _, id := range order {
		if id == "" || !slices.Contains(has, id) {
			continue
		}
		if slices.ContainsFunc(devices, func(d Device) bool { return d.ID == id && d.Online }) {
			return id, true
		}
	}
	return "", false
}

// providerModels is GET /api/v1/model-config/providers/{id}/models: a launcher provider's /model list for 模型配置
// (docs/model-config.md「模型配置选模型」). 模型配置 is not one machine's, so the list comes from a machine that has the
// launcher (pickMachine), and the answer says which. Other providers have no list to ask for: a plugin's comes with
// it, an API connection has none.
func (s *Service) providerModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	name, ok := strings.CutPrefix(r.PathValue("id"), "launcher.")
	if !ok || name == "" {
		apierr.Write(w, http.StatusNotFound, i18n.Err("assistant-models_launchers.err_no_models"))
		return
	}
	found, everywhere := name == "claude", name == "claude"
	var has []string
	for _, v := range s.launcherViews(ctx) {
		if v.Name != name {
			continue
		}
		if v.Kind == "api" {
			apierr.Write(w, http.StatusNotFound, i18n.Err("assistant-models_launchers.err_api_no_models"))
			return
		}
		found = true
		everywhere = everywhere || v.Source == "jarvis" || len(v.Machines) == 0 // personad launch runs on every machine
		has = append(has, v.Machines...)
	}
	if !found {
		apierr.Write(w, http.StatusNotFound, i18n.Err("assistant-models_launchers.err_no_provider"))
		return
	}
	if everywhere {
		has = has[:0]
		for _, d := range s.devices(ctx) {
			has = append(has, d.ID)
		}
	}
	machine, ok := s.pickMachine(ctx, has, r.URL.Query().Get("machine"))
	if !ok {
		apierr.Write(w, http.StatusServiceUnavailable, i18n.Err("assistant-models_launchers.err_all_offline"))
		return
	}
	res, err := s.askLauncherModels(ctx, machine, name, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeLauncherModelsErr(w, err)
		return
	}
	apierr.WriteJSON(w, struct {
		proto.LauncherModels
		Machine string `json:"machine"`
	}{res, machine})
}

// writeLauncherModelsErr answers a failed /model list: a node too old to list them is 404 like an older hub (the apps
// leave the picker out instead of offering a retry that cannot work).
func writeLauncherModelsErr(w http.ResponseWriter, err error) {
	if strings.HasPrefix(err.Error(), "unknown method") {
		apierr.WriteCode(w, http.StatusNotFound, proto.CodeNodeOutdated, i18n.Err("assistant-models_launchers.err_node_outdated"))
		return
	}
	apierr.Call(w, err)
}
