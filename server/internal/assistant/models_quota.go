package assistant

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A launcher's quota at its gateway (docs/integrations/launchers.md "Quota"): GET /api/v1/launchers/{name}/quota. Which gateway and
// which machine is the launchers' business (here); reading it is the quota plugins' (the hub's, plugins.quota), and
// what they send goes back here with the key (models.launcher_http). Answers are kept a minute: the page opens often,
// the gateways count every read.

const quotaFresh = time.Minute

type quotaEntry struct {
	report  quota.Report
	machine string
	cause   error // why there is no report, for people in the language of the request reading it
	at      time.Time
}

type quotaCache struct {
	mu sync.Mutex
	m  map[string]quotaEntry
}

func (c *quotaCache) get(name string) (quotaEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[name]
	return e, ok && time.Since(e.at) < quotaFresh
}

func (c *quotaCache) put(name string, e quotaEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]quotaEntry{}
	}
	c.m[name] = e
}

var (
	errNoGateway   = i18n.Err("assistant-models_launchers.no_gateway")
	errNoLauncher  = errors.New("no such launcher")
	errUnsupported = quota.ErrUnsupported
)

// launcherQuota reads (or recalls) one launcher's quota; machine picks which machine reads a ~/.zshrc launcher. An
// error is this request's failure (no such launcher, the machine offline, the hub away); a gateway that has nothing to
// read or says no is an entry with its cause.
func (s *Service) launcherQuota(ctx context.Context, name, machine string, refresh bool) (quotaEntry, error) {
	if e, ok := s.models.quotas.get(name); ok && !refresh && (machine == "" || machine == e.machine) {
		return e, nil
	}
	e := quotaEntry{at: time.Now()}
	gw, err := s.launcherHTTP(ctx, name, machine, nil)
	var re *rpc.Error
	switch {
	case errors.As(err, &re) && re.Code == assistantwire.CodeNoLauncher:
		return quotaEntry{}, errNoLauncher
	case errors.As(err, &re) && re.Code == assistantwire.CodeMachineOffline:
		return quotaEntry{}, apierr.ErrOffline
	case errors.As(err, &re) && re.Code == assistantwire.CodeNoGateway:
		e.cause = errNoGateway
	case err != nil:
		e.cause = err
	}
	if err == nil {
		where, _ := gw.(map[string]any)
		base, _ := where["base_url"].(string)
		e.machine, _ = where["machine"].(string)
		var res struct {
			Report quota.Report `json:"report"`
		}
		params := map[string]any{"name": name, "machine": e.machine, "base_url": base, "tz": s.location(ctx).String()}
		if err := s.appCall(ctx, AppPlugins, assistantwire.PluginsQuota, params, &res); err != nil {
			if errors.As(err, &re) && re.Code == assistantwire.CodeAssistantOffline {
				return quotaEntry{}, err
			}
			if !strings.Contains(err.Error(), errUnsupported.Error()) && ctx.Err() != nil {
				return quotaEntry{}, err // cancelled: nothing to remember
			}
			e.cause = err
		} else {
			e.report = res.Report
		}
	}
	s.models.quotas.put(name, e)
	return e, nil
}

func (s *Service) launcherQuotaRoutes() {
	s.handle("GET /api/v1/launchers/{name}/quota", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		e, err := s.launcherQuota(ctx, r.PathValue("name"), r.URL.Query().Get("machine"), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			apierr.Call(w, err)
			return
		}
		out := map[string]any{"machine": e.machine, "checked_at": e.at.UTC()}
		if e.cause != nil {
			// Not a failure of this request: the gateway has nothing to read, or said no; the page shows why.
			out["error"] = i18n.Localize(e.cause, s.requestCtxLang(r.Context()))
			out["unsupported"] = strings.Contains(e.cause.Error(), errUnsupported.Error()) || errors.Is(e.cause, errNoGateway)
		} else {
			out["quota"] = e.report
		}
		apierr.WriteJSON(w, out)
	})
}
