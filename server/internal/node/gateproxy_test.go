package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// proxiedGate is the example gate as a hub hands it over since #1317: its credentials go on the requests at the
// node's proxy (Authorization: Bearer ${EXAMPLE_TOKEN}), not into the assistant's environment.
func proxiedGate(base string, user ...proto.LarkRule) proto.CommandGate {
	g := exampleGate(base, user...)
	g.Auth = &proto.GateAuth{Header: "Authorization", Value: "Bearer ${EXAMPLE_TOKEN}"}
	return g
}

// apiSeen is a request the example API got.
type apiSeen struct {
	Method, Path, Query, Auth, Origin, Host, Account, Cookie, Other, Body string
}

type fakeAPI struct {
	*httptest.Server
	mu   sync.Mutex
	seen []apiSeen
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	a := &fakeAPI{}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.seen = append(a.seen, apiSeen{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"),
			Origin: r.Header.Get("Origin"), Host: r.Host, Account: r.Header.Get("X-Example-Account"), Cookie: r.Header.Get("Cookie"),
			Other: r.Header.Get("X-Other"), Body: string(b)})
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if i := strings.LastIndex(r.URL.Path, "/status/"); i >= 0 { // the API answers this one with the status it names
			code, _ := strconv.Atoi(r.URL.Path[i+len("/status/"):])
			w.WriteHeader(code)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/boom") { // the API fails this one
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *fakeAPI) requests() []apiSeen {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apiSeen(nil), a.seen...)
}

// gateNode is a node the hub gave the proxied example gate on api, with the example skill in cwd.
func gateNode(t *testing.T, api string, user ...proto.LarkRule) (*Node, string) {
	t.Helper()
	t.Setenv("JARVIS_SKILLS_DIR", t.TempDir())
	n := &Node{dir: t.TempDir(), larkCfgAt: time.Now(), logger: log.New(io.Discard, "", 0),
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api, user...)}}}
	t.Cleanup(func() { n.gates().close() })
	cwd := t.TempDir()
	skill := filepath.Join(cwd, ".claude", "skills", "example", "scripts")
	_ = os.MkdirAll(skill, 0o755)
	_ = os.WriteFile(filepath.Join(skill, "ex.sh"), []byte("#!/bin/bash\n"), 0o755)
	return n, cwd
}

// send is a request to url, as the gated command's script, or anything else the assistant runs, would make it;
// header is name, value, ….
func send(t *testing.T, method, url, body string, header ...string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func envOf(env []string, name string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v, true
		}
	}
	return "", false
}

// The assistant's process gets its own address at the node's proxy for a gate with Auth, never the credentials;
// the proxy adds them, and the address goes when the process does (#1317).
func TestGateCredentialsStayInTheNode(t *testing.T) {
	api := newFakeAPI(t)
	n, cwd := gateNode(t, api.URL)
	n.larkCfg.Gates[0].Env["EXAMPLE_ACCOUNT"] = "2"
	env, release := n.assistantLarkEnv(cwd, "")
	if all := strings.Join(env, "\n"); strings.Contains(all, "test-token") || strings.Contains(all, "EXAMPLE_TOKEN") {
		t.Fatalf("the credentials reached the assistant's environment:\n%s", all)
	}
	if v, _ := envOf(env, "EXAMPLE_ACCOUNT"); v != "2" {
		t.Fatalf("EXAMPLE_ACCOUNT %q: the gate's other env goes in as before", v)
	}
	base, _ := envOf(env, "EXAMPLE_BASE")
	if !regexp.MustCompile(`^http://127\.0\.0\.1:\d+/[0-9a-f]{32}$`).MatchString(base) {
		t.Fatalf("EXAMPLE_BASE %q: not the process's own address at the proxy", base)
	}
	if code, body := send(t, "GET", base+"/api/v1/me?x=1", ""); code != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("a read: %d %s", code, body)
	}
	got := api.requests()
	if len(got) != 1 || got[0].Path != "/api/v1/me" || got[0].Query != "x=1" || got[0].Auth != "Bearer test-token" ||
		got[0].Host != strings.TrimPrefix(api.URL, "http://") {
		t.Fatalf("the API got %+v", got)
	}
	env2, release2 := n.assistantLarkEnv(cwd, "")
	defer release2()
	if base2, _ := envOf(env2, "EXAMPLE_BASE"); base2 == base {
		t.Fatal("two processes share an address")
	}
	release()
	if code, _ := send(t, "GET", base+"/api/v1/me", ""); code != http.StatusNotFound {
		t.Fatalf("the address of a process that ended: %d", code)
	}
	if len(api.requests()) != 1 {
		t.Fatal("a request went through an address taken back")
	}
}

// The proxy holds every request to the gate's rules, whoever sends it: reads go, a write goes once after the gate let
// that very call through, a rule's deny stays denied, and the sender's own credentials and cookies stay behind.
func TestGateProxyHoldsRequestsToTheRules(t *testing.T) {
	api := newFakeAPI(t)
	n, _ := gateNode(t, api.URL, proto.LarkRule{Method: "DELETE", Path: "/messages/*", Action: "deny"})
	p := n.gates()
	base, release, err := p.open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	root := base[:strings.LastIndex(base, "/")]
	msg := base + "/api/v1/conversations/F1/messages"

	if code, _ := send(t, "GET", base+"/api/v1/me", "", "Authorization", "Bearer mine", "Cookie", "s=1", "X-Example-Account", "3", "X-Other", "1"); code != http.StatusOK {
		t.Fatalf("a read: %d", code)
	}
	if r := api.requests()[0]; r.Auth != "Bearer test-token" || r.Cookie != "" || r.Other != "" || r.Account != "3" {
		t.Fatalf("the headers the API got: %+v", r)
	}
	if code, body := send(t, "POST", msg, `{"text":"hi"}`); code != http.StatusForbidden || !strings.Contains(body, "手机上确认") {
		t.Fatalf("a write nobody confirmed: %d %s", code, body)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations", `{"type":"p2p"}`); code != http.StatusOK {
		t.Fatalf("a write the rules allow: %d", code)
	}
	p.grant("ex", "", larkCall{Method: "POST", Path: "/conversations/F1/messages", Body: `{"text":"hi"}`}, proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	if code, _ := send(t, "POST", msg, `{"text":"bye"}`); code != http.StatusForbidden {
		t.Fatalf("another body than the one confirmed: %d", code)
	}
	if code, _ := send(t, "POST", msg, `{ "text" : "hi" }`, "Origin", root); code != http.StatusOK {
		t.Fatalf("the confirmed write: %d", code)
	}
	if code, _ := send(t, "POST", msg, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	p.grant("ex", "", larkCall{Method: "DELETE", Path: "/messages/M1"}, proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	if code, body := send(t, "DELETE", base+"/api/v1/messages/M1", ""); code != http.StatusForbidden || !strings.Contains(body, "不允许") {
		t.Fatalf("a call the rules deny: %d %s", code, body)
	}
	p.grant("ex", "", larkCall{Method: "POST", Path: "/conversations/F2/messages", Body: `{"text":"$MSG"}`}, proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F2/messages", `{"text":"what the shell filled in"}`); code != http.StatusOK {
		t.Fatalf("a confirmed body the shell fills in: %d", code)
	}
	p.grant("ex", "", larkCall{Method: "POST", Path: "/conversations/F3/messages"}, proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	p.mu.Lock()
	p.grants[len(p.grants)-1].until = time.Now().Add(-time.Second)
	p.mu.Unlock()
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F3/messages", `{}`); code != http.StatusForbidden {
		t.Fatalf("a grant past its time: %d", code)
	}
	if code, _ := send(t, "GET", root+"/"+strings.Repeat("0", 32)+"/api/v1/me", ""); code != http.StatusNotFound {
		t.Fatalf("an address nobody was given: %d", code)
	}
	var writes []apiSeen
	for _, r := range api.requests() {
		if r.Method != "GET" {
			writes = append(writes, r)
		}
	}
	if len(writes) != 3 || writes[1].Path != "/api/v1/conversations/F1/messages" || writes[1].Body != `{ "text" : "hi" }` {
		t.Fatalf("the writes the API got: %+v", writes)
	}
	if writes[1].Origin != api.URL {
		t.Fatalf("Origin %q: the API's own, not the proxy's", writes[1].Origin)
	}
}

// What the phone confirms is what the proxy lets through, once; a refusal lets nothing through; "Always allow for this session" lets
// the same route through again without asking.
func TestGateLetsThroughWhatThePhoneConfirmed(t *testing.T) {
	api := newFakeAPI(t)
	var mu sync.Mutex
	answer := proto.Resolution{Allow: boolp(false), Message: "不发"}
	h := newScriptHub(t, func(context.Context, proto.Interaction) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		return answer, nil
	}, notifyOK)
	n := h.n
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}, time.Now()
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	check := func(cmd string) (string, string) {
		t.Helper()
		ix := proto.Interaction{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}
		d, reason, err := n.larkCheck(context.Background(), "s1", t.TempDir(), cmd, ix)
		if err != nil {
			t.Fatal(err)
		}
		return d, reason
	}
	asked := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.created)
	}
	write := `ex POST /conversations/F1/messages '{"text":"hi"}'`
	if d, reason := check(write); d != "deny" || reason != "不发" {
		t.Fatalf("refused on the phone: %s %q", d, reason)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F1/messages", `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("a write the phone refused went through: %d", code)
	}
	mu.Lock()
	answer = proto.Resolution{Allow: boolp(true), Always: true}
	mu.Unlock()
	if d, _ := check(write); d != "allow" {
		t.Fatalf("confirmed on the phone: %s", d)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F1/messages", `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write the phone confirmed: %d", code)
	}
	cards := asked()
	if d, _ := check(`ex POST /conversations/F2/messages '{"text":"again"}'`); d != "allow" || asked() != cards {
		t.Fatalf("本会话总是允许: %s, %d cards (was %d)", d, asked(), cards)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F2/messages", `{"text":"again"}`); code != http.StatusOK {
		t.Fatalf("a write Always allow covers: %d", code)
	}
	if got := api.requests(); len(got) != 2 {
		t.Fatalf("the API got %+v", got)
	}
}

// A write the gate cannot follow (a loop) through a gate whose credentials are at the proxy is refused at once, with
// how to send it instead; through a gate that still puts its credentials in the environment it is put to the phone
// whole, as before.
func TestGateWriteItCannotFollow(t *testing.T) {
	n := &Node{dir: t.TempDir(), larkCfgAt: time.Now(), logger: log.New(io.Discard, "", 0),
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate("http://api.example.test")}}}
	loop := `for i in 1 2; do ex POST /conversations/F$i/messages '{"text":"hi"}'; done`
	ix := proto.Interaction{ExpiresAt: time.Now().Add(time.Minute)}
	if d, reason, err := n.larkCheck(context.Background(), "s1", t.TempDir(), loop, ix); err != nil || d != "deny" || !strings.Contains(reason, "单独一条") {
		t.Fatalf("a write in a loop: %s %q %v", d, reason, err)
	}
	n.larkCfg = proto.AssistantLark{Gates: []proto.CommandGate{exampleGate("http://api.example.test")}}
	// No hub to ask here: put to the phone, and not confirmed.
	if d, reason, _ := n.larkCheck(context.Background(), "s1", t.TempDir(), loop, ix); d != "deny" || strings.Contains(reason, "单独一条") {
		t.Fatalf("a write in a loop, credentials in the environment: %s %q", d, reason)
	}
}

// A resident node's next connection lets through what a conversation kept from the last one sends, with the
// credentials it has now: the proxy is the process's, not the connection's.
func TestKeptConversationSendsThroughTheNextConnection(t *testing.T) {
	api := newFakeAPI(t)
	keeper := newAssistantKeeper()
	discard := log.New(io.Discard, "", 0)
	first := &Node{dir: t.TempDir(), keeper: keeper, larkCfgAt: time.Now(), logger: discard,
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}}
	t.Cleanup(func() { first.gates().close() })
	base, release, err := first.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	next := &Node{dir: t.TempDir(), keeper: keeper, larkCfgAt: time.Now(), logger: discard,
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}}
	next.larkCfg.Gates[0].Env["EXAMPLE_TOKEN"] = "rotated"
	next.grantGateCalls(next.larkCfg, "", []larkCall{{Gate: 0, Method: "POST", Path: "/conversations/F1/messages", Body: `{"text":"hi"}`}}, proto.LarkLetThrough{Kind: proto.LarkThroughCard})
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F1/messages", `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("a kept conversation's write the next connection let through: %d", code)
	}
	if got := api.requests(); len(got) != 1 || got[0].Auth != "Bearer rotated" {
		t.Fatalf("the API got %+v", got)
	}
}

// End to end, as a runtime's assistant runs the gated command: the skill's script (curl here) reaches the API only
// through the proxy and never has the credentials; the write the phone confirms goes, the one it refuses does not.
func TestGatedToolSendsThroughTheProxy(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	api := newFakeAPI(t)
	n, hub := runtimeNode(t)
	assistantHub(hub)
	skills := filepath.Join(n.home, ".jarvis", "skills")
	t.Setenv("JARVIS_SKILLS_DIR", skills)
	n.dir = filepath.Join(n.home, ".jarvis")
	script := filepath.Join(skills, "example", "scripts", "ex.sh")
	_ = os.MkdirAll(filepath.Dir(script), 0o755)
	_ = os.WriteFile(filepath.Join(skills, "example", "SKILL.md"), []byte("---\nname: example\n---\n"), 0o644)
	_ = os.WriteFile(script, []byte(`#!/bin/bash
echo "token:${EXAMPLE_TOKEN:-none}"
if [ -n "${3:-}" ]; then
  curl -sS -X "$1" -H 'content-type: application/json' --data-raw "$3" "$EXAMPLE_BASE/api/v1$2"
else
  curl -sS -X "$1" "$EXAMPLE_BASE/api/v1$2"
fi
`), 0o755)
	g := proxiedGate(api.URL)
	g.Tool = &proto.GateTool{Name: "example_api", Description: "Run ex."}
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{g}}, time.Now().Add(time.Hour)
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	host := dialRuntime(t, n, assistantRuntimeHello())
	sid := startedAssistant(t, n, host)
	run := func(args ...string) proto.ToolResult {
		t.Helper()
		var res proto.ToolResult
		if err := host.peer.Call(context.Background(), "mcp.call", map[string]any{"name": "example_api", "args": map[string]any{"args": args}, "session_id": sid, "assistant": true}, &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("GET", "/me"); res.IsError || !strings.Contains(res.Content[0].Text, "token:none") || !strings.Contains(res.Content[0].Text, `{"ok":true}`) {
		t.Fatalf("a read: %+v", res)
	}
	resolve := func(allow bool, msg string) {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		hub.resolve = func(proto.Interaction) proto.Resolution { return proto.Resolution{Allow: boolp(allow), Message: msg} }
	}
	resolve(true, "")
	if res := run("POST", "/conversations/oc_test/messages", `{"text":"hi"}`); res.IsError || !strings.Contains(res.Content[0].Text, `{"ok":true}`) {
		t.Fatalf("a confirmed write: %+v", res)
	}
	resolve(false, "not now")
	if res := run("POST", "/conversations/oc_test/messages", `{"text":"again"}`); !res.IsError || !strings.Contains(res.Content[0].Text, "not now") {
		t.Fatalf("a refused write: %+v", res)
	}
	got := api.requests()
	if len(got) != 2 || got[0].Auth != "Bearer test-token" || got[1].Method != "POST" || got[1].Body != `{"text":"hi"}` {
		var b []byte
		b, _ = json.Marshal(got)
		t.Fatalf("the API got %s", b)
	}
}

// ledgerHub is the manager as the gate proxy meets it: it takes lark.sent and keeps what it was told.
type ledgerHub struct {
	mu   sync.Mutex
	got  []proto.LarkSent
	hold chan struct{} // when set, a report waits for it to close before it is answered
	fail bool          // the manager answers with an error
}

// lockedLog is a log the test reads while the node writes it.
type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// ledgerNode is gateNode with a hub behind it that takes the proxy's reports.
func ledgerNode(t *testing.T, api string, user ...proto.LarkRule) (*Node, *ledgerHub, *lockedLog) {
	t.Helper()
	n, _ := gateNode(t, api, user...)
	logs := &lockedLog{}
	n.logger = log.New(logs, "", 0)
	h := &ledgerHub{}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	hub.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != proto.MethodLarkSent {
			return nil, nil
		}
		h.mu.Lock()
		hold, fail := h.hold, h.fail
		h.mu.Unlock()
		if hold != nil {
			<-hold
		}
		if fail {
			return nil, errors.New("manager down")
		}
		var s proto.LarkSent
		_ = json.Unmarshal(params, &s)
		h.mu.Lock()
		h.got = append(h.got, s)
		h.mu.Unlock()
		return struct{}{}, nil
	}
	peer := n.peer
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return n, h, logs
}

// reportList waits for at least want reports and then a moment more, so one too many shows; they come in the order the
// manager took them, which is not the order of the writes.
func (h *ledgerHub) reportList(t *testing.T, want int) []proto.LarkSent {
	t.Helper()
	waitFor(t, "reports to the manager", func() bool { // catchup_test.go: on every platform, unlike fullWait (#5924)
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.got) >= want
	})
	time.Sleep(100 * time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.got) != want {
		t.Fatalf("%d reports, want %d: %+v", len(h.got), want, h.got)
	}
	return append([]proto.LarkSent(nil), h.got...)
}

// reports is reportList by path (the last report of a path stands for it).
func (h *ledgerHub) reports(t *testing.T, want int) map[string]proto.LarkSent {
	t.Helper()
	out := map[string]proto.LarkSent{}
	for _, s := range h.reportList(t, want) {
		out[s.Path] = s
	}
	return out
}

// A write the proxy forwarded and the API answered with a 2xx is told to the manager, with how the gate let it
// through (a rule, a card, "Always allow") and the conversation it belongs to. A read, a write the proxy refused and
// one the API failed are not.
func TestGateProxyTellsTheManagerOfWritesTheAPITook(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, _ := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	p := n.gates()
	base, release, err := p.open("ex", "s-proc")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// A process the node knew no conversation of takes the grant of any, and is told as the grant's.
	anon, releaseAnon, err := p.open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAnon()
	card := proto.LarkLetThrough{Kind: proto.LarkThroughCard, InteractionID: "ix_1", AnsweredBy: proto.AnsweredByAssistant}

	if code, _ := send(t, "GET", base+"/api/v1/me", ""); code != http.StatusOK {
		t.Fatalf("a read: %d", code)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F0/messages", `{"text":"nobody asked"}`); code != http.StatusForbidden {
		t.Fatalf("a write nobody confirmed: %d", code)
	}
	if code, _ := send(t, "POST", base+"/api/v1/notes/n1/boom", `{"text":"the API fails"}`); code != http.StatusInternalServerError {
		t.Fatalf("a rule's write the API failed: %d", code)
	}
	p.grant("ex", "s-card", larkCall{Method: "POST", Path: "/conversations/F9/boom"}, card)
	if code, _ := send(t, "POST", anon+"/api/v1/conversations/F9/boom", `{"text":"fails too"}`); code != http.StatusInternalServerError {
		t.Fatalf("a confirmed write the API failed: %d", code)
	}

	if code, _ := send(t, "POST", base+"/api/v1/notes/n1", `{"text":"from a rule","n":1}`); code != http.StatusOK {
		t.Fatalf("a rule's write: %d", code)
	}
	p.grant("ex", "s-card", larkCall{Method: "POST", Path: "/conversations/F1/messages", Body: `{"text":"hi"}`}, card)
	if code, _ := send(t, "POST", anon+"/api/v1/conversations/F1/messages", `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("a confirmed write: %d", code)
	}
	p.grant("ex", "s-always", larkCall{Method: "POST", Path: "/conversations/F2/messages"}, proto.LarkLetThrough{Kind: proto.LarkThroughAlways})
	if code, _ := send(t, "POST", anon+"/api/v1/conversations/F2/messages", `{"text":"again"}`); code != http.StatusOK {
		t.Fatalf("a write Always allow covers: %d", code)
	}
	// A rule's allow that the gate also confirmed (a health conversation confirms every write) was let through by the
	// card, which is used up by it: the next such write is the rule's.
	p.grant("ex", "s-proc", larkCall{Method: "POST", Path: "/notes/n2"}, card)
	for i := 0; i < 2; i++ {
		if code, _ := send(t, "POST", base+"/api/v1/notes/n2", `{"text":"twice"}`); code != http.StatusOK {
			t.Fatalf("a rule's write the card also covers: %d", code)
		}
	}

	got := hub.reports(t, 5)
	if r := got["/notes/n1"]; r.Command != "ex" || r.Method != "POST" || r.Text != "from a rule" || r.SessionID != "s-proc" || r.LetThrough.Kind != proto.LarkThroughRule ||
		r.LetThrough.Rule == nil || r.LetThrough.Rule.Method != "POST" || r.LetThrough.Rule.Path != "/notes/**" || r.LetThrough.Rule.Action != "allow" {
		t.Fatalf("the rule's write: %+v", r)
	}
	if r := got["/conversations/F1/messages"]; r.Method != "POST" || r.Text != "hi" || r.SessionID != "s-card" || r.LetThrough != card {
		t.Fatalf("the confirmed write: %+v", r)
	}
	if r := got["/conversations/F2/messages"]; r.Text != "again" || r.SessionID != "s-always" || r.LetThrough != (proto.LarkLetThrough{Kind: proto.LarkThroughAlways}) {
		t.Fatalf("the write Always allow covers: %+v", r)
	}
	// Both /notes/n2 writes are in got under one key: the later one, the rule's, is what it holds (reports by path).
	hub.mu.Lock()
	var n2 []proto.LarkSent
	for _, s := range hub.got {
		if s.Path == "/notes/n2" {
			n2 = append(n2, s)
		}
	}
	hub.mu.Unlock()
	kinds := map[string]string{}
	for _, s := range n2 {
		kinds[s.LetThrough.Kind] = s.SessionID
	}
	if len(n2) != 2 || kinds[proto.LarkThroughCard] != "s-proc" || kinds[proto.LarkThroughRule] != "s-proc" {
		t.Fatalf("the writes a card and a rule both cover: %+v", n2)
	}
}

// What the report says it wrote is the body's words cut to 4000 characters, and reading the body for it does not
// change what the API gets; a body that is not JSON says none.
func TestGateProxyReportCutsTheText(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, _ := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	long := strings.Repeat("好", 5000)
	if code, _ := send(t, "POST", base+"/api/v1/notes/long", `{"title":"t","body":"`+long+`","text":"not this one"}`); code != http.StatusOK {
		t.Fatalf("a long write: %d", code)
	}
	if code, _ := send(t, "POST", base+"/api/v1/notes/plain", "plain words", "Content-Type", "text/plain"); code != http.StatusOK {
		t.Fatalf("a write that is not JSON: %d", code)
	}
	got := hub.reports(t, 2)
	if r := got["/notes/long"]; r.Text != strings.Repeat("好", gateTextMost) || r.SessionID != "" {
		t.Fatalf("the long write told as %d characters, session %q", len([]rune(r.Text)), r.SessionID)
	}
	if r := got["/notes/plain"]; r.Text != "" {
		t.Fatalf("the plain write told as %q", r.Text)
	}
	seen := api.requests()
	if len(seen) != 2 || strings.Count(seen[0].Body, "好") != 5000 || seen[1].Body != "plain words" {
		t.Fatalf("what the API got changed: %d requests", len(seen))
	}
}

// The report never holds the command's response, and a manager that fails it only leaves a line in the log.
func TestGateProxyReportDoesNotHoldTheResponse(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, logs := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	hub.mu.Lock()
	hub.hold = make(chan struct{})
	hub.mu.Unlock()
	if code, body := send(t, "POST", base+"/api/v1/notes/slow", `{"text":"hi"}`); code != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("a write while the manager is slow: %d %s", code, body)
	}
	hub.mu.Lock()
	told := len(hub.got)
	close(hub.hold)
	hub.hold = nil
	hub.mu.Unlock()
	if told != 0 {
		t.Fatalf("%d reports before the manager answered", told)
	}
	hub.reports(t, 1)

	hub.mu.Lock()
	hub.fail = true
	hub.mu.Unlock()
	if code, body := send(t, "POST", base+"/api/v1/notes/failing", `{"text":"secret words"}`); code != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("a write whose report fails: %d %s", code, body)
	}
	waitFor(t, "the failure in the log", func() bool { return strings.Contains(logs.String(), "telling the manager of POST /notes/failing") })
	if strings.Contains(logs.String(), "secret words") {
		t.Fatalf("the words reached the log: %s", logs)
	}
}

// larkCheck's side of it: the card a write was confirmed on, who answered it and the conversation come to the proxy
// with the grant, and "Always allow for this session" makes the next write the conversation's own, without a card.
func TestGateReportNamesTheCardAndTheConversation(t *testing.T) {
	api := newFakeAPI(t)
	h := newScriptHub(t, func(context.Context, proto.Interaction) (any, error) {
		return proto.Resolution{Allow: boolp(true), Always: true, AnsweredBy: proto.AnsweredByAssistant}, nil
	}, notifyOK)
	n := h.n
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}, time.Now()
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	base, release, err := n.gates().open("ex", "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, c := range []struct{ path, text string }{{"F1", "first"}, {"F2", "second"}} {
		ix := proto.Interaction{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}
		cmd := `ex POST /conversations/` + c.path + `/messages '{"text":"` + c.text + `"}'`
		if d, _, err := n.larkCheck(context.Background(), "s1", t.TempDir(), cmd, ix); err != nil || d != "allow" {
			t.Fatalf("%s: %s %v", c.path, d, err)
		}
		if code, _ := send(t, "POST", base+"/api/v1/conversations/"+c.path+"/messages", `{"text":"`+c.text+`"}`); code != http.StatusOK {
			t.Fatalf("%s: %d", c.path, code)
		}
	}
	waitFor(t, "the reports", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.larkSent) == 2
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 1 || h.created[0].ID == "" {
		t.Fatalf("cards %+v: one, with an id of its own", h.created)
	}
	first, second := h.larkSent[0], h.larkSent[1]
	if first.Path == "/conversations/F2/messages" {
		first, second = second, first
	}
	want := proto.LarkLetThrough{Kind: proto.LarkThroughCard, InteractionID: h.created[0].ID, AnsweredBy: proto.AnsweredByAssistant}
	if first.Path != "/conversations/F1/messages" || first.Text != "first" || first.SessionID != "s1" || first.LetThrough != want {
		t.Fatalf("the write the card allowed: %+v, want %+v", first, want)
	}
	if second.Text != "second" || second.SessionID != "s1" || second.LetThrough != (proto.LarkLetThrough{Kind: proto.LarkThroughAlways}) {
		t.Fatalf("the write Always allow covered: %+v", second)
	}
}

// The status the API answered with decides: any 2xx is a write that went out (201 and 204 as much as 200); a
// redirect, 304 and the failures are not.
func TestGateProxyReportsOnly2xx(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, _ := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, c := range []struct {
		path string
		code int
		told bool
	}{
		{"/notes/a", 200, true},
		{"/notes/b/status/201", 201, true},
		{"/notes/c/status/204", 204, true},
		{"/notes/d/status/301", 301, false},
		{"/notes/e/status/304", 304, false},
		{"/notes/f/status/404", 404, false},
		{"/notes/g/status/503", 503, false},
	} {
		if code, _ := send(t, "POST", base+"/api/v1"+c.path, `{"text":"x"}`); code != c.code {
			t.Fatalf("%s: the command got %d", c.path, code)
		}
	}
	got := hub.reports(t, 3)
	for _, path := range []string{"/notes/a", "/notes/b/status/201", "/notes/c/status/204"} {
		if _, ok := got[path]; !ok {
			t.Errorf("%s was not told: %+v", path, got)
		}
	}
}

// An answer the proxy could not finish passing on (the API sent its headers, a 2xx, and dropped the body) is a write the
// API took all the same: it is told, though ReverseProxy aborts the response with a panic.
func TestGateProxyReportsAWriteWhoseBodyBroke(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush() // and goes: the connection ends short of what it said
	}))
	t.Cleanup(api.Close)
	n, hub, _ := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	base, release, err := n.gates().open("ex", "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	req, _ := http.NewRequest("POST", base+"/api/v1/notes/cut", strings.NewReader(`{"text":"half an answer"}`))
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil { // the command may see the headers or only the break
		_, _ = io.ReadAll(res.Body)
		res.Body.Close()
	}
	got := hub.reportList(t, 1)
	if r := got[0]; r.Path != "/notes/cut" || r.Text != "half an answer" || r.SessionID != "s1" || r.LetThrough.Kind != proto.LarkThroughRule {
		t.Fatalf("told %+v", got)
	}
}

// A grant belongs to the conversation larkCheck let the call through for: another conversation's identical call does not
// use it up, and the ledger names the conversation and the card of the one that did. A process the node knew no
// conversation of takes any, and is told as the grant's.
func TestGateGrantsStayWithTheirConversation(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, _ := ledgerNode(t, api.URL)
	p := n.gates()
	open := func(session string) string {
		t.Helper()
		base, release, err := p.open("ex", session)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		return base
	}
	a, b, anon := open("sA"), open("sB"), open("")
	call := larkCall{Method: "POST", Path: "/conversations/F1/messages"}
	cardA := proto.LarkLetThrough{Kind: proto.LarkThroughCard, InteractionID: "ix_A"}
	cardB := proto.LarkLetThrough{Kind: proto.LarkThroughCard, InteractionID: "ix_B", AnsweredBy: proto.AnsweredByAssistant}
	p.grant("ex", "sA", call, cardA)
	p.grant("ex", "sB", call, cardB)
	write := func(base string) int {
		code, _ := send(t, "POST", base+"/api/v1/conversations/F1/messages", `{"text":"same call"}`)
		return code
	}
	if code := write(b); code != http.StatusOK {
		t.Fatalf("B with its own grant: %d", code)
	}
	if code := write(b); code != http.StatusForbidden {
		t.Fatalf("B with only A's grant left: %d", code)
	}
	if code := write(a); code != http.StatusOK {
		t.Fatalf("A with its own grant: %d", code)
	}
	p.grant("ex", "sA", call, cardA)
	if code := write(anon); code != http.StatusOK {
		t.Fatalf("a process with no conversation known: %d", code)
	}
	bySession := map[string][]proto.LarkLetThrough{}
	for _, r := range hub.reportList(t, 3) {
		bySession[r.SessionID] = append(bySession[r.SessionID], r.LetThrough)
	}
	if len(bySession["sB"]) != 1 || bySession["sB"][0] != cardB || len(bySession["sA"]) != 2 || bySession["sA"][0] != cardA || bySession["sA"][1] != cardA {
		t.Fatalf("the ledger got %+v", bySession)
	}
}

// Reports in flight are limited; one over waits a moment for a place and then is given up on with a line in the log,
// while the command's response is not held back more than that.
func TestGateReportWaitsForAPlaceThenGivesUp(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, logs := ledgerNode(t, api.URL, proto.LarkRule{Method: "POST", Path: "/notes/**", Action: "allow"})
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	hub.mu.Lock()
	hub.hold = make(chan struct{})
	hub.mu.Unlock()
	for i := 0; i < gateReportsMax+1; i++ {
		start := time.Now()
		if code, _ := send(t, "POST", base+"/api/v1/notes/n"+strconv.Itoa(i), `{"text":"hi"}`); code != http.StatusOK {
			t.Fatalf("write %d: %d", i, code)
		}
		if i < gateReportsMax && time.Since(start) >= gateReportWait {
			t.Fatalf("write %d waited %v with places left", i, time.Since(start))
		}
	}
	// The response is not held for it: the command has its answer while the report still waits for a place.
	waitFor(t, "the line about the one over the limit", func() bool {
		return strings.Contains(logs.String(), "too many reports in flight")
	})
	hub.mu.Lock()
	close(hub.hold)
	hub.hold = nil
	hub.mu.Unlock()
	hub.reportList(t, gateReportsMax)
}

// A write the manager let through from an "Always allow" it kept (it says so: remembered) was not let through by a card,
// whatever the card's id: the ledger says conversation_always.
func TestGateReportRememberedAlwaysIsNotACard(t *testing.T) {
	api := newFakeAPI(t)
	h := newScriptHub(t, func(context.Context, proto.Interaction) (any, error) {
		return proto.Resolution{Allow: boolp(true), Always: true, Remembered: true}, nil
	}, notifyOK)
	n := h.n
	n.mu.Lock()
	n.larkCfg, n.larkCfgAt = proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}, time.Now()
	n.mu.Unlock()
	t.Cleanup(func() { n.gates().close() })
	base, release, err := n.gates().open("ex", "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ix := proto.Interaction{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}
	if d, _, err := n.larkCheck(context.Background(), "s1", t.TempDir(), `ex POST /conversations/F1/messages '{"text":"hi"}'`, ix); err != nil || d != "allow" {
		t.Fatalf("%s %v", d, err)
	}
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F1/messages", `{"text":"hi"}`); code != http.StatusOK {
		t.Fatalf("the write: %d", code)
	}
	waitFor(t, "the report", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.larkSent) == 1
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.larkSent[0]; r.SessionID != "s1" || r.LetThrough != (proto.LarkLetThrough{Kind: proto.LarkThroughAlways}) {
		t.Fatalf("told %+v", r)
	}
}
