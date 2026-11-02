package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/prompts"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The assistant ("对话" tab) is a Claude Code session like any other, started in a dedicated directory on one
// machine (the hub's own by default) whose CLAUDE.md tells it which of the user's data it can reach and how.

const assistantDir = ".jarvis/assistant" // relative to the machine's home

type AssistantConfig struct {
	MachineID string `json:"machine_id"`
	Launcher  string `json:"launcher"`
	// Timezone is the phone's (IANA name): routines and the snapshot read times in it.
	Timezone string `json:"timezone,omitempty"`
	// Notes, once edited in the app, replace hub.json's assistant.notes.
	Notes *string `json:"notes,omitempty"`
	// Main is the main conversation (the Jarvis tab opens it): one long-running conversation, the others are side ones.
	Main string `json:"main,omitempty"`
}

func (h *Hub) assistantConfig(ctx context.Context) AssistantConfig {
	cfg := h.assistantConfigLegacy(ctx)
	if r, err := h.modelRecord(ctx, "assistant"); err == nil && r.Binding.Provider != "" {
		cfg.Launcher = strings.TrimPrefix(r.Binding.Provider, "launcher.")
	}
	return cfg
}

func (h *Hub) assistantConfigLegacy(ctx context.Context) AssistantConfig {
	cfg := AssistantConfig{MachineID: h.cfg.Assistant.Machine, Launcher: h.cfg.Assistant.Launcher}
	if cfg.Launcher == "" {
		cfg.Launcher = "claude"
	}
	if h.byID[cfg.MachineID] == nil {
		cfg.MachineID = ""
		for _, m := range h.machines { // the hub's own machine, else the first one
			if m.cfg.Kind == "local" {
				cfg.MachineID = m.cfg.ID
				break
			}
		}
		if cfg.MachineID == "" && len(h.machines) > 0 {
			cfg.MachineID = h.machines[0].cfg.ID
		}
	}
	if h.store != nil {
		var saved AssistantConfig
		if ok, err := h.store.Setting(ctx, "assistant", &saved); err == nil && ok {
			if saved.MachineID != "" && h.byID[saved.MachineID] != nil {
				cfg.MachineID = saved.MachineID
			}
			if saved.Launcher != "" {
				cfg.Launcher = saved.Launcher
			}
			cfg.Timezone, cfg.Notes, cfg.Main = saved.Timezone, saved.Notes, saved.Main
		}
	}
	return cfg
}

// assistantNotes is what the assistant knows about the user: edited in the app, else hub.json's assistant.notes.
func (h *Hub) assistantNotes(ctx context.Context) string {
	if n := h.assistantConfig(ctx).Notes; n != nil {
		return strings.TrimSpace(*n)
	}
	return strings.TrimSpace(h.cfg.Assistant.Notes)
}

// assistantCwd is the assistant directory on a machine, or "" while the machine has not reported its home.
func (m *Machine) assistantCwd() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.info.Home == "" {
		return ""
	}
	return path.Join(m.info.Home, assistantDir)
}

// isAssistant reports whether a session runs in the assistant directory (cwd may go through a symlinked home).
func isAssistant(cwd, dir string) bool {
	if dir == "" || cwd == "" {
		return false
	}
	return cwd == dir || strings.HasSuffix(cwd, "/"+assistantDir)
}

// assistantClaudeMD is the assistant directory's CLAUDE.md (prompts/assistant/claude_md.md); lark-connector: the assistant's
// machine has the lark-connector skill (assistantSkills).
func (h *Hub) assistantClaudeMD(ctx context.Context, lark-connector bool) string {
	d := prompts.AssistantClaudeMD{Owner: strings.TrimSpace(h.cfg.Assistant.Owner), Notes: h.assistantNotes(ctx), ExtraDirs: h.kb.cfg.ExtraDirs,
		lark-connector: lark-connector}
	for _, m := range h.machines {
		d.Machines = append(d.Machines, prompts.AssistantMachine{Name: m.cfg.Name, ID: m.cfg.ID, Local: m.cfg.Kind == "local", SSHHost: m.cfg.SSHHost})
	}
	return prompts.RenderAssistantClaudeMD(d)
}

type assistantView struct {
	AssistantConfig
	Cwd            string          `json:"cwd"`
	Conversations  []proto.Session `json:"conversations"`
	NotesEffective string          `json:"notes_effective"` // what the assistant is told about the user
}

// assistantState is GET /api/v1/assistant; withArchived lists archived conversations too (flagged `archived`).
func (h *Hub) assistantState(ctx context.Context, withArchived bool) assistantView {
	cfg := h.assistantConfig(ctx)
	v := assistantView{AssistantConfig: cfg, Conversations: []proto.Session{}, NotesEffective: h.assistantNotes(ctx)}
	if m := h.byID[cfg.MachineID]; m != nil {
		v.Cwd = m.assistantCwd()
		for _, s := range h.decorated(m) {
			if s.Assistant && (withArchived || !s.Archived) {
				v.Conversations = append(v.Conversations, s)
			}
		}
	}
	return v
}

func (h *Hub) assistantRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/skills", h.serveSkills)
	api.HandleFunc("GET /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		archived := r.URL.Query().Get("archived")
		writeJSON(w, h.assistantState(r.Context(), archived == "1" || archived == "true"))
	})
	api.HandleFunc("PUT /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		var in AssistantConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		cfg := h.assistantConfig(r.Context())
		if in.MachineID != "" {
			if h.byID[in.MachineID] == nil {
				writeErr(w, http.StatusBadRequest, errors.New("unknown machine"))
				return
			}
			cfg.MachineID = in.MachineID
		}
		if in.Launcher != "" {
			// The assistant runs Claude Code (claude -p) or Codex (codex app-server); anything else would fail every message.
			if m := h.byID[cfg.MachineID]; m != nil && in.Launcher != "claude" {
				var res struct {
					Detail proto.LauncherDetail `json:"detail"`
				}
				pctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				err := m.call(pctx, "launchers.probe", map[string]any{"name": in.Launcher}, &res)
				cancel()
				if err == nil && res.Detail.Agent != "" && res.Detail.Agent != "claude" && res.Detail.Agent != "codex" {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("%s 跑的是 %s，助手只能用启动 Claude Code 或 Codex 的启动器", in.Launcher, res.Detail.Agent))
					return
				}
			}
			cfg.Launcher = in.Launcher
		}
		if in.Main != "" {
			cfg.Main = in.Main
			if in.Main == "-" { // clear
				cfg.Main = ""
			}
		}
		if in.Timezone != "" {
			if _, err := time.LoadLocation(in.Timezone); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			cfg.Timezone = in.Timezone
			h.mu.Lock()
			h.userTZ = in.Timezone
			h.forgetSnapshotLocked()
			h.mu.Unlock()
		}
		if in.Notes != nil {
			cfg.Notes = in.Notes
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		if in.Launcher != "" {
			h.modelMu.Lock()
			record, err := h.modelRecord(r.Context(), "assistant")
			if err == nil {
				record.Binding = mergeModel(record.Binding, ModelBinding{Provider: launcherProvider(in.Launcher)})
				err = h.saveModelRecord(r.Context(), "assistant", record.Revision, record.Binding)
			}
			h.modelMu.Unlock()
			if err != nil {
				writeErr(w, 500, err)
				return
			}
		}
		if err := h.store.PutSetting(r.Context(), "assistant", cfg); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if in.Main != "" {
			h.keepMain(r.Context())
		}
		writeJSON(w, h.assistantState(r.Context(), false))
	})
	// warm: the app opened the 对话 tab; get a process ready so the first message skips claude's startup.
	api.HandleFunc("POST /api/v1/assistant/warm", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Launcher string `json:"launcher"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		go h.warmAssistant(in.Launcher)
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /api/v1/assistant/memory", func(w http.ResponseWriter, r *http.Request) {
		var res json.RawMessage
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "list"}, &res); err != nil {
			callErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	api.HandleFunc("PUT /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "write", "name": r.PathValue("name"), "content": in.Content}, nil); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	api.HandleFunc("DELETE /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.assistantMemory(r.Context(), map[string]any{"op": "delete", "name": r.PathValue("name")}, nil); err != nil {
			callErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	h.scheduleRoutes(api)
	h.archiveRoutes(api)
	api.HandleFunc("POST /api/v1/assistant/conversations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prompt         string        `json:"prompt"`
			Images         []proto.Image `json:"images"`
			Launcher       string        `json:"launcher"`
			PermissionMode string        `json:"permission_mode"`
			Effort         string        `json:"effort"`
			Main           bool          `json:"main"` // becomes the main conversation
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
		defer cancel()
		s, err := h.startAssistant(ctx, in.Prompt, in.Images, in.Launcher, in.PermissionMode, in.Effort)
		if err != nil {
			callErr(w, err)
			return
		}
		h.titleNew(s.MachineID, s.SessionID, time.Now(), "")
		if in.Main && h.store != nil {
			cfg := h.assistantConfig(ctx)
			cfg.Main = s.SessionID
			if err := h.store.PutSetting(ctx, "assistant", cfg); err != nil {
				h.logf("assistant: save main: %v", err)
			}
			h.keepMain(ctx)
		}
		writeJSON(w, map[string]any{"session": s})
	})
}

func (h *Hub) startAssistant(ctx context.Context, prompt string, images []proto.Image, launcher, mode, effort string, purposes ...string) (*proto.Session, error) {
	cfg := h.assistantConfig(ctx)
	if launcher != "" {
		cfg.Launcher = launcher // one-off choice from the app; the saved default stays
	}
	m := h.byID[cfg.MachineID]
	if m == nil {
		return nil, errors.New("assistant machine is not configured")
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx, h.hasSkill(ctx, m, "lark-connector"))}, &prep); err != nil {
		return nil, err
	}
	// Headless claude (node/assistant.go): no herdr tab, streamed tokens, any launcher per message.
	var res struct {
		SessionID string `json:"session_id"`
	}
	params := map[string]any{"cwd": prep.Cwd, "launcher": launcher, "text": prompt, "images": images, "permission_mode": mode, "effort": effort}
	purpose := "assistant"
	if len(purposes) > 0 {
		purpose = purposes[0]
	}
	if err := h.applyModel(ctx, purpose, params); err != nil {
		return nil, err
	}
	cfg.Launcher, _ = params["launcher"].(string)
	if err := m.call(ctx, "assistant.send", params, &res); err != nil {
		return nil, err
	}
	s := proto.Session{SessionID: res.SessionID, Cwd: prep.Cwd, Status: proto.StatusWorking, Launcher: cfg.Launcher,
		PermissionMode: mode, Effort: effort, StartedByJarvis: true, UpdatedAt: time.Now().UTC()}
	s.MachineID = m.cfg.ID
	s.Assistant = true
	s.State = proto.StateWorking
	return &s, nil
}

func (h *Hub) warmAssistant(launcher string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg := h.assistantConfig(ctx)
	if launcher == "" {
		launcher = cfg.Launcher
	}
	m := h.byID[cfg.MachineID]
	if m == nil {
		return
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx, h.hasSkill(ctx, m, "lark-connector"))}, &prep); err != nil {
		return
	}
	params := map[string]any{"cwd": prep.Cwd, "launcher": launcher}
	if err := h.applyModel(ctx, "assistant", params); err != nil {
		h.logf("warm model: %v", err)
		return
	}
	if err := m.call(ctx, "assistant.warm", params, nil); err != nil {
		h.logf("warm assistant: %v", err)
	}
}

// assistantMemory runs one memory operation (node/memory.go) on the assistant's machine.
func (h *Hub) assistantMemory(ctx context.Context, op map[string]any, out any) error {
	m := h.byID[h.assistantConfig(ctx).MachineID]
	if m == nil {
		return errors.New("assistant machine is not configured")
	}
	cwd := m.assistantCwd()
	if cwd == "" {
		return errors.New("assistant machine is offline")
	}
	op["cwd"] = cwd
	return m.call(ctx, "assistant.memory", op, out)
}

// keptSessions are the sessions machine id's node lists however long they have been quiet: the assistant's main
// conversation and scheduled tasks' own ones, on the assistant's machine. Otherwise a busy machine's 80 newest
// transcripts crowd it out of the list and the Jarvis tab has no 主对话 (2026-11-01: the main one, quiet for two days, was gone).
func (h *Hub) keptSessions(ctx context.Context, machine string) []string {
	cfg := h.assistantConfig(ctx)
	out := []string{}
	if cfg.Main != "" && cfg.MachineID == machine {
		out = append(out, cfg.Main)
	}
	// Scheduled tasks' own conversations too: a weekly one would drop out of the list in between, and with it the
	// app's link to it and the check that its last run is still going (runTask).
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	for _, t := range s.tasks {
		if t.RunSessionID != "" && t.RunMachineID == machine && !slices.Contains(out, t.RunSessionID) {
			out = append(out, t.RunSessionID)
		}
	}
	s.mu.Unlock()
	return out
}

// keepMain tells the assistant's node the main conversation changed.
func (h *Hub) keepMain(ctx context.Context) {
	cfg := h.assistantConfig(ctx)
	m := h.byID[cfg.MachineID]
	if m == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := m.call(cctx, "node.configure", map[string]any{"keep_sessions": h.keptSessions(cctx, m.cfg.ID)}, nil); err != nil {
		h.logf("assistant: keep main: %v", err)
	}
}
