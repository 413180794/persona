package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/proto"
)

func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	api := http.NewServeMux()
	h.apiMux = api
	h.workspaceBoardRoutes(api)
	if h.upd != nil {
		h.upd.routes(api)
	}
	api.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"machines": h.machineList()})
	})
	h.deviceRoutesRelayed(api) // the manager's own: reconnect, engine, herdr, deploy, claude-dirs (devices_link.go)
	api.HandleFunc("GET /api/v1/machines/{mid}/computer", h.withMachine(h.getComputer))
	api.HandleFunc("PUT /api/v1/machines/{mid}/computer", h.withMachine(h.putComputer))
	api.HandleFunc("POST /api/v1/machines/{mid}/computer/{action}", h.withMachine(h.postComputerJob))
	api.HandleFunc("GET /api/v1/machines/{mid}/computer/clipboard", h.withMachine(h.computerClipboard))
	api.HandleFunc("POST /api/v1/machines/{mid}/computer/clipboard", h.withMachine(h.computerClipboard))
	api.HandleFunc("GET /api/v1/machines/{mid}/computer/apps", h.withMachine(h.getComputerApps))
	api.HandleFunc("DELETE /api/v1/machines/{mid}/computer/apps/{bundle}", h.withMachine(h.deleteComputerApp))
	api.HandleFunc("GET /api/v1/machines/{mid}/browser", h.withMachine(h.getBrowser))
	api.HandleFunc("PUT /api/v1/machines/{mid}/browser", h.withMachine(h.putBrowser))
	api.HandleFunc("POST /api/v1/machines/{mid}/browser/{action}", h.withMachine(h.postBrowserJob))
	// sign-ins in ego lite (browser_cookies.go): only the user's own apps, the hub keeps nothing
	api.HandleFunc("GET /api/v1/machines/{mid}/browser/cookies", h.withMachine(h.browserCookies))
	api.HandleFunc("POST /api/v1/machines/{mid}/browser/cookies", h.withMachine(h.browserCookies))
	api.HandleFunc("DELETE /api/v1/machines/{mid}/browser/cookies", h.withMachine(h.browserCookies))
	api.HandleFunc("GET /api/v1/machines/{mid}/launchers", h.withMachine(h.listLaunchers))
	api.HandleFunc("GET /api/v1/machines/{mid}/launchers/{name}/models", h.withMachine(h.launcherModels))
	h.launcherRoutes(api)
	h.modelConfigRoutes(api)
	api.HandleFunc("GET /api/v1/machines/{mid}/dirs", h.proxy("dirs.list", func(r *http.Request) any {
		return map[string]any{"path": r.URL.Query().Get("path")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/file/download", h.withMachine(h.downloadFile))
	api.HandleFunc("GET /api/v1/machines/{mid}/file/thumb", h.withMachine(h.fileThumb))
	api.HandleFunc("GET /api/v1/machines/{mid}/file/convert", h.withMachine(h.fileConvert))
	// stream=1: a binary file comes back described (stream, revision), its bytes from file/download.
	api.HandleFunc("GET /api/v1/machines/{mid}/file", h.proxy("file.read", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"path": q.Get("path"), "cwd": q.Get("cwd"), "offset": atoi(q.Get("offset")), "stream": q.Get("stream") == "1"}
	}))
	// Which of the paths a conversation mentions are there on the machine (?paths=a%0Ab or ?path=a&path=b, and cwd):
	// the phone links only those.
	api.HandleFunc("GET /api/v1/machines/{mid}/stat", h.proxy("fs.stat", func(r *http.Request) any {
		q := r.URL.Query()
		paths := q["path"]
		if s := q.Get("paths"); s != "" {
			paths = append(paths, strings.Split(s, "\n")...)
		}
		return map[string]any{"paths": paths, "cwd": q.Get("cwd")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/fs", h.proxy("fs.list", func(r *http.Request) any {
		return map[string]any{"path": r.URL.Query().Get("path"), "cwd": r.URL.Query().Get("cwd")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/git/status", h.proxy("git.status", func(r *http.Request) any {
		return map[string]any{"cwd": r.URL.Query().Get("cwd")}
	}))
	api.HandleFunc("POST /api/v1/machines/{mid}/git/pull", h.proxyBody("git.pull"))
	// One version of a file, written into the node's cache for comparing (docs/protocol.md「文件预览」).
	api.HandleFunc("GET /api/v1/machines/{mid}/git/blob", h.proxy("git.blob", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"cwd": q.Get("cwd"), "path": q.Get("path"), "rev": q.Get("rev")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/git/diff", h.proxy("git.diff", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"cwd": q.Get("cwd"), "path": q.Get("path"), "staged": q.Get("staged") == "1"}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/git/show", h.proxy("git.show", func(r *http.Request) any {
		return map[string]any{"cwd": r.URL.Query().Get("cwd"), "hash": r.URL.Query().Get("hash")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/git/log", h.proxy("git.log", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"cwd": q.Get("cwd"), "skip": atoi(q.Get("skip")), "limit": atoi(q.Get("limit"))}
	}))
	api.HandleFunc("POST /api/v1/machines/{mid}/files", h.withMachine(h.uploadFile))
	api.HandleFunc("GET /api/v1/machines/{mid}/claude-settings", h.proxy("claude.settings", nil))
	api.HandleFunc("PUT /api/v1/machines/{mid}/claude-settings", h.proxyBody("claude.settings.set"))
	api.HandleFunc("GET /api/v1/machines/{mid}/codex-settings", h.proxy("codex.settings", nil))
	api.HandleFunc("PUT /api/v1/machines/{mid}/codex-settings", h.proxyBody("codex.settings.set"))
	api.HandleFunc("GET /api/v1/sessions", h.listSessions)
	api.HandleFunc("POST /api/v1/sessions", h.withClientOp("start", h.startSession))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.withMachine(h.sessionItems))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items/{iid}/image", h.withMachine(h.sessionImage))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items/{iid}/full", h.withMachine(h.sessionItemFull))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/messages", h.withClientOp("message", h.withMachine(h.sendMessage)))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/attach", h.proxyBody("session.attach"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/interrupt", h.withClientOp("interrupt", h.proxyBody("session.interrupt")))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/keys", h.proxyBody("session.keys"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/fork", h.withClientOp("fork", h.proxyBody("session.fork"))) // 分支到新聊天 (node/fork.go)
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/close", h.withClientOp("close", h.proxyBody("session.close")))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/queue", h.proxyBody("session.queue.edit"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/rename", h.withClientOp("rename", h.withMachine(h.renameSession)))
	api.HandleFunc("DELETE /api/v1/sessions/{mid}/{sid}", h.withMachine(h.deleteSession))
	// {"permission_mode": …, "effort": …}: either may be left out (node/modes.go).
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/modes", h.withClientOp("modes", h.proxyBody("session.modes")))
	api.HandleFunc("POST /api/v1/machines/{mid}/spaces/{wid}/rename", h.withMachine(h.renameSpace))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/info", h.proxy("session.info", func(r *http.Request) any {
		return map[string]any{"session_id": r.PathValue("sid")}
	}))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/seen", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		h.markSeen(m.cfg.ID, r.PathValue("sid"))
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/screen/before", paneAddressFullOnly(h.proxyBody("session.screen.before")))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/screen", paneAddressFullOnly(h.proxy("session.screen", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"session_id": r.PathValue("sid"), "lines": atoi(q.Get("lines")), "source": q.Get("source"), "format": q.Get("format")}
	})))
	// The terminal browser (docs/terminal.md「独立终端」): a machine's workspaces, tabs and panes with plain shells, and
	// 新建终端. A pane is full shell access on the computer: full devices only (paneRoutesFullOnly).
	api.HandleFunc("GET /api/v1/machines/{mid}/panes", h.proxy("panes.list", nil))
	api.HandleFunc("POST /api/v1/machines/{mid}/panes", h.withMachine(h.newShell))
	// close / split / run an agent / git in a pane, close / rename a tab: full devices only (panes.go)
	api.HandleFunc("POST /api/v1/machines/{mid}/panes/close", h.nodeAction(nodeActions["panes/close"]))
	api.HandleFunc("POST /api/v1/machines/{mid}/panes/split", h.nodeAction(nodeActions["panes/split"]))
	api.HandleFunc("POST /api/v1/machines/{mid}/panes/agent", h.nodeAction(nodeActions["panes/agent"]))
	api.HandleFunc("POST /api/v1/machines/{mid}/panes/git", h.nodeAction(nodeActions["panes/git"]))
	api.HandleFunc("POST /api/v1/machines/{mid}/tabs/close", h.nodeAction(nodeActions["tabs/close"]))
	api.HandleFunc("POST /api/v1/machines/{mid}/tabs/rename", h.nodeAction(nodeActions["tabs/rename"]))
	api.HandleFunc("GET /api/v1/interactions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"interactions": visibleInteractions(h.pendingInteractions(), h.requestHasElicitation(r))})
	})
	api.HandleFunc("GET /api/v1/ops/{id}", h.getClientOp)
	api.HandleFunc("POST /api/v1/interactions/{id}/resolve", h.withClientOp("resolve", func(w http.ResponseWriter, r *http.Request) {
		var res proto.Resolution
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		res.AnsweredBy = "" // only the assistant's interaction_answer says it answered
		if answerOnly(r) && (!answerMayResolve(res) || h.picksFreeText(r.PathValue("id"), res)) {
			writeErr(w, http.StatusForbidden, errors.New("this device can only answer with a card's own choices"))
			return
		}
		if err := h.resolve(r.PathValue("id"), res); err != nil {
			if errors.Is(err, errNotPending) {
				h.writeEnded(w, r.PathValue("id"), res, err)
			} else if errors.Is(err, errInvalidElicitation) {
				writeErr(w, http.StatusBadRequest, err)
			} else {
				callErr(w, err) // prompt_changed: 409, nothing pressed
			}
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("POST /api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		var d Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil || d.DeviceID == "" {
			writeErr(w, http.StatusBadRequest, errors.New("device_id is required"))
			return
		}
		// Both name the device token that registered it: credential_id picks the phone a tool call or wake goes to,
		// credential stops the pushes when it is revoked (Pusher.signedOut).
		d.Locale = string(i18n.Resolve(d.Locale)) // its pushes' language; "" (an older app) is the user's
		d.CredentialID = principalOf(r.Context()).ID
		d.Credential = d.CredentialID
		h.push.register(d)
		writeJSON(w, map[string]any{"ok": true})
	})
	h.live.routes(api)
	h.usageRoutes(api)
	h.metricsRoutes(api)
	api.HandleFunc("GET /api/v1/devices/{id}/sessions", h.serveDeviceSessions)
	h.lockTestRoutes(api)
	// Which pushes this device wants (notify.go NotifyPrefs); the app's 设置 › 通知.
	api.HandleFunc("GET /api/v1/devices/{id}/prefs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.prefsView(h.push.prefs(r.PathValue("id"))))
	})
	api.HandleFunc("PUT /api/v1/devices/{id}/prefs", func(w http.ResponseWriter, r *http.Request) {
		prefs := h.push.prefs(r.PathValue("id"))
		if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		raw, _ := json.Marshal(prefs)
		prefs = devicePrefs(raw) // normalizes an unknown "done"
		if err := h.push.setPrefs(r.PathValue("id"), prefs); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.live.settingsChanged()
		writeJSON(w, h.prefsView(prefs))
	})
	api.HandleFunc("GET /api/v1/interactions/history", func(w http.ResponseWriter, r *http.Request) {
		if h.store == nil {
			writeJSON(w, map[string]any{"interactions": []any{}})
			return
		}
		q := r.URL.Query()
		mid := q.Get("machine_id")
		if m := h.localMachine(mid); m != nil {
			mid = m.cfg.ID
		}
		recs, err := h.store.Interactions(r.Context(), mid, q.Get("session_id"), atoi(q.Get("limit")))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !h.requestHasElicitation(r) {
			filtered := make([]db.InteractionRecord, 0, len(recs))
			for _, rec := range recs {
				if rec.Interaction.Kind != proto.IxElicitation {
					filtered = append(filtered, rec)
				}
			}
			recs = filtered
		}
		writeJSON(w, map[string]any{"interactions": recs})
	})
	h.assistantRoutes(api)
	h.pinRoutes(api)
	h.notifyRoutes(api)
	h.personaRoutes(api)
	h.botRoutes(api) // the channels' routes are the assistant's (assistantwire.Routes)
	h.agentRoutes(api)
	h.inboxRoutesRelayed(api)
	h.connectorRoutes(api)
	h.extensionRoutes(api)
	api.HandleFunc("GET /api/v1/ws", h.serveWS)
	if h.mail != nil {
		api.Handle("/api/v1/mail/", h.mail.Handler())
	}
	h.tokenRoutes(api)
	h.portRoutes(api)
	h.pairRoutes(api)
	h.linkRoutes(api)
	h.publicRoutes(api)
	h.androidAppRoutes(api)
	h.peerRoutes(api)
	h.runtimeCLIRoutes(api)
	// What the center serves: its own apps' requests and those the members pass on (peer_http.go), whose source the
	// member judged (no gate here for them). A member passes everything but its own few routes on (memberForward).
	centerAPI := h.auth(h.extensionAPIs(jsonRouteErrors(api), api))
	h.mesh.centerAPI.Store(centerAPI)
	appAPI := h.gate(h.memberForward(centerAPI))
	h.busRoutes(mux)
	mux.Handle("/api/", appAPI)
	mux.Handle("/", h.adminRoutes(mux, appAPI, webApp(filepath.Join(h.cfg.dir, "web"))))
	return gzipResponses(h.logf, logRequests(h, jsonRouteErrors(mux)))
}

func (h *Hub) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := h.sourceOf(r) == sourcePublic
		limited := public
		if _, fwd := forwardedOf(r); fwd {
			limited = false // the member that passed it on keeps the count (peer_http.go memberForward)
		}
		if limited {
			if retry := h.devtok.failures.retry(authSourceKey(r), time.Now(), h.cfg.Security); retry > 0 {
				retryAfter(w, retry)
				h.securityError(w, r, http.StatusTooManyRequests, "rate_limited", 0)
				return
			}
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		p, ok := h.authenticate(r.Context(), tok)
		if !ok {
			failures := 0
			if limited {
				failures = h.devtok.failures.fail(authSourceKey(r), time.Now(), h.cfg.Security)
			}
			h.securityError(w, r, http.StatusUnauthorized, "", failures)
			return
		}
		if public && p.ID == "" {
			h.securityError(w, r, http.StatusForbidden, "master_token_local_only", 0)
			return
		}
		if p.Role == roleExtension {
			// An extension's token is for the hub it runs beside, on this machine (hub.url).
			if public {
				h.securityError(w, r, http.StatusForbidden, "extension_token_local_only", 0)
				return
			}
			if ok, scope := h.extensionMay(p, r); !ok {
				body := errorBody(http.StatusForbidden, "permission_denied", errors.New("this extension was not granted this part of the hub's API"))
				if scope != "" {
					body["scope"] = scope
				}
				writeErrorBody(w, http.StatusForbidden, body)
				return
			}
		} else if !allowed(p, r.Method, r.URL.Path) {
			msg := "this device can only read and answer"
			if p.Role == roleLink || p.Role == roleLinkPair {
				msg = "a Persona Link credential only opens its machine's link"
			} else if p.Role == rolePeer || p.Role == rolePeerPair {
				msg = "a peer credential only opens the mesh link"
			}
			writeErr(w, http.StatusForbidden, errors.New(msg))
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		if p.PairHash != "" && r.URL.Path == "/api/v1/ws" {
			ctx, cancel := h.pairContext(r.Context(), p)
			defer cancel()
			r = r.WithContext(ctx)
		}
		// API answers are live data: without this the phone's URLCache kept a copy of every page it pulled (megabytes,
		// never read again). Images and media that may be cached set their own Cache-Control.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status          int
	securityFailure bool
	lang            i18n.Lang
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Lang is the request's language: apierr writes an i18n.Message error in it.
func (s *statusWriter) Lang() i18n.Lang { return s.lang }

// Unwrap lets websocket.Accept reach the underlying Hijacker.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequests logs failed and slow requests (method, path, status, time; never the query, body or token) under
// a request id: the app's X-Client-Request-Id when it sent one, which it shows next to a server error, else a new
// one. Every response carries it as X-Request-Id.
func logRequests(h *Hub, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		client := parseRequestClient(r.Header.Get("X-Jarvis-Client"))
		r = r.WithContext(context.WithValue(r.Context(), requestClientKey{}, client))
		id := requestID(r.Header.Get("X-Client-Request-Id"))
		w.Header().Set("X-Request-Id", id)
		// the language of this response's text for people: its Accept-Language, else the user's (locale.go)
		lang := i18n.FromRequest(r).Or(h.userLang())
		r = r.WithContext(i18n.With(r.Context(), lang))
		sw := &statusWriter{ResponseWriter: w, status: 200, lang: lang}
		next.ServeHTTP(sw, r)
		// Auth and ingress refusals are logged by securityError (masked, rate limited), WebSocket ones included.
		if !sw.securityFailure && (sw.status >= 400 || (!longLived(r.URL.Path) && time.Since(start) > 2*time.Second)) {
			if client.Platform != "" {
				h.logf("[%s] %s %s -> %d (%s) client=%q", id, r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond), client.Platform+"/"+client.Version)
			} else {
				h.logf("[%s] %s %s -> %d (%s)", id, r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
			}
		}
	})
}

// longLived requests (WebSockets, a plugin's sockets and streams, a forwarded port's connections: ports.go) are not
// slow requests however long they last.
func longLived(path string) bool {
	return path == "/api/v1/ws" || path == link.Path || path == link.StreamPath || path == bus.Path ||
		strings.HasPrefix(path, "/api/v1/plugins/") || strings.HasPrefix(path, "/api/v1/extensions/") || strings.HasSuffix(path, "/connect")
}

// requestID keeps a client's id if it is a short token (it lands in logs), else makes one.
func requestID(client string) string {
	if n := len(client); n >= 4 && n <= 32 && strings.Trim(client, "0123456789abcdefABCDEF-") == "" {
		return client
	}
	return randomHex(4)
}

func writeJSON(w http.ResponseWriter, v any) { apierr.WriteJSON(w, v) }

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (h *Hub) withMachine(fn func(http.ResponseWriter, *http.Request, *Machine)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := h.localMachine(r.PathValue("mid"))
		if m == nil {
			writeErr(w, http.StatusNotFound, errors.New("unknown machine"))
			return
		}
		fn(w, r, m)
	}
}

func callErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errManagerOffline) { // the manager that carries the machine is not there (devices_link.go)
		writeErrCode(w, http.StatusServiceUnavailable, assistantwire.CodeAssistantOffline, errAssistantOffline)
		return
	}
	apierr.Call(w, err)
}

// proxy forwards a GET to the machine's node and returns its result verbatim.
func (h *Hub) proxy(method string, params func(*http.Request) any) http.HandlerFunc {
	return h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		var p any = map[string]any{}
		if params != nil {
			p = params(r)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		var out json.RawMessage
		if err := m.call(ctx, method, p, &out); err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(out)
	})
}

// sessionItems pages through a session's history. With lazy_images=1 images come without their data (the app
// fetches each one from sessionImage when it scrolls into view); older apps still get them inline.
func (h *Hub) sessionItems(w http.ResponseWriter, r *http.Request, m *Machine) {
	q := r.URL.Query()
	params := map[string]any{"session_id": r.PathValue("sid"), "before_seq": atoi(q.Get("before_seq")), "limit": atoi(q.Get("limit")),
		"turns": atoi(q.Get("turns"))}
	if q.Has("after_seq") { // what came after the app's newest item (background sync); has_more is meaningless here
		params["after_seq"] = atoi(q.Get("after_seq"))
	}
	if q.Get("lazy_images") == "1" {
		params["lazy_images"] = true // the node keeps images out of its items; it would read them back for nothing
	}
	if q.Get("fold_work") == "1" {
		params["fold_work"] = true // runs of tools and thoughts come as one fold each (transcript.FoldWork)
	}
	if q.Has("through_seq") { // opening a fold: its lines, from_seq through through_seq
		params["from_seq"], params["through_seq"] = atoi(q.Get("from_seq")), atoi(q.Get("through_seq"))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var res struct {
		Items     []proto.Item `json:"items"`
		HasMore   bool         `json:"has_more"`
		MoreAfter bool         `json:"more_after,omitempty"`
		More      bool         `json:"more,omitempty"` // a fold's lines go on after these
		itemSpan
	}
	if err := m.call(ctx, "session.items", params, &res); err != nil {
		callErr(w, err)
		return
	}
	res.Items = h.withInjected(res.Items)
	if q.Get("lazy_images") == "1" {
		res.Items = proto.WithoutImageData(res.Items)
	}
	writeJSON(w, res)
}

// sessionItemFull serves one item whole: pages cut long text (the item says full_size), the app fetches the rest when
// the user opens it.
func (h *Hub) sessionItemFull(w http.ResponseWriter, r *http.Request, m *Machine) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var it proto.Item
	if err := m.call(ctx, "session.item", map[string]any{"session_id": r.PathValue("sid"), "item_id": r.PathValue("iid")}, &it); err != nil {
		callErr(w, err)
		return
	}
	h.followedItems(m.cfg.ID, r.PathValue("sid"), []proto.Item{it})
	writeJSON(w, h.withInjected([]proto.Item{it})[0])
}

// sessionImage serves one item's image as raw bytes. An item's image never changes, so the app may cache it forever.
func (h *Hub) sessionImage(w http.ResponseWriter, r *http.Request, m *Machine) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var img proto.Image
	if err := m.call(ctx, "session.image", map[string]any{"session_id": r.PathValue("sid"), "item_id": r.PathValue("iid")}, &img); err != nil {
		callErr(w, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if img.Mime != "" {
		w.Header().Set("Content-Type", img.Mime)
	}
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// proxyBody forwards a POST body (plus session_id from the path) to the node.
func (h *Hub) proxyBody(method string) http.HandlerFunc {
	return h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		p := map[string]any{}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&p); err != nil && !errors.Is(err, errEOF) {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if p == nil {
			writeErr(w, http.StatusBadRequest, errors.New("request body must be a JSON object"))
			return
		}
		p["session_id"] = r.PathValue("sid")
		// Resuming an offline session can take a while (shell + claude startup).
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		var out json.RawMessage
		if err := m.call(ctx, method, p, &out); err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(out)
	})
}

var errEOF = errors.New("EOF")

func (h *Hub) listSessions(w http.ResponseWriter, r *http.Request) {
	mid := r.URL.Query().Get("machine_id")
	out := []proto.Session{}
	for _, m := range h.machinesNow() {
		if mid != "" && m.cfg.ID != mid {
			continue
		}
		out = append(out, h.shown(m.cfg.ID, h.decorated(m))...)
	}
	writeJSON(w, map[string]any{"sessions": out})
}

func (h *Hub) startSession(w http.ResponseWriter, r *http.Request) {
	var p struct {
		MachineID       string `json:"machine_id"`
		Cwd             string `json:"cwd"`
		Launcher        string `json:"launcher"`
		AgentType       string `json:"agent_type,omitempty"`
		Model           string `json:"model,omitempty"` // picked in 新建 Agent for this start; empty: applyModel's
		Prompt          string `json:"prompt"`
		ResumeSessionID string `json:"resume_session_id"`
		WorkspaceID     string `json:"workspace_id"`
		PermissionMode  string `json:"permission_mode,omitempty"`
		Effort          string `json:"effort,omitempty"`
		// TabName names the herdr tab; empty: one made from the first message (tabname.go), else agent-<n>.
		TabName string `json:"tab_name,omitempty"`
		// Async: answer at once with the session it is going to be and start it in the background (node/starting.go);
		// messages sent to it meanwhile go in once it is ready. NameHint: what the tab name is made from when the
		// app sends the first message itself (so it shows as a message right away) instead of as prompt.
		Async    bool   `json:"async,omitempty"`
		NameHint string `json:"name_hint,omitempty"`
		TodoID   string `json:"todo_id,omitempty"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err == nil {
		err = json.Unmarshal(body, &p)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if p.TodoID != "" {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("read-only role cannot modify todos"))
			return
		}
		t, err := h.getTodoForLink(r.Context(), p.TodoID)
		if err != nil {
			writeAssistantErr(w, err) // 503 assistant_offline while the assistant is not connected
			return
		}
		if t == nil {
			writeErr(w, http.StatusNotFound, errors.New("todo not found"))
			return
		}
		if t.DoneAt != nil {
			writeErr(w, http.StatusConflict, errors.New("todo is already completed"))
			return
		}
	}
	m := h.localMachine(p.MachineID)
	if m == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown machine"))
		return
	}
	if p.AgentType == "" {
		for _, launcher := range m.cfg.Launchers {
			if launcher.Name == p.Launcher {
				p.AgentType = launcher.AgentType
				break
			}
		}
	}
	if p.AgentType != "" && !agentTypeRe.MatchString(p.AgentType) {
		writeErr(w, http.StatusBadRequest, errors.New("unsupported agent type"))
		return
	}
	p.TabName = strings.TrimSpace(p.TabName)
	nameFrom := p.Prompt
	if strings.TrimSpace(p.NameHint) != "" {
		nameFrom = p.NameHint
	}
	if p.TodoID != "" {
		// The decisions go to the agent, not into its tab's name (nameFrom is taken before).
		p.Prompt = h.withTodoDecisions(r.Context(), p.TodoID, p.Prompt)
	}
	async := startsAsync(p.Async, p.ResumeSessionID, p.AgentType)
	parent := r.Context()
	if r.Header.Get("X-Jarvis-Op-Id") != "" {
		// Finish an accepted start even if its HTTP response is lost; client_ops records the outcome.
		parent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(parent, startBudget(async))
	defer cancel()
	var res struct {
		Session proto.Session `json:"session"`
	}
	raw, _ := json.Marshal(p)
	params := map[string]any{}
	_ = json.Unmarshal(raw, &params)
	name := "" // a tab named now, before it opens; one started in the background is renamed when its name comes (below)
	if !async && p.TabName == "" && p.ResumeSessionID == "" {
		name = nameFrom
	}
	// The manager says what it runs on (模型配置 › Agents, a model picked for this start) and names its tab
	// (agents_link.go).
	if err := h.agentStartParams(ctx, m, params, name); err != nil {
		var refused *startRefused
		if errors.As(err, &refused) {
			writeErr(w, 400, err)
			return
		}
		callErr(w, err) // 503 assistant_offline without the manager (the machine is its to reach), else 502
		return
	}
	if err := m.call(ctx, "session.start", params, &res); err != nil {
		callErr(w, err)
		return
	}
	res.Session.MachineID = m.cfg.ID
	res.Session.State = agentState(res.Session, time.Time{})
	if p.TodoID != "" {
		title := res.Session.Title
		if title == "" {
			title = cleanPromptTitle(nameFrom)
		}
		if err := h.linkTodoAgent(ctx, p.TodoID, m.cfg.ID, res.Session.SessionID, title, res.Session.Cwd, res.Session.Agent, ""); err != nil {
			h.logf("startSession: link todo %s for %s/%s failed: %v", p.TodoID, m.cfg.ID, res.Session.SessionID, err)
		} else {
			res.Session.Todo = &proto.TodoRef{ID: p.TodoID, Title: title}
			if t, err := h.getTodoForLink(ctx, p.TodoID); err == nil && t != nil {
				res.Session.Todo.Title = t.Title
			}
		}
	}
	writeJSON(w, res)
	if async && p.TabName == "" {
		// The name takes a model call (seconds): the tab opens as agent-<n> and is renamed when it comes.
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if name := h.autoTabName(ctx, nameFrom); name != "" {
				if err := m.call(ctx, "session.rename", map[string]any{"session_id": id, "tab_name": name}, nil); err != nil {
					h.logf("tab name %s: %v", id, err)
				}
			}
		}(res.Session.SessionID)
	}
}

// startsAsync: a start the app asked to have answered at once is, when the node can acknowledge it before the session
// is ready (only Claude can, and not a resume).
func startsAsync(asked bool, resume, agentType string) bool {
	return asked && resume == "" && (agentType == "" || agentType == "claude")
}

// startBudget is how long a start may take: one acknowledged before the session is ready a moment, one answered only
// after startup (a sync one, Codex, TRAEX) as long as a busy machine needs.
func startBudget(async bool) time.Duration {
	if async {
		return 15 * time.Second
	}
	return 100 * time.Second
}

// ---- WebSocket ----

type client struct {
	cancel   context.CancelFunc
	stopping atomic.Bool

	hub            *Hub
	conn           *websocket.Conn
	out            chan []byte
	mu             sync.Mutex
	subs           map[[2]string]bool
	fsw            map[[2]string]fsWatchReq // {machine id, cwd} -> the folders this browser has open there (fswatch.go)
	phone          bool
	phoneID        string
	phoneToolNames map[string]bool
	activeAt       time.Time
	sim            bool // a simulator registered as phone: only used when no real iPhone is connected
	// runtimeCLI: the phone's DEBUG app offers its runtime CLI (runtime_cli.go); never one of the phone's tools.
	runtimeCLI bool
	// desktop: a desktop app (the Mac app, register_desktop); never the phone (desktops.go).
	desktop bool
	// device and role: whose credential opened the connection (devicetokens.go); device is empty for the pairing token.
	device, role string
	// pushDevice: the app's id for pushes (register_phone's / register_desktop's device_id, PushRegistrar.deviceID in
	// the app): while it is connected its app is open, and a phone draws its Live Activity itself (liveactivity.go), a
	// desktop posts its own notifications (desktopOpen). Empty for an older app.
	pushDevice string
	// delta: the app asked for sessions_delta (sessions_push.go); have is, by machine, each session it has (id -> its
	// JSON as sent), under the hub's sessionsMu.
	delta bool
	have  map[string]map[string][]byte
	// urlFeatures: what the app listed in the WebSocket URL's ?features= (wants).
	urlFeatures map[string]bool
	// clean: the app closed the connection itself (gone to the background), rather than the network dropping it.
	clean           bool
	lazy            bool // asked for items without image data (subscribe with lazy_images)
	fold            bool // asked for backlogs with the agent's work folded (subscribe with fold_work)
	name            string
	since           time.Time
	meta            clientMeta // what it said about itself in client_hello (clientmeta.go)
	acceptLang      i18n.Lang  // its upgrade request's Accept-Language (locale.go)
	deviceSessionID int64      // immutable after registration in Hub.clients
}

func (c *client) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.out <- b:
	default:
		// Slow consumer: drop the connection; the app reconnects and resyncs.
		if c.conn != nil {
			c.conn.Close(websocket.StatusPolicyViolation, "too slow")
		}
	}
}

func (h *Hub) broadcast(v any) {
	m, _ := v.(map[string]any)
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	typ, _ := m["type"].(string)
	var elicitation bool
	if ix, ok := m["interaction"].(proto.Interaction); ok {
		elicitation = ix.Kind == proto.IxElicitation
	}
	h.broadcastTo(typ, b, elicitation)
}

// broadcastRaw sends every app the message b (of type typ).
func (h *Hub) broadcastRaw(typ string, b []byte) { h.broadcastTo(typ, b, false) }

// broadcastTo sends every app the message b (of type typ); an MCP form or URL request (elicitation) only goes to the
// apps that said they can answer one.
func (h *Hub) broadcastTo(typ string, b []byte, elicitation bool) {
	switch typ {
	case "sessions", "interaction", "interaction_resolved": // what the Fleet activity is made of
		h.live.changed()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.events == nil {
		h.events = newEventLog()
	}
	b = h.events.add(b, elicitation, time.Now())
	for c := range h.clients {
		if elicitation && !c.has("elicitation") {
			continue
		}
		c.queue(b)
	}
}

func (h *Hub) toSubscribers(machineID, sessionID string, v any) {
	h.toSubscribersWith(machineID, sessionID, "", v)
}

// toSubscribersWith is toSubscribers for the subscribers that asked for feature ("": all of them), on the WebSocket
// URL's ?features= or in client_hello.
func (h *Hub) toSubscribersWith(machineID, sessionID, feature string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	key := [2]string{machineID, sessionID}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.mu.Lock()
		sub := c.subs[key]
		c.mu.Unlock()
		if sub && (feature == "" || c.wants(feature)) {
			select {
			case c.out <- b:
			default:
			}
		}
	}
}

// itemsToSubscribers fans new items out, without image data to clients that load images on demand.
func (h *Hub) itemsToSubscribers(machineID, sessionID string, items []proto.Item, span itemSpan) {
	if items == nil {
		items = []proto.Item{}
	}
	msg := func(items []proto.Item) []byte {
		b, _ := json.Marshal(span.into(map[string]any{"type": "items", "machine_id": machineID, "session_id": sessionID, "items": items}))
		return b
	}
	var full, lazy []byte
	key := [2]string{machineID, sessionID}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.mu.Lock()
		sub, wantsLazy := c.subs[key], c.lazy
		c.mu.Unlock()
		if !sub {
			continue
		}
		var b []byte
		if wantsLazy {
			if lazy == nil {
				lazy = msg(proto.WithoutImageData(items))
			}
			b = lazy
		} else {
			if full == nil {
				full = msg(items)
			}
			b = full
		}
		select {
		case c.out <- b:
		default:
		}
	}
}

func (h *Hub) phoneClient(tool ...string) *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Only connected providers of the requested tool are eligible; prefer real devices, then recent activity.
	var best *client
	for c := range h.clients {
		if !c.phone || (len(tool) > 0 && !c.phoneToolNames[tool[0]]) {
			continue
		}
		if best == nil || (best.sim && !c.sim) || (best.sim == c.sim &&
			(c.activeAt.After(best.activeAt) || (c.activeAt.Equal(best.activeAt) && c.since.After(best.since)))) {
			best = c
		}
	}
	return best
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.wsStopping {
		h.mu.Unlock()
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	h.wsHandlers.Add(1)
	h.mu.Unlock()
	defer h.wsHandlers.Done()
	upgrade := &wsUpgrade{ResponseWriter: w}
	conn, err := websocket.Accept(upgrade, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(64 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	who := principalOf(r.Context())
	c := &client{cancel: cancel, hub: h, conn: conn, out: make(chan []byte, 512), subs: map[[2]string]bool{}, since: time.Now(),
		device: who.ID, role: who.Role, delta: wantsFeature(r, featureSessionsDelta), have: map[string]map[string][]byte{},
		urlFeatures: urlFeatures(r), acceptLang: i18n.FromRequest(r), meta: clientMeta{updates: make(chan heartbeatConfig, 512), done: ctx.Done()}}
	h.mu.Lock()
	if h.wsStopping || h.isDeviceRevoked(c.device) {
		h.mu.Unlock()
		cancel()
		_ = conn.CloseNow()
		return
	}
	if h.wsConnections == nil {
		h.wsConnections = map[*client]struct{}{}
	}
	h.wsConnections[c] = struct{}{}
	h.mu.Unlock()
	var session *deviceSessionConnection
	var writerDone chan struct{}
	defer func() {
		cancel()
		_ = conn.CloseNow()
		if writerDone != nil {
			<-writerDone
		}
		session.disconnected(err)
		h.dropClient(c)
		c.mu.Lock()
		subs := c.subs
		c.subs = map[[2]string]bool{}
		c.mu.Unlock()
		for k := range subs {
			if m := h.localMachine(k[0]); m != nil {
				m.removeWatcher(k[1])
			}
		}
		h.terminalDrop(c)
	}()

	// Track registration too: shutdown must not miss a newly hijacked connection.
	session = h.deviceSessionConnected(r.WithContext(ctx), c)
	if session != nil {
		// Normal client cancellation is not a hub shutdown.
		session.ctx = r.Context()
		c.deviceSessionID = session.id
	}
	// The hello goes first, then (resuming) what the client missed, before any live event: under h.mu, which
	// broadcastTo holds while it numbers and sends (eventlog.go).
	hello := h.hello()
	stream, after, resuming := resumeOf(r)
	h.mu.Lock()
	// Check under the registration lock so revocation either rejects this client or sees it to close it.
	if h.wsStopping || h.isDeviceRevoked(c.device) {
		h.mu.Unlock()
		return
	}
	h.clients[c] = struct{}{}
	h.helloLocked(c, hello, stream, after, resuming)
	h.mu.Unlock()

	writerDone = make(chan struct{})
	go func() {
		defer close(writerDone)
		defer cancel()
		session.writerStopped(c.writeWS(ctx, conn, func(hb heartbeatConfig) {
			session.heartbeat(hb)
			if err := setHeartbeatKeepAlive(upgrade.conn, hb); err != nil {
				h.logf("websocket keepalive: %v", err)
			}
		}))
	}()

	c.sendJSON(map[string]any{"type": "machines", "machines": h.machineList()})
	for _, m := range h.machinesNow() {
		if c.delta {
			h.snapshotSessions(c, m)
		} else {
			c.sendJSON(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": h.shown(m.cfg.ID, h.decorated(m))})
		}
	}
	h.sendPendingInteractions(c, false)
	h.sendDesktops(c)

	for {
		_, data, readErr := conn.Read(ctx)
		if readErr != nil {
			err = readErr
			s := websocket.CloseStatus(readErr)
			c.clean = s == websocket.StatusNormalClosure || s == websocket.StatusGoingAway
			return
		}
		h.onClientMessage(ctx, c, data)
	}
}

func (h *Hub) onClientMessage(ctx context.Context, c *client, data []byte) {
	var msg struct {
		Type       string          `json:"type"`
		MachineID  string          `json:"machine_id"`
		SessionID  string          `json:"session_id"`
		SinceSeq   int             `json:"since_seq"`
		LazyImages bool            `json:"lazy_images"`
		FoldWork   bool            `json:"fold_work"`
		DeviceName string          `json:"device_name"`
		DeviceID   string          `json:"device_id"` // register_phone / register_desktop: the app's id for pushes
		Platform   string          `json:"platform"`
		Simulator  bool            `json:"simulator"`
		Tools      []proto.Tool    `json:"tools"`
		CallID     string          `json:"call_id"`
		Content    []proto.Content `json:"content"`
		IsError    bool            `json:"is_error"`
		Epoch      string          `json:"epoch"`
		AfterSeq   int             `json:"after_seq"`
		// terminal_subscribe, terminal_input: the grid the app drives the terminal at (docs/terminal.md)
		Control  *termCtl `json:"control"`
		Takeover bool     `json:"takeover"`
		Text     string   `json:"text"`
		// terminal_input: the wheel over a terminal the app drives, in lines (> 0 back in history, < 0 forward), over
		// the cell of its screen where the finger went down ([column, row], 0-based)
		Scroll int   `json:"scroll"`
		At     []int `json:"at"`
		// terminal_input: a tap on a terminal the app drives, at a cell of its screen ([column, row], 0-based)
		Click []int `json:"click"`
		// fs_watch: the folders of machine_id this browser has open, and whether it shows cwd's Git status (fswatch.go)
		Cwd   string   `json:"cwd"`
		Paths []string `json:"paths"`
		Git   bool     `json:"git"`
	}
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	h.mu.Lock()
	c.activeAt = time.Now()
	h.mu.Unlock()
	switch msg.Type {
	case "client_hello":
		h.onClientHello(c, data)
	case "ping":
		h.onAppPing(c, data)
	case "terminal_subscribe":
		if c.role != roleFull && paneAddress(msg.SessionID) { // a shell by pane: full devices only (panes.go)
			c.sendJSON(map[string]any{"type": "terminal", "machine_id": msg.MachineID, "session_id": msg.SessionID, "error": "forbidden: a terminal opened by pane needs a full device"})
			return
		}
		if c.role != roleFull && msg.Control != nil { // driving a terminal is typing into it: an answer-only device only watches
			msg.Control, msg.Takeover = nil, false
			c.sendTerm(termControlMessage(msg.MachineID, msg.SessionID, "unsupported", nil, "这台设备只能看，不能接管"))
		}
		h.terminalSubscribe(ctx, c, msg.MachineID, msg.SessionID, msg.Epoch, msg.AfterSeq, msg.Control, msg.Takeover)
	case "terminal_input":
		h.terminalInput(c, msg.MachineID, msg.SessionID, msg.Text, msg.Control, msg.Scroll, msg.At)
		if len(msg.Click) == 2 {
			h.terminalClick(c, msg.MachineID, msg.SessionID, msg.Click[0], msg.Click[1])
		}
	case "terminal_unsubscribe":
		h.terminalUnsubscribe(c, msg.MachineID, msg.SessionID)
	case "fs_watch": // #2439: full devices only, like listing the folders (an answer-only device cannot)
		if c.role != roleFull {
			return
		}
		h.fsWatchSet(c, msg.MachineID, cleanFsWatch(msg.Cwd, msg.Paths, msg.Git))
	case "subscribe":
		m := h.localMachine(msg.MachineID)
		if m == nil {
			return
		}
		key := [2]string{msg.MachineID, msg.SessionID}
		c.mu.Lock()
		already := c.subs[key]
		c.subs[key] = true
		if msg.LazyImages {
			c.lazy = true
		}
		if msg.FoldWork {
			c.fold = true
		}
		c.mu.Unlock()
		if !already {
			m.addWatcher(ctx, msg.SessionID)
		}
		go h.sendBacklog(ctx, c, m, msg.SessionID, msg.SinceSeq)
	case "unsubscribe":
		key := [2]string{msg.MachineID, msg.SessionID}
		c.mu.Lock()
		had := c.subs[key]
		delete(c.subs, key)
		c.mu.Unlock()
		if m := h.localMachine(msg.MachineID); m != nil && had {
			m.removeWatcher(msg.SessionID)
		}
	case "register_phone":
		if c.role != roleFull { // runs tools on behalf of claude: not for an answer-only device
			return
		}
		tools, cli := splitRuntimeCLI(msg.Tools)
		if cli && len(tools) == 0 {
			// Only the runtime CLI: a simulator driven by a script (docs/runtime-cli.md「经 hub 连真机」). Not the
			// phone: it takes no phone_* call and changes nothing about pushes.
			h.mu.Lock()
			c.runtimeCLI, c.sim, c.name, c.since = true, msg.Simulator, msg.DeviceName, time.Now()
			h.mu.Unlock()
			return
		}
		deviceID := msg.DeviceID
		if deviceID == "" {
			deviceID = c.device
		}
		if deviceID == "" {
			deviceID = "legacy"
		}
		h.mu.Lock()
		c.phone, c.sim, c.name, c.since, c.pushDevice = true, msg.Simulator, msg.DeviceName, time.Now(), msg.DeviceID
		c.runtimeCLI = cli
		c.phoneID = deviceID
		c.phoneToolNames = map[string]bool{}
		for _, tool := range tools {
			c.phoneToolNames[tool.Name] = true
		}
		h.mu.Unlock()
		if strings.HasPrefix(c.device, "pair_") {
			// A pairing token is traded for a device token within seconds and the phone registers again under it:
			// calls still reach this connection, but nothing is saved under a credential about to disappear.
			return
		}
		h.setPhoneTools(db.PhoneToolRegistration{DeviceID: deviceID, Platform: msg.Platform, CredentialID: c.device, Tools: tools})
	case "register_desktop":
		h.registerDesktop(c, msg.DeviceName, msg.DeviceID)
	case "seen":
		if m := h.localMachine(msg.MachineID); m != nil && msg.SessionID != "" {
			h.markSeen(m.cfg.ID, msg.SessionID)
		}
	case "tool_result":
		if c.role != roleFull {
			return
		}
		h.toolResult(msg.CallID, proto.ToolResult{Content: msg.Content, IsError: msg.IsError})
	}
}

// backlogPages caps a catch-up at 4000 items; past that the last batch says more_after and the app takes the latest
// page instead (an old app would keep the hole, as before).
const backlogPages = 8

func (h *Hub) sendBacklog(ctx context.Context, c *client, m *Machine, sid string, since int) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c.mu.Lock()
	lazy, fold := c.lazy, c.fold
	c.mu.Unlock()
	after := since
	for page := 0; ; page++ {
		params := map[string]any{"session_id": sid, "turns": 2, "lazy_images": lazy} // the last two exchanges, however many rows they are
		if since > 0 {
			params["after_seq"] = after
		} else if fold {
			params["fold_work"] = true
		}
		var res struct {
			Items     []proto.Item `json:"items"`
			HasMore   bool         `json:"has_more"`
			MoreAfter bool         `json:"more_after"`
			itemSpan
		}
		if err := m.call(ctx, "session.items", params, &res); err != nil {
			c.sendJSON(map[string]any{"type": "error", "machine_id": m.cfg.ID, "session_id": sid, "error": err.Error()})
			return
		}
		res.Items = h.withInjected(res.Items)
		if lazy {
			res.Items = proto.WithoutImageData(res.Items)
		}
		out := res.itemSpan.into(map[string]any{"type": "items", "machine_id": m.cfg.ID, "session_id": sid, "items": res.Items, "backlog": true})
		if since == 0 {
			// Only a page counted back from the end knows whether older items exist. A catch-up after since_seq does
			// not: sending false there told the app the history was complete, and scrolling up stopped at its oldest.
			out["has_more"] = res.HasMore
		}
		// A catch-up continues page by page (one page used to be all: whatever came after its 500 items was lost).
		more := since > 0 && res.MoreAfter && len(res.Items) > 0
		if more && page+1 >= backlogPages {
			out["more_after"] = true
			more = false
		}
		c.sendJSON(out)
		if !more {
			h.sendLiveText(c, m.cfg.ID, sid)
			return
		}
		after = res.Items[len(res.Items)-1].Seq
		if res.ToSeq != nil && *res.ToSeq > after {
			after = *res.ToSeq
		}
	}
}

// itemSpan is where a batch of items sits in the transcript (node: transcript.Span), passed through to the apps:
// history names the version of the file (it changes when the file is replaced and seqs start over), from_seq the seq
// the batch continues from (only for batches that continue: catch-up, live pushes, after_seq), to_seq the last line it
// accounts for. Older nodes send none of it.
type itemSpan struct {
	History string `json:"history,omitempty"`
	FromSeq *int   `json:"from_seq,omitempty"`
	ToSeq   *int   `json:"to_seq,omitempty"`
}

func (sp itemSpan) into(m map[string]any) map[string]any {
	if sp.History != "" {
		m["history"] = sp.History
	}
	if sp.FromSeq != nil {
		m["from_seq"] = *sp.FromSeq
	}
	if sp.ToSeq != nil {
		m["to_seq"] = *sp.ToSeq
	}
	return m
}
