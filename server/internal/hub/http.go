package hub

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func (h *Hub) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "version": h.version})
	})
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"machines": h.machineList()})
	})
	api.HandleFunc("POST /api/v1/machines/{mid}/reconnect", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		m.reconnect()
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/launchers", h.proxy("launchers.list", nil))
	api.HandleFunc("GET /api/v1/machines/{mid}/dirs", h.proxy("dirs.list", func(r *http.Request) any {
		return map[string]any{"path": r.URL.Query().Get("path")}
	}))
	api.HandleFunc("GET /api/v1/machines/{mid}/file", h.proxy("file.read", func(r *http.Request) any {
		return map[string]any{"path": r.URL.Query().Get("path"), "cwd": r.URL.Query().Get("cwd")}
	}))
	api.HandleFunc("GET /api/v1/sessions", h.listSessions)
	api.HandleFunc("POST /api/v1/sessions", h.startSession)
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/items", h.proxy("session.items", func(r *http.Request) any {
		q := r.URL.Query()
		return map[string]any{"session_id": r.PathValue("sid"), "before_seq": atoi(q.Get("before_seq")), "limit": atoi(q.Get("limit"))}
	}))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/messages", h.proxyBody("session.send"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/interrupt", h.proxyBody("session.interrupt"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/keys", h.proxyBody("session.keys"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/close", h.proxyBody("session.close"))
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/seen", h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		h.markSeen(m.cfg.ID, r.PathValue("sid"))
		writeJSON(w, map[string]any{"ok": true})
	}))
	api.HandleFunc("GET /api/v1/sessions/{mid}/{sid}/screen", h.proxy("session.screen", func(r *http.Request) any {
		return map[string]any{"session_id": r.PathValue("sid"), "lines": atoi(r.URL.Query().Get("lines"))}
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
			writeErr(w, http.StatusGone, err)
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
	api.HandleFunc("GET /api/v1/ws", h.serveWS)
	if h.mail != nil {
		api.Handle("/api/v1/mail/", h.mail.Handler())
	}
	mux.Handle("/api/", h.auth(api))
	return logRequests(h, mux)
}

func (h *Hub) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.Token)) != 1 {
			writeErr(w, http.StatusUnauthorized, errors.New("invalid token"))
			return
		}
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

func logRequests(h *Hub, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path != "/api/v1/ws" && (sw.status >= 400 || time.Since(start) > 2*time.Second) {
			h.logf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
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
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
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
}

// ---- WebSocket ----

type client struct {
	hub   *Hub
	conn  *websocket.Conn
	out   chan []byte
	mu    sync.Mutex
	subs  map[[2]string]bool
	phone bool
	name  string
	since time.Time
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

func (h *Hub) phoneClient() *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *client
	for c := range h.clients {
		if c.phone && (best == nil || c.since.After(best.since)) {
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
	c := &client{hub: h, conn: conn, out: make(chan []byte, 512), subs: map[[2]string]bool{}, since: time.Now()}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(r.Context())
	defer func() {
		cancel()
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		c.mu.Lock()
		subs := c.subs
		c.subs = map[[2]string]bool{}
		c.mu.Unlock()
		for k := range subs {
			if m := h.byID[k[0]]; m != nil {
				m.removeWatcher(k[1])
			}
		}
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

	c.sendJSON(map[string]any{"type": "hello", "server_version": h.version, "client_id": randomHex(6)})
	c.sendJSON(map[string]any{"type": "machines", "machines": h.machineList()})
	for _, m := range h.machines {
		c.sendJSON(map[string]any{"type": "sessions", "machine_id": m.cfg.ID, "sessions": h.decorated(m)})
	}
	for _, ix := range h.pendingInteractions() {
		c.sendJSON(map[string]any{"type": "interaction", "interaction": ix})
	}

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
		DeviceName string          `json:"device_name"`
		Tools      []proto.Tool    `json:"tools"`
		CallID     string          `json:"call_id"`
		Content    []proto.Content `json:"content"`
		IsError    bool            `json:"is_error"`
	}
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	switch msg.Type {
	case "subscribe":
		m := h.byID[msg.MachineID]
		if m == nil {
			return
		}
		key := [2]string{msg.MachineID, msg.SessionID}
		c.mu.Lock()
		already := c.subs[key]
		c.subs[key] = true
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
		h.mu.Lock()
		c.phone, c.name, c.since = true, msg.DeviceName, time.Now()
		h.mu.Unlock()
		if len(msg.Tools) > 0 {
			h.setPhoneTools(msg.Tools)
		}
	case "seen":
		if h.byID[msg.MachineID] != nil && msg.SessionID != "" {
			h.markSeen(msg.MachineID, msg.SessionID)
		}
	case "tool_result":
		h.toolResult(msg.CallID, proto.ToolResult{Content: msg.Content, IsError: msg.IsError})
	}
}

func (h *Hub) sendBacklog(ctx context.Context, c *client, m *Machine, sid string, since int) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := map[string]any{"session_id": sid, "limit": 80}
	if since > 0 {
		params["after_seq"] = since
	}
	var res struct {
		Items   []proto.Item `json:"items"`
		HasMore bool         `json:"has_more"`
	}
	if err := m.call(ctx, "session.items", params, &res); err != nil {
		c.sendJSON(map[string]any{"type": "error", "machine_id": m.cfg.ID, "session_id": sid, "error": err.Error()})
		return
	}
	c.sendJSON(map[string]any{"type": "items", "machine_id": m.cfg.ID, "session_id": sid, "items": res.Items,
		"backlog": true, "has_more": res.HasMore && since == 0})
}
