package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/depguard"
)

// The manager's program links none of the hub's own packages (its server, its edge, its command line and the web app
// it serves, push and the relay's), on every platform the guards check (docs/server/server.md "Programs"). The
// manager talks to the hub over the bus only.
func TestPersonaManagerDoesNotLinkTheHub(t *testing.T) {
	for _, target := range depguard.Platforms {
		goos, goarch, _ := strings.Cut(target, "/")
		for _, dep := range depguard.Deps(t, ".", goos, goarch) {
			if slices.ContainsFunc(depguard.HubOwn, func(p string) bool { return depguard.Under(dep, p) }) {
				t.Errorf("persona-manager (%s) links %s, the hub's", target, dep)
			}
		}
	}
}
