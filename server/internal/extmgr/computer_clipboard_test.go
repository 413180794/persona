package extmgr

import (
	"context"
	"testing"
)

// The Mac's clipboard through its node, for a Mac whose screen the app may show; not before.
func TestComputerClipboard(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	computerMac(t, h, "mac")
	if code, _ := call(t, mux, "GET", "/api/v1/machines/mac/computer/clipboard", h.cfg.Token, ""); code != 404 {
		t.Fatalf("the plugin off: %d", code)
	}
	if err := h.extensions.reg.SetEnabled(ctx, cuaExtensionID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/mac/computer", h.cfg.Token, `{"enabled": true}`); code != 200 {
		t.Fatal(code)
	}
	if code, out := call(t, mux, "POST", "/api/v1/machines/mac/computer/clipboard", h.cfg.Token, `{"text": "从手机来的"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/machines/mac/computer/clipboard", h.cfg.Token, ""); code != 200 || out["text"] != "从手机来的" {
		t.Fatalf("%d %v", code, out)
	}
}
