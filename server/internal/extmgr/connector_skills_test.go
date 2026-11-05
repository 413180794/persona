package extmgr

import (
	"context"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// stateConnector is a connector in a given state.
type stateConnector struct {
	id    string
	state string
}

func (c stateConnector) ID() string   { return c.id }
func (c stateConnector) Name() string { return c.id }
func (stateConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus}}
}
func (c stateConnector) Status(context.Context) connector.Status {
	return connector.Status{State: c.state}
}

// The node is told the connectors signed in (rate limited is still signed in): their skills go to the assistant.
func TestSignedInConnectors(t *testing.T) {
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	for _, c := range []stateConnector{{"github", connector.StateOK}, {"telegram", connector.StateRateLimited},
		{"imessage", connector.StateNoPermission}, {"example", connector.StateOK}} {
		if err := h.addConnector(c); err != nil {
			t.Fatal(err)
		}
	}
	_ = h.connectors.SetEnabled("example", false)
	if got := h.signedInConnectors(context.Background()); !slices.Equal(got, []string{"github", "telegram"}) {
		t.Fatalf("signed in: %v", got)
	}
}
