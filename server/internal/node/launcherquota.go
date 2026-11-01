package node

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/chyroc/jarvis/server/internal/quota"
)

// launcherQuota reads a ~/.zshrc launcher's quota at its gateway (relay-history, FAKEMODEL): the probe gives the base URL
// and the key it sets, this machine asks the gateway (the address is the one this machine reaches it by), and only
// the report leaves the machine, never the key.
// tz: the user's time zone, from the hub ("" when it does not know it).
func (n *Node) launcherQuota(ctx context.Context, name, tz string) (quota.Report, error) {
	p, err := n.launcherProbe(ctx, name)
	if err != nil {
		return quota.Report{}, err
	}
	base := firstNonEmpty(p.Env["ANTHROPIC_BASE_URL"], p.Env["OPENAI_BASE_URL"])
	key := firstNonEmpty(p.Env["ANTHROPIC_AUTH_TOKEN"], p.Env["ANTHROPIC_API_KEY"], p.Env["OPENAI_API_KEY"])
	if base == "" {
		return quota.Report{}, errors.New("这个启动器没有设网关地址（用的是默认的官方接口）")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return quota.Fetch(ctx, &http.Client{Timeout: 25 * time.Second}, base, key, tz)
}
