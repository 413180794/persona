package hub

// A relayed manager's extensions reach the hub through the manager (extension/hubrelay), which proves each request
// came through it (extwire.HeaderManagerProof): the hub takes an extension's token from the internet only with that
// proof, signed with the key of the manager's live bus connection, fresh, once. Revoking the manager ends its
// extensions' tokens at once; neither the hub nor the relay logs a token or a proof.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extension/hubrelay"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// proofTestHub: A (the assistant) owns ma and knows the extension token aTok, B ("m-0b0b0b") owns mb and knows bTok;
// the hub's API as from the internet (over TLS) is front, which keeps the proofs it saw.
type proofTestHub struct {
	h          *Hub
	routes     http.Handler
	a, b       *guardManager
	aTok, bTok string
	front      *httptest.Server
	busURL     string // the hub's bus, as a manager on this machine dials it
	// bRoute is B's answer to a route the hub passes on to it (http.request); nil: 200 {}.
	bRoute       atomic.Pointer[assistantwire.HTTPResponse]
	mu           sync.Mutex
	proofs       []string
	logs, relayL syncBuffer
}

// proofAnswers answers the hub as a manager that knows the extension token tok (opening every route: the scope is not
// what is tested here).
func proofAnswers(tok string) func(string, json.RawMessage) (any, error) {
	return func(method string, params json.RawMessage) (any, error) {
		var a extwire.Authorize
		if method != extwire.MethodAuthorize {
			return map[string]any{}, nil
		}
		if json.Unmarshal(params, &a) == nil && a.Hash == hashToken(tok) {
			return extwire.Authorized{OK: true, Extension: "x", Name: "X"}, nil
		}
		return extwire.Authorized{}, nil
	}
}

func newProofTestHub(t *testing.T) *proofTestHub {
	h, srv, _ := newBusHub(t)
	f := &proofTestHub{h: h, aTok: extTokenPrefix + "of-a-" + randomToken(), bTok: extTokenPrefix + "of-b-" + randomToken()}
	h.logger.SetOutput(&f.logs)
	h.cfg.Machines = []MachineConfig{{ID: "ma", Name: "ma", Kind: "local"}, {ID: "mb", Name: "mb", Kind: "local"}}
	h.cfg.Security.AuthFailures = 1000 // the tests refuse many from one address (TestBadProofsCountAgainstTheSource)
	f.busURL = srv.URL
	f.a = connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, proofAnswers(f.aTok))
	bAnswers := proofAnswers(f.bTok)
	f.b = connectGuardManager(t, h, srv.URL, "m-0b0b0b", func(method string, params json.RawMessage) (any, error) {
		if method == assistantwire.MethodHTTP {
			if res := f.bRoute.Load(); res != nil {
				return res, nil
			}
			return assistantwire.HTTPResponse{Status: 200, Body: []byte(`{}`)}, nil
		}
		return bAnswers(method, params)
	})
	if _, err := h.moveMachine(context.Background(), "mb", "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	f.a.report(t, devicewire.State{Machine: proto.Machine{ID: "ma", Name: "ma"}})
	f.b.report(t, devicewire.State{Machine: proto.Machine{ID: "mb", Name: "mb"}})
	eventually(t, 5*time.Second, "ma, mb", func() bool { return h.localMachine("ma") != nil && h.localMachine("mb") != nil })
	f.routes = h.routes()
	f.front = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr, r.TLS = "198.51.100.7:40000", &tls.ConnectionState{} // from the internet, over TLS
		if v := r.Header.Get(extwire.HeaderManagerProof); v != "" {
			f.mu.Lock()
			f.proofs = append(f.proofs, v)
			f.mu.Unlock()
		}
		f.routes.ServeHTTP(w, r)
	}))
	t.Cleanup(f.front.Close)
	return f
}

// relay is B's relay to front, with B's proof (or the one of key, when given).
func (f *proofTestHub) relay(t *testing.T, key string) string {
	if key == "" {
		key = extwire.ProofKey(f.b.token)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	u, err := hubrelay.Start(ctx, hubrelay.Options{Hub: f.front.URL, Manager: "m-0b0b0b", ProofKey: key,
		Knows: func(tok string) bool { return tok == f.bTok }, Logf: log.New(&f.relayL, "", 0).Printf})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// do is a request to base+path with tok and the given proof ("": none).
func proofDo(t *testing.T, base, path, tok, proof string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if proof != "" {
		req.Header.Set(extwire.HeaderManagerProof, proof)
	}
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out json.RawMessage
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, string(out)
}

func (f *proofTestHub) lastProof() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proofs[len(f.proofs)-1]
}

func TestAnExtensionsTokenFromTheInternetNeedsItsManagersProof(t *testing.T) {
	f := newProofTestHub(t)
	relay := f.relay(t, "")
	key := extwire.ProofKey(f.b.token)
	const path = "/api/v1/machines"
	sign := func(key, manager, uri, tok string, at time.Time) string {
		return extwire.SignProof(key, manager, "GET", uri, tok, at)
	}

	// Through its manager's relay: taken, as its manager, its manager's computers only.
	code, body := proofDo(t, relay, path, f.bTok, "")
	if code != 200 || !strings.Contains(body, `"mb"`) || strings.Contains(body, `"ma"`) {
		t.Fatalf("through the relay: %d %s", code, body)
	}
	seen := f.lastProof() // of GET /api/v1/machines
	if code, body := proofDo(t, relay, "/api/v1/machines/ma/metrics", f.bTok, ""); code != 403 || !strings.Contains(body, codeOtherManager) {
		t.Fatalf("another manager's computer through the relay: %d %s", code, body)
	}

	refusedAs := func(what, tok, proof, want string) {
		t.Helper()
		if code, body := proofDo(t, f.front.URL, path, tok, proof); code != 403 || !strings.Contains(body, `"`+want+`"`) {
			t.Fatalf("%s: %d %s, want 403 %s", what, code, body, want)
		}
	}
	refused := func(what, tok, proof string) { t.Helper(); refusedAs(what, tok, proof, "extension_token_local_only") }
	refused("the token alone", f.bTok, "")
	refusedAs("the relay's proof again", f.bTok, seen, codeProofReplayed)
	refused("a proof of another key", f.bTok, sign(extwire.ProofKey("not-its-bus-token"), "m-0b0b0b", path, f.bTok, time.Now()))
	// The hash the hub keeps of B's token (its database, a backup) forges nothing.
	if extwire.ProofKey(f.b.token) == hashToken(f.b.token) {
		t.Fatal("the proof key is the stored hash of the token")
	}
	refused("a proof keyed by the stored hash", f.bTok, sign(hashToken(f.b.token), "m-0b0b0b", path, f.bTok, time.Now()))
	refusedAs("a stale proof", f.bTok, sign(key, "m-0b0b0b", path, f.bTok, time.Now().Add(-2*extwire.ProofWindow)), codeClockSkew)
	refusedAs("a proof from the future", f.bTok, sign(key, "m-0b0b0b", path, f.bTok, time.Now().Add(2*extwire.ProofWindow)), codeClockSkew)
	refused("a forged stale proof", f.bTok, sign(extwire.ProofKey("not-its-bus-token"), "m-0b0b0b", path, f.bTok, time.Now().Add(-2*extwire.ProofWindow)))
	refused("a proof of another request", f.bTok, sign(key, "m-0b0b0b", "/api/v1/interactions", f.bTok, time.Now()))
	refused("a proof of another token", f.bTok, sign(key, "m-0b0b0b", path, f.aTok, time.Now()))
	refused("a proof naming another manager", f.bTok, sign(key, assistantwire.AssistantID, path, f.bTok, time.Now()))
	refused("another manager's proof", f.bTok, sign(extwire.ProofKey(f.a.token), assistantwire.AssistantID, path, f.bTok, time.Now()))
	refused("B's proof for A's token", f.aTok, sign(key, "m-0b0b0b", path, f.aTok, time.Now()))
	refused("garbage", f.bTok, "v1,m=m-0b0b0b,t=1,n=x,s=y")
	// The built-in manager's extensions send no proof: refused from the internet as ever.
	refused("the built-in's token alone", f.aTok, "")

	fresh := sign(key, "m-0b0b0b", path, f.bTok, time.Now())
	if code, body := proofDo(t, f.front.URL, path, f.bTok, fresh); code != 200 {
		t.Fatalf("a fresh proof of its manager's: %d %s", code, body)
	}
	refusedAs("that proof again", f.bTok, fresh, codeProofReplayed)

	// From this machine, as before: no proof needed (and the built-in's token too).
	for _, tok := range []string{f.bTok, f.aTok} {
		if w := securityRequest(f.routes, "GET", path, tok, "", "127.0.0.1:1", false); w.Code != 200 {
			t.Fatalf("from this machine: %d %s", w.Code, w.Body.String())
		}
	}

	// The proof goes no further than the hub's auth, from this machine or from the internet.
	var passed []http.Header
	next := f.h.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { passed = append(passed, r.Header.Clone()) }))
	for _, remote := range []string{"127.0.0.1:1", "198.51.100.8:1"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr, r.TLS = remote, &tls.ConnectionState{}
		r.Header.Set("Authorization", "Bearer "+f.bTok)
		r.Header.Set(extwire.HeaderManagerProof, sign(key, "m-0b0b0b", path, f.bTok, time.Now()))
		next.ServeHTTP(httptest.NewRecorder(), r)
	}
	if len(passed) != 2 || passed[0].Get(extwire.HeaderManagerProof) != "" || passed[1].Get(extwire.HeaderManagerProof) != "" {
		t.Fatalf("the proof went on: %v", passed)
	}

	// A relay with the wrong key proves nothing.
	if code, body := proofDo(t, f.relay(t, extwire.ProofKey("not-its-bus-token")), path, f.bTok, ""); code != 403 {
		t.Fatalf("a relay with another key: %d %s", code, body)
	}

	// Neither the hub nor the relay logged a token, a proof or the key.
	f.mu.Lock()
	proofs := append([]string{}, f.proofs...)
	f.mu.Unlock()
	for _, secret := range append([]string{f.bTok, f.aTok, f.b.token, f.a.token, key, fresh}, proofs...) {
		for _, l := range []string{f.logs.String(), f.relayL.String()} {
			if strings.Contains(l, secret) || strings.Contains(l, strings.TrimPrefix(secret, extTokenPrefix)) {
				t.Fatalf("a log has a secret (%.12s…): %s", secret, l)
			}
		}
	}
}

// Revoking the manager ends its extensions' tokens at once: through its relay, with a proof of its old key, and
// straight from this machine.
func TestRevokingAManagerEndsItsExtensionsTokens(t *testing.T) {
	f := newProofTestHub(t)
	relay := f.relay(t, "")
	const path = "/api/v1/machines"
	if code, body := proofDo(t, relay, path, f.bTok, ""); code != 200 {
		t.Fatalf("before: %d %s", code, body)
	}
	oldKey := extwire.ProofKey(f.b.token)
	if w := securityRequest(f.routes, "DELETE", "/api/v1/managers/m-0b0b0b", f.h.cfg.Token, "", "127.0.0.1:1", false); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if code, body := proofDo(t, relay, path, f.bTok, ""); code != 401 {
		t.Fatalf("through the relay after the revoke: %d %s", code, body)
	}
	if code, body := proofDo(t, f.front.URL, path, f.bTok, extwire.SignProof(oldKey, "m-0b0b0b", "GET", path, f.bTok, time.Now())); code != 401 {
		t.Fatalf("with the revoked manager's proof: %d %s", code, body)
	}
	if w := securityRequest(f.routes, "GET", path, f.bTok, "", "127.0.0.1:1", false); w.Code != 401 {
		t.Fatalf("from this machine after the revoke: %d %s", w.Code, w.Body.String())
	}
	if f.h.busProofKey("m-0b0b0b") != "" {
		t.Fatal("the revoked manager's key is still there")
	}
}

// A proof the manager signed but that is out of time, sent twice, or one too many just now is refused without
// counting against the source (it is the manager's, which did nothing wrong); the manager's own proofs being too many
// refuses no other manager's.
func TestTheManagersOwnProofsDoNotCountAgainstTheSource(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 2
	const path = "/api/v1/machines"
	key := extwire.ProofKey(f.b.token)
	sign := func(at time.Time) string { return extwire.SignProof(key, "m-0b0b0b", "GET", path, f.bTok, at) }
	once := sign(time.Now())
	if code, body := proofDo(t, f.front.URL, path, f.bTok, once); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for i := 0; i < 3; i++ {
		if code, body := proofDo(t, f.front.URL, path, f.bTok, sign(time.Now().Add(-5*time.Minute))); code != 403 || !strings.Contains(body, codeClockSkew) {
			t.Fatalf("skewed %d: %d %s", i, code, body)
		}
		if code, body := proofDo(t, f.front.URL, path, f.bTok, once); code != 403 || !strings.Contains(body, codeProofReplayed) {
			t.Fatalf("again %d: %d %s", i, code, body)
		}
	}
	// B's nonces full: B's proofs wait (429, Retry-After), A's are taken.
	now := time.Now()
	for i := f.h.extProofs.count("m-0b0b0b"); i < proofNoncesMost; i++ {
		if v := f.h.extProofs.first("m-0b0b0b", fmt.Sprintf("filler-%08d", i), now, now); v != proofOK {
			t.Fatalf("filler %d: %v", i, v)
		}
	}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.front.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+f.bTok)
		req.Header.Set(extwire.HeaderManagerProof, sign(time.Now()))
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
			t.Fatalf("B's nonces full: %d %v", res.StatusCode, res.Header)
		}
	}
	aProof := extwire.SignProof(extwire.ProofKey(f.a.token), assistantwire.AssistantID, "GET", path, f.aTok, time.Now())
	if code, body := proofDo(t, f.front.URL, path, f.aTok, aProof); code != 200 {
		t.Fatalf("A's proof while B's nonces are full: %d %s", code, body)
	}
	// None of it counted: the source is not locked out.
	if retry := f.h.devtok.failures.retry("198.51.100.7", time.Now(), f.h.cfg.Security); retry > 0 {
		t.Fatalf("the source was counted against: retry in %s", retry)
	}
}

// One manager's nonces at the cap refuse only that manager's proofs, until they age out.
func TestProofNonceCapIsEachManagers(t *testing.T) {
	var n proofNonces
	now := time.Now()
	for i := 0; i < proofNoncesMost; i++ {
		if v := n.first("m-0b0b0b", fmt.Sprintf("nonce-b-%08d", i), now, now); v != proofOK {
			t.Fatalf("B's proof %d refused before the cap: %v", i, v)
		}
	}
	if v := n.first("m-0b0b0b", "nonce-b-over", now, now); v != proofBusy {
		t.Fatalf("B past the cap: %v", v)
	}
	if v := n.first("m-0b0b0b", "nonce-b-00000001", now, now); v != proofReplayed {
		t.Fatalf("B's nonce again: %v", v)
	}
	if v := n.first("m-0c0c0c", "nonce-c-00000001", now, now); v != proofOK {
		t.Fatalf("C while B is full: %v", v)
	}
	later := now.Add(extwire.ProofWindow + 2*time.Second)
	if v := n.first("m-0b0b0b", "nonce-b-later", later, later); v != proofOK {
		t.Fatalf("B once its nonces aged out: %v", v)
	}
	if got := n.count("m-0b0b0b"); got != 1 {
		t.Fatalf("B's nonces after aging out: %d", got)
	}
}

// A token without a good proof from the internet counts against the source, as a bad token does.
func TestBadProofsCountAgainstTheSource(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 3
	for i := 0; i < 3; i++ {
		if code, _ := proofDo(t, f.front.URL, "/api/v1/machines", f.bTok, ""); code != 403 {
			t.Fatalf("refused %d: %d", i, code)
		}
	}
	good := extwire.SignProof(extwire.ProofKey(f.b.token), "m-0b0b0b", "GET", "/api/v1/machines", f.bTok, time.Now())
	if code, body := proofDo(t, f.front.URL, "/api/v1/machines", f.bTok, good); code != http.StatusTooManyRequests {
		t.Fatalf("after the source's failures: %d %s", code, body)
	}
}

// count is how many of manager's nonces are kept (tests).
func (n *proofNonces) count(manager string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	if m := n.byMgr[manager]; m != nil {
		return len(m.seen)
	}
	return 0
}

// A remote manager whose bus is down a moment (a hub restart, a network blip) while its extensions keep calling the
// hub: the calls the hub can't place count in the extensions' own bucket, so the manager's bus dial from the same
// address afterwards goes through, at the default limits (the reviewer's repro of #6178).
func TestRelayedExtensionsDoNotLockOutTheirManagersBus(t *testing.T) {
	h, srv, _ := newBusHub(t) // default security: 10 failures / 60 s, 900 s block
	bTok := extTokenPrefix + "of-b-" + randomToken()
	connectGuardManager(t, h, srv.URL, "m-0b0b0b", func(method string, params json.RawMessage) (any, error) {
		var a extwire.Authorize
		if method == extwire.MethodAuthorize && json.Unmarshal(params, &a) == nil && a.Hash == hashToken(bTok) {
			return extwire.Authorized{OK: true, Extension: "x", Name: "X"}, nil
		}
		return map[string]any{}, nil
	})
	routes := h.routes()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr, r.TLS = "198.51.100.7:40000", &tls.ConnectionState{} // B's computer, across the internet
		routes.ServeHTTP(w, r)
	}))
	defer front.Close()
	h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the bus blinks")
	eventually(t, 5*time.Second, "B off the bus", func() bool { return h.busConnOf("m-0b0b0b") == nil })
	var codes []int
	for i := 0; i < 12; i++ {
		code, _ := proofDo(t, front.URL, "/api/v1/inbox", bTok, "")
		codes = append(codes, code)
	}
	if codes[0] != 401 || codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Fatalf("the extensions' calls while B is away: %v (want 401s, then 429 for extensions' tokens)", codes)
	}
	// B comes back from the same address: its bus dial is not refused.
	tok := busToken(t, h, "m-0b0b0b", "m-0b0b0b")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := bus.Dial(ctx, front.URL, tok, nil)
	if err != nil {
		t.Fatalf("B's bus dial after its extensions' calls: %v", err)
	}
	_ = ws.CloseNow()
	// Nor is another credential from there: a bad device token is an ordinary 401, not 429.
	if code, body := proofDo(t, front.URL, "/api/v1/machines", "not-a-token", ""); code != 401 {
		t.Fatalf("another credential from that address: %d %s", code, body)
	}
}

// The other way round, the ordinary bucket still holds an address that failed with other credentials, extensions'
// tokens included: the extensions' bucket only takes their own failures out of it.
func TestAnAddressLockedOutLocksOutItsExtensionsToo(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 2
	for i := 0; i < 2; i++ {
		if code, _ := proofDo(t, f.front.URL, "/api/v1/machines", "not-a-token", ""); code != 401 {
			t.Fatalf("bad token %d: %d", i, code)
		}
	}
	good := extwire.SignProof(extwire.ProofKey(f.b.token), "m-0b0b0b", "GET", "/api/v1/machines", f.bTok, time.Now())
	if code, body := proofDo(t, f.front.URL, "/api/v1/machines", f.bTok, good); code != http.StatusTooManyRequests {
		t.Fatalf("an extension's token from a locked-out address: %d %s", code, body)
	}
}

// A proof that names a manager of the roster with no live bus connection (the hub restarted; the manager has not
// noticed yet) gets 503 manager_offline, not counted: nobody can tell whose the token is. A proof naming no manager of
// the roster, and a proof that fails against a connected manager's key, are counted as before.
func TestAProofOfAManagerAwayIsNotCounted(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 3
	const path = "/api/v1/machines"
	key := extwire.ProofKey(f.b.token)
	f.h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the hub lost it")
	eventually(t, 5*time.Second, "B off the bus", func() bool { return f.h.busConnOf("m-0b0b0b") == nil })
	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.front.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+f.bTok)
		req.Header.Set(extwire.HeaderManagerProof, extwire.SignProof(key, "m-0b0b0b", "GET", path, f.bTok, time.Now()))
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable || body.Code != codeManagerOffline || res.Header.Get("Retry-After") == "" {
			t.Fatalf("B away, call %d: %d %q %v", i, res.StatusCode, body.Code, res.Header)
		}
	}
	if retry := f.h.devtok.extFailures.retry("198.51.100.7", time.Now(), f.h.cfg.Security); retry > 0 {
		t.Fatalf("B's address was counted against while B was away (retry in %s)", retry)
	}
	// Counted as ever: a proof naming no manager of the roster, or one failing against a connected manager's key.
	for _, c := range []struct{ what, tok, proof string }{
		{"no such manager", f.bTok, extwire.SignProof(key, "m-0c0c0c", "GET", path, f.bTok, time.Now())},
		{"not the connected manager's key", f.aTok, extwire.SignProof(key, assistantwire.AssistantID, "GET", path, f.aTok, time.Now())},
		{"malformed", f.bTok, "v1,m=m-0b0b0b,t=1,n=x,s=y"},
	} {
		if code, body := proofDo(t, f.front.URL, path, c.tok, c.proof); code == http.StatusServiceUnavailable || code == 200 {
			t.Fatalf("%s: %d %s", c.what, code, body)
		}
	}
	if code, body := proofDo(t, f.front.URL, path, f.bTok, ""); code != http.StatusTooManyRequests {
		t.Fatalf("after three counted failures: %d %s", code, body)
	}
}

// The reviewer's scenario: the hub loses a remote manager's bus without the manager noticing (a crash, a half-open
// connection) while its extensions keep calling through its relay. Nothing gets 429, the address is not counted
// against, and once the manager is on the bus again its extensions' calls go through.
func TestExtensionsOfAManagerTheHubLostDoNotLockItOut(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 0 // the default: 10 in a minute
	var state sync.Mutex
	conn := 1 // the manager's bus connection as it sees it: still the first one
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	relay, err := hubrelay.Start(ctx, hubrelay.Options{Hub: f.front.URL, Manager: "m-0b0b0b", ProofKey: extwire.ProofKey(f.b.token),
		Knows:      func(tok string) bool { return tok == f.bTok },
		Ready:      func() bool { return true }, // the manager has not noticed
		Connection: func() any { state.Lock(); defer state.Unlock(); return conn },
		Logf:       log.New(&f.relayL, "", 0).Printf})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := proofDo(t, relay, "/api/v1/machines", f.bTok, ""); code != 200 {
		t.Fatalf("before: %d %s", code, body)
	}
	f.mu.Lock()
	before := len(f.proofs)
	f.mu.Unlock()
	f.h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the hub lost it")
	eventually(t, 5*time.Second, "B off the hub's bus", func() bool { return f.h.busConnOf("m-0b0b0b") == nil })
	var codes []int
	for i := 0; i < 15; i++ {
		code, _ := proofDo(t, relay, "/api/v1/machines", f.bTok, "")
		codes = append(codes, code)
		if code == http.StatusTooManyRequests {
			t.Fatalf("429 while the hub had lost the manager: %v", codes)
		}
	}
	f.mu.Lock()
	reached := len(f.proofs) - before
	f.mu.Unlock()
	if codes[0] != http.StatusServiceUnavailable || reached != 1 {
		t.Fatalf("while the hub had lost B: %v, %d reached the hub (want 503s, and one)", codes, reached)
	}
	for _, l := range []*authLimiter{&f.h.devtok.failures, &f.h.devtok.extFailures} {
		if retry := l.retry("198.51.100.7", time.Now(), f.h.cfg.Security); retry > 0 {
			t.Fatalf("B's address was counted against (retry in %s)", retry)
		}
	}
	// B notices, connects again with its token: its extensions go through again.
	f.b = connectGuardManagerWith(t, f.busURL, "m-0b0b0b", f.b.token, proofAnswers(f.bTok))
	state.Lock()
	conn++
	state.Unlock()
	if code, body := proofDo(t, relay, "/api/v1/machines", f.bTok, ""); code != 200 {
		t.Fatalf("B back on the bus: %d %s", code, body)
	}
}

// A 401 that comes from further on (a manager's route whose own upstream refused it), passed on by the hub, is not the
// hub refusing the token: the hub takes extwire.HeaderAuthRefused off it, even should the manager set it, and the
// relay holds nothing. The hub's own refusal says so.
func TestARoutesOwn401HoldsNothing(t *testing.T) {
	f := newProofTestHub(t)
	var state sync.Mutex
	conn := 1
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	relay, err := hubrelay.Start(ctx, hubrelay.Options{Hub: f.front.URL, Manager: "m-0b0b0b", ProofKey: extwire.ProofKey(f.b.token),
		Knows:      func(tok string) bool { return tok == f.bTok },
		Connection: func() any { state.Lock(); defer state.Unlock(); return conn }})
	if err != nil {
		t.Fatal(err)
	}
	f.bRoute.Store(&assistantwire.HTTPResponse{Status: http.StatusUnauthorized,
		Header: map[string][]string{"Content-Type": {"application/json"}, extwire.HeaderAuthRefused: {extwire.RefusedToken}},
		// big enough for the hub to gzip it (the relay's client asks for gzip): the head leaves late, through gzip
		Body: []byte(`{"code":"invalid_token","error":"the upstream refused its token","detail":"` + strings.Repeat("x", 2*gzipMinSize) + `"}`)})
	get := func() (int, http.Header) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, relay+"/api/v1/assistant", nil)
		req.Header.Set("Authorization", "Bearer "+f.bTok)
		res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode, res.Header
	}
	f.mu.Lock()
	before := len(f.proofs)
	f.mu.Unlock()
	for i := 0; i < 3; i++ {
		code, hd := get()
		if code != http.StatusUnauthorized || hd.Get(extwire.HeaderAuthRefused) != "" {
			t.Fatalf("B's route's own 401, call %d: %d %v", i, code, hd)
		}
	}
	f.mu.Lock()
	reached := len(f.proofs) - before
	f.mu.Unlock()
	if reached != 3 {
		t.Fatalf("a route's 401 held the relay: %d of 3 reached the hub", reached)
	}
	// The hub's own refusal of a token does say so.
	w := securityRequest(f.routes, "GET", "/api/v1/assistant", extTokenPrefix+"nobodys", "", "127.0.0.1:1", false)
	if w.Code != 401 || w.Header().Get(extwire.HeaderAuthRefused) != extwire.RefusedToken {
		t.Fatalf("the hub's own refusal: %d %v", w.Code, w.Header())
	}
}

// Whether a manager off the bus is in the roster is asked of the database once in a while, not on each uncounted
// request naming it: the answer is kept for rosterSeenFor.
func TestTheRosterIsAskedOnceInAWhile(t *testing.T) {
	f := newProofTestHub(t)
	const path = "/api/v1/machines"
	key := extwire.ProofKey(f.b.token)
	f.h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the hub lost it")
	eventually(t, 5*time.Second, "B off the bus", func() bool { return f.h.busConnOf("m-0b0b0b") == nil })
	call := func() int {
		t.Helper()
		code, _ := proofDo(t, f.front.URL, path, f.bTok, extwire.SignProof(key, "m-0b0b0b", "GET", path, f.bTok, time.Now()))
		return code
	}
	if code := call(); code != http.StatusServiceUnavailable {
		t.Fatalf("B away: %d", code)
	}
	// B leaves the roster: the kept answer still holds, for a few seconds.
	if _, err := f.h.managerStore().DeleteBusIdentity(context.Background(), "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	if code := call(); code != http.StatusServiceUnavailable {
		t.Fatalf("within rosterSeenFor, the kept answer: %d", code)
	}
	// Once it is stale the roster is asked again: B is not in it, so the token is nobody's.
	f.h.extRoster.mu.Lock()
	for id, a := range f.h.extRoster.seen {
		a.until = time.Now().Add(-time.Second)
		f.h.extRoster.seen[id] = a
	}
	f.h.extRoster.mu.Unlock()
	if code := call(); code != http.StatusUnauthorized {
		t.Fatalf("after rosterSeenFor, asked again: %d", code)
	}
}

func TestRosterSeen(t *testing.T) {
	var c rosterSeen
	now := time.Now()
	if _, ok := c.get("m-1", now); ok {
		t.Fatal("an answer never kept")
	}
	c.put("m-1", true, now)
	c.put("m-2", false, now)
	if has, ok := c.get("m-1", now.Add(rosterSeenFor-time.Millisecond)); !ok || !has {
		t.Fatal("m-1 kept as in the roster")
	}
	if has, ok := c.get("m-2", now); !ok || has {
		t.Fatal("m-2 kept as not in the roster")
	}
	if _, ok := c.get("m-1", now.Add(rosterSeenFor)); ok {
		t.Fatal("kept past rosterSeenFor")
	}
	for i := 0; i < rosterSeenMost+10; i++ {
		c.put(fmt.Sprintf("m-x%d", i), true, now)
	}
	if len(c.seen) > rosterSeenMost {
		t.Fatalf("%d kept, most %d", len(c.seen), rosterSeenMost)
	}
}

// A proof naming a manager of the roster off the bus is answered 503 without counting only within lostWindow of the
// hub's start or of that manager's connection ending here: outside it, the answer would tell anyone, for free, which
// managers are in the roster and off the bus, so it is counted as ever.
func TestAProofOfAManagerAwayOnlyRightAfterItWasLost(t *testing.T) {
	f := newProofTestHub(t)
	const path = "/api/v1/machines"
	key := extwire.ProofKey(f.b.token)
	call := func(manager, tok string) int {
		t.Helper()
		code, _ := proofDo(t, f.front.URL, path, tok, extwire.SignProof(key, manager, "GET", path, tok, time.Now()))
		return code
	}
	f.h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the hub lost it")
	eventually(t, 5*time.Second, "B off the bus", func() bool { return f.h.busConnOf("m-0b0b0b") == nil })
	if code := call("m-0b0b0b", f.bTok); code != http.StatusServiceUnavailable {
		t.Fatalf("right after B's connection ended: %d", code)
	}
	f.h.extLost.mu.Lock()
	f.h.extLost.ended["m-0b0b0b"] = time.Now().Add(-lostWindow - time.Second)
	f.h.extLost.mu.Unlock()
	if code := call("m-0b0b0b", f.bTok); code != http.StatusUnauthorized {
		t.Fatalf("past lostWindow: %d, want a counted 401", code)
	}
	// A manager of the roster that never connected to this hub: counted, unless the hub itself just started.
	_ = busToken(t, f.h, "m-0c0c0c", "m-0c0c0c")
	cTok := extTokenPrefix + "of-c"
	if code := call("m-0c0c0c", cTok); code != http.StatusUnauthorized {
		t.Fatalf("a manager this hub never lost: %d", code)
	}
	f.h.extLost.mu.Lock()
	f.h.extLost.started = time.Now()
	f.h.extLost.mu.Unlock()
	if code := call("m-0c0c0c", cTok); code != http.StatusServiceUnavailable {
		t.Fatalf("right after the hub started: %d", code)
	}
}

// Revoking a manager drops what was kept of it in the roster at once: a proof naming it is counted right away.
func TestRevokingAManagerForgetsItsRosterAnswer(t *testing.T) {
	f := newProofTestHub(t)
	const path = "/api/v1/machines"
	key := extwire.ProofKey(f.b.token)
	call := func() int {
		t.Helper()
		code, _ := proofDo(t, f.front.URL, path, f.bTok, extwire.SignProof(key, "m-0b0b0b", "GET", path, f.bTok, time.Now()))
		return code
	}
	f.h.busHangUp("m-0b0b0b", websocket.StatusGoingAway, "test: the hub lost it")
	eventually(t, 5*time.Second, "B off the bus", func() bool { return f.h.busConnOf("m-0b0b0b") == nil })
	if code := call(); code != http.StatusServiceUnavailable {
		t.Fatalf("B away: %d", code)
	}
	if _, err := f.h.revokeBusToken(context.Background(), "m-0b0b0b"); err != nil {
		t.Fatal(err)
	}
	if code := call(); code != http.StatusUnauthorized {
		t.Fatalf("right after B was revoked: %d, want a counted 401", code)
	}
}

// A manager that does not answer whose a token is (it timed out, it is not ready yet) gives no verdict: the hub answers
// 503 without counting it or marking the token as nobody's (which would hold that manager's extensions). From the
// internet only when a manager's proof vouches for the request; otherwise counted as ever, and still not marked.
func TestAManagerThatDoesNotAnswerGivesNoVerdict(t *testing.T) {
	f := newProofTestHub(t)
	f.h.cfg.Security.AuthFailures = 3
	var silent atomic.Bool
	silent.Store(true)
	connectGuardManager(t, f.h, f.busURL, "m-0c0c0c", func(method string, params json.RawMessage) (any, error) {
		if method == extwire.MethodAuthorize && silent.Load() {
			return nil, &rpc.Error{Code: "assistant_offline", Message: "not ready yet"}
		}
		return proofAnswers("")(method, params)
	})
	const path = "/api/v1/machines"
	unknown := extTokenPrefix + "nobodys-" + randomToken()
	w := securityRequest(f.routes, "GET", path, unknown, "", "127.0.0.1:1", false)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get(extwire.HeaderAuthRefused) != "" || !strings.Contains(w.Body.String(), codeAuthorizeUnsure) {
		t.Fatalf("from this machine, C silent: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	// From the internet, vouched for by B's proof: the same, not counted.
	key := extwire.ProofKey(f.b.token)
	for i := 0; i < 4; i++ {
		code, body := proofDo(t, f.front.URL, path, unknown, extwire.SignProof(key, "m-0b0b0b", "GET", path, unknown, time.Now()))
		if code != http.StatusServiceUnavailable || !strings.Contains(body, codeAuthorizeUnsure) {
			t.Fatalf("vouched, C silent, %d: %d %s", i, code, body)
		}
	}
	// Without a proof that vouches: counted, and not marked.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.front.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+unknown)
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || res.Header.Get(extwire.HeaderAuthRefused) != "" {
		t.Fatalf("unvouched, C silent: %d %v", res.StatusCode, res.Header)
	}
	// Every manager answering "not mine": the token is nobody's, and the hub says so.
	silent.Store(false)
	w = securityRequest(f.routes, "GET", path, unknown, "", "127.0.0.1:1", false)
	if w.Code != http.StatusUnauthorized || w.Header().Get(extwire.HeaderAuthRefused) != extwire.RefusedToken {
		t.Fatalf("every manager answered: %d %v", w.Code, w.Header())
	}
}

// The hub takes extwire.HeaderAuthRefused off a response as its head leaves, with gzip holding it back too: a header
// set after the status (which gzip lets through) is taken off; the auth layer's own stays.
func TestAuthRefusedIsTakenOffAsTheHeadLeaves(t *testing.T) {
	big := strings.Repeat("x", 2*gzipMinSize)
	for _, c := range []struct {
		name   string
		marked bool
	}{{"passed on", false}, {"the auth layer's own", true}} {
		t.Run(c.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.marked {
					authRefused(w, r, extwire.RefusedToken)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Header().Set(extwire.HeaderAuthRefused, extwire.RefusedToken) // after the status: gzip still holds the head
				_, _ = w.Write([]byte(`{"code":"invalid_token","detail":"` + big + `"}`))
			})
			srv := httptest.NewServer(newTestHub(t).outerLayers(handler))
			defer srv.Close()
			req, _ := http.NewRequest("GET", srv.URL+"/api/v1/x", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			res, err := (&http.Client{Transport: &http.Transport{Proxy: nil, DisableCompression: true}}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.Header.Get("Content-Encoding") != "gzip" {
				t.Fatalf("not gzipped: %v", res.Header)
			}
			if got := res.Header.Get(extwire.HeaderAuthRefused) != ""; got != c.marked {
				t.Fatalf("header there %v, want %v: %v", got, c.marked, res.Header)
			}
		})
	}
}
