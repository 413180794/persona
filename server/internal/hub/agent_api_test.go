package hub

import (
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// What a plugin's /agent asks of the hub: a chat's knowledge-base context (read only), a new conversation under a
// purpose the hub knows on one of its machines (anything else is refused before starting), and the hub's own
// lark_agent_reply out of the way while a plugin serves Feishu.
func TestAgentAPIForPlugins(t *testing.T) {
	h := newTestHub(t)
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: st}
	if err := st.PutChat(&kb.Chat{ID: "lark-7", Source: "lark", SourceID: "7", Kind: "group", Name: "示例群"}); err != nil {
		t.Fatal(err)
	}
	mux := h.routes()
	tok := h.cfg.Token
	code, out := call(t, mux, "GET", "/api/v1/kb/chats/lark-7/context", tok, "")
	if code != 200 || out["chat_name"] != "示例群" || out["kind"] != "group" || out["projects"] == nil {
		t.Fatalf("context %d %v", code, out)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/kb/chats/lark-nope/context", tok, ""); code != 404 {
		t.Fatalf("unknown chat %d", code)
	}
	if code, out := call(t, mux, "POST", "/api/v1/assistant/conversations", tok, `{"prompt": "x", "purpose": "no.such"}`); code != 400 {
		t.Fatalf("unknown purpose %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/assistant/conversations", tok, `{"prompt": "x", "purpose": "lark.agent", "machine_id": "nowhere"}`); code != 400 {
		t.Fatalf("unknown machine %d %v", code, out)
	}

	names := func() []string {
		var out []string
		for _, tl := range h.ownAssistantTools() {
			out = append(out, tl.Name)
		}
		return out
	}
	if !slices.Contains(names(), "lark_agent_reply") || !h.isOwnAssistantTool("lark_agent_reply") {
		t.Fatal("the hub's own /agent reply tool")
	}
	h.connectors = connector.NewRegistry(nil)
	h.migrated.Store("lark", proto.Session{}) // a plugin took Feishu over
	if slices.Contains(names(), "lark_agent_reply") || h.isOwnAssistantTool("lark_agent_reply") || slices.Contains(h.builtinTools(), "lark_agent_reply") {
		t.Fatal("the hub's lark_agent_reply stays while a plugin serves Feishu")
	}
}
