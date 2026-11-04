package hub

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// extensionNodeMachine is a machine whose node answers plugin.node.* as one that runs plugins' programs does (or, old,
// as one from before them), and says what it was asked.
func extensionNodeMachine(t *testing.T, h *Hub, id, goos, goarch string, old bool) (*Machine, func() []browserCallSeen) {
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
	h.mu.Lock()
	h.machines, h.byID[id] = append(h.machines, m), m
	h.mu.Unlock()
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
	box.onEvent("plugin.node.state", json.RawMessage(`{"id":"example-helper","version":"1.0.0","state":"lease_expired"}`))
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
