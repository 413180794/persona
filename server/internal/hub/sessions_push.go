package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/proto"
)

// The session lists the apps get (docs/protocol.md「会话增量」, docs/server.md「推送给 App 的量」). An app that asks for
// sessions_delta (?features= on the WebSocket) gets each machine's list once when it connects, then only what
// changed: the sessions that are new or different, each whole, and the ids that went. An older app gets the whole
// list each time, as before, though never the same list twice. A machine's list is ~1 KB a session and up to 150
// sessions, and one session changes at a time: the phone downloaded and decoded ~2.6 MB a minute of lists.
const featureSessionsDelta = "sessions_delta"

// wantsFeature: the app listed feature in the WebSocket URL's ?features= (comma-separated).
func wantsFeature(r *http.Request, feature string) bool {
	return urlFeatures(r)[feature]
}

// urlFeatures is everything the app listed in the WebSocket URL's ?features=.
func urlFeatures(r *http.Request) map[string]bool {
	set := map[string]bool{}
	for _, f := range strings.Split(r.URL.Query().Get("features"), ",") {
		if f != "" {
			set[f] = true
		}
	}
	return set
}

// sentSession is one session of a list, as sent.
type sentSession struct {
	id string
	// history: a Codex session known only from its rollout (codex_history). Not pushed to a delta app unless it has the
	// session already (a live one that ended): the app fetches the history when it wants it (GET /api/v1/sessions).
	history bool
	raw     json.RawMessage
}

func encodeSessions(list []proto.Session) []sentSession {
	out := make([]sentSession, 0, len(list))
	for _, s := range list {
		if b, err := json.Marshal(s); err == nil {
			out = append(out, sentSession{id: s.SessionID, history: s.SourceKind == "codex_history", raw: b})
		}
	}
	return out
}

// sessionsMessage is the whole-list message (the same bytes as marshalling the sessions themselves).
func sessionsMessage(machine string, list []sentSession) []byte {
	raws := make([]json.RawMessage, len(list))
	for i, s := range list {
		raws[i] = s.raw
	}
	b, _ := json.Marshal(map[string]any{"type": "sessions", "machine_id": machine, "sessions": raws})
	return b
}

// forDelta is what a delta app gets of list, given what it has.
func forDelta(list []sentSession, have map[string][]byte) []sentSession {
	out := make([]sentSession, 0, len(list))
	for _, s := range list {
		if _, had := have[s.id]; s.history && !had {
			continue
		}
		out = append(out, s)
	}
	return out
}

// sessionsDelta is the message taking an app from have to list (nil: nothing changed), and what it then has.
func sessionsDelta(machine string, list []sentSession, have map[string][]byte) ([]byte, map[string][]byte) {
	shown := forDelta(list, have)
	next := make(map[string][]byte, len(shown))
	upsert, remove := []json.RawMessage{}, []string{}
	for _, s := range shown {
		next[s.id] = s.raw
		if old, ok := have[s.id]; !ok || !bytes.Equal(old, s.raw) {
			upsert = append(upsert, s.raw)
		}
	}
	for id := range have {
		if _, ok := next[id]; !ok {
			remove = append(remove, id)
		}
	}
	if len(upsert) == 0 && len(remove) == 0 {
		return nil, next
	}
	slices.Sort(remove)
	b, _ := json.Marshal(map[string]any{"type": "sessions_delta", "machine_id": machine, "upsert": upsert, "remove": remove})
	return b, next
}

// broadcastSessions sends every app a machine's session list: an older app the whole list unless it is the list
// they last got (an interaction shown, a session seen, a node pass that changed nothing the app reads), a delta app
// what changed for it. The lock is held until everything is queued, so what was recorded is what the apps got last.
func (h *Hub) broadcastSessions(m *Machine, list []proto.Session) {
	enc := encodeSessions(h.shown(m.cfg.ID, list))
	full := sessionsMessage(m.cfg.ID, enc)
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	changed := !bytes.Equal(h.sessionsSent[m.cfg.ID], full)
	if changed {
		if h.sessionsSent == nil {
			h.sessionsSent = map[string][]byte{}
		}
		h.sessionsSent[m.cfg.ID] = full
		h.live.changed() // what the Fleet activity is made of
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		var b []byte
		if c.delta {
			b, c.have[m.cfg.ID] = sessionsDelta(m.cfg.ID, enc, c.have[m.cfg.ID])
		} else if changed {
			b = full
		}
		if b != nil {
			c.queue(b)
		}
	}
}

// snapshotSessions sends a delta app that just connected a machine's whole list (without the Codex history), what
// later deltas build on. Under sessionsMu, so no delta for the machine can come between the list and the record.
func (h *Hub) snapshotSessions(c *client, m *Machine) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	shown := forDelta(encodeSessions(h.shown(m.cfg.ID, h.decorated(m))), nil)
	have := make(map[string][]byte, len(shown))
	for _, s := range shown {
		have[s.id] = s.raw
	}
	c.have[m.cfg.ID] = have
	c.queue(sessionsMessage(m.cfg.ID, shown))
}

// queue puts b on the client's way out; a client 512 messages behind is dropped (it would miss this one for good
// now that nothing is sent twice) and resyncs when it reconnects.
func (c *client) queue(b []byte) {
	select {
	case c.out <- b:
	default:
		if c.conn != nil {
			go c.conn.Close(websocket.StatusPolicyViolation, "too slow")
		}
	}
}
