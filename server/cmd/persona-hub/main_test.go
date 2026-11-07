package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/depguard"
)

// The hub's program links its own packages and the shared ones, nothing of the manager's side, on every platform the
// guards check (docs/server/server.md "Programs"). The hub talks to managers over the bus only. A package that is
// neither: put it in depguard.HubOwn or depguard.Shared on purpose.
func TestPersonaHubDoesNotLinkTheManager(t *testing.T) {
	allowed := slices.Concat(depguard.HubOwn, depguard.Shared, depguard.HubExceptions)
	for _, target := range depguard.Platforms {
		goos, goarch, _ := strings.Cut(target, "/")
		deps := depguard.Deps(t, ".", goos, goarch)
		for _, dep := range deps {
			if !slices.Contains(allowed, dep) {
				t.Errorf("persona-hub (%s) links %s: neither the hub's own nor shared (depguard)", target, dep)
			}
		}
		for _, e := range depguard.HubExceptions {
			if !slices.Contains(deps, e) {
				t.Errorf("persona-hub (%s) no longer links %s: take it off depguard.HubExceptions", target, e)
			}
		}
	}
}
