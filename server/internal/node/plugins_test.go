package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/proto"
)

// runTestPlugin is the test binary as a plugin's program on a computer (NODE_TEST_PLUGIN, from TestMain):
// demo.echo, demo.secrets (what initialize brought), demo.env; "broken" exits at once.
func runTestPlugin(mode string) {
	if mode == "broken" {
		os.Exit(3)
	}
	var init extension.InitializeParams
	_ = serve.Serve(context.Background(), os.Stdin, os.Stdout, serve.Program{ID: "notes", Version: os.Getenv("NODE_TEST_VERSION"),
		Init: func(_ context.Context, p extension.InitializeParams, h *serve.Hub) (map[string]any, error) {
			init = p
			_ = h.Notify("notes.ready", map[string]string{"dir": p.Extension.Dir})
			return nil, nil
		},
		Namespaces: map[string]serve.Handler{"demo": func(_ context.Context, method string, params json.RawMessage) (any, error) {
			switch method {
			case "echo":
				return params, nil
			case "secrets":
				return init.Secrets, nil
			case "env":
				return map[string]string{"data": os.Getenv("PERSONA_PLUGIN_DATA"), "hub": os.Getenv("PERSONA_HUB_TOKEN"), "mode": os.Getenv("NODE_TEST_PLUGIN")}, nil
			}
			return nil, nil
		}}})
	os.Exit(0)
}

// pluginTestNode is a node with its own directory, its events kept.
func pluginTestNode(t *testing.T) (*Node, *[]string) {
	t.Helper()
	n := &Node{home: t.TempDir(), logger: log.New(os.Stderr, "", 0)}
	n.dir = filepath.Join(n.home, ".persona")
	var events []string
	ph := n.plugins()
	ph.mu.Lock()
	ph.notify = func(event string, params any) {
		b, _ := json.Marshal(params)
		events = append(events, event+" "+string(b))
	}
	ph.mu.Unlock()
	t.Cleanup(func() {
		for _, id := range []string{"notes"} {
			ph.stop(id)
		}
		pluginHosts.Lock()
		delete(pluginHosts.byDir, n.dir)
		pluginHosts.Unlock()
	})
	return n, &events
}

// testPackage is a plugin package for this test's program, as the hub sends it: a script that runs the test binary.
func testPackage(t *testing.T, version string) (proto.PluginNodeProgram, []byte) {
	t.Helper()
	root := t.TempDir()
	bin, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(root, "node"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "node", "helper"), []byte("#!/bin/sh\nexec '"+bin+"' \"$@\"\n"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "README"), []byte("notes "+version), 0o644)
	m := &extension.Manifest{ID: "notes", Node: &extension.NodeRuntime{Command: extension.Command{ByPlatform: map[string]string{"darwin-arm64": "node/helper", "linux-amd64": "node/helper"}}}}
	var buf bytes.Buffer
	if err := extension.NodePackage(m, root, "linux-amd64", &buf); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return proto.PluginNodeProgram{ID: "notes", Version: version, Digest: hex.EncodeToString(sum[:])}, buf.Bytes()
}

func put(t *testing.T, n *Node, prog proto.PluginNodeProgram, pkg []byte) error {
	t.Helper()
	ctx := context.Background()
	call := func(method string, params any) (map[string]any, error) {
		b, _ := json.Marshal(params)
		res, err := n.handle(ctx, method, b)
		out := map[string]any{}
		rb, _ := json.Marshal(res)
		_ = json.Unmarshal(rb, &out)
		return out, err
	}
	begun, err := call("plugin.node.put.begin", proto.PluginNodePutBegin{PluginNodeProgram: prog, Size: int64(len(pkg))})
	if err != nil {
		return err
	}
	id, _ := begun["upload_id"].(string)
	for off := 0; off < len(pkg); off += 1000 {
		end := min(off+1000, len(pkg))
		if _, err := call("plugin.node.put.chunk", proto.PluginNodePutChunk{UploadID: id, Offset: int64(off), Data: pkg[off:end]}); err != nil {
			return err
		}
	}
	_, err = call("plugin.node.put.commit", map[string]string{"upload_id": id})
	return err
}

func startParams(version, mode string) proto.PluginNodeStart {
	init, _ := json.Marshal(extension.InitializeParams{Extension: extension.ExtensionInfo{ID: "notes", Version: version}, Secrets: map[string]string{"api_key": "k-1"}})
	return proto.PluginNodeStart{ID: "notes", Name: "Notes", Version: version, Command: "node/helper", RPC: true, Keep: extension.KeepLease,
		LeaseSeconds: 1800, Env: map[string]string{"NODE_TEST_PLUGIN": mode, "NODE_TEST_VERSION": version, "PERSONA_HUB_TOKEN": "no"}, Init: init}
}

func pluginCall(t *testing.T, n *Node, method string, params any) (any, error) {
	t.Helper()
	b, _ := json.Marshal(params)
	return n.handle(context.Background(), method, b)
}

// A package arrives in chunks and is checked; the program starts with its secrets in initialize, answers calls,
// tells the hub its events; the same start again changes nothing; it stops when the hub's lease runs out.
func TestPluginProgramOnThisComputer(t *testing.T) {
	n, events := pluginTestNode(t)
	prog, pkg := testPackage(t, "1.0.0")

	if res, _ := pluginCall(t, n, "plugin.node.have", prog); res.(proto.PluginNodeHave).Have {
		t.Fatal("has it before it came")
	}
	bad := prog
	bad.Digest = strings.Repeat("0", 64)
	if err := put(t, n, bad, pkg); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("a package that is not the one said: %v", err)
	}
	if err := put(t, n, prog, pkg); err != nil {
		t.Fatal(err)
	}
	if res, _ := pluginCall(t, n, "plugin.node.have", prog); !res.(proto.PluginNodeHave).Have {
		t.Fatal("no package after it came")
	}
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes", "1.0.0", "README")); err != nil {
		t.Fatal(err)
	}

	res, err := pluginCall(t, n, "plugin.node.start", startParams("1.0.0", "rpc"))
	if err != nil {
		t.Fatal(err)
	}
	if st := res.(proto.PluginNodeState); st.State != "running" {
		t.Fatalf("%+v", st)
	}
	out, err := pluginCall(t, n, "plugin.node.call", proto.PluginNodeCall{ID: "notes", Method: "demo.secrets"})
	if err != nil || !strings.Contains(string(out.(json.RawMessage)), `"api_key":"k-1"`) {
		t.Fatalf("%s %v", out, err)
	}
	out, _ = pluginCall(t, n, "plugin.node.call", proto.PluginNodeCall{ID: "notes", Method: "demo.env"})
	var env map[string]string
	_ = json.Unmarshal(out.(json.RawMessage), &env)
	if env["hub"] != "" || env["data"] != filepath.Join(n.dir, nodePluginsDir, "notes", "data") || env["mode"] != "rpc" {
		t.Fatalf("no hub token, its data directory, its own variables: %v", env)
	}
	waitFor(t, "the program's event", func() bool {
		return strings.Contains(strings.Join(*events, "\n"), `plugin.node.event {"id":"notes","event":"notes.ready"`)
	})

	// The same start (another lease) leaves it running: the same process.
	ph := n.plugins()
	ph.mu.Lock()
	first := ph.running["notes"].proc
	ph.mu.Unlock()
	again := startParams("1.0.0", "rpc")
	again.LeaseSeconds = 60
	if _, err := pluginCall(t, n, "plugin.node.start", again); err != nil {
		t.Fatal(err)
	}
	ph.mu.Lock()
	same := ph.running["notes"].proc == first
	ph.mu.Unlock()
	if !same {
		t.Fatal("restarted for a renewal")
	}

	// The hub is gone longer than the lease: stopped, told.
	ph.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	ph.expireLeases()
	ph.now = time.Now
	if st := ph.status().Programs; len(st) != 1 || st[0].State != pluginLeaseExpired {
		t.Fatalf("%+v", st)
	}
	if _, err := pluginCall(t, n, "plugin.node.call", proto.PluginNodeCall{ID: "notes", Method: "demo.echo"}); err == nil {
		t.Fatal("a call to a stopped program")
	}
	// The hub is back: it starts it again.
	if res, _ := pluginCall(t, n, "plugin.node.start", startParams("1.0.0", "rpc")); res.(proto.PluginNodeState).State != "running" {
		t.Fatalf("%+v", res)
	}
}

// An upgrade that does not come up falls back to the version before it; sync stops and removes what should not run,
// keeping one version back, and the data of a plugin still installed.
func TestPluginProgramUpgradeRollbackAndSync(t *testing.T) {
	n, _ := pluginTestNode(t)
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		prog, pkg := testPackage(t, v)
		if err := put(t, n, prog, pkg); err != nil {
			t.Fatal(err)
		}
	}
	if res, _ := pluginCall(t, n, "plugin.node.start", startParams("1.1.0", "rpc")); res.(proto.PluginNodeState).State != "running" {
		t.Fatalf("%+v", res)
	}
	_, err := pluginCall(t, n, "plugin.node.start", startParams("2.0.0", "broken"))
	if err == nil || !strings.Contains(err.Error(), "退回了 1.1.0") {
		t.Fatalf("a version that does not come up: %v", err)
	}
	st := n.plugins().status().Programs
	if len(st) != 1 || st[0].Version != "1.1.0" {
		t.Fatalf("back on 1.1.0: %+v", st)
	}
	if _, err := pluginCall(t, n, "plugin.node.call", proto.PluginNodeCall{ID: "notes", Method: "demo.echo", Params: json.RawMessage(`"hi"`)}); err != nil {
		t.Fatal(err)
	}

	// Should run: 2.0.0's version (as the hub says); 1.1.0 stays as the one to fall back to, 1.0.0 goes.
	_ = os.WriteFile(filepath.Join(n.dir, nodePluginsDir, "notes", "data", "kept"), []byte("x"), 0o600)
	pluginCall(t, n, "plugin.node.sync", proto.PluginNodeSync{Programs: []proto.PluginNodeProgram{{ID: "notes", Version: "2.0.0"}}, Installed: []string{"notes"}})
	if st := n.plugins().status().Programs; len(st) != 0 {
		t.Fatalf("1.1.0 runs on though the hub wants 2.0.0: %+v", st)
	}
	for v, want := range map[string]bool{"1.0.0": false, "1.1.0": true, "2.0.0": true} {
		if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes", v)); (err == nil) != want {
			t.Errorf("version %s there: %v", v, err == nil)
		}
	}
	// Switched off here, still installed: packages go, data stays; not installed any more: all of it goes.
	pluginCall(t, n, "plugin.node.sync", proto.PluginNodeSync{Programs: []proto.PluginNodeProgram{}, Installed: []string{"notes"}})
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes", "data", "kept")); err != nil {
		t.Fatal("the data of a plugin still installed:", err)
	}
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes", "2.0.0")); err == nil {
		t.Fatal("a package of a plugin switched off here")
	}
	pluginCall(t, n, "plugin.node.sync", proto.PluginNodeSync{Programs: []proto.PluginNodeProgram{}, Installed: []string{}})
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes")); err == nil {
		t.Fatal("a plugin not installed any more left something")
	}
}

// On the hub's machine the node shares the persona dir with the hub (#1933): its sync leaves the hub's plugins/ alone
// (installed/, data/, staging/, markets/, node-packages/, the sockets, and what a #1921 node left there), and in its
// own directory removes only what it made: not a directory without its mark, nor one named as the hub's.
func TestPluginSyncLeavesTheHubsPluginsAlone(t *testing.T) {
	n, _ := pluginTestNode(t)
	hubs := []string{
		"plugins/installed/example-chat/0.5.2/persona-plugin.json",
		"plugins/installed/example-notes/0.1.2/bin/example-notes-linux-amd64",
		"plugins/data/example-chat/cache",
		"plugins/staging/0123456789ab/pkg/persona-plugin.json",
		"plugins/markets/0123456789/persona-extensions.json",
		"plugins/node-packages/notes/1.0.0-linux-amd64.tgz",
		"plugins/example-chat.sock",
		"plugins/notes/1.0.0/.persona-digest",  // a #1921 node's package, where it put them
		nodePluginsDir + "/stray/1.0.0/README", // made by something else: no mark
		nodePluginsDir + "/installed/" + pluginOwnMark,
	}
	for _, f := range hubs {
		p := filepath.Join(n.dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prog, pkg := testPackage(t, "1.0.0")
	if err := put(t, n, prog, pkg); err != nil {
		t.Fatal(err)
	}
	if res, _ := pluginCall(t, n, "plugin.node.start", startParams("1.0.0", "rpc")); res.(proto.PluginNodeState).State != "running" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes", pluginOwnMark)); err != nil {
		t.Fatal("the node's own directory is not marked:", err)
	}
	for _, installed := range [][]string{{"notes", "example-chat"}, {}} { // switched off here, then uninstalled
		pluginCall(t, n, "plugin.node.sync", proto.PluginNodeSync{Programs: []proto.PluginNodeProgram{}, Installed: installed})
	}
	for _, f := range hubs {
		if _, err := os.Stat(filepath.Join(n.dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(n.dir, nodePluginsDir, "notes")); err == nil {
		t.Fatal("the node's own plugin, uninstalled, stayed")
	}
}

// What the hub may not say: a start without the package, a bad keep, a command out of the package, a bad id.
func TestPluginProgramRefuses(t *testing.T) {
	n, _ := pluginTestNode(t)
	if res, _ := pluginCall(t, n, "plugin.node.start", startParams("1.0.0", "rpc")); res.(proto.PluginNodeState).State != pluginStateMissing {
		t.Fatalf("no package: %+v", res)
	}
	prog, pkg := testPackage(t, "1.0.0")
	if err := put(t, n, prog, pkg); err != nil {
		t.Fatal(err)
	}
	s := startParams("1.0.0", "rpc")
	s.Keep = "forever"
	if _, err := pluginCall(t, n, "plugin.node.start", s); err == nil {
		t.Fatal("keep forever")
	}
	s = startParams("1.0.0", "rpc")
	s.Command = "../../../bin/sh"
	if res, _ := pluginCall(t, n, "plugin.node.start", s); res.(proto.PluginNodeState).State != "error" {
		t.Fatalf("a command outside the package: %+v", res)
	}
	if err := put(t, n, proto.PluginNodeProgram{ID: "../x", Version: "1.0.0", Digest: prog.Digest}, pkg); err == nil {
		t.Fatal("an id out of the plugins' directory")
	}
}
