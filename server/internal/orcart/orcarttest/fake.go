// Package orcarttest is a scriptable fake of Orca's runtime socket for tests: it writes its own orca-runtime.json,
// checks the auth token, answers status.get, terminal.list, worktree.ps and terminal.read from its state, records every
// call in order, and lets a test replace any method's answer. Nothing here talks to a real Orca.
package orcarttest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Fake is one fake Orca runtime.
type Fake struct {
	// MetaPath is its orca-runtime.json, Socket its unix socket.
	MetaPath string
	Socket   string

	mu        sync.Mutex
	token     string
	runtimeID string
	log       []Call
	terms     []map[string]any
	layouts   []any
	trees     []map[string]any
	screens   map[string]map[string]any
	status    map[string]any
	on        map[string]func(params map[string]any) any
	keepalive int
	ln        net.Listener
}

// Call is one request the fake got (status.get included).
type Call struct {
	Method string
	Params map[string]any
}

// String is "method {params}" with the params as sorted JSON (client left out: every send carries it).
func (c Call) String() string {
	p := map[string]any{}
	for k, v := range c.Params {
		if k != "client" {
			p[k] = v
		}
	}
	b, _ := json.Marshal(p)
	return c.Method + " " + string(b)
}

// Fail makes a handler answer with an error of that code.
type Fail struct{ Code string }

// New starts a fake runtime in a fresh directory under /tmp (unix socket paths are short).
func New(t testing.TB) *Fake {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "orca")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &Fake{
		MetaPath: filepath.Join(dir, "orca-runtime.json"), Socket: filepath.Join(dir, "o.sock"),
		token: "test-token-1", runtimeID: "rt-1", screens: map[string]map[string]any{},
		on: map[string]func(map[string]any) any{},
		status: map[string]any{"appVersion": "1.4.245", "runtimeProtocolVersion": 3, "minCompatibleRuntimeClientVersion": 2,
			"protocolVersion": 3, "capabilities": []string{"terminal.prompt-delivery.v1"}},
	}
	if err := f.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Stop)
	return f
}

// Start listens (again, after Stop) and writes the metadata file.
func (f *Fake) Start() error {
	_ = os.Remove(f.Socket)
	ln, err := net.Listen("unix", f.Socket)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.ln = ln
	f.mu.Unlock()
	if err := f.writeMeta(); err != nil {
		return err
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return nil
}

// Stop closes the socket (Orca quit); the metadata file stays behind, as Orca's may.
func (f *Fake) Stop() {
	f.mu.Lock()
	ln := f.ln
	f.ln = nil
	f.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
}

// Restart is Orca starting again: a new token and runtime id in the metadata file.
func (f *Fake) Restart(token string) error {
	f.Stop()
	f.mu.Lock()
	f.token, f.runtimeID = token, "rt-"+token
	f.mu.Unlock()
	return f.Start()
}

func (f *Fake) writeMeta() error {
	f.mu.Lock()
	meta := map[string]any{"runtimeId": f.runtimeID, "pid": os.Getpid(), "authToken": f.token, "startedAt": 1,
		"transports": []any{map[string]any{"kind": "unix", "endpoint": f.Socket},
			map[string]any{"kind": "websocket", "endpoint": "ws://127.0.0.1:1"}}}
	f.mu.Unlock()
	b, _ := json.Marshal(meta)
	return os.WriteFile(f.MetaPath, b, 0o600)
}

func (f *Fake) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID        any            `json:"id"`
			AuthToken string         `json:"authToken"`
			Method    string         `json:"method"`
			Params    map[string]any `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			return
		}
		if req.Params == nil {
			req.Params = map[string]any{}
		}
		f.mu.Lock()
		authed := req.AuthToken == f.token
		var result any
		if authed {
			f.log = append(f.log, Call{Method: req.Method, Params: req.Params})
			result = f.answerLocked(req.Method, req.Params)
		}
		h, keepalive := f.on[req.Method], f.keepalive
		f.mu.Unlock()
		if !authed {
			f.write(c, map[string]any{"id": req.ID, "ok": false, "error": map[string]any{"code": "unauthorized",
				"message": "Invalid auth token"}})
			continue
		}
		if h != nil {
			result = h(req.Params)
		}
		for range keepalive {
			f.write(c, map[string]any{"_keepalive": true})
		}
		resp := map[string]any{"id": req.ID, "_meta": map[string]any{"runtimeId": "rt"}}
		if e, ok := result.(Fail); ok {
			resp["ok"] = false
			resp["error"] = map[string]any{"code": e.Code, "message": e.Code}
		} else {
			resp["ok"], resp["result"] = true, result
		}
		f.write(c, resp)
	}
}

func (f *Fake) answerLocked(method string, p map[string]any) any {
	switch method {
	case "status.get":
		return f.status
	case "terminal.list":
		return map[string]any{"terminals": slices.Clone(f.terms), "visualLayouts": slices.Clone(f.layouts),
			"hostScope": map[string]any{"hostIds": []string{"local"}}}
	case "worktree.ps":
		return map[string]any{"worktrees": slices.Clone(f.trees), "totalCount": len(f.trees), "truncated": false}
	case "terminal.read":
		h, _ := p["terminal"].(string)
		if !f.hasHandleLocked(h) {
			return Fail{"terminal_handle_stale"}
		}
		s := f.screens[h]
		if s == nil {
			s = map[string]any{"tail": []string{}, "tailRows": []any{}}
		}
		out := map[string]any{"handle": h, "status": "running", "truncated": false, "source": "screen"}
		maps.Copy(out, s)
		return map[string]any{"terminal": out}
	case "terminal.send", "terminal.close":
		h, _ := p["terminal"].(string)
		if !f.hasHandleLocked(h) {
			return Fail{"terminal_handle_stale"}
		}
		if method == "terminal.close" {
			return map[string]any{"close": map[string]any{"handle": h, "tabId": "t", "ptyKilled": true}}
		}
		return map[string]any{"send": map[string]any{"handle": h, "accepted": true, "bytesWritten": 1}}
	}
	return Fail{"method_not_found"}
}

func (f *Fake) hasHandleLocked(h string) bool {
	return slices.ContainsFunc(f.terms, func(t map[string]any) bool { return t["handle"] == h })
}

func (f *Fake) write(c net.Conn, msg any) {
	b, _ := json.Marshal(msg)
	_, _ = c.Write(append(b, '\n'))
}

// SetStatus replaces status.get's answer fields.
func (f *Fake) SetStatus(fields map[string]any) {
	f.mu.Lock()
	maps.Copy(f.status, fields)
	f.mu.Unlock()
}

// Keepalives makes every answer come after n keepalive lines.
func (f *Fake) Keepalives(n int) {
	f.mu.Lock()
	f.keepalive = n
	f.mu.Unlock()
}

// Terminal is a terminal.list entry on this machine.
func Terminal(handle, tab, leaf, worktree, path, branch, title, identity string) map[string]any {
	return map[string]any{"handle": handle, "ptyId": "pty-" + handle, "orphaned": false, "worktreeId": worktree,
		"worktreePath": path, "branch": branch, "tabId": tab, "leafId": leaf, "title": title, "connected": true,
		"writable": true, "lastOutputAt": 1, "preview": "", "executionHostId": "local", "agentIdentity": identity}
}

// SetTerminals replaces terminal.list's terminals; the layout gets one tab per tab id seen, titled "tab <id>".
func (f *Fake) SetTerminals(terms ...map[string]any) {
	byTree := map[string][]any{}
	seen := map[string]bool{}
	var order []string
	for _, t := range terms {
		w, tab := t["worktreeId"].(string), t["tabId"].(string)
		if _, ok := byTree[w]; !ok {
			order = append(order, w)
		}
		if !seen[tab] {
			seen[tab] = true
			byTree[w] = append(byTree[w], map[string]any{"tabId": tab, "title": "tab " + tab, "activeLeafId": t["leafId"]})
		}
	}
	var layouts []any
	for _, w := range order {
		layouts = append(layouts, map[string]any{"worktreeId": w, "root": map[string]any{"type": "group",
			"groupId": "g-" + w, "tabs": byTree[w]}})
	}
	f.mu.Lock()
	f.terms, f.layouts = terms, layouts
	f.mu.Unlock()
}

// Agent is a worktree.ps agent row.
func Agent(paneKey, agentType, state string) map[string]any {
	return map[string]any{"paneKey": paneKey, "parentPaneKey": nil, "state": state, "agentType": agentType,
		"hasLivePty": true, "updatedAt": 1, "stateStartedAt": 1}
}

// Worktree is a worktree.ps entry.
func Worktree(id, path, branch string, agents ...map[string]any) map[string]any {
	a := make([]any, len(agents))
	for i, x := range agents {
		a[i] = x
	}
	return map[string]any{"worktreeId": id, "path": path, "branch": branch, "displayName": filepath.Base(path),
		"repo": filepath.Base(path), "agents": a}
}

// SetWorktrees replaces worktree.ps's worktrees.
func (f *Fake) SetWorktrees(trees ...map[string]any) {
	f.mu.Lock()
	f.trees = trees
	f.mu.Unlock()
}

// SetScreen sets terminal.read's answer for a handle: its lines (none wrapped).
func (f *Fake) SetScreen(handle string, text string) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	rows := make([]any, len(lines))
	for i := range rows {
		rows[i] = map[string]any{"wrapped": false, "trailingSpaces": 0}
	}
	f.mu.Lock()
	f.screens[handle] = map[string]any{"tail": lines, "tailRows": rows, "returnedLineCount": len(lines)}
	f.mu.Unlock()
}

// SetScreenRows sets terminal.read's answer with explicit rows and wrap flags.
func (f *Fake) SetScreenRows(handle string, lines []string, wrapped []bool) {
	rows := make([]any, len(lines))
	for i := range rows {
		rows[i] = map[string]any{"wrapped": i < len(wrapped) && wrapped[i]}
	}
	f.mu.Lock()
	f.screens[handle] = map[string]any{"tail": lines, "tailRows": rows}
	f.mu.Unlock()
}

// Handle makes a method answer through fn (a Fail for an error).
func (f *Fake) Handle(method string, fn func(params map[string]any) any) {
	f.mu.Lock()
	f.on[method] = fn
	f.mu.Unlock()
}

// Trace is the calls so far ("method {params}"), only of the given methods when any are named, else all but the
// listing ones every refresh makes (status.get, terminal.list, worktree.ps).
func (f *Fake) Trace(methods ...string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.log {
		if len(methods) > 0 && !slices.Contains(methods, c.Method) ||
			len(methods) == 0 && slices.Contains([]string{"status.get", "terminal.list", "worktree.ps"}, c.Method) {
			continue
		}
		out = append(out, c.String())
	}
	return out
}

// Count is how many calls of method came.
func (f *Fake) Count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.log {
		if c.Method == method {
			n++
		}
	}
	return n
}

// Reset forgets the calls so far.
func (f *Fake) Reset() {
	f.mu.Lock()
	f.log = nil
	f.mu.Unlock()
}

// Token is the auth token the fake takes now (tests check it never leaks into logs).
func (f *Fake) Token() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}

// SendCall formats an expected terminal.send trace line.
func SendCall(handle string, fields map[string]any) string {
	p := map[string]any{"terminal": handle}
	maps.Copy(p, fields)
	b, _ := json.Marshal(p)
	return fmt.Sprintf("terminal.send %s", b)
}
