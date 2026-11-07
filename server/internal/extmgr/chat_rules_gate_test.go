package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The assistant's real Lark commands are an extension's command gate (a gate file with connector "lark"), and its rules
// are what the node's gate reads for them: the hand-written ones (connector_gate's Lark rules, then lark_gate's), the
// user's per-chat rules after them, then the rules the gate file ships (here a catch-all confirm, which the user's rule
// has to come before to mean anything). Adding or dropping a rule changes what is served to that gate on the next
// pull, and only a row made through that gate's own command offers a rule: not another command's, and not one that names none.
func TestChatRulesReachTheLarkCommandGate(t *testing.T) {
	ctx := context.Background()
	h, _ := extensionTestHub(t)
	dir := apiPackage(t)
	if err := os.WriteFile(filepath.Join(dir, "gate.json"), []byte(`{"command": "feed", "script": "example-feed/scripts/feed.sh", "title": "示例",
		"connector": "lark", "env": ["FEED_BASE", "FEED_TOKEN"], "base_env": "FEED_BASE", "secret": ["FEED_TOKEN"],
		"rules": [{"method": "GET", "path": "**", "action": "allow"}, {"method": "*", "path": "**", "action": "confirm"}],
		"tool": {"name": "example_feed", "description": "Run feed."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mux, master := h.routes(), h.cfg.Token
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+dir+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	waitLong(t, "the plugin up", func() bool {
		hp := h.extensions.get("example-feed")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})
	h.cfg.ConnectorGate = []connector.Rule{{Connector: "lark", Action: "POST /conversations/oc_1/messages", Decision: connector.Deny}}
	h.cfg.LarkGate = []proto.LarkRule{{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"}}
	st := &memRuleStore{}
	h.ruleStore = st

	served := func() (cli, gate []proto.LarkRule) {
		t.Helper()
		lk, err := h.assistantLark(ctx)
		if err != nil || len(lk.Gates) != 1 || lk.Gates[0].Command != "feed" {
			t.Fatalf("gates %+v, %v", lk.Gates, err)
		}
		return lk.Rules, lk.Gates[0].Rules
	}
	hand := []proto.LarkRule{
		{Method: "POST", Path: "/conversations/oc_1/messages", Action: "deny"},
		{Method: "POST", Path: "/conversations/oc_2/messages", Action: "confirm"},
	}
	fromFile := []proto.LarkRule{{Method: "GET", Path: "**", Action: "allow"}, {Method: "*", Path: "**", Action: "confirm"}}
	allow := func(conv string) proto.LarkRule {
		return proto.LarkRule{Method: "POST", Path: "/conversations/" + conv + "/messages", Action: "allow"}
	}
	row := func(conv, command string) db.SentAsUser {
		target := map[string]any{"method": "POST", "path": "/conversations/" + conv + "/messages", "conversation": conv}
		if command != "" {
			target["command"] = command
		}
		raw, _ := json.Marshal(target)
		return db.SentAsUser{Connector: "lark", Action: "POST /conversations/" + conv + "/messages", Target: raw}
	}
	join := func(parts ...[]proto.LarkRule) (out []proto.LarkRule) {
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	// With no user rule: the hand-written rules, then the gate file's, exactly as before.
	if cli, gate := served(); !reflect.DeepEqual(cli, hand) || !reflect.DeepEqual(gate, join(hand, fromFile)) {
		t.Fatalf("no user rules: lark-cli's %+v, the gate's %+v", cli, gate)
	}

	// Rows made through the gate's own command give rules; another command's do not, and neither does a row that names none.
	for _, conv := range []string{"oc_1", "oc_3"} {
		if _, _, err := h.addChatRule(ctx, row(conv, "feed")); err != nil {
			t.Fatalf("%s: %v", conv, err)
		}
	}
	var no *connector.NoChatRuleError
	if _, _, err := h.addChatRule(ctx, row("oc_4", "othergate")); !errors.As(err, &no) || no.Code != connector.NoChatCommand || len(st.ids()) != 2 {
		t.Fatalf("a row of another gate's command: %v, kept %v", err, st.ids())
	}
	if _, _, err := h.addChatRule(ctx, row("oc_5", "")); !errors.As(err, &no) || no.Code != connector.NoChatCommand || len(st.ids()) != 2 {
		t.Fatalf("a row that names no command: %v, kept %v", err, st.ids())
	}
	user := []proto.LarkRule{allow("oc_1"), allow("oc_3")}
	if cli, gate := served(); !reflect.DeepEqual(cli, join(hand, user)) || !reflect.DeepEqual(gate, join(hand, user, fromFile)) {
		t.Fatalf("with user rules: lark-cli's %+v\nthe gate's %+v", cli, gate)
	}

	// The ledger page says the same: a row of the gate's own command can give a rule, another gate's or none cannot.
	got := h.chatRuleStates(ctx, []db.SentAsUser{row("oc_8", "feed"), row("oc_9", "othergate"), row("oc_6", "")})
	if !got[0].Can || got[1].Can || got[1].Reason != connector.NoChatCommand || got[2].Can || got[2].Reason != connector.NoChatCommand {
		t.Fatalf("what the ledger page says of rows of the gate's command: %+v", got)
	}

	// Dropped: served without them on the next pull.
	for _, id := range []string{"lark:oc_1", "lark:oc_3"} {
		if had, err := h.removeChatRule(ctx, id); err != nil || !had {
			t.Fatalf("remove %s: %v %v", id, had, err)
		}
	}
	if cli, gate := served(); !reflect.DeepEqual(cli, hand) || !reflect.DeepEqual(gate, join(hand, fromFile)) {
		t.Fatalf("after dropping them: lark-cli's %+v, the gate's %+v", cli, gate)
	}

	// With no extension that has that command, a row that names one offers no rule.
	bare := newTestManager(t)
	if _, err := bare.chatRuleOfRow(row("oc_1", "feed")); !errors.As(err, &no) || no.Code != connector.NoChatCommand {
		t.Fatalf("no Lark gate has the command: %v", err)
	}
}
