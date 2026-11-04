package assistant

import (
	"context"
	"time"
)

// location is the user's time zone (the phone reports it, the hub's hello passes it on), else the hub's own: the
// hub's zone only while the user's is unknown, as the hub's userLocation.
func (s *Service) location(ctx context.Context) *time.Location {
	if tz := s.hello(ctx).Timezone; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}
