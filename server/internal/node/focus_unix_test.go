//go:build !windows

package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func focusCalls(f *fakeHerdr) []string {
	return f.trace("pane.focus", "pane.get", "workspace.focus", "tab.focus")
}

func TestFocusRequestMovesHerdrFocusAndIsRememberedAsOurs(t *testing.T) {
	n, f, show, pushes := focusNode(t)
	fo := focuserOf(t, n)
	ctx := context.Background()
	n.reportFocus(ctx, fo)
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": {id: "hc_1"}}
	n.mu.Unlock()
	f.reset()

	// A plain shell by its address.
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":"pane:w2:p3"}`))
	fullWait(t, 3*time.Second, "pane.focus", func() bool { return len(focusCalls(f)) > 0 })
	if got := focusCalls(f); len(got) != 1 || got[0] != `pane.focus {"pane_id":"w2:p3"}` {
		t.Fatalf("herdr calls = %v", got)
	}
	// The echo of our own focus comes back as Persona's, naming the view that asked.
	show("w2", "w2:t1", "w2:p3")
	n.reportFocus(ctx, fo)
	if p := nextFocus(t, pushes); p["source"] != "persona" || p["client_id"] != "hc_1" || p["pane_id"] != "w2:p3" {
		t.Fatalf("the echo = %v", p)
	}

	// An agent by its session id.
	f.reset()
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":"` + sidA + `"}`))
	fullWait(t, 3*time.Second, "pane.focus", func() bool { return len(focusCalls(f)) > 0 })
	if got := focusCalls(f); len(got) != 1 || got[0] != `pane.focus {"pane_id":"w1:p1"}` {
		t.Fatalf("herdr calls = %v", got)
	}
}

func TestFocusRequestIsRefusedWithoutAViewOrForAPaneThatIsNotThere(t *testing.T) {
	n, f, _, _ := focusNode(t)
	f.reset()
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_nobody","session_id":"pane:w2:p3"}`))
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": {id: "hc_1"}}
	n.mu.Unlock()
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":"pane:w9:p9"}`))
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":""}`))
	time.Sleep(300 * time.Millisecond)
	if got := focusCalls(f); len(got) != 0 {
		t.Fatalf("herdr was asked to focus: %v", got)
	}
}

func TestFocusRequestsOfOneViewReplaceEachOther(t *testing.T) {
	n, f, _, _ := focusNode(t)
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": {id: "hc_1"}}
	n.mu.Unlock()
	f.reset()
	for _, id := range []string{"pane:w1:p2", "pane:w2:p3", sidA} {
		n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":"` + id + `"}`))
	}
	fullWait(t, 3*time.Second, "the last focus", func() bool {
		got := focusCalls(f)
		return len(got) > 0 && got[len(got)-1] == `pane.focus {"pane_id":"w1:p1"}`
	})
}

func TestFocusFallsBackToWorkspaceAndTabOnAnOlderHerdr(t *testing.T) {
	n, f, _, _ := focusNode(t)
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": {id: "hc_1"}}
	n.mu.Unlock()
	f.handle("pane.focus", func(map[string]any) any {
		return herdrFail{Code: "invalid_request", Message: "unknown variant `pane.focus`"}
	})
	f.reply("pane.get", map[string]any{"pane": map[string]any{"pane_id": "w2:p3", "tab_id": "w2:t1", "workspace_id": "w2"}})
	f.reset()
	n.onFocusRequest(json.RawMessage(`{"client_id":"hc_1","session_id":"pane:w2:p3"}`))
	want := []string{`pane.focus {"pane_id":"w2:p3"}`, `pane.get {"pane_id":"w2:p3"}`, `workspace.focus {"workspace_id":"w2"}`, `tab.focus {"tab_id":"w2:t1"}`}
	fullWait(t, 3*time.Second, "the fallback", func() bool { return len(focusCalls(f)) >= len(want) })
	got := focusCalls(f)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("herdr calls = %v, want %v", got, want)
		}
	}
}
