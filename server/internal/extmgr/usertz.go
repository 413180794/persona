package extmgr

import "time"

// userLocation is the user's time zone, else this machine's.
func (h *Manager) userLocation() *time.Location {
	if tz := h.userTZName(); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

// userTZName is the user's zone by name, "" when not known.
func (h *Manager) userTZName() string {
	if h.opts.Timezone != nil {
		return h.opts.Timezone()
	}
	return ""
}
