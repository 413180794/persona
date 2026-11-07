package hubrelay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extwire"
)

// Without a proof key the relay sends no proof, and none an extension made either.
func TestNoKeyNoProof(t *testing.T) {
	var got http.Header
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		got = r.Header.Clone()
	}))
	defer hub.Close()
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", Knows: func(tok string) bool { return tok == "pext_x" }})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	req, _ := http.NewRequest("GET", relay.URL+"/api/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer pext_x")
	req.Header.Set(extwire.HeaderManagerProof, "v1,m=work,t=1,n=AAAAAAAAAAAAAAAAAAAAAA,s=x")
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || got == nil || got.Get(extwire.HeaderManagerProof) != "" || got.Get("Authorization") != "Bearer pext_x" {
		t.Fatalf("%d %v", res.StatusCode, got)
	}
}

func TestBadHub(t *testing.T) {
	for _, hub := range []string{"", "ftp://hub", "hub:18660", "http://"} {
		if _, err := Handler(Options{Hub: hub, Knows: func(string) bool { return true }}); err == nil {
			t.Errorf("%q taken", hub)
		}
	}
}

// A hub whose clock is far from this computer's is said in the log, once in a while, never with the token.
func TestClockSkewIsSaid(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().Add(-3*time.Minute).UTC().Format(http.TimeFormat))
	}))
	defer hub.Close()
	var mu sync.Mutex
	var logs []string
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", ProofKey: "k", Knows: func(tok string) bool { return tok == "pext_x" },
		Logf: func(format string, args ...any) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(format, args...))
			mu.Unlock()
		}})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", relay.URL+"/api/v1/machines", nil)
		req.Header.Set("Authorization", "Bearer pext_x")
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 || !strings.Contains(logs[0], "clock_skew") || strings.Contains(logs[0], "pext_x") {
		t.Fatalf("%q", logs)
	}
}

// relayDo is a request through the relay with the token pext_x (body: what to send, "" none).
func relayDo(t *testing.T, relay, method, body string) (int, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, relay+"/api/v1/machines", rd)
	req.Header.Set("Authorization", "Bearer pext_x")
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode, res.Header
}

// While the manager is off the bus the relay answers 503 itself and sends nothing to the hub.
func TestNotReadyIsAnsweredHere(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer hub.Close()
	var ready sync.Mutex
	up := false
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", ProofKey: "k", Knows: func(tok string) bool { return tok == "pext_x" },
		Ready: func() bool { ready.Lock(); defer ready.Unlock(); return up }})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	if code, hd := relayDo(t, relay.URL, "GET", ""); code != http.StatusServiceUnavailable || hd.Get("Retry-After") == "" {
		t.Fatalf("off the bus: %d %v", code, hd)
	}
	mu.Lock()
	if hits != 0 {
		t.Fatalf("the hub got %d requests while the manager was off the bus", hits)
	}
	mu.Unlock()
	ready.Lock()
	up = true
	ready.Unlock()
	if code, _ := relayDo(t, relay.URL, "GET", ""); code != 200 {
		t.Fatalf("back on the bus: %d", code)
	}
}

// A request the hub took as sent before (403 proof_replayed) is sent once more with a new proof when it is a bodiless
// GET or HEAD; a request with a body is not.
func TestReplayedIsSentAgainWithANewProof(t *testing.T) {
	var mu sync.Mutex
	var proofs []string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		proofs = append(proofs, r.Header.Get(extwire.HeaderManagerProof))
		first := len(proofs)%2 == 1
		mu.Unlock()
		if first {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"` + extwire.CodeProofReplayed + `","error":"this request was sent before"}`))
		}
	}))
	defer hub.Close()
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", ProofKey: "k", Knows: func(tok string) bool { return tok == "pext_x" }})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	if code, _ := relayDo(t, relay.URL, "GET", ""); code != 200 {
		t.Fatalf("a GET refused as replayed, sent again: %d", code)
	}
	mu.Lock()
	if len(proofs) != 2 || proofs[0] == "" || proofs[0] == proofs[1] {
		t.Fatalf("proofs: %q", proofs)
	}
	proofs = nil
	mu.Unlock()
	if code, _ := relayDo(t, relay.URL, "POST", `{"a":1}`); code != http.StatusForbidden {
		t.Fatalf("a POST with a body is not sent again: %d", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(proofs) != 1 {
		t.Fatalf("a POST was sent %d times", len(proofs))
	}
}

// When the hub's auth layer refuses a token the relay knows (401, or 503 manager_offline, saying so in
// extwire.HeaderAuthRefused), the relay holds its extensions' requests, answering 503 itself, until the manager's bus
// connection is another one, or suspendMost passed; any other answer holds nothing, a 401 from further on included.
func TestARefusalHoldsUntilTheNextConnection(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	answer := func(w http.ResponseWriter) {} // what the hub says now
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		hits++
		a := answer
		mu.Unlock()
		a(w)
	}))
	defer hub.Close()
	say := func(status int, code string) {
		mu.Lock()
		defer mu.Unlock()
		answer = func(w http.ResponseWriter) {
			if status != 200 {
				switch { // the hub's auth layer says it is its own refusal
				case status == 401 && code == "invalid_token":
					w.Header().Set(extwire.HeaderAuthRefused, extwire.RefusedToken)
				case status == 503 && code == extwire.CodeManagerOffline:
					w.Header().Set(extwire.HeaderAuthRefused, extwire.RefusedManagerOffline)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"code":"` + code + `","error":"x"}`))
			}
		}
	}
	hitsNow := func() int { mu.Lock(); defer mu.Unlock(); return hits }
	var state sync.Mutex
	conn, now, known := 1, time.Now(), true
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", ProofKey: "k",
		Knows:      func(tok string) bool { state.Lock(); defer state.Unlock(); return tok == "pext_x" && known },
		Connection: func() any { state.Lock(); defer state.Unlock(); return conn },
		Now:        func() time.Time { state.Lock(); defer state.Unlock(); return now }})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	set := func(f func()) { state.Lock(); f(); state.Unlock() }
	expect := func(what string, want, wantHits int) {
		t.Helper()
		if code, _ := relayDo(t, relay.URL, "GET", ""); code != want || hitsNow() != wantHits {
			t.Fatalf("%s: %d with %d requests at the hub, want %d with %d", what, code, hitsNow(), want, wantHits)
		}
	}

	for _, c := range []struct {
		status int
		code   string
	}{{401, "invalid_token"}, {503, extwire.CodeManagerOffline}} {
		say(c.status, c.code)
		base := hitsNow()
		expect("the hub refuses", c.status, base+1)
		say(200, "")
		expect("held on that connection", http.StatusServiceUnavailable, base+1)
		expect("still held", http.StatusServiceUnavailable, base+1)
		set(func() { conn++ })
		expect("another connection", 200, base+2)
	}

	// Past suspendMost, it tries again.
	say(401, "invalid_token")
	base := hitsNow()
	expect("refused once more", 401, base+1)
	say(200, "")
	expect("held", http.StatusServiceUnavailable, base+1)
	set(func() { now = now.Add(suspendMost) })
	expect("tried again", 200, base+2)

	// Another 503, a 403, a 401 of a token that went meanwhile (its extension stopped), or a 401 without the hub's
	// word hold nothing.
	for _, c := range []struct {
		status int
		code   string
		goes   bool // the token goes while the hub answers
	}{{503, "store_unavailable", false}, {403, "permission_denied", false}, {401, "invalid_token", true},
		{401, "unauthorized", false}, // a route's own 401, passed on: no HeaderAuthRefused
	} {
		say(c.status, c.code)
		if c.goes {
			mu.Lock()
			refuse := answer
			answer = func(w http.ResponseWriter) { set(func() { known = false }); refuse(w) }
			mu.Unlock()
		}
		base := hitsNow()
		expect(fmt.Sprintf("%d %s", c.status, c.code), c.status, base+1)
		set(func() { known = true })
		say(200, "")
		expect(fmt.Sprintf("after %d %s", c.status, c.code), 200, base+2)
	}
}

// A refusal that comes back after the manager reconnected is about the connection the request went on, not the new
// one: it holds nothing.
func TestARefusalFromBeforeAReconnectHoldsNothing(t *testing.T) {
	var state sync.Mutex
	conn := 1
	var mu sync.Mutex
	hits, reconnect := 0, true
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		again := reconnect
		reconnect = false
		mu.Unlock()
		if again { // the manager reconnects while the hub answers the old connection's request
			state.Lock()
			conn++
			state.Unlock()
			w.Header().Set(extwire.HeaderAuthRefused, extwire.RefusedToken)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer hub.Close()
	h, err := Handler(Options{Hub: hub.URL, Manager: "work", ProofKey: "k", Knows: func(tok string) bool { return tok == "pext_x" },
		Connection: func() any { state.Lock(); defer state.Unlock(); return conn }})
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(h)
	defer relay.Close()
	if code, _ := relayDo(t, relay.URL, "GET", ""); code != http.StatusUnauthorized {
		t.Fatalf("the old connection's refusal: %d", code)
	}
	if code, _ := relayDo(t, relay.URL, "GET", ""); code != 200 {
		t.Fatalf("on the new connection: %d (held)", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 2 {
		t.Fatalf("%d requests at the hub", hits)
	}
}
