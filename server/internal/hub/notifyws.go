package hub

import (
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// pushWants is which devices' settings take a push.
type pushWants struct {
	Kind string        // "" every device | input | done
	Took time.Duration // done: how long the turn took
	Long time.Duration // done: what counts as a long one
}

func (w pushWants) fn() func(NotifyPrefs) bool {
	switch w.Kind {
	case "input":
		return func(p NotifyPrefs) bool { return p.Input }
	case "done":
		return func(p NotifyPrefs) bool { return p.wantsDone(w.Took, w.Long) }
	}
	return nil
}

// notifyDone delivers the same alert and preference decision through both channels. Only the center has apps and
// devices: a member's computers are the center's machines.
func (h *Hub) notifyDone(kind, machineID, machine, sessionID, title, text string, wants pushWants) {
	payload := donePayload(machineID, machine, sessionID, title, text)
	h.push.send(payload, "", wants.fn())
	h.notifyWS(kind, payload, wants.fn())
}

func (h *Hub) notifyInteraction(ix proto.Interaction, machine string) {
	payload := interactionPayload(ix, machine)
	wants := pushWants{Kind: "input"}
	if ix.Kind == proto.IxElicitation {
		h.notifyWS("interaction", payload, wants.fn(), "elicitation")
	} else {
		h.push.send(payload, ix.ID, wants.fn())
		h.notifyWS("interaction", payload, wants.fn())
	}
}

// notifyWS projects the APNs alert onto the opt-in WebSocket notification event.
// It uses the authenticated device's preferences, even without an APNs token or key.
func (h *Hub) notifyWS(kind string, payload map[string]any, wants func(NotifyPrefs) bool, required ...string) {
	aps := payload["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	msg := map[string]any{
		"type": "notify", "kind": kind,
		"machine_id": payload["machine_id"], "session_id": payload["session_id"],
		"title": alert["title"], "subtitle": alert["subtitle"], "body": alert["body"],
	}
	if id, ok := payload["interaction_id"]; ok {
		msg["interaction_id"] = id
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if !c.has("ws_notify") {
			continue
		}
		if len(required) > 0 && !c.has(required[0]) {
			continue
		}
		prefs := defaultPrefs()
		if h.push != nil {
			prefs = h.push.prefs(c.device)
		}
		if wants == nil || wants(prefs) {
			c.sendJSON(msg)
		}
	}
}
