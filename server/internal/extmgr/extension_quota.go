package extmgr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The quota extension point (docs/extensions/extensions.md "Quota"): a plugin contributing quota answers quota.query with a
// quota.Report, sending what it needs to the launcher's gateway through quota.http. Those requests are only taken
// while the query runs, from the plugin it was asked of, and go out through whoever holds the key (quotaSender).

// quotaSender sends a plugin's request with the launcher's key (quota.Do on the hub, launchers.http on a machine).
type quotaSender func(ctx context.Context, r quota.Request) (quota.Response, error)

// quotaRequestsMost is how many requests one query may send.
const quotaRequestsMost = 16

type quotaQuery struct {
	extension string
	send      quotaSender
	mu        sync.Mutex
	sent      int
}

var quotaQueries sync.Map // query id → *quotaQuery

// hostMatches says whether host is one of patterns (an exact name or *.suffix).
func hostMatches(host string, patterns []string) bool {
	host = strings.ToLower(host)
	for _, p := range patterns {
		if p == host || strings.HasPrefix(p, "*.") && strings.HasSuffix(host, p[1:]) {
			return true
		}
	}
	return false
}

// quota asks the quota plugins about the gateway at base in turn — those naming its host first, then those that read
// any — until one answers; quota.ErrUnsupported when none does.
func (ps *extensions) quota(ctx context.Context, base, tz string, send quotaSender) (quota.Report, error) {
	if ps == nil || send == nil {
		return quota.Report{}, quota.ErrUnsupported
	}
	root, err := quota.Root(base)
	if err != nil {
		return quota.Report{}, err
	}
	type candidate struct {
		hp   *hubExtension
		item extension.QuotaItem
	}
	var named, any []candidate
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" || !p.Record.Granted.LauncherKeys {
			continue
		}
		hp := ps.get(p.Record.ID)
		if hp == nil || hp.proc == nil {
			continue
		}
		for _, q := range p.Manifest.Quotas() {
			switch {
			case len(q.Hosts) == 0:
				any = append(any, candidate{hp, q})
			case hostMatches(root.Hostname(), q.Hosts):
				named = append(named, candidate{hp, q})
			}
		}
	}
	for _, c := range append(named, any...) {
		var id [12]byte
		_, _ = rand.Read(id[:])
		qid := hex.EncodeToString(id[:])
		quotaQueries.Store(qid, &quotaQuery{extension: c.hp.id, send: send})
		params, _ := extension.Targeted(map[string]string{"query_id": qid, "base_url": root.String(), "tz": tz}, c.item.ID)
		var res struct {
			Report quota.Report `json:"report"`
		}
		err := c.hp.proc.Call(ctx, "quota.query", params, &res)
		quotaQueries.Delete(qid)
		var re *rpc.Error
		if errors.As(err, &re) && re.Code == extension.CodeUnsupported {
			continue
		}
		if err != nil {
			return quota.Report{}, err
		}
		r := res.Report
		if r.Gateway == "" {
			r.Gateway = c.item.ID
		}
		if r.GatewayName == "" {
			r.GatewayName = c.item.Name
		}
		if r.CheckedAt.IsZero() {
			r.CheckedAt = time.Now()
		}
		return r, nil
	}
	return quota.Report{}, quota.ErrUnsupported
}

// quotaHTTP sends a request of a running query for plugin id: only its own query's, and a few of them.
func (ps *extensions) quotaHTTP(ctx context.Context, id string, raw json.RawMessage) (any, error) {
	var p struct {
		QueryID string `json:"query_id"`
		quota.Request
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	v, ok := quotaQueries.Load(p.QueryID)
	q, _ := v.(*quotaQuery)
	if !ok || q.extension != id {
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: "quota.http only while the hub asks this plugin about a launcher (quota.query)"}
	}
	q.mu.Lock()
	q.sent++
	n := q.sent
	q.mu.Unlock()
	if n > quotaRequestsMost {
		return nil, fmt.Errorf("one quota query sends at most %d requests", quotaRequestsMost)
	}
	return q.send(ctx, p.Request)
}

// Outside a launcher's page (docs/extensions/extensions.md "Quota", outside a launcher page): an extension granted launcher_keys
// lists the launchers that have a gateway (gateway.launchers) and opens a short session on one (gateway.open), whose
// query id gateway.http takes as a launcher section's — for a nav_page, a card or a widget that draws a launcher's
// quota. It reaches no more than its launcher sections would: the gateways its quota and launcher_section hosts match
// (any, when one of them names none). The key stays with whoever holds it.

// gatewaySessionLife is how long a gateway.open session takes requests.
const gatewaySessionLife = time.Minute

// gatewayLauncher is one of gateway.launchers' launchers.
type gatewayLauncher struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

// gatewayHosts are the gateway hosts extension p may reach outside a launcher's page; any: every gateway.
func gatewayHosts(p *extension.Extension) (hosts []string, any bool) {
	m := p.Full
	if m == nil {
		m = p.Manifest
	}
	named := 0
	for _, q := range m.Quotas() {
		if len(q.Hosts) == 0 {
			return nil, true
		}
		hosts, named = append(hosts, q.Hosts...), named+1
	}
	for _, u := range m.UIs() {
		if u.Placement != "launcher_section" {
			continue
		}
		if len(u.Hosts) == 0 {
			return nil, true
		}
		hosts, named = append(hosts, u.Hosts...), named+1
	}
	return hosts, named == 0
}

// gatewayCall answers gateway.launchers and gateway.open for extension id.
func (h *Manager) gatewayCall(ctx context.Context, id, method string, raw json.RawMessage) (any, error) {
	p := h.extensions.reg.Get(id)
	if p == nil || !p.Record.Granted.LauncherKeys {
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: method + " needs the permission launcher_keys"}
	}
	hosts, anyHost := gatewayHosts(p)
	allowed := func(base string) (string, bool) {
		root, err := quota.Root(base)
		if err != nil {
			return "", false
		}
		return root.String(), anyHost || hostMatches(root.Hostname(), hosts)
	}
	if method == "gateway.launchers" {
		list, err := h.gatewayLaunchers(ctx)
		if err != nil {
			return nil, err
		}
		out := []gatewayLauncher{}
		for _, v := range list {
			if v.BaseURL == "" {
				continue
			}
			if base, ok := allowed(v.BaseURL); ok {
				out = append(out, gatewayLauncher{Name: v.Name, BaseURL: base})
			}
		}
		return map[string]any{"launchers": out}, nil
	}
	var in struct {
		Launcher string `json:"launcher"`
		Machine  string `json:"machine"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if in.Launcher == "" {
		return nil, errors.New("gateway.open needs a launcher")
	}
	gw, err := h.launcherGateway(ctx, in.Launcher, in.Machine)
	if err != nil {
		return nil, err
	}
	base, ok := allowed(gw.base)
	if !ok {
		return nil, &rpc.Error{Code: extension.CodeUnsupported, Message: "launcher " + in.Launcher + "'s gateway is not one this extension reads"}
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	qid := hex.EncodeToString(b[:])
	quotaQueries.Store(qid, &quotaQuery{extension: id, send: gw.send})
	time.AfterFunc(gatewaySessionLife, func() { quotaQueries.Delete(qid) })
	return map[string]any{"query_id": qid, "base_url": base, "machine": gw.machine}, nil
}
