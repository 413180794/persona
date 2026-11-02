package hub

import "time"

// MeshConfig tunes the hub mesh (docs/mesh.md). The peers themselves and the group's center are in Postgres (paired in
// the admin page or with `jarvisd hub peer add`), not here.
type MeshConfig struct {
	// Discovery: advertise this hub on the LAN over mDNS and list the hubs found there (default on). Docker's bridge
	// network does not pass multicast: there it finds nothing (network_mode: host does).
	Discovery *bool `json:"discovery,omitempty"`
	// PathSwitchSeconds: how long a better path to a computer must stay better before the center moves to it
	// (default 60; a broken path is left at once). Keeps a computer from flapping between two paths.
	PathSwitchSeconds int `json:"path_switch_seconds,omitempty"`
}

func (c MeshConfig) discovery() bool { return c.Discovery == nil || *c.Discovery }

func (c MeshConfig) pathSwitch() time.Duration { return secondsOr(c.PathSwitchSeconds, 60) }
