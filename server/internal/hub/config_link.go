package hub

import (
	"time"

	"github.com/nwverse/persona/server/internal/link"
)

// LinkConfig tunes the heartbeat with the computers' Persona Links (docs/operations/config.md, docs/operations/link.md "Heartbeat"): the hub's
// side of each link and its streams, the manager's side of the streams it opens to connectors (kind service), and
// Persona Link's own side, which the manager tells it. Links to other hubs (mesh) keep link.PingEvery / PingTimeout.
type LinkConfig struct {
	// PingSeconds: how often a ping goes out (default 10); PingTimeoutSeconds: how long its pong may take before the
	// link or stream is dropped (default 5). 1–600, more is taken as 600. On a slow link a ping waits behind the message
	// being written, so the timeout must outlast writing one (a 1 MiB chunk at 30 KB/s takes over 30 s).
	PingSeconds        int `json:"ping_seconds,omitempty"`
	PingTimeoutSeconds int `json:"ping_timeout_seconds,omitempty"`
}

// pingTimes is the heartbeat hub.json sets, the default for the one it leaves out; ok is false when it sets neither
// (and nothing about the heartbeat goes on the wire).
func (c LinkConfig) pingTimes() (every, timeout time.Duration, ok bool) {
	if c.PingSeconds <= 0 && c.PingTimeoutSeconds <= 0 {
		return 0, 0, false
	}
	return link.PingTimes(pingSecondsOr(c.PingSeconds, link.PingEvery),
		pingSecondsOr(c.PingTimeoutSeconds, link.PingTimeout))
}

// pingSecondsOr is n seconds, def when n is not set; past link.MaxPing it is MaxPing (and no overflow).
func pingSecondsOr(n int, def time.Duration) time.Duration {
	if n <= 0 {
		return def
	}
	return time.Duration(min(n, int(link.MaxPing/time.Second))) * time.Second
}
