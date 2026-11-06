package hub

import (
	"context"
	"time"
)

// The user's time zone is part of the assistant's settings (the phone reports it to the assistant, which reports it
// in its profile, assistant_profile.go): the hub reads times in it.

// userLocation is the default manager's user time zone for hub features with no request to select a manager.
func (h *Hub) userLocation() *time.Location {
	return h.userLocationOf(h.defaultOwner(context.Background()))
}

// userLocationOf is manager id's user time zone, using the built-in profile until it reports one. An empty or
// invalid zone keeps the existing fallback to the hub's zone.
func (h *Hub) userLocationOf(id string) *time.Location {
	if tz := h.assistantProfileOf(id).Timezone; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}
