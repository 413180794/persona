package connector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// metaFake is a grouping inbox with a manifest of the test's choosing (a proxy: it implements every interface).
type metaFake struct{ m Manifest }

func (metaFake) ID() string                                    { return "examplechat" }
func (metaFake) Name() string                                  { return "Example Chat" }
func (f metaFake) Manifest() Manifest                          { return f.m }
func (metaFake) Collect(context.Context) ([]InboxThing, error) { return nil, nil }
func (metaFake) Reply(context.Context, InboxReply) error       { return nil }
func (metaFake) GroupTool(context.Context, GroupToolCall) (json.RawMessage, error) {
	return nil, nil
}
func (metaFake) GroupFallback(context.Context, GroupFallback) (GroupFallbackResult, error) {
	return GroupFallbackResult{}, nil
}
func (metaFake) GroupAppend(context.Context, GroupAppend) (InboxThing, error) {
	return InboxThing{}, nil
}

// meta goes with group and a meta_description; a meta_description goes with meta; group alone is fine.
func TestCheckMeta(t *testing.T) {
	caps := func(cs ...Capability) []Capability { return append([]Capability{CapInbox}, cs...) }
	for name, c := range map[string]struct {
		m    Manifest
		want string
	}{
		"group alone":        {Manifest{Capabilities: caps(CapGroup), Proxy: true}, ""},
		"group and meta":     {Manifest{Capabilities: caps(CapGroup, CapMeta), MetaDescription: "示例", Proxy: true}, ""},
		"meta without group": {Manifest{Capabilities: caps(CapMeta), MetaDescription: "示例", Proxy: true}, "meta without group"},
		"meta, no desc":      {Manifest{Capabilities: caps(CapGroup, CapMeta), Proxy: true}, "without a meta_description"},
		"desc without meta":  {Manifest{Capabilities: caps(CapGroup), MetaDescription: "示例", Proxy: true}, "without declaring meta"},
	} {
		err := Check(metaFake{c.m})
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
