package devices

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// healthReportCall asks the machine about the health space's reports (docs/health.md"Health reports"); a node that predates
// them says so in words the app can show.
func healthReportCall(w http.ResponseWriter, r *http.Request, m *Machine, method string, params any) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var out json.RawMessage
	if err := m.call(ctx, method, params, &out); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method") {
			msg := i18n.T(i18n.FromRequest(r), "hub-health.reports_outdated", i18n.Args{"name": m.cfg.Name})
			writeErrCode(w, http.StatusConflict, proto.CodeNodeOutdated, errors.New(msg))
			return
		}
		apierr.Call(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// listHealthReports: GET /api/v1/machines/{mid}/health/reports.
func (d *Devices) listHealthReports(w http.ResponseWriter, r *http.Request, m *Machine) {
	healthReportCall(w, r, m, "health.reports.list", map[string]any{})
}

// deleteHealthReport: DELETE /api/v1/machines/{mid}/health/reports/{name}.
func (d *Devices) deleteHealthReport(w http.ResponseWriter, r *http.Request, m *Machine) {
	healthReportCall(w, r, m, "health.reports.delete", map[string]any{"name": r.PathValue("name")})
}
