package node

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// An assistant conversation's permission card names what 本会话总是允许 grants, for the hub to keep past the process
// (#2202); an Agents session's card names nothing (its rule stays Claude's own, as before).
func TestPermissionHookAlwaysKeys(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	fh.mu.Lock()
	fh.answer = func(proto.Interaction) proto.Resolution {
		yes := true
		return proto.Resolution{Allow: &yes, Always: true}
	}
	fh.mu.Unlock()
	const assistant, agent = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	n.mu.Lock()
	n.procs[assistant] = &assistantProc{id: assistant, agent: "claude", exited: make(chan struct{})}
	n.mu.Unlock()
	hook := func(session string) any {
		t.Helper()
		raw, _ := json.Marshal(hookInput{HookEventName: "PermissionRequest", SessionID: session, ToolName: "mcp__computer__click",
			ToolUseID: "toolu_" + session[:4], ToolInput: json.RawMessage(`{"x":1}`)})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := n.handleHook(ctx, sockRequest{Input: raw, Attempt: "a1"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, c := range []struct {
		session string
		keys    []string
	}{{assistant, []string{"tool:mcp__computer__click"}}, {agent, nil}} {
		out := hook(c.session)
		ix := fh.interactions()
		if got := ix[len(ix)-1]; got.SessionID != c.session || !slices.Equal(got.AlwaysKeys, c.keys) {
			t.Fatalf("%s: always keys %q, want %q", c.session[:4], got.AlwaysKeys, c.keys)
		}
		// Either way the process gets the session rule (the hub answers a kept grant with always too).
		if b, _ := json.Marshal(out); !strings.Contains(string(b), `"type":"addRules"`) || !strings.Contains(string(b), `"toolName":"mcp__computer__click"`) {
			t.Fatalf("%s: no session rule: %s", c.session[:4], b)
		}
	}
}

// A gated write's card names its routes, ids blanked: the hub keeps them when this node's own memory of them is gone.
func TestLarkGateAlwaysKeys(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = olCfg(), time.Now().Add(time.Hour)
	n.mu.Unlock()
	fh.mu.Lock()
	fh.answer = func(proto.Interaction) proto.Resolution {
		yes := true
		return proto.Resolution{Allow: &yes, Always: true}
	}
	fh.mu.Unlock()
	cwd := t.TempDir()
	cmd := `ex POST /conversations/7123/messages '{"text":"hi"}' && ex POST /conversations/8456/messages '{"text":"yo"}'`
	if d, _, err := n.larkCheck(context.Background(), "s1", cwd, cmd, proto.Interaction{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}); err != nil || d != "allow" {
		t.Fatalf("larkCheck: %q %v", d, err)
	}
	ix := fh.interactions()
	if len(ix) != 1 || !slices.Equal(ix[0].AlwaysKeys, []string{"lark:POST /conversations/*/messages"}) {
		t.Fatalf("cards %d, always keys %q", len(ix), ix[len(ix)-1].AlwaysKeys)
	}
	// This node remembers too: the same route is not asked about again while it runs.
	if d, _, _ := n.larkCheck(context.Background(), "s1", cwd, `ex POST /conversations/9/messages '{"text":"x"}'`, proto.Interaction{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}); d != "allow" || len(fh.interactions()) != 1 {
		t.Fatalf("remembered route asked again: %q, %d cards", d, len(fh.interactions()))
	}
}
