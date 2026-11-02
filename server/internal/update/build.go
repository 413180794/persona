// Package update is how a hub knows what it is and whether a newer one is out (docs/hub-update.md): the build it was
// made as (channel, build number), the manifest the publisher puts next to the downloads (latest.json, with every
// file's sha256), the publisher's checks before a release, and installing one (download, swap, the watchdog).
package update

import (
	"fmt"
	"strconv"
	"time"
)

// Channels: how a binary was built, which decides whether it updates itself.
const (
	Release = "release" // published by publish.sh, or deployed straight from master (deploy.sh): updates itself
	Docker  = "docker"  // in the Docker image: says when a newer image is out, the user pulls it
	Dev     = "dev"     // a local build: shows its version only
)

// Build is what this binary is.
type Build struct {
	Version   string // for people: git describe and the time
	Channel   string // Release, Docker or Dev
	Number    int64  // the build number: when it was built, UTC YYYYMMDDHHMM; 0 when unknown
	UpdateURL string // where the publisher's dl/ is (latest.json); empty: nowhere to look
}

// Make turns what the build wrote into the binary (-X main.version, main.channel, main.build, main.updateURL) into a
// Build. An unknown channel is Dev; a release without a valid build number is Dev too (it could not tell newer from
// older, so it must not update).
func Make(version, channel, number, updateURL string) Build {
	b := Build{Version: version, Channel: Dev, UpdateURL: updateURL}
	n, err := ParseNumber(number)
	if err == nil {
		b.Number = n
	}
	switch channel {
	case Release:
		if err == nil {
			b.Channel = Release
		}
	case Docker:
		b.Channel = Docker
	}
	return b
}

// SelfUpdates says whether this binary replaces itself with newer releases.
func (b Build) SelfUpdates() bool { return b.Channel == Release && b.Number > 0 && b.UpdateURL != "" }

// ParseNumber reads a build number: twelve digits, a UTC time YYYYMMDDHHMM.
func ParseNumber(s string) (int64, error) {
	if len(s) != 12 {
		return 0, fmt.Errorf("build number %q is not YYYYMMDDHHMM", s)
	}
	if _, err := time.Parse("200601021504", s); err != nil {
		return 0, fmt.Errorf("build number %q is not a time: %w", s, err)
	}
	return strconv.ParseInt(s, 10, 64)
}

// NumberAt is the build number for t.
func NumberAt(t time.Time) int64 {
	n, _ := strconv.ParseInt(t.UTC().Format("200601021504"), 10, 64)
	return n
}

// Newer says whether a release numbered latest replaces this build: only a strictly larger number does. A build
// deployed straight from master carries its build time, so an older release seen later never downgrades it
// (TestNoDowngrade).
func (b Build) Newer(latest int64) bool { return b.Number > 0 && latest > b.Number }
