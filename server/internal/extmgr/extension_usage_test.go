package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// inventoryDevices is the devices with gateway account lists (devices.Devices.Request's usage_gateway_accounts).
type inventoryDevices struct {
	asked []string
	list  proto.GatewayAccounts
}

func TestUsageWindowsRequiresExplicitPermission(t *testing.T) {
	h := newTestManager(t)
	devs := &windowDevices{}
	h.opts.Devices = devs
	granted := kbExtensionSpec(t, h, "windows-reader", extension.Permissions{UsageWindows: true})
	gateway := kbExtensionSpec(t, h, "gateway-reader", extension.Permissions{UsageGateways: true})
	full := kbExtensionSpec(t, h, "full-reader", extension.Permissions{HubAPI: true})
	for _, denied := range []struct {
		name string
		call func(context.Context, string, json.RawMessage) (any, error)
	}{{"gateway", gateway.Handle}, {"full", full.Handle}} {
		_, err := denied.call(context.Background(), "usage.windows", json.RawMessage(`{}`))
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != extension.CodePermissionDenied || len(devs.asked) != 0 {
			t.Fatalf("%s: %v, asked %v", denied.name, err, devs.asked)
		}
	}
	out, err := granted.Handle(context.Background(), "usage.windows", json.RawMessage(`{}`))
	if err != nil || len(devs.asked) != 1 || devs.asked[0] != devicewire.MethodUsageWindows {
		t.Fatalf("%v %+v", err, devs.asked)
	}
	b, _ := json.Marshal(out)
	if string(b) != `{"accounts":[{"provider":"agy"},{"provider":"codex"}]}` {
		t.Fatalf("%s", b)
	}
	h.opts.Devices = nil
	_, err = granted.Handle(context.Background(), "usage.windows", json.RawMessage(`{}`))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != extension.CodeUnknownMethod {
		t.Fatalf("missing device method: %v", err)
	}
}

type windowDevices struct{ asked []string }

func (d *windowDevices) Has(string) bool { return false }
func (d *windowDevices) CallNode(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, errOffline
}
func (d *windowDevices) Request(_ context.Context, method string, _ json.RawMessage) (any, bool, error) {
	d.asked = append(d.asked, method)
	return map[string]any{"accounts": []map[string]string{{"provider": "agy"}, {"provider": "codex"}}}, true, nil
}

func (d *inventoryDevices) Has(string) bool { return false }
func (d *inventoryDevices) CallNode(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, errOffline
}
func (d *inventoryDevices) Request(_ context.Context, method string, _ json.RawMessage) (any, bool, error) {
	d.asked = append(d.asked, method)
	if method != devicewire.MethodUsageGatewayAccounts {
		return nil, false, nil
	}
	return d.list, true, nil
}

func TestUsageGatewayAccountsNeedsItsPermission(t *testing.T) {
	h := newTestManager(t)
	devs := &inventoryDevices{list: proto.GatewayAccounts{
		Sources:  []proto.GatewaySource{{ID: "s1", Name: "Relay", Type: "relay", Listed: 1, Complete: true}},
		Accounts: []proto.GatewayAccount{{Source: "s1", AccountID: "k1", Provider: "kiro", Status: "disabled", Stale: true, Windows: []proto.GatewayWindow{}}},
	}}
	h.opts.Devices = devs
	granted := kbExtensionSpec(t, h, "example-usage", extension.Permissions{UsageGateways: true})
	keys := kbExtensionSpec(t, h, "example-keys", extension.Permissions{LauncherKeys: true})

	_, err := keys.Handle(context.Background(), "usage.gateway_accounts", json.RawMessage(`{}`))
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != extension.CodePermissionDenied || len(devs.asked) != 0 {
		t.Fatalf("launcher_keys alone: %v (asked %v)", err, devs.asked)
	}
	out, err := granted.Handle(context.Background(), "usage.gateway_accounts", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var got proto.GatewayAccounts
	if err := json.Unmarshal(b, &got); err != nil || len(got.Sources) != 1 || len(got.Accounts) != 1 || got.Accounts[0].AccountID != "k1" {
		t.Fatalf("%v %s", err, b)
	}
	// devices without the method (an older manager's, tests): no sources, not an error
	h.opts.Devices = nil
	out, err = granted.Handle(context.Background(), "usage.gateway_accounts", json.RawMessage(`{}`))
	if b, _ := json.Marshal(out); err != nil || string(b) != `{"sources":[],"accounts":[]}` {
		t.Fatalf("%v %s", err, b)
	}
}
