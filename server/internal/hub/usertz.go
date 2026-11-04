package hub

import (
	"context"
	"time"
)

// The user's time zone is part of their profile (settings assistant's timezone, set from the phone): the hub reads
// times in it, and passes it to the assistant in its hello (assistant_link.go).

// userLocation is the phone's time zone (the app reports it), else the hub's.
func (h *Hub) userLocation() *time.Location {
	h.mu.Lock()
	tz := h.userTZ
	h.mu.Unlock()
	if tz == "" && h.store != nil {
		var cfg AssistantConfig
		if ok, _ := h.store.Setting(context.Background(), "assistant", &cfg); ok && cfg.Timezone != "" {
			tz = cfg.Timezone
			h.mu.Lock()
			h.userTZ = tz
			h.mu.Unlock()
		}
	}
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

// userTZName is the user's time zone by name ("Asia/Shanghai"), "" when only the hub's own is known: for what goes to
// another program (a gateway's daily windows).
func (h *Hub) userTZName() string {
	if loc := h.userLocation(); loc != time.Local {
		return loc.String()
	}
	return ""
}
