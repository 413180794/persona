package assistant

import (
	"context"
	"time"
)

// location is the user's time zone (the phone reports it: PUT /api/v1/assistant, kept in the settings), else this
// machine's own while the user's is unknown (the hub reads times the same way: its userLocation).
func (s *Service) location(ctx context.Context) *time.Location {
	if tz := s.settings(ctx).Timezone; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}
