package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// Point is an extension point: a place in the hub a plugin contributes to (docs/plan-plugins.md「contributes」).
type Point struct {
	Name string // the key under contributes ("connectors")
	// Namespace is the prefix of its methods ("connector": connector.status).
	Namespace string
	Version   int
	// NeedsRPC / NeedsHTTP: a contribution here needs the plugin to speak the protocol / serve HTTP.
	NeedsRPC, NeedsHTTP bool
	// Item checks one contribution and says its id.
	Item func(raw json.RawMessage) (id string, err error)
}

var connectorID = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ConnectorItem is a contribution to connectors: the connector protocol's manifest (docs/connector-protocol.md), known
// before the program ever runs.
type ConnectorItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Hosts        []string `json:"hosts,omitempty"`
	AgentTools   []string `json:"agent_tools,omitempty"`
	Unlisted     []string `json:"unlisted,omitempty"`
	// ConfirmAlways are the write actions (patterns, path.Match: "example_send") the hub's write gate never lets a
	// connector_gate rule allow: the user confirms them on the phone whatever the rules say (a deny rule still denies).
	// The user's own sends in the app are not gated, so not affected.
	ConfirmAlways []string `json:"confirm_always,omitempty"`
	PollSeconds   int      `json:"poll_seconds,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	// KindDescriptions tell the models what each kind of its inbox things is ("类型：…" in the inbox's prompts), by
	// kind: a plugin's connector says it itself (a built-in one has prompts/inbox/kinds/<id>.md).
	KindDescriptions map[string]string `json:"kind_descriptions,omitempty"`
	// ArchiveSources are other names its archive files conversations under (connector.Manifest.ArchiveSources).
	ArchiveSources []string `json:"archive_sources,omitempty"`
}

// VoiceItem is a contribution to voice: the voice conversation's contract (docs/plugins.md「语音」).
type VoiceItem struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ComputerItem is a contribution to computer: operating the user's computers for the assistant (the built-in plugin
// cua, docs/computer-use.md).
type ComputerItem struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// QuotaItem is a contribution to quota: a gateway whose launchers' quota the plugin reads (docs/plugins.md「额度」).
type QuotaItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Hosts are the gateways' host names it reads ("*.example.com" allowed); empty: it is asked about any gateway and
	// answers unsupported for one that is not its.
	Hosts []string `json:"hosts,omitempty"`
}

// AgentSourceItem is a contribution to agent_sources: a source of personal agents' sessions in the Agents tab, next
// to the hub's own (docs/plugins.md「个人 Agent 来源」). Its id is the source part of the sessions' ids
// (<id>:<sessionKey>).
type AgentSourceItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Providers are the agents it has ("grok_bot"): the apps group and label sessions by provider.
	Providers []string `json:"providers,omitempty"`
}

var providerPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// ProviderItem is a contribution to providers: a model provider the user can add a launcher from (设置 › 模型供应商),
// its address and models filled in (docs/plugins.md「模型供应商」).
type ProviderItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Kind: launcher (Claude Code through it, the default) or api (the hub calls it directly).
	Kind string `json:"kind,omitempty"`
	// API: messages (Anthropic's, the default) or responses (OpenAI's), for an api provider.
	API        string          `json:"api,omitempty"`
	BaseURL    string          `json:"base_url"`
	Model      string          `json:"model,omitempty"`
	SmallModel string          `json:"small_model,omitempty"`
	Models     []ProviderModel `json:"models,omitempty"`
	// DynamicModels: the plugin lists the models itself (provider.models), on top of Models.
	DynamicModels bool `json:"dynamic_models,omitempty"`
	// KeyHint says where to get a key ("在控制台 › API Keys 里创建").
	KeyHint string `json:"key_hint,omitempty"`
}

// ProviderModel is one model of a provider.
type ProviderModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// APIItem is a contribution to api: a part of the app's API the plugin serves on its HTTP side, under /api/v1/<prefix>/
// (docs/plugins.md「App 接口」).
type APIItem struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	// Features are added to the hub's (hello, /health): the apps show what needs them.
	Features []string `json:"features,omitempty"`
	// Events are the types of the events it pushes to the apps (api.broadcast).
	Events []string `json:"events,omitempty"`
	// Receipts are its sends the hub keeps a receipt of (GET /api/v1/ops/{id}): a retry reaches the plugin once.
	Receipts []APIReceipt `json:"receipts,omitempty"`
}

// APIReceipt is one route of a plugin's API with a send receipt: Method and Path (under the prefix, "*" one segment)
// and the receipt's Kind. The receipt's id comes from the body's client_id, or the caller's X-Jarvis-Op-Id.
type APIReceipt struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
}

// Matches says whether a request (its path under the prefix) is the receipt's route.
func (r APIReceipt) Matches(method, path string) bool {
	if !strings.EqualFold(r.Method, method) {
		return false
	}
	want := strings.Split(strings.Trim(r.Path, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != "*" && want[i] != got[i] {
			return false
		}
	}
	return true
}

// AssistantItem is a contribution to assistant: skills and a part of the assistant's instructions, in the package
// (docs/plugins.md「助手」).
type AssistantItem struct {
	ID string `json:"id"`
	// Skills are directories in the package, each a skill (SKILL.md first), put in with the assistant's.
	Skills []string `json:"skills,omitempty"`
	// Instructions is a markdown file in the package, added to the assistant's CLAUDE.md.
	Instructions string `json:"instructions,omitempty"`
	// Gate is a JSON file in the package (GateFile): a command of its skills that calls an HTTP API as the user, the
	// rules its calls go by and the credentials it needs (the plugin's assistant.credentials).
	Gate string `json:"gate,omitempty"`
}

// GateFile is an assistant contribution's gate file: a command gate (proto.CommandGate) without the credentials'
// values. Env are the names assistant.credentials may fill; Connector is the connector whose connector_gate rules go
// before Rules (the user's say first).
type GateFile struct {
	Command    string             `json:"command"`
	Script     string             `json:"script"`
	Title      string             `json:"title"`
	Connector  string             `json:"connector,omitempty"`
	Env        []string           `json:"env,omitempty"`
	BaseEnv    string             `json:"base_env,omitempty"`
	Secret     []string           `json:"secret,omitempty"`
	AllowedEnv []string           `json:"allowed_env,omitempty"`
	Headers    []string           `json:"headers,omitempty"`
	PathPrefix string             `json:"path_prefix,omitempty"`
	Rules      []proto.LarkRule   `json:"rules,omitempty"`
	Actions    []proto.GateAction `json:"actions,omitempty"`
	Tool       *proto.GateTool    `json:"tool,omitempty"`
}

var (
	gateCommand = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	gateEnv     = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// ParseGate reads a gate file strictly and checks it.
func ParseGate(raw []byte) (*GateFile, error) {
	var g GateFile
	if err := strict(raw, &g); err != nil {
		return nil, err
	}
	if !gateCommand.MatchString(g.Command) || g.Command == "lark-cli" {
		return nil, fmt.Errorf("gate command %q: a plain lowercase name", g.Command)
	}
	if err := checkPackagePath(g.Script); err != nil {
		return nil, fmt.Errorf("gate script: %v", err)
	}
	if g.Title == "" {
		return nil, errors.New("gate: no title")
	}
	for _, e := range append(append(append([]string(nil), g.Env...), g.Secret...), g.AllowedEnv...) {
		if !gateEnv.MatchString(e) || strings.HasPrefix(e, "PERSONA_") || strings.HasPrefix(e, "JARVIS_") || e == "PATH" {
			return nil, fmt.Errorf("gate env %q", e)
		}
	}
	if g.BaseEnv != "" && !slices.Contains(g.Env, g.BaseEnv) {
		return nil, errors.New("gate base_env is not one of env")
	}
	for _, r := range g.Rules {
		if r.Action != "allow" && r.Action != "confirm" && r.Action != "deny" || r.Path == "" || r.Method == "" {
			return nil, fmt.Errorf("gate rule %+v", r)
		}
	}
	for _, a := range g.Actions {
		if _, err := regexp.Compile(a.Pattern); err != nil || a.What == "" {
			return nil, fmt.Errorf("gate action %q", a.Pattern)
		}
	}
	if g.Tool != nil && (!regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`).MatchString(g.Tool.Name) || g.Tool.Description == "") {
		return nil, errors.New("gate tool: a name (a-z0-9_) and a description")
	}
	return &g, nil
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// checkPackagePath is a path inside a package: relative, clean, not out of it.
func checkPackagePath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%q is not a path inside the package", p)
	}
	return nil
}

// Points are the extension points this hub has, by name.
var Points = map[string]Point{
	"connectors": {Name: "connectors", Namespace: "connector", Version: 2, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var c ConnectorItem
		if err := strict(raw, &c); err != nil {
			return "", err
		}
		if !connectorID.MatchString(c.ID) {
			return c.ID, fmt.Errorf("connector id %q: lowercase letters and digits, a letter first", c.ID)
		}
		if c.Name == "" {
			return c.ID, errors.New("connector " + c.ID + ": no name")
		}
		if len(c.Capabilities) == 0 {
			return c.ID, errors.New("connector " + c.ID + ": no capabilities")
		}
		if c.PollSeconds < 0 {
			return c.ID, errors.New("connector " + c.ID + ": poll_seconds below 0")
		}
		for _, p := range c.ConfirmAlways {
			if _, err := path.Match(p, ""); err != nil || p == "" {
				return c.ID, fmt.Errorf("connector %s: confirm_always pattern %q is not a valid action pattern (path.Match)", c.ID, p)
			}
		}
		if len(c.ArchiveSources) > 4 {
			return c.ID, errors.New("connector " + c.ID + ": more than 4 archive_sources")
		}
		for _, s := range c.ArchiveSources {
			if !connectorID.MatchString(s) || s == c.ID {
				return c.ID, fmt.Errorf("connector %s: archive source %q: lowercase letters and digits, a letter first, not its own id", c.ID, s)
			}
		}
		return c.ID, nil
	}},
	"quota": {Name: "quota", Namespace: "quota", Version: 1, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var q QuotaItem
		if err := strict(raw, &q); err != nil {
			return "", err
		}
		if !idPattern.MatchString(q.ID) {
			return q.ID, fmt.Errorf("quota id %q: a-z0-9_-", q.ID)
		}
		if q.Name == "" {
			return q.ID, errors.New("quota " + q.ID + ": no name")
		}
		for _, h := range q.Hosts {
			if !hostPattern.MatchString(h) {
				return q.ID, fmt.Errorf("quota %s: %q is not a host name", q.ID, h)
			}
		}
		return q.ID, nil
	}},
	"agent_sources": {Name: "agent_sources", Namespace: "agentsrc", Version: 1, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var a AgentSourceItem
		if err := strict(raw, &a); err != nil {
			return "", err
		}
		if !idPattern.MatchString(a.ID) {
			return a.ID, fmt.Errorf("agent_sources id %q: a-z0-9_-", a.ID)
		}
		if a.Name == "" {
			return a.ID, errors.New("agent_sources " + a.ID + ": no name")
		}
		for _, pr := range a.Providers {
			if !providerPattern.MatchString(pr) {
				return a.ID, fmt.Errorf("agent_sources %s: provider %q: a-z0-9_", a.ID, pr)
			}
		}
		return a.ID, nil
	}},
	"providers": {Name: "providers", Namespace: "provider", Version: 1, Item: func(raw json.RawMessage) (string, error) {
		var p ProviderItem
		if err := strict(raw, &p); err != nil {
			return "", err
		}
		if !idPattern.MatchString(p.ID) {
			return p.ID, fmt.Errorf("provider id %q: a-z0-9_-", p.ID)
		}
		if p.Name == "" {
			return p.ID, errors.New("provider " + p.ID + ": no name")
		}
		if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return p.ID, fmt.Errorf("provider %s: base_url is an http(s) URL", p.ID)
		}
		if p.Kind != "" && p.Kind != "launcher" && p.Kind != "api" {
			return p.ID, fmt.Errorf("provider %s: kind is launcher or api", p.ID)
		}
		if p.API != "" && p.API != "messages" && p.API != "responses" {
			return p.ID, fmt.Errorf("provider %s: api is messages or responses", p.ID)
		}
		seen := map[string]bool{}
		for _, m := range p.Models {
			if m.ID == "" || seen[m.ID] {
				return p.ID, fmt.Errorf("provider %s: a model without an id, or twice", p.ID)
			}
			seen[m.ID] = true
		}
		return p.ID, nil
	}},
	"api": {Name: "api", Namespace: "api", Version: 1, NeedsHTTP: true, Item: func(raw json.RawMessage) (string, error) {
		var a APIItem
		if err := strict(raw, &a); err != nil {
			return "", err
		}
		if !idPattern.MatchString(a.ID) {
			return a.ID, fmt.Errorf("api id %q: a-z0-9_-", a.ID)
		}
		if !idPattern.MatchString(a.Prefix) {
			return a.ID, fmt.Errorf("api %s: prefix %q: a-z0-9_-, one path segment", a.ID, a.Prefix)
		}
		for _, f := range append(slices.Clone(a.Features), a.Events...) {
			if !namePattern.MatchString(f) {
				return a.ID, fmt.Errorf("api %s: %q is not a feature or event name (a-z0-9_)", a.ID, f)
			}
		}
		for _, rc := range a.Receipts {
			if rc.Method != "POST" && rc.Method != "PUT" && rc.Method != "PATCH" && rc.Method != "DELETE" || !strings.HasPrefix(rc.Path, "/") ||
				!regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`).MatchString(rc.Kind) {
				return a.ID, fmt.Errorf("api %s: receipt %+v: a write method, a path from /, a kind (a-z0-9_)", a.ID, rc)
			}
		}
		return a.ID, nil
	}},
	"assistant": {Name: "assistant", Namespace: "assistant", Version: 1, Item: func(raw json.RawMessage) (string, error) {
		var a AssistantItem
		if err := strict(raw, &a); err != nil {
			return "", err
		}
		if !idPattern.MatchString(a.ID) {
			return a.ID, fmt.Errorf("assistant id %q: a-z0-9_-", a.ID)
		}
		if a.Gate != "" {
			if err := checkPackagePath(a.Gate); err != nil || !strings.HasSuffix(a.Gate, ".json") {
				return a.ID, fmt.Errorf("assistant %s: gate is a .json file inside the package", a.ID)
			}
			if len(a.Skills) == 0 {
				return a.ID, fmt.Errorf("assistant %s: a gate without the skills its command is in", a.ID)
			}
		}
		if len(a.Skills) == 0 && a.Instructions == "" {
			return a.ID, fmt.Errorf("assistant %s: no skills and no instructions", a.ID)
		}
		for _, sk := range a.Skills {
			if err := checkPackagePath(sk); err != nil {
				return a.ID, fmt.Errorf("assistant %s: skills: %v", a.ID, err)
			}
		}
		if a.Instructions != "" {
			if err := checkPackagePath(a.Instructions); err != nil || !strings.HasSuffix(a.Instructions, ".md") {
				return a.ID, fmt.Errorf("assistant %s: instructions is a .md file inside the package", a.ID)
			}
		}
		return a.ID, nil
	}},
	"ui": {Name: "ui", Namespace: "ui", Version: UIVersion, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var u UIItem
		if err := strict(raw, &u); err != nil {
			return "", err
		}
		if !idPattern.MatchString(u.ID) {
			return u.ID, fmt.Errorf("ui id %q: a-z0-9_-", u.ID)
		}
		if u.Title == "" || utf8.RuneCountInString(u.Title) > 40 {
			return u.ID, fmt.Errorf("ui %s: title, 1 to 40 characters", u.ID)
		}
		if _, ok := Slot(u.Placement); !ok {
			var names []string
			for _, sl := range UISlots {
				names = append(names, sl.Name)
			}
			return u.ID, fmt.Errorf("ui %s: placement is one of %s", u.ID, strings.Join(names, ", "))
		}
		if u.Placement != "launcher_section" && len(u.Hosts) > 0 {
			return u.ID, fmt.Errorf("ui %s: only a launcher_section has hosts", u.ID)
		}
		if u.Icon != "" && (u.Placement != "nav_page" || !slices.Contains(UIIcons, u.Icon)) {
			return u.ID, fmt.Errorf("ui %s: only a nav_page has an icon, one of %s", u.ID, strings.Join(UIIcons, ", "))
		}
		if u.Opens != "" && u.Placement != "overview_card" {
			return u.ID, fmt.Errorf("ui %s: only an overview_card opens a page", u.ID)
		}
		for _, h := range u.Hosts {
			if !hostPattern.MatchString(h) {
				return u.ID, fmt.Errorf("ui %s: %q is not a host name", u.ID, h)
			}
		}
		return u.ID, nil
	}},
	"events": {Name: "events", Namespace: "event", Version: 1, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var e EventsItem
		if err := strict(raw, &e); err != nil {
			return "", err
		}
		if !idPattern.MatchString(e.ID) {
			return e.ID, fmt.Errorf("events id %q: a-z0-9_-", e.ID)
		}
		if len(e.Subscribe) == 0 {
			return e.ID, fmt.Errorf("events %s: subscribes to nothing", e.ID)
		}
		for _, t := range e.Subscribe {
			if _, ok := EventTopics[t]; !ok {
				return e.ID, fmt.Errorf("events %s: no event %q", e.ID, t)
			}
		}
		return e.ID, nil
	}},
	"voice": {Name: "voice", Namespace: "voice", Version: 1, NeedsHTTP: true, Item: func(raw json.RawMessage) (string, error) {
		var v VoiceItem
		if err := strict(raw, &v); err != nil {
			return "", err
		}
		if !idPattern.MatchString(v.ID) {
			return v.ID, fmt.Errorf("voice id %q: a-z0-9_-", v.ID)
		}
		return v.ID, nil
	}},
	"browser": {Name: "browser", Namespace: "browser", Version: 1, Item: func(raw json.RawMessage) (string, error) {
		var c ComputerItem // the same contribution: an id and a name
		if err := strict(raw, &c); err != nil {
			return "", err
		}
		if !idPattern.MatchString(c.ID) {
			return c.ID, fmt.Errorf("browser id %q: a-z0-9_-", c.ID)
		}
		return c.ID, nil
	}},
	// channel: a way to talk to Persona, a chat app's bot (docs/channels.md「插件怎么贡献渠道」). The hub follows the
	// turns; the program only talks to the chat app.
	"channel": {Name: "channel", Namespace: "channel", Version: 1, NeedsRPC: true, Item: func(raw json.RawMessage) (string, error) {
		var c channel.Item
		if err := strict(raw, &c); err != nil {
			return "", err
		}
		return c.ID, c.Validate()
	}},
	"computer": {Name: "computer", Namespace: "computer", Version: 1, Item: func(raw json.RawMessage) (string, error) {
		var c ComputerItem
		if err := strict(raw, &c); err != nil {
			return "", err
		}
		if !idPattern.MatchString(c.ID) {
			return c.ID, fmt.Errorf("computer id %q: a-z0-9_-", c.ID)
		}
		return c.ID, nil
	}},
}

// PointVersions are Points' versions, as initialize tells a program.
func PointVersions() map[string]int {
	out := map[string]int{}
	for name, p := range Points {
		out[name] = p.Version
	}
	return out
}
