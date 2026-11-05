package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

// What the code moved here from the hub still asks of the hub's own parts — its interaction queue, its pushes, its
// model configuration and launchers — over the bus (app.call on extwire.AppHub).

// adminNow is the admin UI's clock (tests move it).
func (h *Manager) adminNow() time.Time {
	if h.admin.now != nil {
		return h.admin.now()
	}
	return time.Now()
}

// The send_messages states (the hub's send.go): a send claimed, done, or cut off without knowing.
const (
	sendDelivered = "delivered"
	sendUnknown   = "unknown"
	sendSending   = "sending"
)

// sendOutcome is the state a send ends in.
func sendOutcome(err error) string {
	if err == nil {
		return sendDelivered
	}
	return sendUnknown
}

// screenPort is macOS's Screen Sharing (VNC).
const screenPort = 5900

// portAllowed: the port of m the app may open that is this code's to say: the screen of a Mac the plugin cua or ego
// is on for.
func (h *Manager) portAllowed(m *Machine, port int) bool {
	if port != screenPort {
		return false
	}
	if c := h.computerOf(m); c != nil && c.Enabled {
		return true
	}
	if b := h.browserOf(m); b != nil && b.Enabled {
		return true
	}
	return false
}

// mailTargets are the languages the devices want mail translated into.
func (h *Manager) mailTargets(ctx context.Context) []string {
	var res struct {
		Targets []string `json:"targets"`
	}
	_ = h.callHub(ctx, extwire.HubMailTargets, map[string]any{}, &res)
	return res.Targets
}

// pushLocalized sends every device a push in its language (build is called once per language).
func (h *Manager) pushLocalized(build func(i18n.Lang) map[string]any, collapseID string) {
	payloads := map[string]map[string]any{}
	for _, l := range i18n.Langs {
		payloads[string(l)] = build(l)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.callHub(ctx, extwire.HubPushLocalized, map[string]any{"payloads": payloads, "collapse_id": collapseID}, nil); err != nil {
		h.logf("push: %v", err)
	}
}

// pushBackground sends devices a silent push.
func (h *Manager) pushBackground(payload map[string]any, devices []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.callHub(ctx, extwire.HubPushBackground, map[string]any{"payload": payload, "devices": devices}, nil); err != nil {
		h.logf("push: %v", err)
	}
}

// asker is the conversation a card the manager has the hub make belongs to: the clients put a card into the chat of
// its machine and session, so one with no session shows only in 需要你.
type asker struct{ machine, session string }

// assistantAsker is the assistant conversation sessionID (on the assistant's machine); with none, a card of machine.
func (h *Manager) assistantAsker(sessionID, machine string) asker {
	if h.opts.AssistantMachine != nil {
		if mid := h.opts.AssistantMachine(); sessionID != "" && mid != "" {
			return asker{mid, sessionID}
		}
	}
	return asker{machine, ""}
}

// errNotPending: the card is no longer waiting.
var errNotPending = errors.New("no longer pending")

// hubTools are the names of the hub's own tools a connector may not shadow (the phone's by their prefix), and the
// assistant's.
func (h *Manager) assistantToolsNow() []proto.Tool {
	if h.opts.AssistantTools == nil {
		return nil
	}
	return h.opts.AssistantTools()
}

// rawOr is raw, or {} when empty.
func rawOr(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// inHealth: an assistant conversation working in cwd is in the health space. What it says leaves the space only with
// the user knowing (docs/health.md「外发」): its writes are confirmed on the phone each time, whatever the gate rules
// allow.
func inHealth(cwd string) bool { return cwd != "" && identity.SpaceOf(cwd) == identity.SpaceHealth }

// askerCwd is the directory of the conversation by is ("" when it names none or its machine does not list it).
func (h *Manager) askerCwd(by asker) string {
	if by.session == "" {
		return ""
	}
	if m := h.machineByID(by.machine); m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, s := range m.sessions {
			if s.SessionID == by.session {
				return s.Cwd
			}
		}
	}
	return ""
}
