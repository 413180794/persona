package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// endlessBody is a JSON object whose string value never ends; it counts what the server read of it.
type endlessBody struct {
	head []byte
	read int64
}

func (b *endlessBody) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if b.read < int64(len(b.head)) {
			p[n] = b.head[b.read]
		} else {
			p[n] = 'a'
		}
		n++
		b.read++
	}
	return n, nil
}

func (b *endlessBody) Close() error { return nil }

// postEndless sends an endless body to path with token and returns the status, the error code and how much was read.
func postEndless(t *testing.T, mux http.Handler, method, path, token string) (int, string, int64) {
	t.Helper()
	body := &endlessBody{head: []byte(`{"name":"`)}
	req := httptest.NewRequest(method, path, nil)
	req.Body = body
	req.ContentLength = -1
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out.Code, body.read
}

// A watch (answer-only) token may answer cards; an oversized answer is refused with 413 after reading about its cap,
// and a normal one still reaches the handler.
func TestResolveBodyLimitForAnswerRole(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	mux := h.routes()
	_, phone := call(t, mux, "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"iPhone","kind":"phone"}`)
	_, watch := call(t, mux, "POST", "/api/v1/auth/tokens", phone["token"].(string), `{"name":"Watch","kind":"watch"}`)
	watchTok := watch["token"].(string)

	code, errCode, read := postEndless(t, mux, "POST", "/api/v1/interactions/ix_x/resolve", watchTok)
	if code != http.StatusRequestEntityTooLarge || errCode != proto.CodeTooLarge {
		t.Fatalf("oversized answer: %d %q, want 413 %q", code, errCode, proto.CodeTooLarge)
	}
	if read > 2<<20 {
		t.Fatalf("read %d bytes of an oversized answer (cap 1 MiB)", read)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/interactions/ix_x/resolve", watchTok, `{"allow":true}`); code != http.StatusGone {
		t.Fatalf("a normal answer reaches the handler (410: no such card), got %d", code)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/interactions/ix_x/resolve", watchTok, `{"allow":`); code != http.StatusBadRequest {
		t.Fatalf("a malformed answer: %d, want 400", code)
	}
}

// A pairing code for one machine's jarvis-link can only trade itself for a token: an oversized body is refused before
// its role is checked, without reading it all.
func TestAuthTokensBodyLimitForLinkPair(t *testing.T) {
	h := newTestHub(t)
	store := newPairStore()
	h.store = store
	mux := h.routes()
	const pair = "jp_link_pair_code_for_test"
	if err := store.CreatePairToken(context.Background(), hashToken(pair), time.Now().Add(time.Hour), "box"); err != nil {
		t.Fatal(err)
	}

	code, errCode, read := postEndless(t, mux, "POST", "/api/v1/auth/tokens", pair)
	if code != http.StatusRequestEntityTooLarge || errCode != proto.CodeTooLarge {
		t.Fatalf("oversized pairing request: %d %q, want 413 %q", code, errCode, proto.CodeTooLarge)
	}
	if read > 64<<10 {
		t.Fatalf("read %d bytes of an oversized pairing request (cap 4 KiB)", read)
	}
	// A small body gets past decoding to the hub.json check (no machine "box" of kind link here).
	code, out := call(t, mux, "POST", "/api/v1/auth/tokens", pair, `{"name":"box link","kind":"link"}`)
	if code != http.StatusBadRequest || !strings.Contains(out["error"].(string), "kind link") {
		t.Fatalf("small pairing request: %d %v", code, out)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"iPhone","kind":"phone"}`); code != 200 {
		t.Fatalf("a full device adds a phone: %d", code)
	}
}

// PUT /api/v1/pins and /api/v1/mutes store their arrays as they come: an oversized body is refused and the stored
// setting stays as it was.
func TestPinsBodyLimit(t *testing.T) {
	h := newTestHub(t)
	store := &settingsStore{vals: map[string][]byte{}}
	h.store = store
	mux := h.routes()
	for _, key := range []string{"pins", "mutes"} {
		path := "/api/v1/" + key
		if code, _ := call(t, mux, "PUT", path, h.cfg.Token, `{"spaces":["m/a"],"sessions":["m/s1"]}`); code != 200 {
			t.Fatalf("PUT %s: %d", key, code)
		}
		before := string(store.vals[key])
		puts := store.puts

		big := `{"spaces":["` + strings.Repeat("m/x", 1<<19) + `"],"sessions":[]}` // 1.5 MiB
		code, out := call(t, mux, "PUT", path, h.cfg.Token, big)
		if code != http.StatusRequestEntityTooLarge || out["code"] != proto.CodeTooLarge {
			t.Fatalf("oversized PUT %s: %d %v", key, code, out)
		}
		if got := string(store.vals[key]); got != before || store.puts != puts {
			t.Fatalf("%s changed after a refused PUT: %s", key, got)
		}
		if code, _ := call(t, mux, "PUT", path, h.cfg.Token, `{"spaces":[`); code != http.StatusBadRequest {
			t.Fatalf("malformed PUT %s: %d, want 400", key, code)
		}
	}
}

// decodeJSONBody's cap: at the limit the body decodes, one byte over it is 413, a malformed one 400.
func TestDecodeJSONBodyTooLarge(t *testing.T) {
	const limit = 64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			S string `json:"s"`
		}
		if err := decodeJSONBody(w, r, &v, limit); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		_, _ = io.WriteString(w, v.S)
	})
	fill := func(n int) string { // a body of exactly n bytes
		return `{"s":"` + strings.Repeat("x", n-len(`{"s":""}`)) + `"}`
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{fill(limit), 200},
		{fill(limit + 1), http.StatusRequestEntityTooLarge},
		{`{"s":`, http.StatusBadRequest},
		{``, http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(c.body)))
		if w.Code != c.want {
			t.Errorf("%d-byte body: %d, want %d (%s)", len(c.body), w.Code, c.want, w.Body.String())
		}
	}
}
