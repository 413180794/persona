package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/kb"
)

// What a plugin's /agent asks of the hub: a chat's knowledge-base context (read only), a new conversation under a
// purpose the hub knows on one of its machines (the assistant's route: anything else is refused before starting), and
// the hub's own lark_agent_reply out of the way while a plugin serves Feishu.
func TestAgentAPIForExtensions(t *testing.T) {
	h := newTestHub(t)
	st := withKB(t, h).Store // the assistant's (relayed to it)
	if err := st.PutChat(&kb.Chat{ID: "lark-7", Source: "lark", SourceID: "7", Kind: "group", Name: "示例群"}); err != nil {
		t.Fatal(err)
	}
	connectAssistant(t, h)
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

}
