package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
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
	env, release := n.assistantLarkEnv(cwd)
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
	env2, release2 := n.assistantLarkEnv(cwd)
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
	base, release, err := p.open("ex")
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
	p.grant("ex", larkCall{Method: "POST", Path: "/conversations/F1/messages", Body: `{"text":"hi"}`})
	if code, _ := send(t, "POST", msg, `{"text":"bye"}`); code != http.StatusForbidden {
		t.Fatalf("another body than the one confirmed: %d", code)
	}
	if code, _ := send(t, "POST", msg, `{ "text" : "hi" }`, "Origin", root); code != http.StatusOK {
		t.Fatalf("the confirmed write: %d", code)
	}
	if code, _ := send(t, "POST", msg, `{"text":"hi"}`); code != http.StatusForbidden {
		t.Fatalf("the confirmed write, sent again: %d", code)
	}
	p.grant("ex", larkCall{Method: "DELETE", Path: "/messages/M1"})
	if code, body := send(t, "DELETE", base+"/api/v1/messages/M1", ""); code != http.StatusForbidden || !strings.Contains(body, "不允许") {
		t.Fatalf("a call the rules deny: %d %s", code, body)
	}
	p.grant("ex", larkCall{Method: "POST", Path: "/conversations/F2/messages", Body: `{"text":"$MSG"}`})
	if code, _ := send(t, "POST", base+"/api/v1/conversations/F2/messages", `{"text":"what the shell filled in"}`); code != http.StatusOK {
		t.Fatalf("a confirmed body the shell fills in: %d", code)
	}
	p.grant("ex", larkCall{Method: "POST", Path: "/conversations/F3/messages"})
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
	base, release, err := n.gates().open("ex")
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
		t.Fatalf("a write 本会话总是允许 covers: %d", code)
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
	base, release, err := first.gates().open("ex")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	next := &Node{dir: t.TempDir(), keeper: keeper, larkCfgAt: time.Now(), logger: discard,
		larkCfg: proto.AssistantLark{Gates: []proto.CommandGate{proxiedGate(api.URL)}}}
	next.larkCfg.Gates[0].Env["EXAMPLE_TOKEN"] = "rotated"
	next.grantGateCalls(next.larkCfg, []larkCall{{Gate: 0, Method: "POST", Path: "/conversations/F1/messages", Body: `{"text":"hi"}`}})
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
