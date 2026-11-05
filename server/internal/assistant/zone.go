package assistant

import (
	"context"
	"time"
)

// location is the user's time zone (the phone reports it: PUT /api/v1/assistant, kept in the settings), else this
// machine's own while the user's is unknown (the hub reads times the same way: its userLocation).
func (s *Service) location(ctx context.Context) *time.Location {
	return zoneOf(s.settings(ctx).Timezone)
}

// zoneOf is the time zone named tz, else this machine's own (tz empty or not a zone this machine knows).
func zoneOf(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}
