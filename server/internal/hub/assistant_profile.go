package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The assistant's settings are its own (internal/assistant, profile.go): the hub keeps the profile it last reported
// (Snapshot.Profile) for its own features — the model configuration runs 主对话 on the assistant's machine, a phone's
// message to the main conversation, the user's time zone, the conversations the nodes keep listing — and asks the
// assistant about its conversations (assistant.start, assistant.send_params, assistant.rename). The Feishu bot and the
// channels are the assistant's own (assistant_channels.go): they do not go by it. Before
// the first report since it started the hub goes by the copy it kept (loadAssistantProfile); with none, by hub.json's
// assistant section: its machine (else the hub's own, else the first), its launcher, no main conversation, the hub's
// own time zone, and nothing said to the nodes about what they keep.

// assistantProfileKey is the hub's own copy of the profile the assistant last reported (the hub writes and reads it,
// the assistant never does): what the hub's features go by after a restart, before the assistant reports again. The
// knowledge base and the plugins start before it connects, and they need the user's time zone. Without it they went by
// the hub's own zone for those seconds (the knowledge base filed itself into the hub's zone and back).
const assistantProfileKey = "hub.assistant_profile"

// loadAssistantProfile takes up the profile the hub kept (assistantProfileKey) before the assistant reports one. The
// first start after the settings moved into the assistant has none yet: it is read once from the assistant's settings
// as they were left (settings assistant: machine, main conversation, time zone), and kept as the hub's own from then on.
func (h *Hub) loadAssistantProfile(ctx context.Context) {
	if h.store == nil {
		return
	}
	var p assistantwire.Profile
	ok, err := h.store.Setting(ctx, assistantProfileKey, &p)
	if err != nil {
		h.logf("assistant: the profile kept: %v", err)
		return
	}
	if !ok {
		var was struct {
			MachineID string `json:"machine_id"`
			Main      string `json:"main"`
			Timezone  string `json:"timezone"`
		}
		if found, err := h.store.Setting(ctx, "assistant", &was); err != nil || !found {
			return
		}
		// Keep stays unknown (nil): the nodes keep what they list until the assistant says.
		p = assistantwire.Profile{MachineID: was.MachineID, Main: was.Main, Timezone: was.Timezone}
		if h.localMachine(p.MachineID) == nil {
			p.MachineID = h.defaultAssistantMachine()
		}
		if p.Launcher = "claude"; h.cfg != nil && h.cfg.Assistant.Launcher != "" {
			p.Launcher = h.cfg.Assistant.Launcher
		}
		h.keepAssistantProfile(ctx, &p)
	}
	a := &h.assistant
	a.mu.Lock()
	if a.profile == nil { // a report that came in meanwhile wins
		a.profile = &p
	}
	a.mu.Unlock()
}

// keepAssistantProfile stores p as the hub's copy (assistantProfileKey).
func (h *Hub) keepAssistantProfile(ctx context.Context, p *assistantwire.Profile) {
	if h.store == nil || p == nil {
		return
	}
	if err := h.store.PutSetting(ctx, assistantProfileKey, p); err != nil {
		h.logf("assistant: keep the profile: %v", err)
	}
}

// assistantProfile is the assistant's settings as it last reported them (or as the hub kept them from before a
// restart, loadAssistantProfile), else hub.json's defaults.
func (h *Hub) assistantProfile() assistantwire.Profile {
	a := &h.assistant
	a.mu.RLock()
	p := a.profile
	a.mu.RUnlock()
	if p != nil {
		return *p
	}
	out := assistantwire.Profile{MachineID: h.defaultAssistantMachine(), Launcher: "claude"}
	if h.cfg != nil && h.cfg.Assistant.Launcher != "" {
		out.Launcher = h.cfg.Assistant.Launcher
	}
	return out
}

// assistantMachine is the machine the assistant's conversations run on (nil when the hub has none).
func (h *Hub) assistantMachine() *Machine { return h.localMachine(h.assistantProfile().MachineID) }

// isMainOn: session on machine is the assistant's main conversation, as it reported.
func (h *Hub) isMainOn(machine, session string) bool {
	p := h.assistantProfile()
	return p.Main != "" && p.Main == session && p.MachineID == machine
}

// defaultAssistantMachine is where hub.json runs the assistant: its assistant.machine when the hub has it, else the
// hub's own machine, else the first one.
func (h *Hub) defaultAssistantMachine() string {
	if h.cfg != nil && h.localMachine(h.cfg.Assistant.Machine) != nil {
		return h.cfg.Assistant.Machine
	}
	machines := h.machinesNow()
	for _, m := range machines {
		if m.cfg.Kind == "local" {
			return m.cfg.ID
		}
	}
	if len(machines) > 0 {
		return machines[0].cfg.ID
	}
	return ""
}

// keptOn are the sessions machine's node lists however long they are quiet, as the assistant reported them; false
// before it reported them (the node keeps what it has).
func (h *Hub) keptOn(machine string) ([]string, bool) {
	a := &h.assistant
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.profile == nil || a.profile.Keep == nil {
		return nil, false
	}
	return keptByMachine(a.profile.Keep)[machine], true
}

func keptByMachine(refs []assistantwire.SessionRef) map[string][]string {
	out := map[string][]string{}
	for _, r := range refs {
		out[r.MachineID] = append(out[r.MachineID], r.SessionID)
	}
	return out
}

// keepChanged tells the nodes whose kept sessions differ between two reported profiles (node.configure
// keep_sessions); after the first report every node is told.
func (h *Hub) keepChanged(old, now *assistantwire.Profile) {
	if now == nil || now.Keep == nil {
		return
	}
	after := keptByMachine(now.Keep)
	var before map[string][]string
	if old != nil && old.Keep != nil {
		before = keptByMachine(old.Keep)
	}
	for _, m := range h.machinesNow() {
		keep := after[m.cfg.ID]
		if before != nil && slices.Equal(before[m.cfg.ID], keep) {
			continue
		}
		go func() {
			// One at a time, and what the latest report says when it goes: two reports close together (a new main
			// conversation, then its first message) must not reach the node in the other order.
			h.keepMu.Lock()
			defer h.keepMu.Unlock()
			keep, ok := h.keptOn(m.cfg.ID)
			if !ok {
				return
			}
			if keep == nil {
				keep = []string{}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := m.call(ctx, "node.configure", map[string]any{"keep_sessions": keep}, nil); err != nil && !errors.Is(err, errOffline) {
				h.logf("[%s] assistant: keep sessions: %v", m.cfg.ID, err)
			}
		}()
	}
}

// ---- asking the assistant about its conversations ----

// Start asks the assistant to start a conversation (assistant.start).
func (a assistantRemote) Start(ctx context.Context, p assistantwire.StartParams) (*proto.Session, error) {
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := a.call(ctx, assistantwire.MethodStart, p, &res); err != nil {
		return nil, err
	}
	return &res.Session, nil
}

// SendParams fills params (a message for the assistant's conversation session on machine) with what the assistant
// runs it on (assistant.send_params).
func (a assistantRemote) SendParams(ctx context.Context, machine, session string, params map[string]any) (map[string]any, error) {
	var res struct {
		Params map[string]any `json:"params"`
	}
	err := a.call(ctx, assistantwire.MethodSendParams, assistantwire.SendParams{MachineID: machine, SessionID: session, Params: params}, &res)
	if err != nil {
		return nil, err
	}
	if res.Params == nil {
		res.Params = map[string]any{}
	}
	return res.Params, nil
}

// Rename has the assistant rename its conversation session on machine for the user (assistant.rename).
func (a assistantRemote) Rename(ctx context.Context, machine, session string, params map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	err := a.call(ctx, assistantwire.MethodRename, assistantwire.RenameParams{MachineID: machine, SessionID: session, Params: params}, &out)
	return out, err
}

// assistantSend fills p, a message for the assistant's conversation sid on m, as the assistant runs it.
func (h *Hub) assistantSend(ctx context.Context, m *Machine, sid string, p map[string]any) error {
	out, err := h.asst().SendParams(ctx, m.cfg.ID, sid, p)
	if err != nil {
		return err
	}
	clear(p)
	for k, v := range out {
		p[k] = v
	}
	return nil
}

// writeAssistantErr answers an app's request the assistant could not take: 503 assistant_offline while it is not
// connected, 400 when it says the request is wrong, else as a node call's error.
func writeAssistantErr(w http.ResponseWriter, err error) {
	var re *rpc.Error
	if errors.As(err, &re) {
		switch re.Code {
		case assistantwire.CodeAssistantOffline:
			writeErrCode(w, http.StatusServiceUnavailable, assistantwire.CodeAssistantOffline, errAssistantOffline)
			return
		case assistantwire.CodeBadRequest:
			writeErr(w, http.StatusBadRequest, errors.New(re.Message))
			return
		case assistantwire.CodeMachineOffline: // as the hub's own call to that machine would answer
			callErr(w, fmt.Errorf("%s: %w", re.Message, errOffline))
			return
		case assistantwire.CodeNodeClosed:
			callErr(w, fmt.Errorf("%s: %w", re.Message, rpc.ErrClosed))
			return
		}
	}
	callErr(w, err)
}

// renameSession is POST /api/v1/sessions/{mid}/{sid}/rename ({"title", "tab_name"}): the node renames the tab and /
// or the session; an assistant conversation is the assistant's to rename (assistant.rename).
func (h *Hub) renameSession(w http.ResponseWriter, r *http.Request, m *Machine) {
	p := map[string]any{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil && !errors.Is(err, errEOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sid := r.PathValue("sid")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out := json.RawMessage(`{"ok":true}`)
	if s, _ := m.session(sid); isAssistant(s, m.assistantCwd()) {
		res, err := h.asst().Rename(ctx, m.cfg.ID, sid, p)
		if err != nil {
			writeAssistantErr(w, err)
			return
		}
		if len(res) > 0 {
			out = res
		}
	} else if p["title"] != nil || p["tab_name"] != nil {
		p["session_id"] = sid
		if err := m.call(ctx, "session.rename", p, &out); err != nil {
			callErr(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// jsonSame: a and b say the same in JSON (a nil profile is not the same as any).
func jsonSame(a, b *assistantwire.Profile) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
