package node

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

// fakeHerdr is a scriptable herdr API socket for node tests. It answers one JSON line per connection (like herdr),
// records every call in order, answers each method from a handler (default: an empty result), serves pane.read from
// a per-pane screen that hooks can change after given calls, and can return herdr errors.
//
// The recorder is what the characterization tests pin: the exact herdr calls (method + params) the node makes.
type fakeHerdr struct {
	t      *testing.T
	sock   string
	mu     sync.Mutex
	log    []herdrCall
	on     map[string]func(p map[string]any) any
	screen map[string]string // pane id -> what pane.read returns
	hooks  []func(c herdrCall)
}

// herdrCall is one recorded request.
type herdrCall struct {
	Method string
	Params map[string]any
}

// String is the canonical form tests compare: the method and its params as JSON with sorted keys.
func (c herdrCall) String() string {
	b, _ := json.Marshal(c.Params)
	return c.Method + " " + string(b)
}

// herdrFail makes a handler answer with a herdr error instead of a result.
type herdrFail struct{ Code, Message string }

func newFakeHerdr(t *testing.T) *fakeHerdr {
	t.Helper()
	dir, err := os.MkdirTemp("", "hf") // short: unix socket paths are limited to ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeHerdr{t: t, sock: filepath.Join(dir, "h.sock"), on: map[string]func(map[string]any) any{}, screen: map[string]string{}}
	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.serveConn(c) // one at a time: the recorded order is the order the node made the calls
		}
	}()
	return f
}

func (f *fakeHerdr) serveConn(c net.Conn) {
	defer c.Close()
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal(line, &req)
	if req.Params == nil {
		req.Params = map[string]any{}
	}
	call := herdrCall{Method: req.Method, Params: req.Params}
	f.mu.Lock()
	h := f.on[req.Method]
	var result any = map[string]any{}
	if h == nil && req.Method == "pane.read" {
		result = map[string]any{"read": map[string]any{"text": f.screen[str(req.Params["pane_id"])]}}
	}
	f.log = append(f.log, call)
	hooks := slices.Clone(f.hooks)
	f.mu.Unlock()
	if h != nil {
		result = h(req.Params)
	}
	// Hooks run after the answer is chosen: a send_keys that changes the screen shows on the next read.
	for _, hook := range hooks {
		hook(call)
	}
	resp := map[string]any{"id": req.ID}
	if e, ok := result.(herdrFail); ok {
		resp["error"] = map[string]any{"code": e.Code, "message": e.Message}
	} else {
		resp["result"] = result
	}
	b, _ := json.Marshal(resp)
	_, _ = c.Write(append(b, '\n'))
}

func str(v any) string { s, _ := v.(string); return s }

// client is a herdr.Client talking only to this socket (no CLI fallback: Bin is empty).
func (f *fakeHerdr) client() *herdr.Client { return &herdr.Client{Socket: f.sock} }

// handle answers method with fn (a herdrFail result is sent as a herdr error).
func (f *fakeHerdr) handle(method string, fn func(p map[string]any) any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on[method] = fn
}

// reply answers method with a fixed result.
func (f *fakeHerdr) reply(method string, result any) {
	f.handle(method, func(map[string]any) any { return result })
}

// setScreen is what pane.read of pane returns from now on (when pane.read has no handler).
func (f *fakeHerdr) setScreen(pane, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screen[pane] = text
}

// after runs fn after every call (outside the fake's lock, so fn may call setScreen).
func (f *fakeHerdr) after(fn func(c herdrCall)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks = append(f.hooks, fn)
}

// screenSteps shows screens[0] on pane, moving to the next one after each call that step matches.
func (f *fakeHerdr) screenSteps(pane string, step func(c herdrCall) bool, screens ...string) {
	var mu sync.Mutex
	at := 0
	f.setScreen(pane, screens[0])
	f.after(func(c herdrCall) {
		if str(c.Params["pane_id"]) != pane || !step(c) {
			return
		}
		mu.Lock()
		if at+1 < len(screens) {
			at++
		}
		s := screens[at]
		mu.Unlock()
		f.setScreen(pane, s)
	})
}

// calls returns every recorded call so far.
func (f *fakeHerdr) calls() []herdrCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

// trace is the recorded calls in canonical form; with methods, only those.
func (f *fakeHerdr) trace(methods ...string) []string {
	var out []string
	for _, c := range f.calls() {
		if len(methods) == 0 || slices.Contains(methods, c.Method) {
			out = append(out, c.String())
		}
	}
	return out
}

// reset forgets the calls recorded so far.
func (f *fakeHerdr) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = nil
}

func isKeys(keys ...string) func(c herdrCall) bool {
	return func(c herdrCall) bool {
		if c.Method != "pane.send_keys" {
			return false
		}
		return len(keys) == 0 || slices.Equal(keysOf(c), keys)
	}
}

func keysOf(c herdrCall) []string {
	var out []string
	if ks, ok := c.Params["keys"].([]any); ok {
		for _, k := range ks {
			out = append(out, str(k))
		}
	}
	return out
}

// call builds the canonical form of an expected call.
func call(method string, params map[string]any) string {
	if params == nil {
		params = map[string]any{}
	}
	// Round-trip so numbers and slices print the way the recorder's decoded params do.
	b, _ := json.Marshal(params)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return herdrCall{Method: method, Params: m}.String()
}

func assertTrace(t *testing.T, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	t.Fatalf("herdr calls differ\n got:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
}

// herdrNode is a Node wired to a fresh fake herdr, with a temp home (~/.claude/projects exists, empty) and every map
// the herdr paths touch. Global caches that outlive a test (offline list, TRAEX panes / status) are cleared.
func herdrNode(t *testing.T) (*Node, *fakeHerdr, string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	f := newFakeHerdr(t)
	n, home := testNode(t)
	n.herdr, n.dir = f.client(), filepath.Join(home, ".jarvis")
	n.panes, n.panePIDs, n.modes, n.driving = map[string]string{}, map[string]panePID{}, map[string]sessionModes{}, map[string]bool{}
	n.files, n.metas, n.bgScans = map[string]*transcript.File{}, map[string]quickMeta{}, map[string]*bgScan{}
	for _, d := range []string{n.dir, filepath.Join(home, ".claude", "projects")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resetGlobalCaches := func() {
		offlineCache.Lock()
		offlineCache.at, offlineCache.list = time.Time{}, nil
		offlineCache.Unlock()
		traexCache.Lock()
		traexCache.panes, traexCache.status = map[int]string{}, map[string]traexState{}
		traexCache.Unlock()
	}
	resetGlobalCaches()
	t.Cleanup(resetGlobalCaches)
	return n, f, home
}

// snapshot is a session.snapshot result.
func snapshot(workspaces, tabs, agents, panes []any) map[string]any {
	nonNil := func(v []any) []any {
		if v == nil {
			return []any{}
		}
		return v
	}
	return map[string]any{"snapshot": map[string]any{"workspaces": nonNil(workspaces), "tabs": nonNil(tabs),
		"agents": nonNil(agents), "panes": nonNil(panes)}}
}

// claudeAgent is a herdr agent entry for a claude whose session herdr knows ("" leaves agent_session out).
func claudeAgent(session, status, pane, tab, ws, cwd, title string) map[string]any {
	a := map[string]any{"agent": "claude", "agent_status": status, "pane_id": pane, "tab_id": tab, "workspace_id": ws,
		"cwd": cwd, "terminal_title_stripped": title, "terminal_title": title}
	if session != "" {
		a["agent_session"] = map[string]any{"kind": "id", "value": session}
	}
	return a
}
