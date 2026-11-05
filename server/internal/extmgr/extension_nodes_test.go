package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/streams"
)

// extensionNodeMachine is a machine whose node answers plugin.node.* as one that runs plugins' programs does (or, old,
// as one from before them), and says what it was asked.
func extensionNodeMachine(t *testing.T, h *Manager, id, goos, goarch string, old bool) (*Machine, func() []browserCallSeen) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	var mu sync.Mutex
	var seen []browserCallSeen
	have := map[string]bool{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		seen = append(seen, browserCallSeen{method, p})
		if old {
			return nil, &rpc.Error{Message: "unknown method " + method}
		}
		switch method {
		case "plugin.node.have":
			return proto.PluginNodeHave{Have: have[p["digest"].(string)]}, nil
		case "plugin.node.put.begin":
			have[p["digest"].(string)] = true // as if it all came
			return map[string]string{"upload_id": "u1"}, nil
		case "plugin.node.put.chunk", "plugin.node.put.commit", "plugin.node.renew", "plugin.node.stop":
			return map[string]bool{"ok": true}, nil
		case "plugin.node.start":
			return proto.PluginNodeState{ID: p["id"].(string), Version: p["version"].(string), State: "running"}, nil
		case "plugin.node.sync":
			return proto.PluginNodeStates{}, nil
		case "plugin.node.call":
			return json.RawMessage(`{"echo":` + string(must(json.Marshal(p["params"]))) + `}`), nil
		}
		return nil, &rpc.Error{Message: "unknown method " + method}
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online, m.info = hubSide, true, proto.NodeInfo{OS: goos, Arch: goarch}
	h.fleetMu.Lock()
	h.machines, h.byID[id] = append(h.machines, m), m
	h.fleetMu.Unlock()
	return m, func() []browserCallSeen {
		mu.Lock()
		defer mu.Unlock()
		return append([]browserCallSeen(nil), seen...)
	}
}

func must(b []byte, _ error) []byte { return b }

// helperPackage is a plugin whose only program runs on computers (Linux and Mac ones), picked in "computers"; its
// api_key goes to the computers, hub_only does not.
func helperPackage(t *testing.T) string {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "node"), 0o755)
	for _, f := range []string{"helper-linux", "helper-mac"} {
		_ = os.WriteFile(filepath.Join(dir, "node", f), []byte("#!/bin/sh\n"), 0o755)
	}
	manifest := `{"manifest_version": 1, "id": "example-helper", "name": "Example Helper", "version": "1.0.0",
		"engines": {"plugin_protocol": 1},
		"node": {"command": {"linux-amd64": "node/helper-linux", "darwin-arm64": "node/helper-mac"}, "machines": "computers", "secrets": ["api_key"]},
		"contributes": {"providers": [{"id": "example-ai", "name": "Example AI", "kind": "api", "api": "responses",
			"base_url": "https://api.example.com/v1", "models": [{"id": "example-large"}]}]},
		"configuration": {"type": "object", "properties": {"computers": {"type": "array", "title": "在哪些电脑上运行",
			"items": {"type": "string", "format": "machine", "x-os": ["darwin", "linux"]}}}},
		"secrets": [{"name": "api_key", "title": "API Key"}, {"name": "hub_only", "title": "只在 hub 上"}]}`
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A plugin's program goes to the machines picked for it and starts there with only the secrets it sends them;
// the others are left alone; a node from before #1830 says so; the program's state and calls go through the hub.
func TestPluginProgramsOnMachines(t *testing.T) {
	ctx := context.Background()
	h, _ := extensionTestHub(t)
	box, boxSeen := extensionNodeMachine(t, h, "box", "linux", "amd64", false)
	_, otherSeen := extensionNodeMachine(t, h, "other", "linux", "amd64", false)
	oldBox, _ := extensionNodeMachine(t, h, "oldbox", "darwin", "arm64", true)
	win, winSeen := extensionNodeMachine(t, h, "win", "windows", "amd64", false)

	reg := h.extensions.reg
	pv, err := reg.Preview(ctx, "dir:"+helperPackage(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	_ = reg.SetSecret(ctx, "example-helper", "api_key", "k-1", "test")
	_ = reg.SetSecret(ctx, "example-helper", "hub_only", "h-1", "test")
	if err := reg.SetConfig(ctx, "example-helper", json.RawMessage(`{"computers": ["box", "oldbox", "win"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []*Machine{box, oldBox, win} {
		h.syncExtensionNodes(ctx, m)
	}

	var start map[string]any
	for _, c := range boxSeen() {
		if c.Method == "plugin.node.start" {
			start = c.Params
		}
	}
	if start == nil {
		t.Fatalf("not started on box: %+v", boxSeen())
	}
	if start["command"] != "node/helper-linux" || start["keep"] != "lease" || start["lease_seconds"] != float64(1800) {
		t.Fatalf("%+v", start)
	}
	initJSON, _ := json.Marshal(start["init"])
	if s := string(initJSON); !strings.Contains(s, `"api_key":"k-1"`) || strings.Contains(s, "h-1") {
		t.Fatalf("only the secrets for computers: %s", s)
	}
	for _, c := range otherSeen() {
		if strings.HasPrefix(c.Method, "plugin.node.start") || strings.HasPrefix(c.Method, "plugin.node.put") {
			t.Fatalf("a machine not picked got it: %+v", c)
		}
	}
	for _, c := range winSeen() {
		if strings.HasPrefix(c.Method, "plugin.node.start") {
			t.Fatal("started on a system it has no program for")
		}
	}
	var puts int
	for _, c := range boxSeen() {
		if c.Method == "plugin.node.put.commit" {
			puts++
		}
	}
	h.syncExtensionNodes(ctx, box) // again: it has the package
	var putsAfter int
	for _, c := range boxSeen() {
		if c.Method == "plugin.node.put.commit" {
			putsAfter++
		}
	}
	if puts == 0 || putsAfter != puts {
		t.Fatalf("the package goes once: %d, then %d", puts, putsAfter)
	}

	views := map[string]ExtensionNodeView{}
	for _, v := range h.extensions.view(ctx, reg.Get("example-helper")).Nodes {
		views[v.MachineID] = v
	}
	if views["box"].State != "running" || views["oldbox"].State != extensionNodeOutdated || views["other"].State != "" {
		t.Fatalf("%+v", views)
	}

	// What the node says of it later (its lease ran out while the hub was away) shows.
	h.NodeEvent("box", "plugin.node.state", json.RawMessage(`{"id":"example-helper","version":"1.0.0","state":"lease_expired"}`))
	for _, v := range h.extensions.view(ctx, reg.Get("example-helper")).Nodes {
		if v.MachineID == "box" && v.State != "lease_expired" {
			t.Fatalf("%+v", v)
		}
	}

	// nodes.call reaches the program on a machine it runs on, not on another.
	out, err := h.nodesCall(ctx, "example-helper", json.RawMessage(`{"machine": "box", "method": "x.echo", "params": {"a": 1}}`))
	if err != nil || !strings.Contains(string(out.(json.RawMessage)), `"a":1`) {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := h.nodesCall(ctx, "example-helper", json.RawMessage(`{"machine": "other", "method": "x.echo"}`)); err == nil {
		t.Fatal("a machine it is not switched on for")
	}

	// Switched off for box: box is told to keep nothing of it (the package goes, its data stays: still installed).
	_ = reg.SetConfig(ctx, "example-helper", json.RawMessage(`{"computers": ["oldbox"]}`), "test")
	h.syncExtensionNodes(ctx, box)
	var last map[string]any
	for _, c := range boxSeen() {
		if c.Method == "plugin.node.sync" {
			last = c.Params
		}
	}
	if progs, _ := last["programs"].([]any); len(progs) != 0 || !strings.Contains(string(must(json.Marshal(last["installed"]))), "example-helper") {
		t.Fatalf("%+v", last)
	}
}

// extensionHTTPNode is a machine whose node answers plugin.node.dial by streaming to the program's HTTP server, here
// addr (a dev server standing in for it), and says which plugins it was asked for.
func extensionHTTPNode(t *testing.T, h *Manager, id, addr string) (*Machine, *[]string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	hubMux, nodeMux := streams.New(hubSide.Notify), streams.New(nodeSide.Notify)
	hubSide.OnEvent = func(e string, p json.RawMessage) { hubMux.Event(e, p) }
	nodeSide.OnEvent = func(e string, p json.RawMessage) { nodeMux.Event(e, p) }
	var mu sync.Mutex
	var asked []string
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "plugin.node.dial" {
			return map[string]any{}, nil
		}
		var p struct{ ID, Plugin string }
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		asked = append(asked, p.Plugin)
		mu.Unlock()
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return nil, &rpc.Error{Code: "not_running", Message: "not answering"}
		}
		if err := nodeMux.Open(p.ID); err != nil {
			c.Close()
			return nil, err
		}
		nodeMux.Start(p.ID, c)
		return map[string]any{}, nil
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); h2nW.Close(); n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online, m.streams, m.info = hubSide, true, hubMux, proto.NodeInfo{OS: "linux", Arch: "amd64"}
	h.fleetMu.Lock()
	h.machines, h.byID[id] = append(h.machines, m), m
	h.fleetMu.Unlock()
	return m, &asked
}

// An app's requests reach a plugin's program on the computer it runs on, through the hub: a page, a WebSocket both
// ways; not on a machine it is not switched on for, not while that machine is away, not without the plugin's http.
func TestAppsRequestsReachTheProgramOnAComputer(t *testing.T) {
	ctx := context.Background()
	h, _ := extensionTestHub(t)
	h.cfg.Token = "test-token"
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	dev, _ := b1DevServer(t)
	box, asked := extensionHTTPNode(t, h, "box", dev.Listener.Addr().String())
	extensionHTTPNode(t, h, "other", dev.Listener.Addr().String())

	pkg := helperPackage(t)
	raw, _ := os.ReadFile(filepath.Join(pkg, extension.ManifestFile))
	_ = os.WriteFile(filepath.Join(pkg, extension.ManifestFile), []byte(strings.Replace(string(raw), `"machines": "computers"`, `"machines": "computers", "http": true`, 1)), 0o644)
	reg := h.extensions.reg
	pv, err := reg.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetConfig(ctx, "example-helper", json.RawMessage(`{"computers": ["box"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/api/v1/plugins/example-helper/-/machines/"
	get := func(url string) (int, string) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer test-token")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get(base + "box/"); code != 200 || !strings.Contains(body, "<title>Dev</title>") || !strings.Contains(body, "host=plugin") {
		t.Fatalf("%d %q", code, body)
	}
	if len(*asked) == 0 || (*asked)[0] != "example-helper" {
		t.Fatalf("the node was asked for %v", *asked)
	}
	// The program is told who is calling, as an extension on the hub's machine is (extension_caller.go): the credential,
	// its role and language, nothing else the hub passed on (name, address, owner) or the client sent.
	phone := deviceToken("dev_phone", roleFull)
	for _, c := range []struct{ name, token, device string }{{"the master token", "test-token", ""}, {"a phone", phone, "dev_phone"}} {
		req, _ := http.NewRequest("GET", base+"box/caller", nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept-Language", "ja")
		req.Header.Set("X-Persona-Device", "someone-else")
		req.Header.Set("X-Persona-Forged", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		_ = json.NewDecoder(res.Body).Decode(&got)
		res.Body.Close()
		if res.StatusCode != 200 || len(got) != 3 || got[extwire.HeaderDevice] != c.device || got[extwire.HeaderRole] != roleFull ||
			got[extwire.HeaderLang] != "ja" {
			t.Fatalf("%s: %d %v", c.name, res.StatusCode, got)
		}
	}

	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(base, "http") + "box/hmr"
	c, _, err := websocket.Dial(wctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer test-token"}}})
	if err != nil {
		t.Fatalf("websocket to the program: %v", err)
	}
	defer c.CloseNow()
	if err := c.Write(wctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, got, err := c.Read(wctx); err != nil || string(got) != "echo:hi" {
		t.Fatalf("%q %v", got, err)
	}

	if code, _ := get(base + "other/"); code != http.StatusNotFound {
		t.Fatalf("a machine it is not switched on for: %d", code)
	}
	box.mu.Lock()
	box.online = false
	box.mu.Unlock()
	if code, _ := get(base + "box/"); code != http.StatusConflict {
		t.Fatalf("the machine away: %d", code)
	}
	if code, _ := get(srv.URL + "/api/v1/plugins/nobody/-/machines/box/"); code != http.StatusNotFound {
		t.Fatalf("no such plugin: %d", code)
	}
}

// The apps learn which field picks the computers, and switching one on asks first (a program will run there), unless
// the manifest asks something of its own; the plugin's own form stays as it is.
func TestNodeProgramsFieldAsksBeforeAMachine(t *testing.T) {
	ctx := context.Background()
	h, _ := extensionTestHub(t)
	reg := h.extensions.reg
	pv, err := reg.Preview(ctx, "dir:"+helperPackage(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	p := reg.Get("example-helper")
	d := h.extensions.detail(ctx, p)
	if d.NodeMachines != "computers" {
		t.Fatalf("node_machines %q", d.NodeMachines)
	}
	ask := func(s *extension.Schema) string {
		for _, f := range s.Fields {
			if f.Name == "computers" {
				return f.Confirm
			}
		}
		return "no field"
	}
	if got := ask(d.Form); !strings.Contains(got, "将在这台电脑上运行「Example Helper」的程序") {
		t.Fatalf("confirm %q", got)
	}
	if got := ask(p.Schema); got != "" {
		t.Fatalf("the plugin's own form changed: %q", got)
	}
}
