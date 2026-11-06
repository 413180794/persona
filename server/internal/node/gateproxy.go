package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The command gates' credentials stay in the node (#1317). An assistant process gets, for a gate with Auth, its
// BaseEnv pointing at this proxy (http://127.0.0.1:<port>/<key>, a key of its own that goes when the process ends)
// and none of the credentials. The proxy adds them and holds every request to the gate's rules, however it was sent
// (the gated command, curl, a script): allow goes, deny is refused, and a call to confirm goes only once, after
// larkCheck let that very call through (the phone confirmed it, or "Always allow in this session" covers it). Printing the
// environment so yields an address that works on this machine only and skips nothing.
//
// The proxy also sees what the API answered. A write it forwarded that came back 2xx is told to the manager
// (proto.MethodLarkSent), which keeps it in the sent-as-user ledger with how the gate let it through (docs/connectors.md
// "Sent as the user", #5278): the gate decides before the command runs and cannot know whether it sent. What does not
// pass here is not in the ledger: lark-cli carries its own credentials, and a gate without Auth puts them in the
// command's environment.

const (
	// gateGrantFor: how long a call larkCheck let through may take to be sent.
	gateGrantFor = 10 * time.Minute
	// gateBodyMax: the most of a JSON body read to match it to the rules and to what was confirmed.
	gateBodyMax = 4 << 20
	// gateGrantsMax: grants kept at most (the oldest go first).
	gateGrantsMax = 256
	// gateTextMost: the most of a write's words (characters) told to the manager for the ledger.
	gateTextMost = 4000
	// gateReportsMax: reports to the manager in flight at once. A flood of allowed writes does not pile up goroutines
	// behind a slow manager: one over the limit waits up to gateReportWait for a place, then is logged and not told.
	gateReportsMax = 32
	gateReportWait = time.Second
	// gateReportFor: how long a report may take.
	gateReportFor = 15 * time.Second
)

type gateProxy struct {
	// cur is the Node whose config (credentials, rules) requests go by: the last to use the proxy, the one serving
	// the hub now.
	cur atomic.Pointer[Node]

	mu     sync.Mutex
	ln     net.Listener
	keys   map[string]gateKey // key → the gate's command and the conversation its process was started for
	grants []gateGrant

	// reporting holds a slot for each report to the manager in flight (gateReportsMax).
	reporting chan struct{}
}

// gateKey is what an address at the proxy stands for.
type gateKey struct {
	command string
	// session is the conversation the process was started for ("" when none is known). The assistant's process serves
	// one, so a write a rule allowed belongs to it.
	session string
}

// gateGrant is a call larkCheck let through, to go once.
type gateGrant struct {
	command, method, path string
	body                  any // the JSON body confirmed; nil: any (not JSON, cut short, an upload, $… in it)
	until                 time.Time
	// session is the conversation larkCheck decided for, and through how it let the call through: what the ledger says
	// of the write once it has gone out.
	session string
	through proto.LarkLetThrough
}

func newGateProxy() *gateProxy {
	return &gateProxy{keys: map[string]gateKey{}, reporting: make(chan struct{}, gateReportsMax)}
}

// gates is the gate proxy, made at its first use (it listens once a process needs it, open). A resident node has one
// for the process (assistantKeeper): the conversations it keeps from one connection to the next keep their
// addresses, and a call the next connection's Node lets through is the one they send. Otherwise it is the Node's.
func (n *Node) gates() *gateProxy {
	var p *gateProxy
	if k := n.keeper; k != nil {
		k.mu.Lock()
		if k.gp == nil {
			k.gp = newGateProxy()
		}
		p = k.gp
		k.mu.Unlock()
	} else {
		n.mu.Lock()
		if n.gp == nil {
			n.gp = newGateProxy()
		}
		p = n.gp
		n.mu.Unlock()
	}
	p.cur.Store(n)
	return p
}

// open gives one process of the assistant, the one started for session ("" when that is not known), its own address
// for command's gate: the base to put in its BaseEnv, and release, which takes the address back when the process ends.
func (p *gateProxy) open(command, session string) (base string, release func(), err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			if ln, err = net.Listen("tcp", "[::1]:0"); err != nil {
				return "", nil, err
			}
		}
		p.ln = ln
		srv := &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
		go func() { _ = srv.Serve(ln) }()
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	key := hex.EncodeToString(b)
	p.keys[key] = gateKey{command: command, session: session}
	return "http://" + p.ln.Addr().String() + "/" + key, func() {
		p.mu.Lock()
		delete(p.keys, key)
		p.mu.Unlock()
	}, nil
}

// close stops listening (tests; the node's proxy lives as long as the node).
func (p *gateProxy) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		_ = p.ln.Close()
		p.ln = nil
	}
}

// grant lets c, which larkCheck let through for session and the way through says, go once through command's gate.
func (p *gateProxy) grant(command, session string, c larkCall, through proto.LarkLetThrough) {
	g := gateGrant{command: command, method: strings.ToUpper(c.Method), path: c.Path, until: time.Now().Add(gateGrantFor),
		session: session, through: through}
	// $ in it: the shell fills it in when the command runs, so the body sent is not the one on the card.
	if !strings.Contains(c.Body, "$") {
		var v any
		if json.Unmarshal([]byte(c.Body), &v) == nil {
			g.body = v
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	p.grants = append(p.grants, g)
	if len(p.grants) > gateGrantsMax {
		p.grants = p.grants[len(p.grants)-gateGrantsMax:]
	}
}

// take uses up a grant for a request: command's gate, the method and path, and the body confirmed if one was. A grant
// of one conversation is not for another's request: when both are known and differ, it stays for its own. It returns
// the grant.
func (p *gateProxy) take(command, session, method, path string, body []byte, isJSON bool) (gateGrant, bool) {
	var sent any
	parsed := isJSON && json.Unmarshal(body, &sent) == nil
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	for i, g := range p.grants {
		if g.command != command || g.method != method || g.path != path {
			continue
		}
		if g.session != "" && session != "" && g.session != session {
			continue
		}
		if g.body != nil && (!parsed || !reflect.DeepEqual(g.body, sent)) {
			continue
		}
		p.grants = slices.Delete(p.grants, i, i+1)
		return g, true
	}
	return gateGrant{}, false
}

func (p *gateProxy) pruneLocked() {
	now := time.Now()
	p.grants = slices.DeleteFunc(p.grants, func(g gateGrant) bool { return now.After(g.until) })
}

// ServeHTTP is a request of a gated command's (or anything else the assistant runs) to its API.
func (p *gateProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	p.mu.Lock()
	gk, ok := p.keys[key]
	p.mu.Unlock()
	command := gk.command
	n := p.cur.Load()
	if !ok || n == nil {
		http.NotFound(w, r)
		return
	}
	cfg := n.assistantLark(r.Context())
	gi := slices.IndexFunc(cfg.Gates, func(g proto.CommandGate) bool { return g.Command == command && g.Auth != nil })
	if gi < 0 {
		gateRefuse(w, http.StatusServiceUnavailable, command+" 现在用不了：提供它的扩展停用了，或者 hub 没给它凭据。")
		return
	}
	g := cfg.Gates[gi]
	up, err := url.Parse(g.Env[g.BaseEnv])
	if err != nil || up.Host == "" || up.Scheme != "http" && up.Scheme != "https" {
		gateRefuse(w, http.StatusServiceUnavailable, command+" 没有可用的接口地址（扩展没给，或者给的不对）。")
		return
	}
	restPath, err := url.PathUnescape("/" + rest)
	if err != nil {
		gateRefuse(w, http.StatusBadRequest, "路径不对："+err.Error())
		return
	}
	// The call as larkCheck reads `<command> METHOD PATH`: the path past the base, without the gate's prefix.
	c := larkCall{Gate: gi, Method: strings.ToUpper(r.Method), Path: strings.TrimPrefix(restPath, g.PathPrefix), Query: r.URL.RawQuery}
	if !strings.HasPrefix(c.Path, "/") {
		c.Path = "/" + c.Path
	}
	var body []byte
	isJSON := false
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "application/json" || strings.HasSuffix(mt, "+json") {
		if body, err = io.ReadAll(io.LimitReader(r.Body, gateBodyMax+1)); err != nil {
			gateRefuse(w, http.StatusBadRequest, "读不完请求："+err.Error())
			return
		}
		isJSON = len(body) <= gateBodyMax // longer: no rule's body matches it, nor does a body confirmed
		if isJSON {
			c.Body = string(body)
		}
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
	}
	// How the call was let through, for the ledger once the API has taken it.
	session, through := gk.session, proto.LarkLetThrough{}
	switch larkDecision(cfg, c) {
	case "allow":
		// A call larkCheck confirmed and a rule allows too (a health conversation confirms what a rule allows) was
		// let through by the card: it uses up its grant, as a confirmed call does.
		if gr, ok := p.take(command, gk.session, c.Method, c.Path, body, isJSON); ok {
			session, through = firstNonEmpty(gr.session, session), gr.through
		} else if rule, ok := firstLark(g.Rules, c); ok {
			through = proto.LarkLetThrough{Kind: proto.LarkThroughRule, Rule: &rule}
		}
	case "deny":
		gateRefuse(w, http.StatusForbidden, "这个操作按配置不允许（"+c.Method+" "+c.Path+"）。")
		return
	default:
		gr, ok := p.take(command, gk.session, c.Method, c.Path, body, isJSON)
		if !ok {
			gateRefuse(w, http.StatusForbidden, "这个操作要用户先在手机上确认（"+c.Method+" "+c.Path+"）：单独一条 `"+command+
				" METHOD PATH [BODY]` 发，不放进脚本、循环或 $(…) 里，确认卡片会发到用户手机上，确认过的才发得出去。")
			return
		}
		session, through = firstNonEmpty(gr.session, session), gr.through
	}
	sw := &statusWriter{ResponseWriter: w}
	// Told once the answer has been passed on, also when ReverseProxy gives up on its body halfway (it panics with
	// http.ErrAbortHandler then): the API had taken the write by the time it answered 2xx. A client that went away
	// before the API answered leaves no 2xx, and whether the API executed the call is not known: nothing is told.
	defer func() {
		if sw.status/100 == 2 && !larkRead(c) {
			p.reportSent(n, proto.LarkSent{Command: command, Method: c.Method, Path: c.Path, SessionID: session, LetThrough: through}, body, isJSON)
		}
	}()
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			out := pr.Out
			out.URL.Scheme, out.URL.Host, out.Host = up.Scheme, up.Host, up.Host
			out.URL.Path = strings.TrimSuffix(up.Path, "/") + restPath
			out.URL.RawPath = strings.TrimSuffix(up.EscapedPath(), "/") + "/" + rest
			out.URL.RawQuery = r.URL.RawQuery
			for name := range out.Header {
				if !gatePassHeader(name, g.Headers) {
					out.Header.Del(name)
				}
			}
			if out.Header.Get("Origin") != "" { // the script names its base as the origin: the API knows its own
				out.Header.Set("Origin", up.Scheme+"://"+up.Host)
			}
			out.Header.Set(g.Auth.Header, gateAuthValue(g.Auth.Value, g.Env))
		},
		FlushInterval: -1, // streams (events, long polls) go as they come
		ErrorLog:      log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			gateRefuse(w, http.StatusBadGateway, command+" 的接口连不上："+err.Error())
		},
	}
	rp.ServeHTTP(sw, r)
}

// statusWriter keeps the status the API answered with, which ReverseProxy writes through it. Unwrap lets the
// response controller reach the real writer for flushing (streams go as they come) and upgrades.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if code >= 200 && s.status == 0 { // not an informational 1xx before the answer
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// reportSent tells the manager of a write the API took. It runs once the API's answer has gone to the command and does
// not wait for the manager's: the words it said come from the body when it was JSON (proto.WrittenText), and a report
// that fails is only logged, the write is done all the same. At most gateReportsMax are in flight, the one over waiting
// up to gateReportWait for a place; a node without a hub has nobody to tell.
func (p *gateProxy) reportSent(n *Node, s proto.LarkSent, body []byte, isJSON bool) {
	peer := n.peer
	if peer == nil {
		return
	}
	wait := time.NewTimer(gateReportWait)
	defer wait.Stop()
	select {
	case p.reporting <- struct{}{}:
	case <-wait.C:
		n.logger.Printf("assistant: gate proxy: %s %s: too many reports in flight, not told of it", s.Method, s.Path)
		return
	}
	go func() {
		defer func() { <-p.reporting }()
		if isJSON {
			s.Text = proto.WrittenText(body, gateTextMost)
		}
		ctx, cancel := context.WithTimeout(context.Background(), gateReportFor)
		defer cancel()
		if err := peer.Call(ctx, proto.MethodLarkSent, s, nil); err != nil { // never the words it said
			n.logger.Printf("assistant: gate proxy: telling the manager of %s %s: %v", s.Method, s.Path, err)
		}
	}()
}

// gatePassHeader: a header of the request that goes on to the API. A credential of the sender's own, cookies and
// forwarding headers stay here.
func gatePassHeader(name string, allowed []string) bool {
	switch name = strings.ToLower(name); name {
	case "accept", "accept-encoding", "accept-language", "content-type", "user-agent", "origin", "range", "if-none-match",
		"if-modified-since":
		return true
	}
	return name != "authorization" && slices.Contains(allowed, name)
}

var gateAuthRef = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// gateAuthValue is a gate's Auth.Value with its credentials filled in.
func gateAuthValue(tmpl string, env map[string]string) string {
	return gateAuthRef.ReplaceAllStringFunc(tmpl, func(m string) string { return env[m[2:len(m)-1]] })
}

// gateRefuse answers for the proxy, in JSON as the APIs do: the gated command prints it, so the model reads why.
func gateRefuse(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "from": "persona gate"})
}
