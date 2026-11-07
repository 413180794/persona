package extmgr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// How the extensions run, for the admin UI (docs/operations/admin.md "Extensions" "How they run", extension_metrics.go): the
// counts over a window, the last lines of an extension's log and a stream of the new ones. Only a signed-in admin
// session gets here (adminRoutes): the log is the extensions' own, and may say more than the apps should see.

// logStreamBeat is how often the stream says it is still there (proxies close a quiet connection).
var logStreamBeat = 25 * time.Second

// LogsPage is GET /api/v1/admin/extensions/{id}/logs.
type LogsPage struct {
	Lines []LogLine `json:"lines"`
	// Next is the seq of the line after the last one kept: since=next-1 asks for the newer ones only.
	Next int64 `json:"next"`
	Kept int   `json:"kept"` // how many lines an extension's log keeps
}

func (h *Manager) adminExtensionMetricsRoutes(mux *http.ServeMux, ps func(http.ResponseWriter) *extensions,
	one func(func(http.ResponseWriter, *http.Request, *extensions, *extension.Extension)) http.HandlerFunc) {
	store := func() extensionMetricsStore {
		s, _ := h.store.(extensionMetricsStore)
		return s
	}
	mux.HandleFunc("GET /api/v1/admin/extensions/-/metrics", func(w http.ResponseWriter, r *http.Request) {
		s := ps(w)
		if s == nil {
			return
		}
		var ids []string
		for _, p := range s.reg.List() {
			ids = append(ids, p.Record.ID)
		}
		writeJSON(w, s.stats.overview(r.Context(), store(), ids, r.URL.Query().Get("window"), h.legacyCalls.Snapshot()))
	})
	mux.HandleFunc("GET /api/v1/admin/extensions/{id}/metrics", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		writeJSON(w, ps.stats.metrics(r.Context(), store(), p.Record.ID, r.URL.Query().Get("window")))
	}))
	mux.HandleFunc("GET /api/v1/admin/extensions/{id}/logs", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		q := r.URL.Query()
		since := int64(-1)
		if v := q.Get("since"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeErrCode(w, http.StatusBadRequest, "invalid", i18n.Err("hub-admin_extension_metrics.since_not_number"))
				return
			}
			since = n
		}
		limit := 500
		if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
			limit = min(v, logLinesKept)
		}
		lines, next := ps.stats.of(p.Record.ID).Lines(since, q.Get("q"), limit)
		writeJSON(w, LogsPage{Lines: lines, Next: next, Kept: logLinesKept})
	}))
	// Server-sent events: the kept lines after since (Last-Event-ID when the browser comes back; the last 200 without
	// either), then each new one as it comes, a comment every logStreamBeat.
	mux.HandleFunc("GET /api/v1/admin/extensions/{id}/logs/stream", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		rc := http.NewResponseController(w)
		st := ps.stats.of(p.Record.ID)
		since := int64(-1)
		limit := 200
		for _, v := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("since")} {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				since, limit = n, logLinesKept
				break
			}
		}
		ch := make(chan LogLine, 256)
		stop := st.follow(ch) // before the backlog, so no line falls between them
		defer stop()
		backlog, _ := st.Lines(since, "", limit)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		last := since
		send := func(l LogLine) bool {
			if l.Seq <= last {
				return true
			}
			last = l.Seq
			b, _ := json.Marshal(l)
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", l.Seq, b); err != nil {
				return false
			}
			return true
		}
		for _, l := range backlog {
			if !send(l) {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
		beat := time.NewTicker(logStreamBeat)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case l := <-ch:
				if !send(l) || rc.Flush() != nil {
					return
				}
			case <-beat.C:
				if _, err := fmt.Fprint(w, ": beat\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			}
		}
	}))
}
