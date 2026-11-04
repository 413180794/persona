package hub

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/plugin/conformance"
	"github.com/nwverse/persona/server/internal/wechat"
	wechatspec "github.com/nwverse/persona/server/internal/wechat/spec"
)

// The WeChat plugin as the hub registers it: its connector (a status and a QR sign-in) known before the program runs,
// the agents' events it hears, and the hub's API it may use.
func TestWeChatBuiltin(t *testing.T) {
	h := newTestHub(t)
	reg := plugin.NewRegistry(t.TempDir(), nil, nil, 0)
	if err := reg.AddBuiltin(wechatManifest(h, "/opt/persona/jarvisd-wechat"), "/opt/persona", true); err != nil {
		t.Fatal(err)
	}
	if err := reg.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := reg.Get(wechatspec.ID)
	if p == nil || p.Problem != "" || !p.Record.Enabled || p.Record.Source != plugin.SourceBuiltin {
		t.Fatalf("%+v", p)
	}
	conns := p.Manifest.Connectors()
	if len(conns) != 1 || conns[0].ID != "wechat" || !slices.Equal(conns[0].Capabilities, []string{"status", "login"}) {
		t.Fatalf("connectors %+v", conns)
	}
	if got := p.Manifest.Topics(); !slices.Equal(got, []string{"agent.finished", "agent.needs_you"}) {
		t.Fatalf("topics %v", got)
	}
	if !p.Record.Granted.HubAPI || !slices.Equal(p.Record.Granted.Network, []string{"*.weixin.qq.com"}) {
		t.Fatalf("granted %+v", p.Record.Granted)
	}
	if filepath.Base(p.Manifest.Runtime.Command.Path) != "jarvisd-wechat" {
		t.Fatalf("runtime %+v", p.Manifest.Runtime)
	}
}

// jarvisd-wechat's program, held to the plugin protocol against the manifest the hub registers it with.
func TestWeChatProgramIsAPlugin(t *testing.T) {
	h := newTestHub(t)
	m := wechatManifest(h, "/opt/persona/jarvisd-wechat")
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- serve.Main(context.Background(), progR, progW, func(_ json.RawMessage, s connector.Secrets) (connector.Connector, error) {
			return wechat.New(wechat.Options{Hub: wechat.NewHub("http://127.0.0.1:1", "tok")}, s), nil
		})
		_ = progW.Close()
	}()
	res := conformance.Check(t, m, conformance.Run{ToProgram: hubW, FromProgram: hubR, Exited: exited},
		plugin.InitializeParams{Plugin: plugin.PluginInfo{ID: m.ID, Version: m.Version}, Active: map[string][]string{"connectors": {"wechat"}}})
	var items []plugin.ConnectorItem
	_ = json.Unmarshal(res.Contributes["connectors"], &items)
	if len(items) != 1 || items[0].ID != "wechat" {
		t.Fatalf("%+v", res)
	}
}
