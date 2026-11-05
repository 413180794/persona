package node

import (
	"bytes"
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
// larkCheck let that very call through (the phone confirmed it, or 本会话总是允许 covers it). Printing the
// environment so yields an address that works on this machine only and skips nothing.

const (
	// gateGrantFor: how long a call larkCheck let through may take to be sent.
	gateGrantFor = 10 * time.Minute
	// gateBodyMax: the most of a JSON body read to match it to the rules and to what was confirmed.
	gateBodyMax = 4 << 20
	// gateGrantsMax: grants kept at most (the oldest go first).
	gateGrantsMax = 256
)

type gateProxy struct {
	// cur is the Node whose config (credentials, rules) requests go by: the last to use the proxy, the one serving
	// the hub now.
	cur atomic.Pointer[Node]

	mu     sync.Mutex
	ln     net.Listener
	keys   map[string]string // key → the command of its gate
	grants []gateGrant
}

// gateGrant is a call larkCheck let through, to go once.
type gateGrant struct {
	command, method, path string
	body                  any // the JSON body confirmed; nil: any (not JSON, cut short, an upload, $… in it)
	until                 time.Time
}

// gates is the gate proxy, made at its first use (it listens once a process needs it, open). A resident node has one
// for the process (assistantKeeper): the conversations it keeps from one connection to the next keep their
// addresses, and a call the next connection's Node lets through is the one they send. Otherwise it is the Node's.
func (n *Node) gates() *gateProxy {
	var p *gateProxy
	if k := n.keeper; k != nil {
		k.mu.Lock()
		if k.gp == nil {
			k.gp = &gateProxy{keys: map[string]string{}}
		}
		p = k.gp
		k.mu.Unlock()
	} else {
		n.mu.Lock()
		if n.gp == nil {
			n.gp = &gateProxy{keys: map[string]string{}}
		}
		p = n.gp
		n.mu.Unlock()
	}
	p.cur.Store(n)
	return p
}

// open gives one process of the assistant its own address for command's gate: the base to put in its BaseEnv, and
// release, which takes the address back when the process ends.
func (p *gateProxy) open(command string) (base string, release func(), err error) {
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
	p.keys[key] = command
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

// grant lets c, which larkCheck let through, go once through command's gate.
func (p *gateProxy) grant(command string, c larkCall) {
	g := gateGrant{command: command, method: strings.ToUpper(c.Method), path: c.Path, until: time.Now().Add(gateGrantFor)}
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

// take uses up a grant for a request: command's gate, the method and path, and the body confirmed if one was.
func (p *gateProxy) take(command, method, path string, body []byte, isJSON bool) bool {
	var sent any
	parsed := isJSON && json.Unmarshal(body, &sent) == nil
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	for i, g := range p.grants {
		if g.command != command || g.method != method || g.path != path {
			continue
		}
		if g.body != nil && (!parsed || !reflect.DeepEqual(g.body, sent)) {
			continue
		}
		p.grants = slices.Delete(p.grants, i, i+1)
		return true
	}
	return false
}

func (p *gateProxy) pruneLocked() {
	now := time.Now()
	p.grants = slices.DeleteFunc(p.grants, func(g gateGrant) bool { return now.After(g.until) })
}

// ServeHTTP is a request of a gated command's (or anything else the assistant runs) to its API.
func (p *gateProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	p.mu.Lock()
	command, ok := p.keys[key]
	p.mu.Unlock()
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
	switch larkDecision(cfg, c) {
	case "allow":
	case "deny":
		gateRefuse(w, http.StatusForbidden, "这个操作按配置不允许（"+c.Method+" "+c.Path+"）。")
		return
	default:
		if !p.take(command, c.Method, c.Path, body, isJSON) {
			gateRefuse(w, http.StatusForbidden, "这个操作要用户先在手机上确认（"+c.Method+" "+c.Path+"）：单独一条 `"+command+
				" METHOD PATH [BODY]` 发，不放进脚本、循环或 $(…) 里，确认卡片会发到用户手机上，确认过的才发得出去。")
			return
		}
	}
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
	rp.ServeHTTP(w, r)
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
