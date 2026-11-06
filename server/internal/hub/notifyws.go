package hub

import (
	"maps"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
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
func (h *Hub) notifyDone(owner, kind, machineID, machine, sessionID, title, text string, wants pushWants) {
	build := func(l i18n.Lang) map[string]any { return donePayloadIn(l, machineID, machine, sessionID, title, text) }
	h.emitToExtensions(owner, "agent.finished", map[string]any{"machine_id": machineID, "machine": machine, "session_id": sessionID, "title": title})
	h.push.sendLocalizedQuiet(build, "", wants.fn(), wants.quiet())
	h.notifyWSQuiet(kind, build, wants.fn(), wants.quiet())
}

// notifyReply reminds the devices that take "needs you" pushes of an agent still waiting on the user's answer (#5096).
// It is laid out as a finished turn's (answerable from the notification, opens the session), but nothing finished:
// the extensions hear no agent.finished. On the WebSocket it is a "reply" notify (not "done": Android puts away the
// finished notifications of seen sessions when it starts, and a reminder is about a seen one).
func (h *Hub) notifyReply(machineID, machine, sessionID, title, text string) {
	build := func(l i18n.Lang) map[string]any { return donePayloadIn(l, machineID, machine, sessionID, title, text) }
	wants := pushWants{Kind: "input"}
	h.push.sendLocalizedQuiet(build, "", wants.fn(), wants.quiet())
	h.notifyWSQuiet("reply", build, wants.fn(), wants.quiet())
}

func (h *Hub) notifyInteraction(owner string, ix proto.Interaction, machine string) {
	build := func(l i18n.Lang) map[string]any { return interactionPayloadIn(l, ix, machine) }
	h.emitToExtensions(owner, "agent.needs_you", map[string]any{"machine_id": ix.MachineID, "machine": machine, "session_id": ix.SessionID,
		"interaction_id": ix.ID, "kind": ix.Kind, "summary": ix.Summary})
	wants := pushWants{Kind: "input"}
	if ix.Kind == proto.IxElicitation {
		h.notifyWSQuiet("interaction", build, wants.fn(), wants.quiet(), "elicitation")
	} else {
		h.push.sendCard(build, ix.ID, wants.fn(), wants.quiet())
		h.notifyWSQuiet("interaction", build, wants.fn(), wants.quiet())
	}
}

// notifyWS projects the APNs alert onto the opt-in WebSocket notification event.
// It uses the authenticated device's preferences, even without an APNs token or key.
func (h *Hub) notifyWS(kind string, payload map[string]any, wants func(NotifyPrefs) bool, required ...string) {
	h.notifyWSIn(kind, func(i18n.Lang) map[string]any { return payload }, wants, required...)
}

// notifyWSIn is notifyWS with the alert written in each connection's language (client.lang).
func (h *Hub) notifyWSIn(kind string, build localized, wants func(NotifyPrefs) bool, required ...string) {
	h.notifyWSQuiet(kind, build, wants, quietNone, required...)
}

// notifyWSQuiet is notifyWSIn with what each device's quiet hours do to this notification (quiethours.go): skipped, or
// marked "quiet" for the client to post without a sound.
func (h *Hub) notifyWSQuiet(kind string, build localized, wants func(NotifyPrefs) bool, rule quietRule, required ...string) {
	msgs := map[i18n.Lang]map[string]any{}
	msgIn := func(l i18n.Lang) map[string]any {
		if m, ok := msgs[l]; ok {
			return m
		}
		m := notifyMessage(kind, build(l))
		msgs[l] = m
		return m
	}
	// Mute phone holds these back while a desktop is online, with the same rule as the APNs pushes (alertWanted): an
	// Android phone alerts from them. desktopOnline takes h.mu, so it is read before.
	wants = alertWanted(wants, h.desktopOnline())
	// Read before h.mu as well: the user's time zone asks the assistant's profile.
	now, loc := time.Now(), (*time.Location)(nil)
	if rule != quietNone && h.push != nil {
		now, loc = h.push.now(), h.userLocation()
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
		if wants != nil && !wants(prefs) {
			continue
		}
		quiet := rule != quietNone && prefs.Quiet.activeAt(now, loc)
		switch {
		case quiet && rule == quietSkip:
		case quiet:
			msg := maps.Clone(msgIn(c.lang()))
			msg["quiet"] = true
			c.sendJSON(msg)
		default:
			c.sendJSON(msgIn(c.lang()))
		}
	}
}

func notifyMessage(kind string, payload map[string]any) map[string]any {
	payload = cleanAlert(payload)
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
	if en, ok := payload["extension_notification"]; ok {
		msg["extension_notification"] = en
	}
	return msg
}
