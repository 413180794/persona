// Package plugin is the plugin base (docs/plugins.md, docs/plan-plugins.md): the manifest (persona-plugin.json) and
// its rules, the configuration form's JSON Schema subset, permissions, the registry of installed plugins, and the
// wire types of the process plugin protocol. Running plugins is plugin/host's; a program's side is plugin/serve.
package plugin

import (
	"encoding/json"
	"fmt"
)

// Protocol is the version of the process plugin protocol: the hub sends it with initialize, the program answers with
// its own, and the hub refuses a program of another version.
const Protocol = 1

// ManifestVersion is the version of persona-plugin.json this hub reads.
const ManifestVersion = 1

// ManifestFile is the manifest's name at the root of a plugin package.
const ManifestFile = "persona-plugin.json"

// Methods of the base (every other method is <namespace>.<method>, the namespace being an extension point's).
const (
	MInitialize = "initialize"
	MPing       = "ping"
	// MManifest answers persona-plugin.json, before initialize too: how the hub learns what a plugin elsewhere is
	// (remote: installs). A program the hub starts from its package need not answer it.
	MManifest = "manifest"
)

// Error codes (rpc.Error.Code), for callers that act on the kind of failure.
const (
	CodeNotInitialized   = "not_initialized"
	CodeUnknownMethod    = "unknown_method"
	CodeWrongProtocol    = "wrong_protocol"
	CodeNoCapability     = "no_capability"
	CodeNoContribution   = "no_contribution"
	CodePermissionDenied = "permission_denied"
	CodeTimeout          = "timeout"
	// CodeUnsupported: not this contribution's (a quota plugin asked about a gateway it does not read).
	CodeUnsupported = "unsupported"
)

// InitializeParams is what the hub sends first.
type InitializeParams struct {
	Protocol int        `json:"protocol"`
	Hub      HubInfo    `json:"hub"`
	Plugin   PluginInfo `json:"plugin"`
	// Config is the plugin's configuration with the schema's defaults filled in.
	Config json.RawMessage `json:"config,omitempty"`
	// Secrets are the plugin's own (declared in its manifest, filled in by the user), by name.
	Secrets map[string]string `json:"secrets,omitempty"`
	// Granted are the permissions the user accepted.
	Granted Permissions `json:"granted"`
	// Active are the contributions this run is for, by extension point (a switched-off connector is not in it).
	Active map[string][]string `json:"active,omitempty"`
}

// HubInfo is the hub a plugin runs under.
type HubInfo struct {
	Build int64 `json:"build"`
	// ExtensionPoints are the extension points the hub has and the version of each (connectors: the connector
	// protocol's).
	ExtensionPoints map[string]int `json:"extension_points"`
	// Role is the hub's place in a group of hubs (docs/mesh.md): "center" (alone, or the group's center: it serves
	// the apps and drives the computers) or "member" (another hub is the center; work the center does for the group,
	// like answering a chat service's realtime events, is not this one's). A change restarts the plugin.
	Role string `json:"role,omitempty"`
	// Timezone is the user's time zone by name (IANA, as their devices say it): for the times a plugin shows people
	// ("today", a message's time). Empty while only the hub's own clock is known. Read at each start.
	Timezone string `json:"timezone,omitempty"`
	// Capabilities are what this hub does that a plugin may go by ("inbox_group": it relates a connector's batches of
	// new messages to inbox events, docs/inbox.md「事件合成」). Absent or without one: the hub does not do it.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Hub capabilities (HubInfo.Capabilities).
const (
	HubInboxGroup = "inbox_group"
)

// HubCapabilities are this hub's.
func HubCapabilities() []string { return []string{HubInboxGroup} }

// PluginInfo is the plugin as the hub installed it.
type PluginInfo struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Dir     string `json:"dir,omitempty"`
	DataDir string `json:"data_dir,omitempty"`
}

// InitializeResult is the program's answer: who it is and what it contributes (a subset of its manifest's).
type InitializeResult struct {
	Protocol    int                        `json:"protocol"`
	ID          string                     `json:"id"`
	Version     string                     `json:"version"`
	Contributes map[string]json.RawMessage `json:"contributes,omitempty"`
}

// Target is the field every namespaced call carries: which of the plugin's contributions to that extension point it
// is for ("" when the plugin has only one).
type Target struct {
	Contribution string `json:"contribution,omitempty"`
}

// Targeted is params (an object) with the contribution a namespaced call is for added; params as they are when
// contribution is "".
func Targeted(params any, contribution string) (json.RawMessage, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	if contribution == "" {
		return b, nil
	}
	m := map[string]json.RawMessage{}
	if string(b) != "null" {
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("params of a namespaced call must be an object: %w", err)
		}
	}
	m["contribution"], _ = json.Marshal(contribution)
	return json.Marshal(m)
}
