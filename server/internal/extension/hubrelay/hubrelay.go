// Package hubrelay is a manager's relay to the hub's API for its extensions (docs/managers.md "Extensions and the
// hub's API"). A manager may run anywhere and reach the hub only at its manager.json's hub, maybe across the internet
// with a key it pins, which its extensions know nothing of. So it serves them the hub's API on its own loopback and
// passes each request on with its own client: only the hub's API (/api/, no "." or ".." segment that would lead
// elsewhere), only with a live token of one of its own extensions (read as the hub reads it), as the extension sent
// it, without cookies or forwarding headers, adding only its proof that the request came through it
// (extwire.HeaderManagerProof), never a credential of its own. It logs neither the token nor the proof.
package hubrelay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/extwire"
)

// Options are the relay's.
type Options struct {
	// Hub is the hub's base URL as the manager reaches it (http(s)://host:port).
	Hub string
	// Transport reaches it (the manager's, which pins the hub's key); nil: http.DefaultTransport, as the bus dials.
	Transport http.RoundTripper
	// Manager is the manager's id, and ProofKey its proof's key (extwire.ProofKey of its bus token); "": no proof.
	Manager, ProofKey string
	// Knows says whether token is a live token of one of the manager's extensions.
	Knows func(token string) bool
	// Ready says whether the manager is on the hub's bus now (nil: always). While it is not, the relay answers 503
	// itself and sends nothing on: the hub could not ask the manager whose a token is, and would count every call
	// against the manager's address.
	Ready func() bool
	Logf  func(format string, args ...any)
	// Now is the clock (tests); nil: time.Now.
	Now func() time.Time
}

// Handler is the relay.
func Handler(o Options) (http.Handler, error) {
	target, err := url.Parse(strings.TrimRight(o.Hub, "/"))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("the hub's address %q is not http(s)://host:port", o.Hub)
	}
	if o.Knows == nil {
		return nil, errors.New("no way to tell its extensions' tokens")
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Ready == nil {
		o.Ready = func() bool { return true }
	}
	var transport http.RoundTripper = http.DefaultTransport
	if o.Transport != nil {
		transport = o.Transport
	}
	if o.ProofKey != "" {
		transport = signer{next: transport, o: o}
	}
	skew := &clockSkew{}
	proxy := &httputil.ReverseProxy{
		// Rewrite (not Director): the extension's X-Forwarded-* and Forwarded are dropped, and none are added. It runs
		// after the hop-by-hop headers are gone, and the proof is set later still, as each request goes (signer).
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Header.Del("Cookie") // the hub's admin session is no business of an extension's
			pr.Out.Header.Del(extwire.HeaderManagerProof)
		},
		Transport:     transport,
		FlushInterval: -1, // streams (a chat's feed, events) as they come
		ModifyResponse: func(res *http.Response) error {
			skew.check(res, o)
			return nil
		},
		ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() == nil {
				// The method only: a path or query may hold what the extension put there, an error may quote it.
				o.Logf("the hub's API: a %s through the relay failed: %s", r.Method,
					redact(err.Error(), tokenOf(r), o.ProofKey))
			}
			apierr.WriteCode(w, http.StatusBadGateway, "hub_unreachable", errors.New("the manager could not reach the hub"))
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.EscapedPath(); !strings.HasPrefix(p, "/api/") || dotSegment(p) {
			apierr.WriteCode(w, http.StatusNotFound, "not_found", errors.New("only the hub's API (/api/) is passed on"))
			return
		}
		if tok := tokenOf(r); tok == "" || !o.Knows(tok) {
			apierr.WriteCode(w, http.StatusUnauthorized, "unauthorized", errors.New("not a token of this manager's extensions"))
			return
		}
		if !o.Ready() {
			w.Header().Set("Retry-After", "5")
			apierr.WriteCode(w, http.StatusServiceUnavailable, "manager_offline",
				errors.New("the manager is not connected to the hub right now: try again shortly"))
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

// Start serves the relay on the loopback, on a port the system picks, until ctx ends: its base URL.
func Start(ctx context.Context, o Options) (string, error) {
	h, err := Handler(o)
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if ln, err = net.Listen("tcp", "[::1]:0"); err != nil {
			return "", err
		}
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = srv.Serve(ln) }()
	context.AfterFunc(ctx, func() { _ = srv.Close() })
	return "http://" + ln.Addr().String(), nil
}

// signer sets the manager's proof on each request as it goes. A bodiless GET or HEAD the hub refused as sent before
// (403 proof_replayed: the transport may send a request again, nonce and all, on a connection that dropped) is sent
// once more with a new proof.
type signer struct {
	next http.RoundTripper
	o    Options
}

func (s signer) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := s.next.RoundTrip(s.signed(req))
	if err != nil || res.StatusCode != http.StatusForbidden || !replayable(req) {
		return res, err
	}
	head, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if !strings.Contains(string(head), `"`+extwire.CodeProofReplayed+`"`) {
		res.Body = readCloser{io.MultiReader(bytes.NewReader(head), res.Body), res.Body}
		return res, nil
	}
	_ = res.Body.Close()
	return s.next.RoundTrip(s.signed(req))
}

// signed is req with a new proof (a copy: a RoundTripper leaves its request as it came).
func (s signer) signed(req *http.Request) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set(extwire.HeaderManagerProof, extwire.SignProof(s.o.ProofKey, s.o.Manager, out.Method, out.URL.RequestURI(),
		tokenOf(out), s.o.Now()))
	return out
}

// replayable says whether req may be sent again as it is: a GET or HEAD with no body, not an upgrade.
func replayable(req *http.Request) bool {
	return (req.Method == http.MethodGet || req.Method == http.MethodHead) && (req.Body == nil || req.Body == http.NoBody) &&
		req.Header.Get("Upgrade") == ""
}

type readCloser struct {
	io.Reader
	io.Closer
}

// clockSkew says in the log, at most every ten minutes, that the hub's clock (its answers' Date) and this computer's
// are far enough apart that the hub refuses the proofs, or soon will (403 clock_skew).
type clockSkew struct {
	mu   sync.Mutex
	said time.Time
}

func (c *clockSkew) check(res *http.Response, o Options) {
	at, err := http.ParseTime(res.Header.Get("Date"))
	if err != nil || o.ProofKey == "" {
		return
	}
	now := o.Now()
	d := now.Sub(at).Round(time.Second)
	if d < 0 {
		d = -d
	}
	if d < extwire.ProofWindow/2 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.said.IsZero() && now.Sub(c.said) < 10*time.Minute {
		return
	}
	c.said = now
	o.Logf("the hub's clock and this computer's are %s apart: past %s the hub refuses this manager's extensions' "+
		"requests from the internet (clock_skew); set the clock right", d, extwire.ProofWindow)
}

// tokenOf is the request's token as the hub reads it: the Authorization header, else ?token=.
func tokenOf(r *http.Request) string {
	if tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); tok != "" {
		return tok
	}
	return r.URL.Query().Get("token")
}

// dotSegment says whether path has a "." or ".." segment, each segment unescaped on its own as a mux reads it.
func dotSegment(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if s, err := url.PathUnescape(seg); err != nil || s == "." || s == ".." {
			return true
		}
	}
	return false
}

// redact is s without any of secrets.
func redact(s string, secrets ...string) string {
	for _, x := range secrets {
		if x != "" {
			s = strings.ReplaceAll(s, x, "<redacted>")
		}
	}
	return s
}
