package hub

import (
	"context"
	"testing"
	"time"
)

// The stop button in an app on a Feishu /agent conversation (a reporting one, #3308): the hub interrupts it and tells
// the assistant, which marks it stopped by the app (the plugin reads that in GET /api/v1/assistant/reporters).
// Stopping any other conversation marks nothing.
func TestInterruptingAReportingConversationMarksItStopped(t *testing.T) {
	h, n := conversationsHub(t)
	at := time.Now().UTC()
	if err := h.store.PutSetting(context.Background(), "assistant.task_reporters", map[string]any{
		"code-0003": map[string]any{"machine_id": "server", "at": at}}); err != nil {
		t.Fatal(err)
	}
	restartAssistant(t, h)
	mux := h.routes()
	tok := h.cfg.Token
	if code, out := call(t, mux, "POST", "/api/v1/sessions/server/trip-0002/interrupt", tok, ""); code != 200 {
		t.Fatalf("interrupt %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/sessions/server/code-0003/interrupt", tok, ""); code != 200 {
		t.Fatalf("interrupt %d %v", code, out)
	}
	if last := n.lastSend(t); last["interrupt"] != "code-0003" {
		t.Fatalf("last call to the node = %v", last)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		code, out := call(t, mux, "GET", "/api/v1/assistant/reporters", tok, "")
		list, _ := out["reporters"].([]any)
		if code != 200 || len(list) != 1 {
			t.Fatalf("reporters %d %v", code, out)
		}
		r, _ := list[0].(map[string]any)
		if r["session_id"] != "code-0003" {
			t.Fatalf("reporter %v", r)
		}
		if r["stopped_by"] == "app" && r["stopped_at"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not marked stopped: %v", r)
		}
	}
}
