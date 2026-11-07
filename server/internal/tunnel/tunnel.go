// Package tunnel is the hub↔node link over tailcat (github.com/tailscale/tailcat): WireGuard through Tailscale's DERP
// relays, upgraded to a direct path when NAT traversal works, with no public IP, open port or ssh needed on either side.
// It is the fallback when the hub cannot ssh to a machine (docs/server/server.md "tailcat hole punching").
//
// A node keeps a fixed identity in ~/.persona/tailcat.json and serves only the hub keys listed there; the manager that
// dials it keeps its own (the built-in manager's in ~/.persona/managers/assistant/tailcat-hub.json, docs/assistant/managers.md "The
// built-in manager's own files") and dials a node's address (hub.json machines[].tailcat_addr). The address holds
// the node's public keys, the WireGuard pre-shared key and the DERP region: it is a secret, like the hub token.
package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"os"
	"path/filepath"
	"slices"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/logger"
	"tailscale.com/wgengine/filter"
)

// Ports on a node's tailcat address (its own netstack: nothing listens on the machine's network).
const (
	NodePort   uint16 = 18662 // the node protocol, as on `personad node --stdio`
	UploadPort uint16 = 18663 // a new personad binary (upload.go)
)

func dataDir() string {
	home, _ := os.UserHomeDir()
	return identity.DataDir(home)
}

// NodeKeyPath is where a node keeps its tailcat identity.
func NodeKeyPath() string { return filepath.Join(dataDir(), "tailcat.json") }

// HubKeyPath is the data directory's tailcat identity of the side that dials the nodes: a manager made in the app (its
// data directory is its own); the built-in manager's own copy is in its directory (managerconf.OwnFile).
func HubKeyPath() string { return filepath.Join(dataDir(), "tailcat-hub.json") }

// NodeKey is a node's tailcat identity: fixed, so its address stays the same across restarts.
type NodeKey struct {
	Key          key.NodePrivate      `json:"key"`
	PresharedKey tailcat.PresharedKey `json:"psk"`
	// RegionID is the DERP region the address names, picked once (the lowest latency at `init`): a node choosing
	// again at every start could move to another region and no longer match the address the hub has.
	RegionID int `json:"region_id"`
	// DERPMapURL: a DERP map of your own relays (a derper) instead of Tailscale's public ones; the hub needs the same.
	DERPMapURL string `json:"derp_map_url,omitempty"`
	// Hubs are the hub keys allowed in. Never empty: tailcat lets everyone in when no key is listed.
	Hubs []key.NodePublic `json:"hubs"`
}

// HubKey is the hub's tailcat identity, the key nodes allow in.
type HubKey struct {
	Key key.NodePrivate `json:"key"`
}

// LoadNodeKey reads a node's identity; os.ErrNotExist when this machine was never set up for tailcat.
func LoadNodeKey(path string) (*NodeKey, error) {
	var k NodeKey
	if err := readJSON(path, &k); err != nil {
		return nil, err
	}
	if k.Key.IsZero() || k.PresharedKey.IsZero() || k.RegionID == 0 {
		return nil, fmt.Errorf("%s: incomplete (run `personad node tailcat init`)", path)
	}
	if len(k.Hubs) == 0 {
		return nil, fmt.Errorf("%s: no hub key allowed (run `personad node tailcat init --hub <key>`)", path)
	}
	return &k, nil
}

// InitNodeKey creates a node's identity, or adds hub to the existing one (the address does not change). With no
// region yet it picks the closest one: that needs the network.
func InitNodeKey(ctx context.Context, path, hub, derpMapURL string) (*NodeKey, error) {
	var hk key.NodePublic
	if err := hk.UnmarshalText([]byte(hub)); err != nil {
		return nil, fmt.Errorf("hub key %q: %w (it is what `personad hub tailcat-key` prints on the hub's machine)", hub, err)
	}
	var k NodeKey
	switch err := readJSON(path, &k); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	}
	if k.Key.IsZero() {
		k.Key = key.NewNode()
	}
	if k.PresharedKey.IsZero() {
		k.PresharedKey = tailcat.NewPresharedKey()
	}
	if derpMapURL != "" && derpMapURL != k.DERPMapURL {
		k.DERPMapURL, k.RegionID = derpMapURL, 0 // another relay map: its regions are not the old ones
	}
	if k.RegionID == 0 {
		var opts []any
		if k.DERPMapURL != "" {
			opts = append(opts, tailcat.DERPMapURL(k.DERPMapURL))
		}
		dm, err := tailcat.FetchDERPMap(ctx, append(opts, tailcat.ExpandForServer)...)
		if err != nil {
			return nil, fmt.Errorf("fetch the DERP map: %w", err)
		}
		region, err := tailcat.PickBestRegion(ctx, dm)
		if err != nil {
			return nil, fmt.Errorf("pick a DERP region: %w", err)
		}
		if region == 0 {
			return nil, errors.New("no DERP region answered: is this machine online?")
		}
		k.RegionID = int(region)
	}
	if !slices.Contains(k.Hubs, hk) {
		k.Hubs = append(k.Hubs, hk)
	}
	if err := writeJSON(path, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// Addr is the node's tailcat address. It names the region by id rather than embedding the region's relays, so it
// stays valid when Tailscale changes the servers in a region.
func (k *NodeKey) Addr() tailcat.Addr {
	ci := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: k.Key.Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(k.Key),
		PresharedKey:      k.PresharedKey,
		RegionID:          tailcfg.DERPRegionID(k.RegionID),
	}
	return ci.Addr()
}

// Server is the node's tailcat endpoint: only the listed hubs get a reply, and only the node's two ports are served.
func (k *NodeKey) Server(logf logger.Logf) *tailcat.Server {
	return &tailcat.Server{
		Key:            k.Key,
		PresharedKey:   k.PresharedKey,
		RegionID:       tailcfg.DERPRegionID(k.RegionID),
		DERPMapURL:     k.DERPMapURL,
		AllowedClients: slices.Clone(k.Hubs),
		ServedTCPPorts: []filter.PortRange{{First: NodePort, Last: UploadPort}},
		Logf:           logf,
	}
}

// LoadOrCreateHubKey reads the hub's identity, creating it the first time. The hub and `personad hub tailcat-key`
// running at once must end up with the same key, not each with its own.
func LoadOrCreateHubKey(path string) (*HubKey, error) {
	var k HubKey
	err := readJSON(path, &k)
	switch {
	case err == nil && !k.Key.IsZero():
		return &k, nil
	case err == nil:
		return nil, fmt.Errorf("%s: no key in it", path)
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	k.Key = key.NewNode()
	b, err := json.MarshalIndent(&k, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Written whole to a file of its own, then linked into place: the link fails if another process got there
	// first, and a reader never sees a half-written file.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*") // named after it: the node's path guard refuses it too
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Link(tmp, path); errors.Is(err, os.ErrExist) { // made by the other one meanwhile: use that
		var theirs HubKey
		if err := readJSON(path, &theirs); err != nil {
			return nil, err
		}
		if theirs.Key.IsZero() {
			return nil, fmt.Errorf("%s: no key in it", path)
		}
		return &theirs, nil
	} else if err != nil {
		return nil, err
	}
	return &k, nil
}

// Public is what a node's `personad node tailcat init --hub` takes.
func (k *HubKey) Public() string {
	b, _ := k.Key.Public().MarshalText()
	return string(b)
}

// Logf is tailscale's logging for this package: silent unless PERSONA_TAILCAT_DEBUG=1, magicsock and wireguard
// log a few lines a second while a path is being worked out.
func Logf(to logger.Logf) logger.Logf {
	if identity.Getenv("PERSONA_TAILCAT_DEBUG") == "1" && to != nil {
		return to
	}
	return logger.Discard
}

// readJSON reads a key file, making it the owner's only if it was not (one copied in by hand keeps cp's 0644).
func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("%s is readable by others and cannot be made private: %w", path, err)
		}
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// writeJSON writes a key file readable by its owner only, replacing the old one in one step.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp) // WriteFile keeps the mode of a file that is already there
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
