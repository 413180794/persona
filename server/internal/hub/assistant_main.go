package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The user's hold on the main conversation (docs/assistant.md「手动压缩」「新建主对话」): compacting a conversation
// when they choose to, and putting the main one away for a new one that starts from what it left.

// mainHandoffKey is the setting with what the old main conversation left, until the next one takes it.
const mainHandoffKey = "assistant.main_handoff"

// maxFormerMains is how many earlier main conversations the assistant's machine keeps listing however quiet.
const maxFormerMains = 10

// FormerMain is a main conversation that was replaced, and until when it was the main one.
type FormerMain struct {
	MachineID string    `json:"machine_id"`
	SessionID string    `json:"session_id"`
	Until     time.Time `json:"until"`
}

// mainHandoff is what a replaced main conversation left for the next one (node assistant.handoff).
type mainHandoff struct {
	From      string    `json:"from"`
	MachineID string    `json:"machine_id"`
	Until     time.Time `json:"until"`
	// For: the new main conversation it waits in, started without a message (Android starts it empty, then sends).
	For     string `json:"for,omitempty"`
	Summary string `json:"summary,omitempty"`
	Recent    []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"recent,omitempty"`
}

func (h *Hub) assistantMainRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/v1/assistant/conversations/{id}/compact", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			MachineID string `json:"machine_id"` // "" is the assistant's machine
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.MachineID == "" {
			in.MachineID = h.assistantConfig(r.Context()).MachineID
		}
		m := h.localMachine(in.MachineID)
		if m == nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("no machine %q", in.MachineID))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute) // a process may have to start first
		defer cancel()
		if err := m.call(ctx, "assistant.compact", map[string]any{"session_id": r.PathValue("id")}, nil); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("POST /api/v1/assistant/main/new", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Carry  string `json:"carry"`  // summary (default) | none
			Expect string `json:"expect"` // the main conversation the app shows: refused when it is another by now
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if in.Carry != "" && in.Carry != "summary" && in.Carry != "none" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("carry %q: summary or none", in.Carry))
			return
		}
		old, carried, err := h.newMain(r.Context(), in.Carry != "none", in.Expect)
		if err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "archived": old, "carried": carried})
	})
}

// newMain puts the main conversation away: what it left is kept for the next one (carry), it is no longer the main
// one, renamed and archived. The next message to the main conversation starts a new one (startMain). It answers the
// old conversation's id ("" when there was none) and whether anything of it is carried over.
func (h *Hub) newMain(ctx context.Context, carry bool, expect string) (string, bool, error) {
	if h.store == nil {
		return "", false, errors.New("no database")
	}
	h.mainMu.Lock()
	defer h.mainMu.Unlock()
	cfg := h.assistantConfig(ctx)
	old := cfg.Main
	if expect != "" && expect != old {
		return "", false, &rpc.Error{Code: "main_changed", Message: "主对话已经换过了，刷新后再试"}
	}
	if old == "" {
		return "", false, nil
	}
	m, s := h.findSession(old, cfg.MachineID)
	if s != nil && (s.Status == proto.StatusWorking || (s.Compaction != nil && s.Compaction.State == proto.CompactionRunning)) {
		return "", false, &rpc.Error{Code: "busy", Message: "主对话正在回答或压缩，等它停下再新建"}
	}
	machine := cfg.MachineID
	if m != nil {
		machine = m.cfg.ID
	}
	now := time.Now().UTC()
	handoff := mainHandoff{From: old, MachineID: machine, Until: now}
	carried := false
	if carry && m != nil {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := m.call(cctx, "assistant.handoff", map[string]any{"session_id": old}, &handoff)
		cancel()
		handoff.From, handoff.MachineID, handoff.Until = old, machine, now
		if err != nil {
			h.logf("assistant: what the main conversation left: %v", err)
		}
		carried = handoff.Summary != "" || len(handoff.Recent) > 0
	}
	if err := h.store.PutSetting(ctx, mainHandoffKey, handoff); err != nil {
		return "", false, err
	}
	cfg.Main = ""
	cfg.Former = slices.DeleteFunc(cfg.Former, func(f FormerMain) bool { return f.SessionID == old })
	cfg.Former = append([]FormerMain{{MachineID: machine, SessionID: old, Until: now}}, cfg.Former...)
	if len(cfg.Former) > maxFormerMains {
		cfg.Former = cfg.Former[:maxFormerMains]
	}
	if err := h.store.PutSetting(ctx, "assistant", cfg); err != nil {
		return "", false, err
	}
	h.keepMain(ctx)
	local := now.In(h.userLocation())
	h.asst().ConversationRenamed(ctx, assistantwire.ConversationRenamed{MachineID: machine, SessionID: old,
		Title: fmt.Sprintf("旧主对话 · 至 %d月%d日", local.Month(), local.Day())})
	if err := h.asst().archive(ctx, old); err != nil {
		h.logf("assistant: archive the old main conversation %s: %v", short(old), err)
	}
	h.logf("assistant: main conversation %s put away (carried over: %v)", short(old), carried)
	return old, carried, nil
}

// findSession is the session id on the machine preferred, else on any machine (nil, nil when none lists it).
func (h *Hub) findSession(id, preferred string) (*Machine, *proto.Session) {
	machines := h.machinesNow()
	if m := h.localMachine(preferred); m != nil {
		machines = append([]*Machine{m}, machines...)
	}
	for _, m := range machines {
		if s, ok := m.session(id); ok {
			return m, &s
		}
	}
	return nil, nil
}

// startMain starts a new main conversation with the user's first message: what the old one left goes before it
// (prompts/assistant/main_handoff), once.
func (h *Hub) startMain(ctx context.Context, prompt string, images []proto.Image, launcher, mode, effort string) (*proto.Session, error) {
	h.mainMu.Lock()
	defer h.mainMu.Unlock()
	var handoff mainHandoff
	if h.store != nil {
		if ok, err := h.store.Setting(ctx, mainHandoffKey, &handoff); err != nil || !ok {
			handoff = mainHandoff{}
		}
	}
	// Started without a message: what the old one left waits for the first one sent to it (handoffFor).
	empty := strings.TrimSpace(prompt) == "" && len(images) == 0
	text := prompt
	if handoff.From != "" && !empty {
		text = h.renderHandoff(handoff, prompt)
	}
	s, err := h.startAssistant(ctx, text, images, launcher, mode, effort)
	if err != nil {
		return nil, err
	}
	if h.store != nil {
		cfg := h.assistantConfig(ctx)
		cfg.Main = s.SessionID
		if err := h.store.PutSetting(ctx, "assistant", cfg); err != nil {
			h.logf("assistant: save main: %v", err)
		}
		if handoff.From != "" {
			next := mainHandoff{} // used
			if empty {
				next, next.For = handoff, s.SessionID
			}
			if err := h.store.PutSetting(ctx, mainHandoffKey, next); err != nil {
				h.logf("assistant: what the old main conversation left: %v", err)
			}
		}
		h.keepMain(ctx)
	}
	return s, nil
}

// handoffFor is the text of a message to conversation sid: with what the old main conversation left before it when
// sid is the new main one that was started empty and this is the first message to it.
func (h *Hub) handoffFor(ctx context.Context, sid, text string) string {
	if h.store == nil || sid == "" {
		return text
	}
	h.mainMu.Lock()
	defer h.mainMu.Unlock()
	var handoff mainHandoff
	if ok, err := h.store.Setting(ctx, mainHandoffKey, &handoff); err != nil || !ok || handoff.From == "" || handoff.For != sid {
		return text
	}
	if err := h.store.PutSetting(ctx, mainHandoffKey, mainHandoff{}); err != nil {
		h.logf("assistant: clear what the old main conversation left: %v", err)
		return text
	}
	return h.renderHandoff(handoff, text)
}

// renderHandoff puts what the old main conversation left before text (prompts/assistant/main_handoff).
func (h *Hub) renderHandoff(handoff mainHandoff, text string) string {
	hp := prompts.MainHandoff{FromID: handoff.From, Summary: handoff.Summary, Text: text}
	local := handoff.Until.In(h.userLocation())
	hp.Until = fmt.Sprintf("%d月%d日 %02d:%02d", local.Month(), local.Day(), local.Hour(), local.Minute())
	for _, t := range handoff.Recent {
		hp.Recent = append(hp.Recent, prompts.HandoffTurn{Role: t.Role, Text: t.Text})
	}
	return prompts.RenderMainHandoff(hp)
}

// startMainWith is startMain in startAssistant's shape (purposes are the assistant's).
func (h *Hub) startMainWith(ctx context.Context, prompt string, images []proto.Image, launcher, mode, effort string, _ ...string) (*proto.Session, error) {
	return h.startMain(ctx, prompt, images, launcher, mode, effort)
}

// archive archives an assistant conversation the way the apps do (the assistant keeps the archived list). The
// assistant learns the main conversation changed a moment later (assistantUpdated): until then it refuses to archive
// the one that was main, and is asked again.
func (a assistantRemote) archive(ctx context.Context, id string) error {
	req := assistantwire.HTTPRequest{Method: http.MethodPost, Path: "/api/v1/assistant/conversations/" + id + "/archive",
		Header: map[string][]string{}}
	for try := 0; ; try++ {
		var res assistantwire.HTTPResponse
		if err := a.call(ctx, assistantwire.MethodHTTP, req, &res); err != nil {
			return err
		}
		if res.Status == 0 || res.Status == http.StatusOK {
			return nil
		}
		if res.Status != http.StatusBadRequest || try >= 15 {
			return fmt.Errorf("%d: %s", res.Status, res.Body)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
