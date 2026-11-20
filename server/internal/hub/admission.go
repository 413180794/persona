package hub

import (
	"net/http"

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/devicewire"
)

func (h *Hub) admitHTTP(next http.Handler) http.Handler {
	if h.admission == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The app socket has a separate handshake and frame gate. Manager
		// transports can still deliver results and bodies for admitted work.
		switch r.URL.EscapedPath() {
		case "/api/v1/health", "/api/v1/ws", bus.Path, devicewire.RelayPath:
			next.ServeHTTP(w, r)
			return
		}
		done, ok := h.admission.HTTP("hub-http", w)
		if !ok {
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}

// Frames that finish an existing request must still run after the fence closes.
// New frames are refused on the existing connection, so completion frames can
// still arrive there. The transport itself stays alive.
func (h *Hub) admitClientFrame(c *client, kind string) (func(), bool) {
	switch kind {
	case "ping", "tool_result", "herdr_ack", "herdr_close", "terminal_unsubscribe", "unsubscribe":
		return nil, true
	}
	done, err := h.admission.Admit("app-frame")
	if err != nil {
		c.sendJSON(map[string]any{"type": "error", "code": "maintenance", "error": "service paused for maintenance", "retry_after_ms": 5000})
		return nil, false
	}
	return done, true
}
