package hub

import (
	"net/http"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// health answers GET /api/v1/health, the one route without authentication.
func (h *Hub) health(w http.ResponseWriter, r *http.Request) {
	h.healthWithSource(w, r, h.sourceOf)
}

func (h *Hub) healthWithSource(w http.ResponseWriter, r *http.Request, sourceOf func(*http.Request) source) {
	port, fp := h.tlsPin()
	response := map[string]any{"ok": true, "hub_id": h.hubID(), "hub_name": h.hubName(), "tls_port": port, "fp": fp}
	if sourceOf(r) != sourcePublic {
		response["version"], response["features"] = h.version, proto.Features
		response["channel"], response["build"] = h.build.Channel, h.build.Number
		response["protocol"], response["min_app_build"] = proto.Protocol, proto.MinAppBuild
		response["update"] = map[string]any{"confirmed": h.upd == nil || h.upd.isConfirmed()} // the watchdog waits for this
	}
	writeJSON(w, response)
}

// hello is the first message on every WebSocket. Fields added here must not reuse a key the iOS app decodes with a
// different type (its LiveConnection Envelope), or the whole hello is dropped there (hub-changes.md 1.2).
func (h *Hub) hello() map[string]any {
	port, fp := h.tlsPin()
	return map[string]any{
		"type": "hello", "server_version": h.version, "client_id": randomHex(6), "features": proto.Features,
		"channel": h.build.Channel, "build": h.build.Number, "protocol": proto.Protocol, "min_app_build": proto.MinAppBuild,
		"hub_id": h.hubID(), "hub_name": h.hubName(), "endpoints": h.endpointList(), "tls_port": port, "fp": fp,
		// The hubs of this hub's mesh group, itself included (docs/plan-hub-mesh.md 2.6): only after authentication.
		"mesh": map[string]any{"hubs": h.meshHubs()},
	}
}
