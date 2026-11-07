package extmgr

import (
	"context"
	"errors"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/quota"
)

// A launcher's quota at its gateway (docs/integrations/launchers.md "Quota") is read by the quota plugins (docs/extensions/extensions.md
// "Quota"). The route (GET /api/v1/launchers/{name}/quota) and its cache are the manager's, which has the launchers; it
// asks the hub's plugins to read it (plugins.quota) while the plugins are the hub's. A plugin never sees the key, nor
// does the hub: it says what to send, and whoever holds the key sends it — the manager for a launcher added in Persona
// (its sealed key), the machine for a ~/.zshrc one (node/launcherquota.go), so that key stays on that machine; the
// manager knows which (models.launcher_http).

// errNoGateway: the launcher has no gateway address of its own (it uses the official API), so there is no quota to read.
var errNoGateway = i18n.Err("hub-launcherquota.no_gateway")

var errNoLauncher = errors.New("no such launcher")

// launcherGatewayInfo is where a launcher's gateway is and how to send to it with the key (the plugins' launcher
// sections, the quota), as the manager says: machine picks which machine has a ~/.zshrc one send.
type launcherGatewayInfo struct {
	base    string
	machine string
	send    quotaSender
}

func (h *Manager) launcherGateway(ctx context.Context, name, machine string) (launcherGatewayInfo, error) {
	var out launcherGatewayInfo
	var gw struct {
		BaseURL string `json:"base_url"`
		Machine string `json:"machine"`
	}
	if err := h.launcherCall(ctx, map[string]any{"name": name, "machine": machine}, &gw); err != nil {
		return out, err
	}
	out.base, out.machine = gw.BaseURL, gw.Machine
	out.send = h.launcherSender(name, gw.Machine)
	return out, nil
}

// launcherSender sends a plugin's request to launcher name's gateway with its key, through the manager, on machine
// (the one the gateway was read on).
func (h *Manager) launcherSender(name, machine string) quotaSender {
	return func(ctx context.Context, r quota.Request) (quota.Response, error) {
		var resp quota.Response
		err := h.launcherCall(ctx, map[string]any{"name": name, "machine": machine, "http": r}, &resp)
		return resp, err
	}
}
