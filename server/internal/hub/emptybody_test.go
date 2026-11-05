package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// emptyPost is a POST with no body whose length is unknown (a chunked request that sends nothing).
func emptyPost(sid string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	r.ContentLength = -1
	r.SetPathValue("mid", "server")
	r.SetPathValue("sid", sid)
	return r
}

// An empty body is no parameters, not a 400 "EOF": the call goes to the node as it would with no body at all.
func TestAnEmptyBodyIsNoParameters(t *testing.T) {
	h := newTestHub(t)
	n := newAssistantNode(t, h)

	t.Run("proxyBody", func(t *testing.T) {
		before := n.sent()
		w := httptest.NewRecorder()
		h.proxyBody("session.send")(w, emptyPost("x"))
		if w.Code != http.StatusOK || n.sent() != before+1 {
			t.Fatalf("%d %q, %d calls to the node", w.Code, w.Body.String(), n.sent()-before)
		}
		if p := n.lastSend(t); p["session_id"] != "x" {
			t.Fatalf("%v", p)
		}
	})

	t.Run("sendMessage", func(t *testing.T) {
		before := n.sent()
		w := httptest.NewRecorder()
		h.sendMessage(w, emptyPost("y"), n.m)
		if w.Code != http.StatusOK || n.sent() != before+1 {
			t.Fatalf("%d %q, %d calls to the node", w.Code, w.Body.String(), n.sent()-before)
		}
	})

	t.Run("renameSession", func(t *testing.T) {
		before := n.sent()
		w := httptest.NewRecorder()
		r := emptyPost("z")
		r.ContentLength = 0 // a plain POST with no body: rename has no length check of its own
		h.renameSession(w, r, n.m)
		if w.Code != http.StatusOK || n.sent() != before {
			t.Fatalf("%d %q, %d calls to the node", w.Code, w.Body.String(), n.sent()-before)
		}
	})

	t.Run("not JSON is still a 400", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{"))
		r.SetPathValue("mid", "server")
		r.SetPathValue("sid", "x")
		w := httptest.NewRecorder()
		h.proxyBody("session.send")(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%d %q", w.Code, w.Body.String())
		}
	})
}
