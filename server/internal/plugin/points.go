package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
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
}

// PointVersions are Points' versions, as initialize tells a program.
func PointVersions() map[string]int {
	out := map[string]int{}
	for name, p := range Points {
		out[name] = p.Version
	}
	return out
}
