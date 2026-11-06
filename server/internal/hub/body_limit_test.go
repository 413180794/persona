package hub

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// oversizedJSON is a well-formed JSON object of about n bytes: cut at any cap below n it no longer parses.
func oversizedJSON(field string, n int) string {
	return `{"` + field + `":"` + strings.Repeat("a", n) + `"}`
}

func requireTooLarge(t *testing.T, what string, w *httptest.ResponseRecorder) {
	t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != http.StatusRequestEntityTooLarge || out.Code != proto.CodeTooLarge {
		t.Fatalf("%s: %d %s, want 413 %q", what, w.Code, w.Body, proto.CodeTooLarge)
	}
}

// The admin peer routes answer an over-the-cap body with 413 too_large instead of a misleading "is required",
// and the center route rejects a malformed body while still accepting an empty one.
func TestPeerAdminBodyLimit(t *testing.T) {
	_, _, mux := adminTestHub(t)
	c := signedIn(t, mux, ip4(192, 168, 1, 20)+":1")
	for _, tc := range []struct{ path, field string }{
		{"/api/v1/admin/peers", "code"},
		{"/api/v1/admin/peers/probe", "address"},
		{"/api/v1/admin/peers/hub_other/center", "note"},
	} {
		_, _, w := c.do("POST", tc.path, oversizedJSON(tc.field, 64<<10))
		requireTooLarge(t, tc.path, w)
	}
	if code, out, _ := c.do("POST", "/api/v1/admin/peers/hub_other/center", `{"confirm":tru`); code != http.StatusBadRequest {
		t.Fatalf("malformed center body: %d %v, want 400", code, out)
	}
	for _, body := range []string{"", `{"confirm":true}`} {
		if code, out, _ := c.do("POST", "/api/v1/admin/peers/hub_other/center", body); code != http.StatusNotFound || out["code"] != "not_in_group" {
			t.Fatalf("center body %q: %d %v, want 404 not_in_group", body, code, out)
		}
	}
}

// POST /api/v1/peer/pair: an over-the-cap body is 413 too_large, a malformed one still 400.
func TestPeerPairBodyLimit(t *testing.T) {
	h := newTestHub(t)
	h.store = newMeshStore()
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", peerPairPath, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: rolePeerPair, PairHash: "h"}))
		r.TLS = &tls.ConnectionState{} // plaintext pairing is mesh.plaintext's (peer_plain_test.go)
		w := httptest.NewRecorder()
		h.servePeerPair(w, r)
		return w
	}
	requireTooLarge(t, "peer pair", post(oversizedJSON("name", 128<<10)))
	if w := post(`{"hub_id":`); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed peer pair body: %d %s, want 400", w.Code, w.Body)
	}
}

// POST /api/v1/auth/pair-codes: an endless body is 413 too_large after reading about its cap; no body still works.
func TestPairCodeBodyLimit(t *testing.T) {
	h := newTestHub(t)
	h.store = newPairStore()
	mux := h.routes()
	code, errCode, read := postEndless(t, mux, "POST", "/api/v1/auth/pair-codes", h.cfg.Token)
	if code != http.StatusRequestEntityTooLarge || errCode != proto.CodeTooLarge {
		t.Fatalf("endless pair-code body: %d %q, want 413 %q", code, errCode, proto.CodeTooLarge)
	}
	if read > 64<<10 {
		t.Fatalf("read %d bytes of an endless pair-code body (cap 4 KiB)", read)
	}
	newPairCode(t, h)
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"machine":`, "127.0.0.1:1", false), 400, "")
}
