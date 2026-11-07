package hub

import "time"

// MeshConfig tunes the hub mesh (docs/server/mesh.md). The peers themselves and the group's center are in Postgres (paired in
// the admin page or with `personad hub peer add`), not here.
type MeshConfig struct {
	// Discovery: advertise this hub on the LAN over mDNS and list the hubs found there (default on). Docker's bridge
	// network does not pass multicast: there it finds nothing (network_mode: host does).
	Discovery *bool `json:"discovery,omitempty"`
	// PathSwitchSeconds: how long a better path to a computer must stay better before the center moves to it
	// (default 60; a broken path is left at once). Keeps a computer from flapping between two paths.
	PathSwitchSeconds int `json:"path_switch_seconds,omitempty"`
	// Plaintext: links to other hubs without TLS (ws://), only with the addresses listed (peer_plain.go,
	// docs/server/mesh.md "Plaintext links"). Off while Allow is empty.
	Plaintext MeshPlaintextConfig `json:"plaintext,omitzero"`
}

// MeshPlaintextConfig lets hubs pair and link over plain HTTP. Nothing on such a link is encrypted or pinned: only
// for networks where whoever can see the traffic is trusted.
type MeshPlaintextConfig struct {
	// Allow: what plaintext pairing and links may come from and go to (plainRules, peer_plain.go): "*", networks
	// ("192.0.2.0/24"), addresses ("192.0.2.7"), wildcard addresses ("192.0.2.*"), and for dialing only host
	// names ("hub.example.com", "*.example.com"). Everything else still needs TLS.
	Allow []string `json:"allow,omitempty"`
	// Advertise: http:// addresses the other hubs use to reach this one without TLS (into the pairing code and the
	// group's records, never to apps). A hub without a TLS listener also hands out its own plaintext addresses.
	Advertise []string `json:"advertise,omitempty"`
}

func (c MeshConfig) discovery() bool { return c.Discovery == nil || *c.Discovery }

func (c MeshConfig) pathSwitch() time.Duration { return secondsOr(c.PathSwitchSeconds, 60) }
