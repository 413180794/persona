package extmgr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// endlessBody is a request body that never ends: what a handler reads of it shows whether it stops at its cap.
type endlessBody struct {
	head []byte
	read int64
}

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		if b.read < int64(len(b.head)) {
			p[i] = b.head[b.read]
		} else {
			p[i] = 'a'
		}
		b.read++
	}
	return len(p), nil
}

func (b *endlessBody) Close() error { return nil }

// The personal agents' routes that cap their body (the manager's, relayed by the hub) answer an oversized one with
// 413 too_large after reading about the cap; a small malformed body is still 400.
func TestAgentRoutesBodyLimitTooLarge(t *testing.T) {
	h := newTestManager(t)
	h.initAgents()
	mux := h.routes()
	for _, c := range []struct {
		method, path string
		limit        int64
	}{
		{"PUT", "/api/v1/agents/prefs", 1 << 20},
		{"POST", "/api/v1/agents/sessions/x/messages", 1 << 20},
	} {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			body := &endlessBody{head: []byte(`{"name":"`)}
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Body = body
			req.ContentLength = -1
			req.RemoteAddr = "127.0.0.1:12345"
			req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			var out struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			if w.Code != http.StatusRequestEntityTooLarge || out.Code != proto.CodeTooLarge {
				t.Fatalf("oversized body: %d %q, want 413 %q", w.Code, out.Code, proto.CodeTooLarge)
			}
			if body.read > 2*c.limit+64<<10 {
				t.Fatalf("read %d bytes of an oversized body (cap %d)", body.read, c.limit)
			}
			if code, got := call(t, mux, c.method, c.path, h.cfg.Token, `{"name":`); code != http.StatusBadRequest {
				t.Fatalf("malformed body: %d %v, want 400", code, got)
			}
		})
	}
}
