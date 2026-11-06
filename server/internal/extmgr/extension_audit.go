package extmgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// auditReader reads the audit back (plugin_audit, db.ExtensionAudits).
type auditReader interface {
	ExtensionAudits(ctx context.Context, plugin string, limit int) ([]db.ExtensionAudit, error)
}

// The hub's database keeps the audit and reads it back.
var (
	_ auditStore  = (*db.DB)(nil)
	_ auditReader = (*db.DB)(nil)
)

// How many audit rows one read gives: by default, and at most.
const (
	auditReadDefault = 100
	auditReadMost    = 1000
)

// auditRoutes: GET /api/v1/extensions/-/audit?plugin=<id>&limit=<n>, the audit newest first, everyone's without plugin
// (#5356). Read by a device with full rights or the owner on this machine; never by an extension's token, since it
// names the devices and what every extension and connector did.
func (h *Manager) auditRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/extensions/-/audit", func(w http.ResponseWriter, r *http.Request) {
		if p := principalOf(r.Context()); p.Role != roleFull || p.Extension != "" {
			writeErr(w, http.StatusForbidden, errors.New("only a device with full rights reads the audit"))
			return
		}
		st, _ := h.store.(auditReader)
		if st == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_database", errors.New("no database: there is no audit"))
			return
		}
		limit := auditReadDefault
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("limit %q: a number from 1", v))
				return
			}
			limit = min(n, auditReadMost)
		}
		rows, err := st.ExtensionAudits(r.Context(), r.URL.Query().Get("plugin"), limit)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if rows == nil {
			rows = []db.ExtensionAudit{}
		}
		writeJSON(w, map[string]any{"audit": rows})
	})
}

// auditCommand is `personad extension audit [<id>] [--limit=N]`: one line per row, newest first, the time in UTC.
func auditCommand(ctx context.Context, c *extensionClient, args []string, out io.Writer) error {
	q := url.Values{}
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--limit="):
			q.Set("limit", strings.TrimPrefix(a, "--limit="))
		case strings.HasPrefix(a, "--"):
			return fmt.Errorf("personad extension audit [<id>] [--limit=N]: unknown %s", a)
		case q.Get("plugin") != "":
			return errors.New("personad extension audit [<id>] [--limit=N]: one id at most")
		default:
			q.Set("plugin", a)
		}
	}
	var v struct {
		Audit []db.ExtensionAudit `json:"audit"`
	}
	path := "/api/v1/extensions/-/audit"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	if err := c.do(ctx, "GET", path, nil, &v); err != nil {
		return err
	}
	for _, a := range v.Audit {
		line := a.At.UTC().Format(time.RFC3339) + "  " + a.Plugin + "  " + a.Action + "  " + a.Actor
		if d := strings.TrimSpace(string(a.Detail)); d != "" && d != "{}" {
			line += "  " + d
		}
		fmt.Fprintln(out, line)
	}
	return nil
}
