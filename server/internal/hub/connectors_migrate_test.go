package hub

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// pluginLark stands in for a plugin's connector "lark": an inbox that records replies.
type extensionLark struct {
	things []connector.InboxThing
	got    []connector.InboxReply
}

func (*extensionLark) ID() string   { return "lark" }
func (*extensionLark) Name() string { return "飞书" }
func (*extensionLark) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}, Kinds: []string{"dm", "doc"}}
}
func (c *extensionLark) Collect(context.Context) ([]connector.InboxThing, error) {
	return c.things, nil
}
func (c *extensionLark) Reply(_ context.Context, r connector.InboxReply) error {
	c.got = append(c.got, r)
	return nil
}

// A plugin's connector takes the id lark (the hub has none of its own any more); gone with the plugin.
func TestExtensionTakesOverLark(t *testing.T) {
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	if h.connectors.Get("lark") != nil {
		t.Fatal("the hub has a Feishu connector of its own")
	}
	p := &extensionLark{}
	h.yieldOwnConnector("lark")
	if err := h.addConnector(p); err != nil {
		t.Fatal(err)
	}
	if h.connectors.Get("lark") != p {
		t.Fatal("the plugin's is the one now")
	}
	h.removeExtensionConnector("lark")
	if h.connectors.Get("lark") != nil {
		t.Fatal("still there after the plugin went")
	}
	h.yieldOwnConnector("github") // not migratable: stays
	if h.connectors.Get("github") == nil {
		t.Fatal("github is not a plugin's to take")
	}
}
