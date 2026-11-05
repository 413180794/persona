package hub

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// 设置 › 模型 (docs/launchers.md): every launcher in one list. Those in each machine's ~/.zshrc are found by its node and
// remembered here (the last list of every machine, so an offline one's still show); those added in the app are kept
// by the hub — base URL, model, and the API key AES-GCM sealed with ~/.jarvis/secret.key — and sent to every node,
// which runs them as `jarvisd launch <name>`. Deleting a ~/.zshrc launcher only hides it in Persona.

const (
	launchersManagedKey = "launchers.managed" // []storedLauncher
	launchersHiddenKey  = "launchers.hidden"  // []string: ~/.zshrc launcher names hidden in Persona
	launchersZshrcKey   = "launchers.zshrc"   // map machine id → []proto.Launcher, the last list seen
)

// storedLauncher is a managed launcher as kept in settings: the key sealed.
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

type launcherState struct {
	mu  sync.Mutex
	key []byte // AES key for API keys (secret.key)
}

var managedLauncherName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// reservedLauncherNames are taken by claude itself and the /api/v1/launchers/<word> routes.
var reservedLauncherNames = []string{"claude", "presets", "test", "discover", "import"}

func (h *Hub) launcherKey() ([]byte, error) {
	h.launchers.mu.Lock()
	defer h.launchers.mu.Unlock()
	if h.launchers.key != nil {
		return h.launchers.key, nil
	}
	path := filepath.Join(h.cfg.dir, "secret.key")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		b = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, b); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("%s must hold 32 bytes", path)
	}
	h.launchers.key = b
	return b, nil
}

func (h *Hub) sealKey(plain string) (string, error) {
	key, err := h.launcherKey()
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (h *Hub) openKey(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	key, err := h.launcherKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed key too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}

func (h *Hub) storedLaunchers(ctx context.Context) []storedLauncher {
	var list []storedLauncher
	if h.store != nil {
		_, _ = h.store.Setting(ctx, launchersManagedKey, &list)
	}
	return list
}

func (h *Hub) hiddenLaunchers(ctx context.Context) []string {
	var list []string
	if h.store != nil {
		_, _ = h.store.Setting(ctx, launchersHiddenKey, &list)
	}
	return list
}

func (h *Hub) put(ctx context.Context, key string, v any) error {
	if h.store == nil {
		return errors.New("no database")
	}
	return h.store.PutSetting(ctx, key, v)
}

// managedForNodes: the managed launchers with their keys, for nodes.
func (h *Hub) managedForNodes(ctx context.Context) []proto.ManagedLauncher {
	var out []proto.ManagedLauncher
	for _, s := range h.storedLaunchers(ctx) {
		if s.Kind == "api" && s.API == "responses" {
			continue // a Messages gateway (launcher or kind:api) can run claude on a node; a Responses one cannot
		}
		l := s.ManagedLauncher
		if k, err := h.openKey(s.KeySealed); err == nil {
			l.APIKey = k
		} else {
			h.logf("launchers: open key of %s: %v", s.Name, err)
		}
		if hd, err := h.openKey(s.HeadersSealed); err == nil {
			l.Headers, l.HasHeaders = hd, hd != ""
		} else {
			h.logf("launchers: open headers of %s: %v", s.Name, err)
		}
		out = append(out, l)
	}
	return out
}

// sendManaged gives one node the managed launchers (on connect).
func (m *Machine) sendManaged(ctx context.Context, call func(context.Context, string, any, any) error) {
	_ = call(ctx, "launchers.managed", map[string]any{"launchers": m.hub.managedForNodes(ctx)}, nil)
}

// pushManaged gives every online node the managed launchers (after a change).
func (h *Hub) pushManaged(ctx context.Context) {
	list := h.managedForNodes(ctx)
	for _, m := range h.machinesNow() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = m.call(cctx, "launchers.managed", map[string]any{"launchers": list}, nil)
		cancel()
	}
}

// zshrcLaunchers reads every online machine's ~/.zshrc launchers (the node caches them a minute) and remembers them;
// an offline machine's come from the last time.
func (h *Hub) zshrcLaunchers(ctx context.Context) map[string][]proto.Launcher {
	seen := map[string][]proto.Launcher{}
	if h.store != nil {
		_, _ = h.store.Setting(ctx, launchersZshrcKey, &seen)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	changed := false
	for _, m := range h.machinesNow() {
		wg.Add(1)
		go func(m *Machine) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			var res struct {
				Launchers []proto.Launcher `json:"launchers"`
			}
			if err := m.call(cctx, "launchers.list", map[string]any{}, &res); err != nil {
				return
			}
			mu.Lock()
			if !slices.Equal(seen[m.cfg.ID], res.Launchers) {
				seen[m.cfg.ID], changed = res.Launchers, true
			}
			mu.Unlock()
		}(m)
	}
	wg.Wait()
	if changed && h.store != nil {
		_ = h.store.PutSetting(ctx, launchersZshrcKey, seen)
	}
	return seen
}

// view is a managed launcher for the app: never the key or the headers.
func (s storedLauncher) view() LauncherView {
	return LauncherView{Kind: s.Kind, API: s.API, Name: s.Name, Source: "jarvis", BaseURL: s.BaseURL, Model: s.Model, SmallModel: s.SmallModel,
		SubagentModel: s.SubagentModel, CustomModel: s.CustomModel, CustomModelName: s.CustomModelName,
		CustomModelDescription: s.CustomModelDescription, Args: s.Args, HasKey: s.KeySealed != "", Auth: s.Auth,
		Models: s.Models, OpusModel: s.OpusModel, SonnetModel: s.SonnetModel, HaikuModel: s.HaikuModel, FableModel: s.FableModel,
		SubagentForce: s.SubagentForce, HasHeaders: s.HeadersSealed != "", Proxy: s.Proxy, NoProxy: s.NoProxy, TimeoutMS: s.TimeoutMS,
		CompactWindow: s.CompactWindow, MaxContext: s.MaxContext, Effort: s.Effort, Origin: s.Source}
}

// launcherViews is the whole list: managed first (newest last), then ~/.zshrc ones by name; hidden ones flagged.
func (h *Hub) launcherViews(ctx context.Context) []LauncherView {
	var out []LauncherView
	managed := map[string]bool{}
	for _, s := range h.storedLaunchers(ctx) {
		managed[s.Name] = true
		out = append(out, s.view())
	}
	hidden := h.hiddenLaunchers(ctx)
	byName := map[string]*LauncherView{}
	var names []string
	for _, m := range h.machinesNow() {
		for _, l := range h.zshrcLaunchersOf(ctx, m.cfg.ID) {
			if managed[l.Name] || l.Name == "claude" || (l.Source != "" && l.Source != "zshrc") {
				continue // a runtime's launcher (DSH, node/runtimes.go) is not a model to configure here
			}
			v := byName[l.Name]
			if v == nil {
				v = &LauncherView{Name: l.Name, Source: "zshrc", BaseURL: l.BaseURL, Model: l.Model, Hidden: slices.Contains(hidden, l.Name)}
				byName[l.Name] = v
				names = append(names, l.Name)
			}
			v.Machines = append(v.Machines, m.cfg.ID)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, *byName[n])
	}
	return out
}

// zshrcLaunchersOf is one machine's last known ~/.zshrc launchers (filled by zshrcLaunchers).
func (h *Hub) zshrcLaunchersOf(ctx context.Context, machine string) []proto.Launcher {
	h.launchers.mu.Lock()
	cached := h.launchersSeen
	h.launchers.mu.Unlock()
	if cached == nil {
		cached = h.zshrcLaunchers(ctx)
		h.launchers.mu.Lock()
		h.launchersSeen = cached
		h.launchers.mu.Unlock()
	}
	return cached[machine]
}

func (h *Hub) refreshZshrcLaunchers(ctx context.Context) {
	seen := h.zshrcLaunchers(ctx)
	h.launchers.mu.Lock()
	h.launchersSeen = seen
	h.launchers.mu.Unlock()
}

// machineLaunchers is what a machine can start (the pickers: new chat, the assistant's model): its ~/.zshrc
// launchers but the hidden ones, then the managed ones.
func (h *Hub) machineLaunchers(ctx context.Context, m *Machine) ([]proto.Launcher, error) {
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	if err := m.call(ctx, "launchers.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	hidden := h.hiddenLaunchers(ctx)
	managed := h.storedLaunchers(ctx)
	out := make([]proto.Launcher, 0, len(res.Launchers)+len(managed))
	for _, l := range res.Launchers {
		if slices.Contains(hidden, l.Name) || slices.ContainsFunc(managed, func(s storedLauncher) bool { return s.Name == l.Name }) {
			continue
		}
		if l.Source == "" { // a runtime's launcher (node/runtimes.go) keeps its own source, e.g. "dsh"
			l.Source = "zshrc"
		}
		out = append(out, l)
	}
	for _, s := range managed {
		if s.Kind == "api" {
			continue
		}
		out = append(out, proto.Launcher{Name: s.Name, BaseURL: s.BaseURL, Model: s.Model, Source: "jarvis"})
	}
	return out, nil
}

// --- app API ---

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
	return i18n.Err("hub-launchers.err_effort_"+whose, i18n.Args{"model": model, "effort": strconv.Quote(e), "levels": strings.Join(proto.EffortLevels, " / ")})
}

// checkGateway is a gateway's base URL: http(s), a host, nothing that carries a credential.
func checkGateway(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return i18n.Err("hub-launchers.err_base_url")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return i18n.Err("hub-launchers.err_base_url_parts")
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
			return i18n.Err("hub-launchers.err_header_line", i18n.Args{"line": i + 1})
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
				return i18n.Err("hub-launchers.err_model_no_id")
			}
			if seen[id] {
				return i18n.Err("hub-launchers.err_model_duplicate", i18n.Args{"model": id})
			}
			seen[id] = true
			if m.ContextWindow < 0 {
				return i18n.Err("hub-launchers.err_model_context_negative", i18n.Args{"model": id})
			}
			for _, e := range m.Efforts {
				if e == "" {
					return i18n.Err("hub-launchers.err_model_effort_empty", i18n.Args{"model": id})
				}
				if err := checkEffort("model", id, e); err != nil {
					return err
				}
			}
			if err := checkEffort("model_default", id, m.DefaultEffort); err != nil {
				return err
			}
			if m.DefaultEffort != "" && len(m.Efforts) > 0 && !slices.Contains(m.Efforts, m.DefaultEffort) {
				return i18n.Err("hub-launchers.err_model_default_effort_unlisted", i18n.Args{"model": id})
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
			return i18n.Err("hub-launchers." + v.key)
		}
	}
	if in.Proxy != nil && strings.TrimSpace(*in.Proxy) != "" {
		u, err := url.Parse(strings.TrimSpace(*in.Proxy))
		if err != nil || u.Host == "" || !slices.Contains([]string{"http", "https", "socks5", "socks5h"}, u.Scheme) {
			return i18n.Err("hub-launchers.err_proxy")
		}
		// The proxy is stored and shown as is (not sealed): a password in it would go back to every client.
		if u.User != nil {
			return i18n.Err("hub-launchers.err_proxy_credentials")
		}
	}
	if in.Headers != nil {
		if err := checkHeaders(*in.Headers); err != nil {
			return err
		}
	}
	for _, v := range []*string{in.OpusModel, in.SonnetModel, in.HaikuModel, in.FableModel} {
		if v != nil && len(strings.TrimSpace(*v)) > 200 {
			return i18n.Err("hub-launchers.err_tier_model_long")
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

var errNoPreset = i18n.Err("hub-launchers.err_no_preset")

func (in launcherInput) check() error {
	if in.Kind != "" && in.Kind != "api" && in.Kind != "launcher" {
		return i18n.Err("hub-launchers.err_kind")
	}
	if in.API != "" && in.API != "messages" && in.API != "responses" {
		return i18n.Err("hub-launchers.err_api")
	}
	if in.Kind != "api" && in.API == "responses" {
		return i18n.Err("hub-launchers.err_claude_messages")
	}
	if in.Auth != "" && in.Auth != "api_key" && in.Auth != "bearer" {
		return i18n.Err("hub-launchers.err_auth")
	}
	if in.Kind == "api" && (len(in.Args) != 0 || in.SmallModel != "" || in.SubagentModel != "" || in.CustomModel != "") {
		return i18n.Err("hub-launchers.err_api_launch_fields")
	}
	for _, v := range []struct{ name, value string }{{"err_subagent_model_long", in.SubagentModel}, {"err_custom_model_long", in.CustomModel}} {
		if len(strings.TrimSpace(v.value)) > 200 {
			return i18n.Err("hub-launchers." + v.name)
		}
	}
	if in.CustomModelName == "" && in.CustomModelDescription != "" {
		return i18n.Err("hub-launchers.err_custom_model_name")
	}
	if !managedLauncherName.MatchString(in.Name) || slices.Contains(reservedLauncherNames, in.Name) {
		return i18n.Err("hub-launchers.err_name")
	}
	if err := checkGateway(in.BaseURL); err != nil {
		return err
	}
	if err := in.checkProvider(); err != nil {
		return err
	}
	if strings.TrimSpace(in.Model) == "" {
		return i18n.Err("hub-launchers.err_model_required")
	}
	for _, a := range in.Args {
		if !strings.HasPrefix(a, "-") {
			return i18n.Err("hub-launchers.err_arg_dash", i18n.Args{"arg": strconv.Quote(a)})
		}
	}
	return nil
}

func (h *Hub) launcherRoutes(api *http.ServeMux) {
	h.launcherQuotaRoutes(api)
	h.launcherPresetRoutes(api)
	h.providerProbeRoutes(api)
	h.launcherImportRoutes(api)
	api.HandleFunc("GET /api/v1/launchers", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		h.refreshZshrcLaunchers(ctx) // a launcher just added to a ~/.zshrc shows up (the node caches a minute)
		writeJSON(w, map[string]any{"launchers": h.launcherViews(ctx)})
	})
	api.HandleFunc("GET /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		name := r.PathValue("name")
		for _, v := range h.launcherViews(ctx) {
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
					m := h.localMachine(id)
					if m == nil {
						continue
					}
					wg.Add(2)
					go func() {
						defer wg.Done()
						var res struct {
							Definition string `json:"definition"`
						}
						if err := m.call(ctx, "launchers.show", map[string]any{"name": name}, &res); err == nil {
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
						if err := m.call(ctx, "launchers.probe", map[string]any{"name": name, "configured": m.configuredLauncher(name)}, &res); err == nil {
							mu.Lock()
							v.Details[id] = res.Detail
							mu.Unlock()
						}
					}()
				}
				wg.Wait()
			}
			writeJSON(w, map[string]any{"launcher": v})
			return
		}
		writeErr(w, http.StatusNotFound, errors.New("no such launcher"))
	})
	api.HandleFunc("POST /api/v1/launchers", func(w http.ResponseWriter, r *http.Request) {
		var in launcherInput
		if err := decodeJSONBody(w, r, &in, 1<<20); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		if err := h.presetFill(r.Context(), &in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := in.check(); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx := r.Context()
		for _, v := range h.launcherViews(ctx) {
			if v.Name == in.Name {
				writeErr(w, http.StatusConflict, i18n.Err("hub-launchers.err_name_taken", i18n.Args{"name": in.Name}))
				return
			}
		}
		h.saveLauncher(w, r, in, nil)
	})
	api.HandleFunc("PUT /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in launcherInput
		if err := decodeJSONBody(w, r, &in, 1<<20); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		in.Name = r.PathValue("name")
		if err := in.check(); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		for _, s := range h.storedLaunchers(r.Context()) {
			if s.Name == in.Name {
				h.saveLauncher(w, r, in, &s)
				return
			}
		}
		writeErr(w, http.StatusNotFound, i18n.Err("hub-launchers.err_not_managed"))
	})
	api.HandleFunc("DELETE /api/v1/launchers/{name}", func(w http.ResponseWriter, r *http.Request) {
		ctx, name := r.Context(), r.PathValue("name")
		list := h.storedLaunchers(ctx)
		if i := slices.IndexFunc(list, func(s storedLauncher) bool { return s.Name == name }); i >= 0 {
			h.modelMu.Lock()
			defer h.modelMu.Unlock()
			uses, err := h.providerModelUses(ctx, launcherProvider(name))
			if err != nil {
				writeErr(w, 500, err)
				return
			}
			if len(uses) > 0 {
				writeErr(w, 409, i18n.Err("hub-launchers.err_in_use", i18n.Args{"uses": strings.Join(uses, i18n.T(h.requestLang(r), "hub-launchers.list_sep"))}))
				return
			}
			list = slices.Delete(list, i, i+1)
			if err := h.put(ctx, launchersManagedKey, list); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			h.pushManaged(ctx)
			writeJSON(w, map[string]any{"ok": true, "deleted": true})
			return
		}
		hidden := h.hiddenLaunchers(ctx) // a ~/.zshrc launcher: hidden in Persona, the file is not touched
		if !slices.Contains(hidden, name) {
			hidden = append(hidden, name)
		}
		if err := h.put(ctx, launchersHiddenKey, hidden); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "hidden": true})
	})
	// 测试可用性: one real request through the launcher on a machine (default: the assistant's, else the first online).
	api.HandleFunc("POST /api/v1/launchers/{name}/check", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
		defer cancel()
		for _, provider := range h.storedLaunchers(ctx) {
			if provider.Name == r.PathValue("name") && provider.Kind == "api" {
				started := time.Now()
				config, err := h.apiConfig(ctx, ModelBinding{Provider: launcherProvider(provider.Name), Model: provider.Model})
				answer := ""
				if err == nil {
					answer, err = ai.New(config).Complete(ctx, "", prompts.RenderLauncherCheck(), 128)
				}
				result := proto.LauncherCheck{Agent: "api", Model: config.Model, Answer: clip(oneLine(answer), 200), OK: err == nil, Millis: int(time.Since(started).Milliseconds())}
				if err != nil {
					result.Error = clip(oneLine(err.Error()), 400)
				}
				writeJSON(w, map[string]any{"check": result})
				return
			}
		}
		m := h.localMachine(r.URL.Query().Get("machine")) // the id the app knows (mesh_ids.go)
		if m == nil {
			m = h.localMachine(h.assistantProfile().MachineID)
		}
		if m == nil {
			writeErr(w, http.StatusBadRequest, errors.New("no machine to test on"))
			return
		}
		var res proto.LauncherCheck
		if err := m.call(ctx, "launchers.check", map[string]any{"name": r.PathValue("name")}, &res); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"machine": m.cfg.ID, "check": res})
	})
	api.HandleFunc("POST /api/v1/launchers/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		ctx, name := r.Context(), r.PathValue("name")
		hidden := slices.DeleteFunc(h.hiddenLaunchers(ctx), func(s string) bool { return s == name })
		if err := h.put(ctx, launchersHiddenKey, hidden); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
}

func (h *Hub) saveLauncher(w http.ResponseWriter, r *http.Request, in launcherInput, old *storedLauncher) {
	ctx := r.Context()
	now := time.Now().UTC()
	s := storedLauncher{Created: now, Updated: now}
	if old != nil {
		s = *old
		s.Updated = now
	}
	s.Name, s.BaseURL, s.Model, s.SmallModel = in.Name, strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		strings.TrimSpace(in.Model), strings.TrimSpace(in.SmallModel)
	s.SubagentModel, s.CustomModel = strings.TrimSpace(in.SubagentModel), strings.TrimSpace(in.CustomModel)
	s.CustomModelName, s.CustomModelDescription = strings.TrimSpace(in.CustomModelName), strings.TrimSpace(in.CustomModelDescription)
	s.Kind, s.API = in.Kind, in.API
	s.Args = in.Args
	if in.Preset != "" {
		s.Preset = in.Preset
	}
	switch {
	case in.API == "responses": // always goes as Bearer: nothing to keep
		s.Auth = ""
	case in.Auth == "bearer":
		s.Auth = in.Auth
	case in.Auth == "api_key":
		s.Auth = ""
	} // no auth in the request (an app from before it): a change keeps what is stored
	if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		sealed, err := h.sealKey(strings.TrimSpace(*in.APIKey))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.KeySealed = sealed
	}
	in.applyProvider(&s)
	if in.Headers != nil { // left out keeps them, "" clears them
		s.HeadersSealed = ""
		if hd := strings.TrimSpace(*in.Headers); hd != "" {
			sealed, err := h.sealKey(hd)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			s.HeadersSealed = sealed
		}
	}
	s.APIKey, s.Headers, s.HasHeaders = "", "", false // only ever stored sealed
	list := h.storedLaunchers(ctx)
	if i := slices.IndexFunc(list, func(x storedLauncher) bool { return x.Name == s.Name }); i >= 0 {
		list[i] = s
	} else {
		list = append(list, s)
	}
	if err := h.put(ctx, launchersManagedKey, list); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.pushManaged(ctx)
	writeJSON(w, map[string]any{"launcher": s.view()})
}

// --- the new-agent picker's launchers, with the agent each starts ---

// sessionLauncher is a launcher as GET /api/v1/machines/{mid}/launchers lists it: what machineLaunchers found, plus the
// agent it starts (agent_type: claude, codex, traex, or a runtime's agent such as dsh), for the clients that pick an
// agent first (Android).
type sessionLauncher struct {
	proto.Launcher
	AgentType string `json:"agent_type,omitempty"`
}

// agentTypeRe is what POST /api/v1/sessions takes as agent_type: claude, codex, traex, or a runtime's agent as the
// launcher list says (a runtime's launcher starts through its runtime, whatever the type); the node checks the rest.
var agentTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// sessionLaunchers is the machine's launchers with the configured ones (hub.json machines[].launchers) applied: a
// configured launcher overrides the type, base URL and model of the one with its name, or adds a wrapper the node
// does not list. A launcher without a type gets the agent it was found to start, else one by its name.
func sessionLaunchers(discovered, configured []sessionLauncher) []sessionLauncher {
	list := append([]sessionLauncher{}, discovered...)
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

// launcherModels is GET /api/v1/machines/{mid}/launchers/{name}/models: the models the launcher's agent offers in its
// /model menu, for 新建 Agent's model picker (node/launcher_models.go). The node keeps a list 10 minutes; ?refresh=1
// asks the CLI again.
func (h *Hub) launcherModels(w http.ResponseWriter, r *http.Request, m *Machine) {
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	res, err := h.askLauncherModels(ctx, m, r.PathValue("name"), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeLauncherModelsErr(w, err)
		return
	}
	writeJSON(w, res)
}

// configuredLauncher: m's hub.json names launcher name. The node asks a launcher that runs (its probe, gateway, models)
// only when it offers it itself or the hub vouches for it like this (node runnableLauncher).
func (m *Machine) configuredLauncher(name string) bool {
	for _, l := range m.cfg.Launchers {
		if l.Name == name {
			return true
		}
	}
	return false
}

// askLauncherModels asks machine m's node for launcher name's /model list (node/launcher_models.go).
func (h *Hub) askLauncherModels(ctx context.Context, m *Machine, name string, refresh bool) (proto.LauncherModels, error) {
	params := map[string]any{"name": name, "refresh": refresh}
	for _, l := range m.cfg.Launchers {
		if l.Name == name {
			params["configured"] = true // the node may not list it (a wrapper)
			if l.AgentType != "" {
				params["agent_type"] = l.AgentType // what the user said it runs
			}
		}
	}
	var res proto.LauncherModels
	if err := m.call(ctx, "launchers.models", params, &res); err != nil {
		return res, err
	}
	if res.Models == nil {
		res.Models = []proto.AgentModel{}
	}
	return h.presetModels(ctx, m, name, res), nil
}

// launcherAvailable: whether the machine can still run this launcher name (plain claude always; a deleted managed
// launcher or a renamed ~/.zshrc one does not). A failed list counts as available.
func (h *Hub) launcherAvailable(ctx context.Context, m *Machine, name string) bool {
	if name == "" || name == "claude" {
		return true
	}
	for _, s := range h.storedLaunchers(ctx) {
		if s.Name == name {
			return true
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := h.machineLaunchers(cctx, m)
	if err != nil {
		return true
	}
	return slices.ContainsFunc(list, func(l proto.Launcher) bool { return l.Name == name })
}

// launcherModelCache remembers which models a launcher offered, by machine and launcher, for a few minutes: a send
// checks its conversation's model against it (a model the gateway dropped fails every turn otherwise).
var launcherModelCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	have map[string]map[string]bool
}

// launcherHasModel: whether the launcher offers model (its id or what an alias resolves to). Unknown (an agent with
// no list, a failed call) counts as yes.
func (h *Hub) launcherHasModel(ctx context.Context, m *Machine, launcher, model string) bool {
	key := m.cfg.ID + "\x00" + launcher
	c := &launcherModelCache
	c.mu.Lock()
	if c.at != nil && time.Since(c.at[key]) < 5*time.Minute {
		set := c.have[key]
		c.mu.Unlock()
		return set == nil || set[model]
	}
	c.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := h.askLauncherModels(cctx, m, launcher, false)
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
	c.mu.Lock()
	if c.at == nil {
		c.at, c.have = map[string]time.Time{}, map[string]map[string]bool{}
	}
	c.at[key], c.have[key] = time.Now(), set
	c.mu.Unlock()
	return set[model]
}

func writeLauncherModelsErr(w http.ResponseWriter, err error) {
	var re *rpc.Error
	if errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method") {
		// 404 like an older hub: the apps leave the picker out instead of offering a retry that cannot work.
		writeCode(w, http.StatusNotFound, proto.CodeNodeOutdated, i18n.T(i18n.OfWriter(w), "hub-launchers.err_node_outdated"), nil)
		return
	}
	callErr(w, err)
}

// providerModels is GET /api/v1/model-config/providers/{id}/models: a launcher provider's /model list for 模型配置
// (docs/model-config.md「模型配置选模型」). 模型配置 is the hub's, not one machine's, so the list comes from a machine
// that has the launcher — ?machine= when it does, else the assistant's, the hub's own, then any online one — and the
// answer says which. Other providers have no list to ask for: a plugin's comes with it, an API connection has none.
func (h *Hub) providerModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	name, ok := strings.CutPrefix(r.PathValue("id"), "launcher.")
	if !ok || name == "" {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-launchers.err_no_models"))
		return
	}
	found, everywhere := name == "claude", name == "claude"
	var has []string
	for _, v := range h.launcherViews(ctx) {
		if v.Name != name {
			continue
		}
		if v.Kind == "api" {
			writeErr(w, http.StatusNotFound, i18n.Err("hub-launchers.err_api_no_models"))
			return
		}
		found = true
		everywhere = everywhere || v.Source == "jarvis" || len(v.Machines) == 0 // jarvisd launch runs on every machine
		has = append(has, v.Machines...)
	}
	if !found {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-launchers.err_no_provider"))
		return
	}
	if everywhere {
		has = has[:0]
		for _, m := range h.machinesNow() {
			has = append(has, m.cfg.ID)
		}
	}
	m := h.quotaMachine(ctx, has, r.URL.Query().Get("machine"))
	if m == nil {
		writeErr(w, http.StatusServiceUnavailable, i18n.Err("hub-launchers.err_all_offline"))
		return
	}
	res, err := h.askLauncherModels(ctx, m, name, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeLauncherModelsErr(w, err)
		return
	}
	writeJSON(w, struct {
		proto.LauncherModels
		Machine string `json:"machine"`
	}{res, m.cfg.ID})
}

func (h *Hub) listLaunchers(w http.ResponseWriter, r *http.Request, m *Machine) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ls, err := h.machineLaunchers(ctx, m)
	if err != nil {
		callErr(w, err)
		return
	}
	discovered := make([]sessionLauncher, 0, len(ls))
	for _, l := range ls {
		discovered = append(discovered, sessionLauncher{Launcher: l})
	}
	writeJSON(w, map[string]any{"launchers": sessionLaunchers(discovered, m.cfg.Launchers), "default": h.pickerDefault(ctx, r)})
}
