package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// What comes from the nodes is the manager's (internal/assistant fleet_*.go; docs/machines.md "Who handles computer events"):
// the interactions, each Agent's state, which pushes go and when, the Live Activity's content, the nodes' MCP tools,
// the machines' monitoring. The hub shows and delivers what it reports:
//
//   - fleet.sessions: a machine's session list as the apps get it — kept on the machine (its sessions), sent to the
//     apps (broadcastSessions), answered on GET /api/v1/sessions;
//   - fleet.interaction / fleet.interaction_resolved / fleet.interactions: the pending interactions, kept here,
//     sent to the apps, answered on GET /api/v1/interactions and to a connecting app;
//   - fleet.live: the Live Activity's content and the island's numbers (liveactivity.go pushes it per device);
//   - notify.request: a push to deliver (push.go, notifyws.go): which devices take it is each device's settings.
//
// What is kept stays while the manager is away (the apps see the last of it); answers to the cards need it (the
// relayed routes answer 503 assistant_offline). The hub tells it what of the apps it needs (fleet.clients, fleet.seen,
// fleet.mutes), and still runs the phone's tools, the connectors', Cua's and ego's for it (app.call phone, hub).

// fleetCache is what the manager last told of the interactions and the fleet.
type fleetCache struct {
	mu           sync.Mutex
	interactions map[string]proto.Interaction
	muted        map[string][]string                 // machine → its sessions whose pushes are off, as the manager said
	live         *assistantwire.FleetLive            // the assistant's
	lives        map[string]*assistantwire.FleetLive // the other managers' (manager_state.go)
	elicitation  bool                                // what fleet.clients said last
	clientsSent  bool
}

func init() {
	handleBusEvent(assistantwire.EventFleetSessions, func(h *Hub, _ *busConn, params json.RawMessage) {
		var p assistantwire.FleetSessions
		if json.Unmarshal(params, &p) != nil {
			return
		}
		h.takeFleetSessions(p)
	})
	handleBusEvent(assistantwire.EventFleetInteraction, func(h *Hub, c *busConn, params json.RawMessage) {
		var p assistantwire.FleetInteraction
		if json.Unmarshal(params, &p) != nil || p.Interaction.ID == "" {
			return
		}
		p.Interaction.Manager = c.managerID() // its manager's (ownership.go)
		f := &h.reported
		f.mu.Lock()
		if f.interactions == nil {
			f.interactions = map[string]proto.Interaction{}
		}
		f.interactions[p.Interaction.ID] = p.Interaction
		f.mu.Unlock()
		h.broadcast(map[string]any{"type": "interaction", "interaction": p.Interaction})
	})
	handleBusEvent(assistantwire.EventFleetResolved, func(h *Hub, c *busConn, params json.RawMessage) {
		var p assistantwire.FleetResolved
		if json.Unmarshal(params, &p) != nil || p.ID == "" {
			return
		}
		f := &h.reported
		f.mu.Lock()
		if ix, ok := f.interactions[p.ID]; ok && ix.Manager != c.managerID() {
			f.mu.Unlock()
			return // another manager's
		}
		delete(f.interactions, p.ID)
		f.mu.Unlock()
		event := map[string]any{"type": "interaction_resolved", "id": p.ID}
		if p.AnsweredBy != "" {
			event["answered_by"] = p.AnsweredBy // the app marks a question the assistant answered (docs/assistant.md)
			if p.Reason != "" {
				event["reason"] = p.Reason // "Assistant answered: X, because Y"
			}
			if len(p.Answers) > 0 {
				event["answers"] = p.Answers // what "Change answer" (POST …/reanswer) starts from
			}
		}
		h.broadcast(event)
	})
	handleBusEvent(assistantwire.EventFleetInteractions, func(h *Hub, c *busConn, params json.RawMessage) {
		var p assistantwire.FleetInteractions
		if json.Unmarshal(params, &p) != nil {
			return
		}
		h.takeFleetInteractionsFrom(c.managerID(), p.Interactions)
	})
	handleBusEvent(assistantwire.EventFleetLive, func(h *Hub, c *busConn, params json.RawMessage) {
		var p assistantwire.FleetLive
		if json.Unmarshal(params, &p) != nil {
			return
		}
		f := &h.reported
		f.mu.Lock()
		if id := c.managerID(); id == assistantwire.AssistantID {
			f.live = &p
		} else {
			if f.lives == nil {
				f.lives = map[string]*assistantwire.FleetLive{}
			}
			f.lives[id] = &p
		}
		f.mu.Unlock()
		h.live.changed()
	})
}

// takeFleetSessions keeps a machine's list as the manager reported it and sends it to the apps.
func (h *Hub) takeFleetSessions(p assistantwire.FleetSessions) {
	m := h.localMachine(p.MachineID)
	if m == nil {
		return
	}
	list := p.Sessions
	if list == nil {
		list = []proto.Session{}
	}
	m.setSessionList(list)
	f := &h.reported
	f.mu.Lock()
	if f.muted == nil {
		f.muted = map[string][]string{}
	}
	f.muted[m.cfg.ID] = p.Muted
	f.mu.Unlock()
	h.broadcastSessions(m, h.decorated(m))
}

// takeFleetInteractions is takeFleetInteractionsFrom the assistant the hub starts.
func (h *Hub) takeFleetInteractions(list []proto.Interaction) {
	h.takeFleetInteractionsFrom(assistantwire.AssistantID, list)
}

// takeFleetInteractionsFrom replaces manager owner's pending interactions with what it has now that it
// (re)connected: those gone are over for the apps, the others are sent as they are. The other managers' stay.
func (h *Hub) takeFleetInteractionsFrom(owner string, list []proto.Interaction) {
	f := &h.reported
	list = slices.Clone(list)
	now := make(map[string]proto.Interaction, len(list))
	for i := range list {
		list[i].Manager = owner
		now[list[i].ID] = list[i]
	}
	f.mu.Lock()
	old := map[string]proto.Interaction{}
	all := make(map[string]proto.Interaction, len(f.interactions)+len(now))
	for id, ix := range f.interactions {
		if ix.Manager == owner || ix.Manager == "" {
			old[id] = ix
		} else {
			all[id] = ix
		}
	}
	for id, ix := range now {
		all[id] = ix
	}
	f.interactions = all
	f.mu.Unlock()
	for id := range old {
		if _, ok := now[id]; !ok {
			h.broadcast(map[string]any{"type": "interaction_resolved", "id": id})
		}
	}
	for _, ix := range list {
		if o, ok := old[ix.ID]; !ok || !sameJSON(o, ix) {
			h.broadcast(map[string]any{"type": "interaction", "interaction": ix})
		}
	}
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// decorated is a machine's sessions as the apps get them: the manager's last list.
func (h *Hub) decorated(m *Machine) []proto.Session {
	list := m.sessionList()
	h.stampSessions(m, list)
	return list
}

// stampSessions puts on each of m's sessions the manager it belongs to (ownership.go).
// A conversation of another manager than the machine's (which listed it) shows as that manager reported it: its
// title, archived, an assistant conversation.
func (h *Hub) stampSessions(m *Machine, list []proto.Session) {
	owner := m.ownerOrCarrier()
	for i := range list {
		id, v := h.conversation(list[i].SessionID)
		if id == "" || v.MachineID != "" && v.MachineID != m.cfg.ID {
			list[i].Manager = owner
			continue
		}
		list[i].Manager = id
		if id != owner {
			list[i].Assistant = true
			list[i].Archived = v.Archived
			if v.Title != "" {
				list[i].Title = v.Title
			}
		}
	}
}

// decoratedSession is one of m's sessions as the apps get it.
func (h *Hub) decoratedSession(m *Machine, sid string) (proto.Session, bool) {
	s, ok := m.session(sid)
	if ok {
		one := []proto.Session{s}
		h.stampSessions(m, one)
		s = one[0]
	}
	return s, ok
}

// awaitListed waits a little (2 s at most) for the manager's list of m to have session sid: a session just started
// is shown as the manager has it.
func (h *Hub) awaitListed(ctx context.Context, m *Machine, sid string) (proto.Session, bool) {
	for end := time.Now().Add(2 * time.Second); ; {
		if s, ok := m.session(sid); ok {
			return s, true
		}
		if time.Now().After(end) || ctx.Err() != nil {
			return proto.Session{}, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// mutedSessions are those of the machine's sessions whose pushes are off, as the manager said.
func (h *Hub) mutedSessions(mid string, _ []proto.Session) []string {
	f := &h.reported
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.muted[mid])
}

// pendingInteractions are the pending interactions as the manager told them, oldest first.
func (h *Hub) pendingInteractions() []proto.Interaction {
	f := &h.reported
	f.mu.Lock()
	out := make([]proto.Interaction, 0, len(f.interactions))
	for _, ix := range f.interactions {
		out = append(out, ix)
	}
	f.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// pendingInteraction is one of them; false when it is not pending.
func (h *Hub) pendingInteraction(id string) (proto.Interaction, bool) {
	f := &h.reported
	f.mu.Lock()
	defer f.mu.Unlock()
	ix, ok := f.interactions[id]
	return ix, ok
}

// fleetLive is the Live Activity's content and numbers as the manager told them last.
// The Live Activity is one per phone: it shows the default manager's, else the assistant's (manager_state.go).
func (h *Hub) fleetLive() (FleetState, FleetStats) {
	def := h.defaultOwner(context.Background())
	f := &h.reported
	f.mu.Lock()
	defer f.mu.Unlock()
	live := f.live
	if l := f.lives[def]; l != nil && def != assistantwire.AssistantID {
		live = l
	}
	if live == nil {
		return FleetState{}, FleetStats{}
	}
	return live.State, live.Stats
}

// waitInteraction asks the user on the phone for one of the hub's own features (a mail send, Cua's apps, ego's
// scripts, jarvis_settings): the manager shows the card and waits for its answer (fleet.ask). Without the manager
// nothing can be asked: a dismiss.
func (h *Hub) waitInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if ix.ExpiresAt.IsZero() {
		ix.ExpiresAt = time.Now().Add(time.Duration(h.cfg.HookTimeoutMinutes) * time.Minute).UTC()
	}
	if h.askVia != nil { // tests play the manager
		return h.askVia(ctx, ix)
	}
	var r proto.Resolution
	if err := h.asstOf(h.machineOwner(ix.MachineID)).call(ctx, assistantwire.MethodFleetAsk, map[string]any{"interaction": ix}, &r); err != nil {
		h.logf("[%s] ask %s: %v", ix.MachineID, ix.ToolName, err)
		return proto.Resolution{Dismiss: true}, nil
	}
	return r, nil
}

// ---- what the hub tells the manager of the apps ----

// fleetClientsChanged tells the manager what the connected apps can take now, when it changed (fleet.clients).
func (h *Hub) fleetClientsChanged() {
	el := h.hasElicitationClient()
	f := &h.reported
	f.mu.Lock()
	same := f.clientsSent && f.elicitation == el
	f.elicitation, f.clientsSent = el, true
	f.mu.Unlock()
	if !same {
		if err := h.notifyManagers(assistantwire.EventClients, assistantwire.Clients{Elicitation: el}); err != nil {
			f.mu.Lock()
			f.clientsSent = false
			f.mu.Unlock()
		}
	}
}

// fleetConnected: the manager's handshake went through: it is told what of the apps it needs.
func (h *Hub) fleetConnected() {
	f := &h.reported
	f.mu.Lock()
	f.clientsSent = false
	f.mu.Unlock()
	h.fleetClientsChanged()
}

// markSeen tells the manager the user looked at a session (an app's WebSocket seen).
func (h *Hub) markSeen(machine, session string) {
	_ = h.notifyBus(h.sessionOwner(machine, session), assistantwire.EventSeen, assistantwire.FleetSeen{MachineID: machine, SessionID: session})
}

// mutesChanged tells the manager the muted spaces / agents changed (settings mutes).
func (h *Hub) mutesChanged() {
	_ = h.notifyManagers(assistantwire.EventMutes, map[string]any{})
}

// fleetRoutesRelayed relays the apps' requests on the manager's routes on the interactions and the sessions' states
// (it answers them as the hub did); while it is not connected they are answered 503 assistant_offline. A device
// that may only answer is held to a card's own choices first, here.
func (h *Hub) fleetRoutesRelayed(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/interactions/{id}/resolve", h.withClientOp("resolve", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeBodyErr(w, err, err)
			return
		}
		var res proto.Resolution
		if err := json.Unmarshal(body, &res); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if answerOnly(r) && (!answerMayResolve(res) || h.picksFreeText(r.PathValue("id"), res)) {
			writeErr(w, http.StatusForbidden, errors.New("this device can only answer with a card's own choices"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.forwardOwned(w, r, h.byInteraction)
	}))
	// "Change answer": a question the assistant answered gets the user's answer (assistant/fleet_delegate.go); a device that may
	// only answer cards does not send agents messages.
	api.HandleFunc("POST /api/v1/interactions/{id}/reanswer", func(w http.ResponseWriter, r *http.Request) {
		if answerOnly(r) {
			writeErr(w, http.StatusForbidden, errors.New("this device can only answer with a card's own choices"))
			return
		}
		h.forwardOwned(w, r, h.byRequest) // answered already: no longer pending, the request names its manager
	})
	api.HandleFunc("GET /api/v1/interactions/history", func(w http.ResponseWriter, r *http.Request) {
		if h.requestHasElicitation(r) {
			r.Header.Set(assistantwire.ElicitationHeader, "1")
		} else {
			r.Header.Del(assistantwire.ElicitationHeader)
		}
		h.forwardOwned(w, r, h.byRequest)
	})
	api.HandleFunc("POST /api/v1/sessions/{mid}/{sid}/seen", h.withMachine(func(w http.ResponseWriter, r *http.Request, _ *Machine) {
		h.forwardOwned(w, r, h.bySession)
	}))
}

// ---- what the manager asks of the hub for the nodes ----

// fleetAppCall answers the manager's app.call for what is still the hub's to run for the nodes: the phone's tools;
// false for the others. (The connectors', Cua's and ego's tools, the gated commands' text, lark-cli's settings and the
// channels' owner actions are the manager's extensions': internal/extmgr, in its process.)
func (h *Hub) fleetAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	var a struct {
		Assistant     bool            `json:"assistant"`
		MachineID     string          `json:"machine_id"`
		SessionID     string          `json:"session_id"`
		Name          string          `json:"name"`
		Args          json.RawMessage `json:"args"`
		ToolName      string          `json:"tool_name"`
		Summary       string          `json:"summary"`
		InteractionID string          `json:"interaction_id"`
		Action        string          `json:"action"`
	}
	if len(p.Params) > 0 {
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, true, err
		}
	}
	var out any
	switch p.AppID + " " + p.Method {
	case assistantwire.AppPhone + " " + assistantwire.PhoneTools:
		out = map[string]any{"tools": h.phoneToolList()}
	case assistantwire.AppPhone + " " + assistantwire.PhoneCall:
		if a.Name == runtimeCLITool || !strings.HasPrefix(a.Name, "phone_") {
			out = proto.TextResult("unknown tool "+a.Name, true)
		} else {
			out = h.callPhone(ctx, a.Name, a.Args)
		}
	default:
		return nil, false, nil
	}
	b, err := json.Marshal(out)
	return b, true, err
}

// ---- the pushes the manager decided on ----

// deliverPush delivers what the manager asks (notify.request): an assistant's push, an agent's finished turn, a
// card. Only the center has apps and devices.
func (h *Hub) deliverPush(n assistantwire.Notification) error {
	switch n.Kind {
	case assistantwire.NotifyPush:
		h.notifyDone("assistant", n.DeviceID, n.Subtitle, n.SessionID, n.Title, n.Body, pushWants{})
	case assistantwire.NotifyDone:
		h.notifyDone("done", n.DeviceID, n.Subtitle, n.SessionID, n.Title, n.Body,
			pushWants{Kind: "done", Took: time.Duration(n.TookMS) * time.Millisecond, Long: time.Duration(n.LongMS) * time.Millisecond})
	case assistantwire.NotifyInput:
		if n.Interaction == nil {
			return errors.New("an input push needs its interaction")
		}
		h.notifyInteraction(*n.Interaction, n.Subtitle)
	default:
		return fmt.Errorf("unknown notification kind %q", n.Kind)
	}
	return nil
}
