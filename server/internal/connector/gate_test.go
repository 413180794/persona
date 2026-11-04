package connector

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// guarded is fake with a second action, and its comments listed in confirm_always.
type guarded struct{ *fake }

func (g guarded) Manifest() Manifest {
	m := g.fake.Manifest()
	m.ConfirmAlways = []string{"*_comment"}
	return m
}

func (g guarded) Actions() []proto.Tool {
	return []proto.Tool{{Name: g.id + "_comment"}, {Name: g.id + "_note"}}
}

// confirm_always clamps an allow to confirm and leaves deny, the unlisted actions and the connectors without it as
// they were.
func TestConfirmAlwaysClampsAllow(t *testing.T) {
	m := Manifest{ConfirmAlways: []string{"example_comment"}}
	allowAll := []Rule{{Connector: "*", Action: "*", Decision: Allow}}
	denyAll := []Rule{{Connector: "*", Action: "*", Decision: Deny}}
	for _, c := range []struct {
		name  string
		rules []Rule
		m     Manifest
		tool  string
		want  string
	}{
		{"allow is clamped", allowAll, m, "example_comment", Confirm},
		{"deny stays", denyAll, m, "example_comment", Deny},
		{"no rule is confirm", nil, m, "example_comment", Confirm},
		{"other actions are still allowed", allowAll, m, "example_note", Allow},
		{"not declared: unchanged", allowAll, Manifest{}, "example_comment", Allow},
	} {
		if got := Decide(c.rules, c.m, "example", c.tool, nil); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A hub.json rule that allows everything cannot make the AI write what the manifest says to always confirm: the card
// is still asked, and a decline stops it. The user's own write (Run) is not gated, so it is not asked.
func TestConfirmAlwaysStillAsks(t *testing.T) {
	ctx := context.Background()
	asked := 0
	no := func(context.Context, string, json.RawMessage) bool { asked++; return false }
	yes := func(context.Context, string, json.RawMessage) bool { asked++; return true }
	c := guarded{&fake{id: "example", status: Status{State: StateOK}}}
	r := NewRegistry([]Rule{{Connector: "*", Action: "*", Decision: Allow}})
	_ = r.Add(c)

	if res := r.Call(ctx, "example_comment", nil, no); !res.IsError || asked != 1 || len(c.did) != 0 {
		t.Fatalf("an allow rule ran a confirm_always action: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Call(ctx, "example_comment", nil, nil); !res.IsError || len(c.did) != 0 {
		t.Fatalf("ran with no way to ask: %+v %v", res, c.did)
	}
	if res := r.Call(ctx, "example_comment", nil, yes); res.IsError || asked != 2 || len(c.did) != 1 {
		t.Fatalf("confirmed but did not run: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Call(ctx, "example_note", nil, no); res.IsError || asked != 2 || len(c.did) != 2 {
		t.Fatalf("an action not in confirm_always lost its allow rule: %+v, asked %d, did %v", res, asked, c.did)
	}
	if res := r.Run(ctx, "example_comment", nil); res.IsError || asked != 2 || len(c.did) != 3 {
		t.Fatalf("the user's own write was gated: %+v, asked %d, did %v", res, asked, c.did)
	}
}
