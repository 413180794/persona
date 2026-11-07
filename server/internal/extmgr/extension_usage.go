package extmgr

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// usage.gateway_accounts (#4576, docs/extensions/extensions.md"Quota", docs/server/usage-windows.md"Gateway account inventory"): the account
// lists of the account-pool gateways among the hub's usage sources, for an extension granted usage_gateways (a relay's quota
// extension lists on its sidebar page every account of the gateway, not only those one launcher's
// key reaches). The devices in this process keep the lists from their last read of each source; nothing is asked of a
// gateway here, and no key or address is in the answer.

// gatewayInventory is the devices' side (devices.Devices.Request).
type gatewayInventory interface {
	Request(ctx context.Context, method string, params json.RawMessage) (any, bool, error)
}

func (h *Manager) usageGatewayAccounts(ctx context.Context, id string) (any, error) {
	p := h.extensions.reg.Get(id)
	if p == nil || !p.Record.Granted.UsageGateways {
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: "usage.gateway_accounts needs the permission usage_gateways"}
	}
	none := proto.GatewayAccounts{Sources: []proto.GatewaySource{}, Accounts: []proto.GatewayAccount{}}
	d, ok := h.opts.Devices.(gatewayInventory)
	if !ok {
		return none, nil
	}
	out, handled, err := d.Request(ctx, devicewire.MethodUsageGatewayAccounts, json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}
	if !handled || out == nil {
		return none, nil
	}
	return out, nil
}
