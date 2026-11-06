package extmgr

import (
	"context"
	"encoding/json"
	"testing"
)

// "Show the agent's cursor" and "Click sound" (#1507 d): the Macs the plugin is on for are told the cursor's setting,
// any other Mac on; a call carries the click sound for the node to play.
func TestTheCursorAndTheClickSound(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mac, seen := computerMac(t, h, "mac")
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	told := func() bool {
		t.Helper()
		fakeNoLockMu.Lock()
		delete(fakeCursor, "mac")
		fakeNoLockMu.Unlock()
		h.tellCursor(ctx, mac)
		fakeNoLockMu.Lock()
		defer fakeNoLockMu.Unlock()
		on := fakeCursor["mac"]
		if len(on) == 0 {
			t.Fatal("not told")
		}
		return on[len(on)-1]
	}
	config := func(c string) {
		t.Helper()
		if err := h.extensions.reg.SetConfig(ctx, cuaExtensionID, json.RawMessage(c), "test"); err != nil {
			t.Fatal(err)
		}
	}

	config(`{"machines": ["mac"]}`)
	if !told() {
		t.Fatal("on by default")
	}
	config(`{"machines": ["mac"], "show_cursor": false, "click_sound": "foreground"}`)
	if told() {
		t.Fatal("turned off")
	}
	if _, err := h.computerCall(ctx, mac, "persona-c1", "", "run_steps", map[string]any{"steps": []any{}}); err != nil {
		t.Fatal(err)
	}
	if _, calls := seen(); len(calls) == 0 || calls[len(calls)-1].ClickSound != "foreground" {
		t.Fatalf("the click sound with the call: %+v", calls)
	}
	config(`{"machines": [], "show_cursor": false}`)
	if !told() {
		t.Fatal("a Mac the plugin is not on for: on (undoing this hub's off)")
	}
	config(`{"machines": ["mac"], "show_cursor": false}`)
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, false, "test"); err != nil {
		t.Fatal(err)
	}
	if !told() {
		t.Fatal("the plugin off: on")
	}
}
