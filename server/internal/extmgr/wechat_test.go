package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/nwverse/persona/sdk/go/ext"
	"github.com/nwverse/persona/server/internal/channel"
	chconf "github.com/nwverse/persona/server/internal/channel/conformance"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/conformance"
	"github.com/nwverse/persona/server/internal/update"
	wechatspec "github.com/nwverse/persona/server/internal/wechat/spec"
	"github.com/nwverse/persona/server/plugins/wechat"
)

// The WeChat plugin as the hub registers it: its connector (a status and a QR sign-in) known before the program runs
// and its channel; no hub token and no events any more (the hub's pipeline does that part).
func TestWeChatBuiltin(t *testing.T) {
	h := newTestManager(t)
	reg := extension.NewRegistry(t.TempDir(), nil, nil, 0)
	if err := reg.AddBuiltin(wechatManifest(h, "/opt/persona/personad-wechat"), "/opt/persona", true); err != nil {
		t.Fatal(err)
	}
	if err := reg.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := reg.Get(wechatspec.ID)
	if p == nil || p.Problem != "" || !p.Record.Enabled || p.Record.Source != extension.SourceBuiltin {
		t.Fatalf("%+v", p)
	}
	conns := p.Manifest.Connectors()
	if len(conns) != 1 || conns[0].ID != "wechat" || !slices.Equal(conns[0].Capabilities, []string{"status", "login"}) {
		t.Fatalf("connectors %+v", conns)
	}
	if got := p.Manifest.Topics(); len(got) != 0 {
		t.Fatalf("topics %v", got)
	}
	chans := p.Manifest.Channels()
	if len(chans) != 1 || chans[0].ID != "wechat" || chans[0].Connector != "wechat" || chans[0].Capabilities.Asks != channel.AsksText ||
		chans[0].Capabilities.Format != channel.FormatPlain || chans[0].Capabilities.MaxText != 3500 {
		t.Fatalf("channel %+v", chans)
	}
	if p.Record.Granted.HubAPI || !slices.Equal(p.Record.Granted.Network, []string{"*.weixin.qq.com"}) {
		t.Fatalf("granted %+v", p.Record.Granted)
	}
	if filepath.Base(p.Manifest.Runtime.Command.Path) != "personad-wechat" {
		t.Fatalf("runtime %+v", p.Manifest.Runtime)
	}
}

// personad-wechat's program (server/plugins/wechat, on the SDK alone), held to the plugin protocol against the manifest the hub registers it with.
func TestWeChatProgramIsAnExtension(t *testing.T) {
	h := newTestManager(t)
	m := wechatManifest(h, "/opt/persona/personad-wechat")
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- ext.Serve(context.Background(), progR, progW, wechat.Program(m.Version))
		_ = progW.Close()
	}()
	res := conformance.Check(t, m, conformance.Run{ToProgram: hubW, FromProgram: hubR, Exited: exited},
		extension.InitializeParams{Extension: extension.ExtensionInfo{ID: m.ID, Version: m.Version}, Active: map[string][]string{"connectors": {"wechat"}, "channel": {"wechat"}}})
	var items []extension.ConnectorItem
	_ = json.Unmarshal(res.Contributes["connectors"], &items)
	if len(items) != 1 || items[0].ID != "wechat" {
		t.Fatalf("%+v", res)
	}
}

// personad-wechat's program as a channel (docs/channels.md "Protocol"): not bound, it has nowhere to send to yet.
func TestWeChatProgramIsAChannel(t *testing.T) {
	h := newTestManager(t)
	m := wechatManifest(h, "/opt/persona/personad-wechat")
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- ext.Serve(context.Background(), progR, progW, wechat.Program(m.Version))
		_ = progW.Close()
	}()
	chconf.Check(t, m, conformance.Run{ToProgram: hubW, FromProgram: hubR, Exited: exited},
		extension.InitializeParams{Active: map[string][]string{"connectors": {"wechat"}, "channel": {"wechat"}}})
}

// The plugin declares what the hub registers it with (the hub knows it before the program runs; the program, built
// on the SDK alone, cannot import the hub's spec): the same connector, channel, permissions and description.
func TestWeChatPluginDeclaresWhatTheHubRegisters(t *testing.T) {
	h := newTestManager(t)
	hub := wechatManifest(h, "/opt/persona/personad-wechat")
	raw, err := json.Marshal(wechat.Manifest("1.0.0", "personad-wechat")) // the hub's own version is a build's
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := extension.Parse(raw)
	if err != nil {
		t.Fatalf("the plugin's manifest does not install: %v", err)
	}
	same := func(what string, a, b any) {
		t.Helper()
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Errorf("%s: the hub registers %s, the plugin declares %s", what, ja, jb)
		}
	}
	same("connectors", hub.Connectors(), plugin.Connectors())
	same("channel", hub.Channels(), plugin.Channels())
	same("permissions", hub.Permissions, plugin.Permissions)
	same("id, name, description", []string{hub.ID, hub.Name, hub.Description}, []string{plugin.ID, plugin.Name, plugin.Description})
	same("runtime", hub.Runtime, plugin.Runtime)
	if wechatspec.MaxText != wechat.MaxText || wechatspec.ID != wechat.ID || wechatspec.Name != wechat.Name {
		t.Error("spec and plugin disagree on id, name or max text")
	}
}

// The program's `version -v` says what personad's does for the same build, so the hub runs it.
func TestWeChatBuildLineIsPersonads(t *testing.T) {
	for _, c := range [][4]string{
		{"dev", "", "", ""}, {"v1", "release", "202611061230", "https://dl.example.com"}, {"v1", "release", "bad", ""},
		{"v1", "release", "202613061230", ""}, {"v1", "docker", "202611061230", ""}, {"v1", "nightly", "202611061230", ""},
	} {
		b := update.Make(c[0], c[1], c[2], c[3])
		want := "channel " + b.Channel + ", build " + strconv.FormatInt(b.Number, 10)
		if got := (wechat.Build{Version: c[0], Channel: c[1], Number: c[2], UpdateURL: c[3]}).Line(); got != want {
			t.Errorf("%v: the plugin says %q, personad %q", c, got, want)
		}
	}
}
