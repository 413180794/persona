package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
	PollSeconds  int      `json:"poll_seconds,omitempty"`
	Kinds        []string `json:"kinds,omitempty"`
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
