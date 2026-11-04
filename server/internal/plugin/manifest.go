package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// ManifestMost is the largest persona-plugin.json read.
const ManifestMost = 256 << 10

// SecretsMost is how many secrets a plugin may declare.
const SecretsMost = 32

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	secretPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	envPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	platformKey   = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
)

// ValidID says whether id is a plugin id: a-z0-9_-, at most 32, not starting with - or _ (those are the hub's).
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Manifest is persona-plugin.json (docs/plan-plugins.md「三」).
type Manifest struct {
	ManifestVersion  int                        `json:"manifest_version"`
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Version          string                     `json:"version"`
	Description      string                     `json:"description,omitempty"`
	Publisher        string                     `json:"publisher,omitempty"`
	Homepage         string                     `json:"homepage,omitempty"`
	Engines          Engines                    `json:"engines,omitzero"`
	Runtime          *Runtime                   `json:"runtime,omitempty"`
	ActivationEvents []string                   `json:"activation_events,omitempty"`
	Contributes      map[string]json.RawMessage `json:"contributes"`
	Configuration    json.RawMessage            `json:"configuration,omitempty"`
	Secrets          []SecretSpec               `json:"secrets,omitempty"`
	Permissions      Permissions                `json:"permissions,omitzero"`
}

// Engines are what the plugin runs on.
type Engines struct {
	// Persona is a range of hub build numbers (">=202611050000 <202701010000").
	Persona        string `json:"persona,omitempty"`
	PluginProtocol int    `json:"plugin_protocol,omitempty"`
}

// Runtime is how the plugin's program runs; a plugin without one runs no code.
type Runtime struct {
	Command Command           `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// RPC: "stdio" (the default) speaks the plugin protocol on stdin / stdout; "none" does not.
	RPC string `json:"rpc,omitempty"`
	// HTTP: the program serves HTTP on PERSONA_PLUGIN_SOCKET, and the hub relays /api/v1/plugins/<id>/… to it.
	HTTP bool `json:"http,omitempty"`
}

// SpeaksRPC says whether the program speaks the plugin protocol.
func (r *Runtime) SpeaksRPC() bool { return r != nil && r.RPC != "none" }

// Command is one path, or one per platform ("linux-amd64": "bin/x").
type Command struct {
	Path       string
	ByPlatform map[string]string
}

func (c *Command) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &c.Path)
	}
	return json.Unmarshal(b, &c.ByPlatform)
}

func (c Command) MarshalJSON() ([]byte, error) {
	if c.ByPlatform != nil {
		return json.Marshal(c.ByPlatform)
	}
	return json.Marshal(c.Path)
}

// For is the command on platform ("linux-amd64"); false when there is none for it.
func (c Command) For(platform string) (string, bool) {
	if c.ByPlatform == nil {
		return c.Path, c.Path != ""
	}
	p, ok := c.ByPlatform[platform]
	return p, ok && p != ""
}

// SecretSpec is a secret the plugin needs.
type SecretSpec struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Activation events.
const (
	OnStartup = "on_startup"
	OnCall    = "on_call"
	OnHTTP    = "on_http"
)

// Builtin commands: the hub's own binary (a built-in plugin's).
var builtinCommands = []string{"personad", "jarvisd"}

// IsBuiltinCommand says whether a runtime command names the hub's own binary.
func IsBuiltinCommand(c string) bool { return slices.Contains(builtinCommands, c) }

// Parse reads a manifest strictly (no unknown fields anywhere) and checks it (Validate).
func Parse(data []byte) (*Manifest, error) {
	if len(data) > ManifestMost {
		return nil, fmt.Errorf("%s: %d bytes, more than %d", ManifestFile, len(data), ManifestMost)
	}
	var m Manifest
	if err := strict(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// strict decodes one JSON value into v, refusing unknown fields and anything after it.
func strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("something after the JSON value")
	}
	return nil
}

// Validate checks everything a manifest can be checked for on its own (docs/plan-plugins.md「字段」). What depends on
// the hub (the platform's command, engines, ids other plugins use) is the registry's.
func (m *Manifest) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if m.ManifestVersion != ManifestVersion {
		bad("manifest_version %d: this hub reads %d", m.ManifestVersion, ManifestVersion)
	}
	if !ValidID(m.ID) {
		bad("id %q: a-z0-9_-, at most 32, a letter or digit first", m.ID)
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(m.Name)); n == 0 || n > 40 {
		bad("name: 1 to 40 characters")
	}
	if _, err := ParseVersion(m.Version); err != nil {
		bad("version %q: %v", m.Version, err)
	}
	if m.Homepage != "" {
		if u, err := url.Parse(m.Homepage); err != nil || u.Scheme != "https" || u.Host == "" {
			bad("homepage: an https URL")
		}
	}
	if m.Engines.Persona != "" {
		if _, err := ParseRange(m.Engines.Persona); err != nil {
			bad("engines.persona: %v", err)
		}
	}
	if m.Runtime != nil {
		if m.Engines.PluginProtocol != Protocol {
			bad("engines.plugin_protocol %d: this hub speaks %d", m.Engines.PluginProtocol, Protocol)
		}
		errs = append(errs, m.Runtime.validate()...)
	}
	for _, e := range m.ActivationEvents {
		if !slices.Contains([]string{OnStartup, OnCall, OnHTTP}, e) {
			bad("activation_events: %q is not on_startup, on_call or on_http", e)
		}
	}
	if m.Runtime != nil && !m.Runtime.SpeaksRPC() && !slices.Contains(m.ActivationEvents, OnStartup) {
		bad("activation_events: a program that does not speak the protocol (rpc none) runs on_startup")
	}
	if m.Runtime == nil && len(m.ActivationEvents) > 0 {
		bad("activation_events without a runtime")
	}
	if slices.Contains(m.ActivationEvents, OnHTTP) && (m.Runtime == nil || !m.Runtime.HTTP) {
		bad("activation_events: on_http without runtime.http")
	}
	if len(m.Contributes) == 0 {
		bad("contributes: nothing")
	}
	for _, name := range sortedKeys(m.Contributes) {
		p, known := Points[name]
		if !known {
			continue // a newer hub's: Lint warns, it does not take effect
		}
		var items []json.RawMessage
		if err := json.Unmarshal(m.Contributes[name], &items); err != nil {
			bad("contributes.%s: a list", name)
			continue
		}
		if len(items) == 0 {
			bad("contributes.%s: empty", name)
		}
		seen := map[string]bool{}
		for _, raw := range items {
			id, err := p.Item(raw)
			if err != nil {
				bad("contributes.%s: %v", name, err)
				continue
			}
			if seen[id] {
				bad("contributes.%s: %q twice", name, id)
			}
			seen[id] = true
		}
		if p.NeedsRPC && !m.Runtime.SpeaksRPC() {
			bad("contributes.%s needs a runtime that speaks the protocol", name)
		}
		if p.NeedsHTTP && (m.Runtime == nil || !m.Runtime.HTTP) {
			bad("contributes.%s needs runtime.http", name)
		}
	}
	if len(m.Configuration) > 0 {
		if _, err := ParseSchema(m.Configuration); err != nil {
			bad("configuration: %v", err)
		}
	}
	if len(m.Secrets) > SecretsMost {
		bad("secrets: more than %d", SecretsMost)
	}
	seen := map[string]bool{}
	for _, s := range m.Secrets {
		if !secretPattern.MatchString(s.Name) {
			bad("secrets: name %q: a-z0-9_, a letter first, at most 32", s.Name)
		}
		if seen[s.Name] {
			bad("secrets: %q twice", s.Name)
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.Title) == "" {
			bad("secrets: %q has no title", s.Name)
		}
	}
	if err := m.Permissions.validate(); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("plugin %s: %w", m.ID, err)
	}
	return nil
}

func (r *Runtime) validate() []error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	paths := map[string]string{"": r.Command.Path}
	if r.Command.ByPlatform != nil {
		paths = r.Command.ByPlatform
		if len(paths) == 0 {
			bad("runtime.command: no platform")
		}
	}
	for platform, p := range paths {
		if platform != "" && !platformKey.MatchString(platform) {
			bad("runtime.command: platform %q is not <os>-<arch>", platform)
		}
		if err := checkCommandPath(p); err != nil {
			bad("runtime.command %q: %v", p, err)
		}
	}
	if r.RPC != "" && r.RPC != "stdio" && r.RPC != "none" {
		bad("runtime.rpc %q: stdio or none", r.RPC)
	}
	if !r.SpeaksRPC() && !r.HTTP {
		bad("runtime: rpc none and no http: the hub could not talk to it")
	}
	for k := range r.Env {
		if !envPattern.MatchString(k) {
			bad("runtime.env: %q is not a variable name", k)
		}
		if strings.HasPrefix(k, "PERSONA_") || strings.HasPrefix(k, "JARVIS_") {
			bad("runtime.env: %q is the hub's to set", k)
		}
	}
	return errs
}

// checkCommandPath: a path inside the package (relative, no ..), or the hub's own binary.
func checkCommandPath(p string) error {
	switch {
	case p == "":
		return errors.New("empty")
	case IsBuiltinCommand(p):
		return nil
	case strings.HasPrefix(p, "/") || strings.Contains(p, `\`):
		return errors.New("must be relative to the package")
	case path.Clean(p) != p || p == "." || strings.HasPrefix(p, "../") || p == "..":
		return errors.New("must stay inside the package")
	}
	return nil
}

// Activation is what starts the program: on_call when the manifest names nothing.
func (m *Manifest) Activation() []string {
	if len(m.ActivationEvents) == 0 && m.Runtime != nil {
		return []string{OnCall}
	}
	return m.ActivationEvents
}

// Items are the ids of the manifest's contributions to point (nil when it has none there).
func (m *Manifest) Items(point string) []string {
	var items []json.RawMessage
	if json.Unmarshal(m.Contributes[point], &items) != nil {
		return nil
	}
	p, ok := Points[point]
	if !ok {
		return nil
	}
	var out []string
	for _, raw := range items {
		if id, err := p.Item(raw); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// Connectors are the manifest's contributions to connectors.
func (m *Manifest) Connectors() []ConnectorItem {
	var out []ConnectorItem
	_ = json.Unmarshal(m.Contributes["connectors"], &out)
	return out
}

// Contributions are the ids by extension point, the points this hub has only.
func (m *Manifest) Contributions() map[string][]string {
	out := map[string][]string{}
	for name := range m.Contributes {
		if ids := m.Items(name); len(ids) > 0 {
			out[name] = ids
		}
	}
	return out
}

// Kind is the first extension point the plugin contributes to, by a fixed order: what GET /api/v1/plugins has always
// said as "kind" (voice for a voice plugin).
func (m *Manifest) Kind() string {
	for _, name := range []string{"voice", "connectors"} {
		if len(m.Items(name)) > 0 {
			return name
		}
	}
	keys := sortedKeys(m.Contributes)
	if len(keys) > 0 {
		return keys[0]
	}
	return ""
}

// Lint are warnings that do not stop an install: extension points this hub does not have, configuration properties
// that look like secrets.
func (m *Manifest) Lint() []string {
	var out []string
	for _, name := range sortedKeys(m.Contributes) {
		if _, ok := Points[name]; !ok {
			out = append(out, fmt.Sprintf("contributes.%s: this hub has no such extension point (a newer hub's?); it does not take effect", name))
		}
	}
	if s, err := ParseSchema(m.Configuration); err == nil && s != nil {
		for _, f := range s.Fields {
			if looksSecret(f.Name) {
				out = append(out, fmt.Sprintf("configuration.%s looks like a secret: it is stored and shown in plain text; declare it under secrets", f.Name))
			}
		}
	}
	return out
}

func looksSecret(name string) bool {
	n := strings.ToLower(name)
	for _, w := range []string{"password", "passwd", "token", "secret", "api_key", "apikey", "private_key"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
