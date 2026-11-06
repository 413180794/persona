package extmgr

import (
	"context"
	"encoding/json"
	"testing"
)

// "Don't auto-lock while the extension is on": turned on for a Mac from its page, the node is told to hold it (and
// again by noLockLoop); off, or the plugin off, it is told to let go.
func TestNoAutoLock(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	mac, _ := computerMac(t, h, "mac")
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": true}`); code != 200 || out["no_auto_lock"] == true {
		t.Fatalf("on for the Mac, not this: %d %v", code, out)
	}
	fakeNoLockMu.Lock()
	delete(fakeNoLock, "mac")
	fakeNoLockMu.Unlock()
	told := func() []bool {
		fakeNoLockMu.Lock()
		defer fakeNoLockMu.Unlock()
		return append([]bool(nil), fakeNoLock["mac"]...)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"no_auto_lock": true}`); code != 200 || out["no_auto_lock"] != true || out["enabled"] != true {
		t.Fatalf("turned on, the Mac's switch left as it was: %d %v", code, out)
	}
	var cfg map[string]any
	_ = json.Unmarshal(h.extensions.reg.Config(h.extensions.reg.Get(cuaExtensionID)), &cfg)
	if l, _ := cfg["no_auto_lock"].([]any); len(l) != 1 || l[0] != "mac" || !h.noLockOn(mac) {
		t.Fatalf("in the plugin's settings: %v", cfg)
	}
	h.tellNoLock(ctx, mac)
	if on := told(); len(on) == 0 || !on[len(on)-1] {
		t.Fatalf("the node holds it: %v", on)
	}
	call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"no_auto_lock": false}`)
	h.tellNoLock(ctx, mac)
	if on := told(); on[len(on)-1] {
		t.Fatalf("off: let go: %v", on)
	}
	call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"no_auto_lock": true}`)
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, false, "test"); err != nil {
		t.Fatal(err)
	}
	h.tellNoLock(ctx, mac)
	if on := told(); on[len(on)-1] || h.noLockOn(mac) {
		t.Fatalf("the plugin off: let go: %v", on)
	}
}
