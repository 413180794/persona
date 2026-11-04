package hub

import (
	"time"
)

// The user's time zone in tests: times are built in UTC and read in it, so a test gives the same result whatever zone
// the machine running it is in (the hub runs in UTC, its user is not).
var testUserZone = mustZone("Asia/Shanghai")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}
