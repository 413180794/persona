package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/channel"
	chconf "github.com/nwverse/persona/server/internal/channel/conformance"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extension/conformance"
	"github.com/nwverse/persona/server/internal/wechat"
	wechatspec "github.com/nwverse/persona/server/internal/wechat/spec"
)

// The WeChat plugin as the hub registers it: its connector (a status and a QR sign-in) known before the program runs
// and its channel; no hub token and no events any more (the hub's pipeline does that part).
func TestWeChatBuiltin(t *testing.T) {
	h := newTestManager(t)
	reg := extension.NewRegistry(t.TempDir(), nil, nil, 0)
	if err := reg.AddBuiltin(wechatManifest(h, "/opt/persona/jarvisd-wechat"), "/opt/persona", true); err != nil {
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
	if filepath.Base(p.Manifest.Runtime.Command.Path) != "jarvisd-wechat" {
		t.Fatalf("runtime %+v", p.Manifest.Runtime)
	}
}

// jarvisd-wechat's program, held to the plugin protocol against the manifest the hub registers it with.
func TestWeChatProgramIsAnExtension(t *testing.T) {
	h := newTestManager(t)
	m := wechatManifest(h, "/opt/persona/jarvisd-wechat")
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- serve.MainWith(context.Background(), progR, progW, func(init serve.Init, s connector.Secrets) (connector.Connector, error) {
			return wechat.New(wechat.Options{Hub: init.Channel}, s), nil
		})
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

// jarvisd-wechat's program as a channel (docs/channels.md "Protocol"): not bound, it has nowhere to send to yet.
func TestWeChatProgramIsAChannel(t *testing.T) {
	h := newTestManager(t)
	m := wechatManifest(h, "/opt/persona/jarvisd-wechat")
	progR, hubW := io.Pipe()
	hubR, progW := io.Pipe()
	exited := make(chan error, 1)
	go func() {
		exited <- serve.MainWith(context.Background(), progR, progW, func(init serve.Init, s connector.Secrets) (connector.Connector, error) {
			return wechat.New(wechat.Options{Hub: init.Channel}, s), nil
		})
		_ = progW.Close()
	}()
	chconf.Check(t, m, conformance.Run{ToProgram: hubW, FromProgram: hubR, Exited: exited},
		extension.InitializeParams{Active: map[string][]string{"connectors": {"wechat"}, "channel": {"wechat"}}})
}
