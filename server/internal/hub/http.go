package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "version": h.version, "features": proto.Features})
	})
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"machines": h.machineList()})
	})
	api.HandleFunc("POST /api/v1/machines/{mid}/reconnect", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		m.reconnect()
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("PUT /api/v1/machines/{mid}/engine", h.withMachine(h.putEngine))
	api.HandleFunc("POST /api/v1/machines/{mid}/deploy", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		if err := m.startDeploy(); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/launchers", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		ls, err := h.machineLaunchers(ctx, m)
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"launchers": ls})
	}))
	h.launcherRoutes(api)
	api.HandleFunc("GET /api/v1/machines/{mid}/dirs", h.proxy("dirs.list", func(r *http.Request) any {
		return map[string]any{"path": r.URL.Query().Get("path")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/file", h.proxy("file.read", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"path": q.Get("path"), "cwd": q.Get("cwd"), "offset": atoi(q.Get("offset"))}
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
	api.HandleFunc("GET /api/v1/sessions", h.listSessions)
	api.HandleFunc("POST /api/v1/sessions", h.startSession)
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.withMachine(h.sessionItems))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items/{iid}/image", h.withMachine(h.sessionImage))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items/{iid}/full", h.withMachine(h.sessionItemFull))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/messages", h.withMachine(h.sendMessage))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/interrupt", h.proxyBody("session.interrupt"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/keys", h.proxyBody("session.keys"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/close", h.proxyBody("session.close"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/queue", h.proxyBody("session.queue.edit"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/rename", h.withMachine(h.renameSession))
	// {"dry_run": true, "all": false}: rename the tabs of running agents nobody named after their task (tabname.go).
	api.HandleFunc("POST /api/v1/tabs/autoname", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			DryRun bool `json:"dry_run"`
			All    bool `json:"all"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		writeJSON(w, map[string]any{"tabs": h.autonameTabs(ctx, in.DryRun, in.All)})
	})
	// {"permission_mode": …, "effort": …}: either may be left out (node/modes.go).
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/modes", h.proxyBody("session.modes"))
	api.HandleFunc("POST /api/v1/machines/{mid}/spaces/{wid}/rename", h.withMachine(h.renameSpace))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/info", h.proxy("session.info", func(r *http.Request) any {
		return map[string]any{"session_id": r.PathValue("sid")}
	}))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/seen", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		h.markSeen(m.cfg.ID, r.PathValue("sid"))
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/screen/before", h.proxyBody("session.screen.before"))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/screen", h.proxy("session.screen", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"session_id": r.PathValue("sid"), "lines": atoi(q.Get("lines")), "source": q.Get("source"), "format": q.Get("format")}
	}))
	api.HandleFunc("GET /api/v1/interactions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"interactions": h.pendingInteractions()})
	})
	api.HandleFunc("POST /api/v1/interactions/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		var res proto.Resolution
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.resolve(r.PathValue("id"), res); err != nil {
			if errors.Is(err, errNotPending) {
				writeErr(w, http.StatusGone, err)
			} else {
				callErr(w, err) // prompt_changed: 409, nothing pressed
			}
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("POST /api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		var d Device
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil || d.DeviceID == "" {
			writeErr(w, http.StatusBadRequest, errors.New("device_id is required"))
			return
		}
		h.push.register(d)
		writeJSON(w, map[string]any{"ok": true})
	})
	h.live.routes(api)
	h.usageRoutes(api)
	h.metricsRoutes(api)
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
		recs, err := h.store.Interactions(r.Context(), q.Get("machine_id"), q.Get("session_id"), atoi(q.Get("limit")))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"interactions": recs})
	})
	h.assistantRoutes(api)
	h.pinRoutes(api)
	h.kbRoutes(api)
	h.personaRoutes(api)
	h.larkRoutes(api)
	h.inboxRoutes(api)
	h.translateRoutes(api)
	h.pluginRoutes(api)
	api.HandleFunc("GET /api/v1/ws", h.serveWS)
	if h.mail != nil {
		api.Handle("/api/v1/mail/", h.mail.Handler())
	}
	h.tokenRoutes(api)
	mux.Handle("/api/", h.auth(api))
	mux.Handle("/", webApp(filepath.Join(h.cfg.dir, "web")))
	return logRequests(h, mux)
}

func (h *Hub) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		p, ok := h.authenticate(r.Context(), tok)
		if !ok {
			writeErr(w, http.StatusUnauthorized, errors.New("invalid token"))
			return
		}
		if !allowed(p, r.Method, r.URL.Path) {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		// API answers are live data: without this the phone's URLCache kept a copy of every page it pulled (megabytes,
		// never read again). Images and media that may be cached set their own Cache-Control.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Unwrap lets websocket.Accept reach the underlying Hijacker.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequests logs failed and slow requests (method, path, status, time; never the query, body or token) under
// a request id: the app's X-Client-Request-Id when it sent one, which it shows next to a server error, else a new
// one. Every response carries it as X-Request-Id.
func logRequests(h *Hub, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestID(r.Header.Get("X-Client-Request-Id"))
		w.Header().Set("X-Request-Id", id)
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		long := strings.HasPrefix(r.URL.Path, "/api/v1/plugins/") // a plugin's sockets and streams
		if r.URL.Path != "/api/v1/ws" && (sw.status >= 400 || (!long && time.Since(start) > 2*time.Second)) {
			h.logf("[%s] %s %s -> %d (%s)", id, r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

// requestID keeps a client's id if it is a short token (it lands in logs), else makes one.
func requestID(client string) string {
	if n := len(client); n >= 4 && n <= 32 && strings.Trim(client, "0123456789abcdefABCDEF-") == "" {
		return client
	}
	return randomHex(4)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}

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
		m := h.byID[r.PathValue("mid")]
		if m == nil {
			writeErr(w, http.StatusNotFound, errors.New("unknown machine"))
			return
		}
		fn(w, r, m)
	}
}

func callErr(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	if errors.Is(err, errOffline) {
		code = http.StatusServiceUnavailable
	}
	// A node error with a code (a refused send: dialog_open, input_draft) is a 409 the app acts on; its data
	// (delivered, draft…) goes along as top-level fields next to error.
	var re *rpc.Error
	if errors.As(err, &re) && (re.Code != "" || len(re.Data) > 0) {
		body := map[string]any{}
		_ = json.Unmarshal(re.Data, &body)
		body["error"] = re.Message
		if re.Code != "" {
			body["code"], code = re.Code, http.StatusConflict
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	writeErr(w, code, err)
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
	writeJSON(w, it)
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
	for _, m := range h.machines {
		if mid != "" && m.cfg.ID != mid {
			continue
		}
		out = append(out, h.decorated(m)...)
	}
	writeJSON(w, map[string]any{"sessions": out})
}

func (h *Hub) startSession(w http.ResponseWriter, r *http.Request) {
	var p struct {
		MachineID       string `json:"machine_id"`
		Cwd             string `json:"cwd"`
		Launcher        string `json:"launcher"`
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
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	m := h.byID[p.MachineID]
	if m == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown machine"))
		return
	}
	p.TabName = strings.TrimSpace(p.TabName)
	nameFrom := p.Prompt
	if strings.TrimSpace(p.NameHint) != "" {
		nameFrom = p.NameHint
	}
	async := p.Async && p.ResumeSessionID == ""
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	if async {
		ctx, cancel = context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
	} else if p.TabName == "" && p.ResumeSessionID == "" {
		p.TabName = h.autoTabName(ctx, nameFrom)
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := m.call(ctx, "session.start", p, &res); err != nil {
		callErr(w, err)
		return
	}
	res.Session.MachineID = m.cfg.ID
	res.Session.State = agentState(res.Session, time.Time{})
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

// ---- WebSocket ----

type client struct {
	hub   *Hub
	conn  *websocket.Conn
	out   chan []byte
	mu    sync.Mutex
	subs  map[[2]string]bool
	phone bool
	sim   bool // a simulator registered as phone: only used when no real iPhone is connected
	// desktop: a desktop app (the Mac app, register_desktop); never the phone (desktops.go).
	desktop bool
	// device and role: whose credential opened the connection (devicetokens.go); device is empty for the pairing token.
	device, role string
	lazy         bool // asked for items without image data (subscribe with lazy_images)
	fold         bool // asked for backlogs with the agent's work folded (subscribe with fold_work)
	name         string
	since        time.Time
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
		c.conn.Close(websocket.StatusPolicyViolation, "too slow")
	}
}

func (h *Hub) broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	if m, ok := v.(map[string]any); ok {
		switch m["type"] {
		case "sessions", "interaction", "interaction_resolved": // what the Fleet activity is made of
			h.live.changed()
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.out <- b:
		default:
		}
	}
}

func (h *Hub) toSubscribers(machineID, sessionID string, v any) {
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
		if sub {
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

func (h *Hub) phoneClient() *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Prefer a real iPhone over a simulator, then the most recently registered.
	var best *client
	for c := range h.clients {
		if !c.phone {
			continue
		}
		if best == nil || (best.sim && !c.sim) || (best.sim == c.sim && c.since.After(best.since)) {
			best = c
		}
	}
	return best
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(64 << 20)
	who := principalOf(r.Context())
	c := &client{hub: h, conn: conn, out: make(chan []byte, 512), subs: map[[2]string]bool{}, since: time.Now(),
		device: who.ID, role: who.Role}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer func() {
		cancel()
		h.dropClient(c)
		c.mu.Lock()
		subs := c.subs
		c.subs = map[[2]string]bool{}
		c.mu.Unlock()
		for k := range subs {
			if m := h.byID[k[0]]; m != nil {
				m.removeWatcher(k[1])
			}
		}
		h.terminalDrop(c)
		conn.CloseNow()
	}()

	go func() {
		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-c.out:
				wctx, wcancel := context.WithTimeout(ctx, 20*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				wcancel()
				if err != nil {
					cancel()
					return
				}
			case <-ping.C:
				pctx, pcancel := context.WithTimeout(ctx, 15*time.Second)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	c.sendJSON(map[string]any{"type": "hello", "server_version": h.version, "client_id": randomHex(6), "features": proto.Features})
	c.sendJSON(map[string]any{"type": "machines", "machines": h.machineList()})
	for _, m := range h.machines {
		c.sendJSON(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": h.decorated(m)})
	}
	for _, ix := range h.pendingInteractions() {
		c.sendJSON(map[string]any{"type": "interaction", "interaction": ix})
	}
	h.sendDesktops(c) // under the hub lock, so a desktop registering meanwhile cannot be overtaken by this older list

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
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
	}
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	switch msg.Type {
	case "terminal_subscribe":
		h.terminalSubscribe(ctx, c, msg.MachineID, msg.SessionID, msg.Epoch, msg.AfterSeq, msg.Control, msg.Takeover)
	case "terminal_input":
		h.terminalInput(c, msg.MachineID, msg.SessionID, msg.Text, msg.Control, msg.Scroll, msg.At)
		if len(msg.Click) == 2 {
			h.terminalClick(c, msg.MachineID, msg.SessionID, msg.Click[0], msg.Click[1])
		}
	case "terminal_unsubscribe":
		h.terminalUnsubscribe(c, msg.MachineID, msg.SessionID)
	case "subscribe":
		m := h.byID[msg.MachineID]
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
		if m := h.byID[msg.MachineID]; m != nil && had {
			m.removeWatcher(msg.SessionID)
		}
	case "register_phone":
		if c.role != roleFull { // runs tools on behalf of claude: not for an answer-only device
			return
		}
		h.mu.Lock()
		c.phone, c.sim, c.name, c.since = true, msg.Simulator, msg.DeviceName, time.Now()
		h.mu.Unlock()
		if len(msg.Tools) > 0 {
			h.setPhoneTools(msg.Tools)
		}
	case "register_desktop":
		h.registerDesktop(c, msg.DeviceName)
	case "seen":
		if h.byID[msg.MachineID] != nil && msg.SessionID != "" {
			h.markSeen(msg.MachineID, msg.SessionID)
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
