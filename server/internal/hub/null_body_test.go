package hub

import (
	"context"
	"testing"
)

func TestNullBodyReturns400(t *testing.T) {
	h := newTestHub(t)
	h.store = newSentStore()
	pipeNode(t, h, "box", func(context.Context) error { return nil })
	mux := h.routes()
	tok := h.cfg.Token

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"proxyBody", "POST", "/api/v1/sessions/box/s1/attach"},
		{"sendMessage", "POST", sendPath},
		{"renameSession", "POST", "/api/v1/sessions/box/s1/rename"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := call(t, mux, tc.method, tc.path, tok, "null")
			if code != 400 {
				t.Fatalf("null body: got %d, want 400; body: %v", code, out)
			}
		})
	}
}

func TestEmptyObjectBodySucceeds(t *testing.T) {
	h := newTestHub(t)
	h.store = newSentStore()
	pipeNode(t, h, "box", func(context.Context) error { return nil })
	mux := h.routes()
	tok := h.cfg.Token

	code, _ := call(t, mux, "POST", "/api/v1/sessions/box/s1/attach", tok, "{}")
	if code == 400 {
		t.Fatalf("empty object should not be rejected, got %d", code)
	}
}

func TestNormalObjectBodySucceeds(t *testing.T) {
	h := newTestHub(t)
	h.store = newSentStore()
	pipeNode(t, h, "box", func(context.Context) error { return nil })
	mux := h.routes()
	tok := h.cfg.Token

	code, _ := call(t, mux, "POST", "/api/v1/sessions/box/s1/attach", tok, `{"foo":"bar"}`)
	if code == 400 {
		t.Fatalf("normal object should not be rejected, got %d", code)
	}
}
