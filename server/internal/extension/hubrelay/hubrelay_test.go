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
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
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
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mu.Lock(); hits++; mu.Unlock() }))
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
