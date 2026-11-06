package hub

import "time"

// The user's time zone is part of the assistant's settings (the phone reports it to the assistant, which reports it
// in its profile, assistant_profile.go): the hub reads times in it.

// userLocation is the user's time zone (the phone reports it to the assistant, which keeps it), else the hub's.
func (h *Hub) userLocation() *time.Location {
	if tz := h.assistantProfile().Timezone; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}
