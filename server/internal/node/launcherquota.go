package node

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/quota"
)

// launcherHTTP is how a quota plugin on the hub reads a ~/.zshrc launcher's quota (docs/extensions/extensions.md "Quota"): the probe
// gives the gateway and the key the launcher sets; this machine sends the plugin's request to that gateway only (the
// address is the one this machine reaches it by) with the key put in for ${launcher_key}, and answers with the key
// taken out. The key never leaves the machine and the plugin never sees it. Without a request: the gateway's address,
// for the plugin to know which gateway it is asked about.
func (n *Node) launcherHTTP(ctx context.Context, name string, req *quota.Request) (any, error) {
	p, err := n.launcherProbe(ctx, name)
	if err != nil {
		return nil, err
	}
	base := firstNonEmpty(p.Env["ANTHROPIC_BASE_URL"], p.Env["OPENAI_BASE_URL"])
	key := firstNonEmpty(p.Env["ANTHROPIC_AUTH_TOKEN"], p.Env["ANTHROPIC_API_KEY"], p.Env["OPENAI_API_KEY"])
	if base == "" {
		return nil, errors.New("这个启动器没有设网关地址（用的是默认的官方接口）")
	}
	if req == nil {
		root, err := quota.Root(base)
		if err != nil {
			return nil, err
		}
		return map[string]any{"base_url": root.String(), "has_key": key != ""}, nil
	}
	if key == "" {
		return nil, errors.New("这个启动器没有 Key")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return quota.Do(ctx, &http.Client{Timeout: 25 * time.Second}, base, key, *req)
}
