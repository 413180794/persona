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

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
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
	// Main is the main conversation (the Persona tab opens it): one long-running conversation, the others are side ones.
	Main string `json:"main,omitempty"`
	// Former are the main conversations replaced since (newest first, POST /api/v1/assistant/main/new): their machine
	// keeps listing them, archived, however long they are quiet.
	Former []FormerMain `json:"former_main,omitempty"`
	// MainTasksQuiet: tasks the main conversation opens are told only there, not pushed, on the Live Activity or in
	// task badges (#1731). nil is on (the default); the view always says what is in effect.
	MainTasksQuiet *bool `json:"main_tasks_quiet,omitempty"`
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
	if h.localMachine(cfg.MachineID) == nil {
		cfg.MachineID = ""
		for _, m := range h.machinesNow() { // the hub's own machine, else the first one
			if m.cfg.Kind == "local" {
				cfg.MachineID = m.cfg.ID
				break
			}
		}
		if cfg.MachineID == "" && len(h.machinesNow()) > 0 {
			cfg.MachineID = h.machinesNow()[0].cfg.ID
		}
	}
	if h.store != nil {
		var saved AssistantConfig
		if ok, err := h.store.Setting(ctx, "assistant", &saved); err == nil && ok {
			if saved.MachineID != "" && h.localMachine(saved.MachineID) != nil {
				cfg.MachineID = saved.MachineID
			}
			if saved.Launcher != "" {
				cfg.Launcher = saved.Launcher
			}
			cfg.Timezone, cfg.Notes, cfg.Main, cfg.Former, cfg.MainTasksQuiet = saved.Timezone, saved.Notes, saved.Main, saved.Former, saved.MainTasksQuiet
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

// mainTasksQuiet: a task opened from the main conversation now is quiet (assistant.main_tasks_quiet, on by default).
func (h *Hub) mainTasksQuiet(ctx context.Context) bool {
	return quietOn(h.assistantConfigLegacy(ctx).MainTasksQuiet)
}

func quietOn(v *bool) bool { return v == nil || *v }

// assistantCwd is the assistant directory on a machine, or "" while the machine has not reported its home.
func (m *Machine) assistantCwd() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.info.Home == "" {
		return ""
	}
	if m.info.DataDir != "" {
		return path.Join(m.info.DataDir, "assistant")
	}
	return path.Join(m.info.Home, assistantDir)
}

// isAssistant identifies headless conversations. A host pane is stronger evidence than a shared working directory.
func isAssistant(s proto.Session, dir string) bool {
	if s.PaneID != "" || dir == "" || s.Cwd == "" {
		return false
	}
	// The working directory may go through a symlinked home; runtimes and offline assistants have no host pane.
	return s.Cwd == dir || identity.IsAssistantDir(s.Cwd)
}

// assistantClaudeMD is the assistant directory's CLAUDE.md (prompts/assistant/claude_md.md); lark-connector: the assistant's
// machine has the lark-connector skill (assistantSkills). Its machine list is written from where the assistant actually
// runs: the ssh_host names are the hub machine's ssh aliases, which work only for an assistant on the hub's own
// machine, and a link (or tailcat) machine has no ssh at all; from anywhere else there is no command to give, only the
// Agents tools.
// assistantPrepare is what the node is told before a message to the assistant: its directory, its CLAUDE.md, and the
// connectors signed in (rate limited counts: still signed in), whose built-in skills it puts in the directory
// (assistantskills.Requires) or takes out.
//
// clis are the command lines of those connectors (connector.Manifest.CLI → its id): the node puts them on the
// assistant's PATH (personad tool --cli <id>).
func (h *Hub) assistantPrepare(ctx context.Context, m *Machine) map[string]any {
	signedIn := h.signedInConnectors(ctx)
	clis := map[string]string{}
	for _, id := range signedIn {
		if c := h.connectors.Get(id); c != nil && c.Manifest().CLI != "" {
			clis[c.Manifest().CLI] = id
		}
	}
	return map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx) + h.extensions.extensionInstructions(),
		"connectors": signedIn, "clis": clis, "plugin_skills": h.extensions.extensionSkillsFor(m.cfg.ID)}
}

// signedInConnectors are the connectors that are ok or rate limited (switched on and signed in).
func (h *Hub) signedInConnectors(ctx context.Context) []string {
	out := []string{}
	for _, c := range h.connectors.All() {
		if st := connector.StatusOf(ctx, c).State; st == connector.StateOK || st == connector.StateRateLimited {
			out = append(out, c.ID())
		}
	}
	return out
}

func (h *Hub) assistantClaudeMD(ctx context.Context) string {
	lc := h.cfg.Lark
	d := prompts.AssistantClaudeMD{Owner: strings.TrimSpace(h.cfg.Assistant.Owner), Notes: h.assistantNotes(ctx), ExtraDirs: h.kb.cfg.ExtraDirs,
		LarkCLI: lc.CLI != nil}
	here := h.assistantConfig(ctx).MachineID
	onHub := h.localMachine(here) != nil && h.localMachine(here).cfg.Kind == "local"
	d.OnHub = onHub
	if h.cfg.dir != "" {
		d.KnowledgeDir = path.Join(h.cfg.dir, "data")
	}
	if machine := h.localMachine(here); machine != nil {
		if cwd := machine.assistantCwd(); cwd != "" {
			d.StateDir = path.Dir(cwd)
		}
	}
	for _, m := range h.machinesNow() {
		local := m.cfg.ID == here
		d.Machines = append(d.Machines, prompts.AssistantMachine{Name: m.cfg.Name, ID: m.cfg.ID, Local: local, SSHHost: m.cfg.SSHHost,
			AgentsOnly: !local && !(onHub && m.cfg.Kind == "ssh")})
	}
	return prompts.RenderAssistantClaudeMD(d)
}

type assistantView struct {
	AssistantConfig
	Cwd            string          `json:"cwd"`
	Conversations  []proto.Session `json:"conversations"`
	NotesEffective string          `json:"notes_effective"`  // what the assistant is told about the user
	MainTasksQuiet bool            `json:"main_tasks_quiet"` // in effect (the config's own is omitted while unset)
}

// assistantState is GET /api/v1/assistant; withArchived lists archived conversations too (flagged `archived`).
func (h *Hub) assistantState(ctx context.Context, withArchived bool) assistantView {
	cfg := h.assistantConfig(ctx)
	v := assistantView{AssistantConfig: cfg, Conversations: []proto.Session{}, NotesEffective: h.assistantNotes(ctx),
		MainTasksQuiet: quietOn(cfg.MainTasksQuiet)}
	if m := h.localMachine(cfg.MachineID); m != nil {
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
			if h.localMachine(in.MachineID) == nil {
				writeErr(w, http.StatusBadRequest, errors.New("unknown machine"))
				return
			}
			cfg.MachineID = in.MachineID
		}
		if in.Launcher != "" {
			// The assistant runs Claude Code (claude -p), Codex (codex app-server) or an agent runtime that says it can
			// (its launcher has assistant: docs/runtimes.md「助手」); anything else would fail every message.
			if m := h.localMachine(cfg.MachineID); m != nil && in.Launcher != "claude" && !h.runtimeAssistantLauncher(r.Context(), m, in.Launcher) {
				var res struct {
					Detail proto.LauncherDetail `json:"detail"`
				}
				pctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
				err := m.call(pctx, "launchers.probe", map[string]any{"name": in.Launcher}, &res)
				cancel()
				if err == nil && res.Detail.Agent != "" && res.Detail.Agent != "claude" && res.Detail.Agent != "codex" {
					writeErr(w, http.StatusBadRequest, fmt.Errorf("%s 跑的是 %s，助手只能用启动 Claude Code、Codex 或能当助手的 runtime 的启动器", in.Launcher, res.Detail.Agent))
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
			h.mu.Unlock()
		}
		if in.Notes != nil {
			cfg.Notes = in.Notes
		}
		if in.MainTasksQuiet != nil {
			cfg.MainTasksQuiet = in.MainTasksQuiet
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
		h.assistantUpdated()
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
	h.assistantRoutesRelayed(api)
	h.assistantMainRoutes(api)
	api.HandleFunc("POST /api/v1/assistant/conversations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Prompt         string        `json:"prompt"`
			Images         []proto.Image `json:"images"`
			Launcher       string        `json:"launcher"`
			PermissionMode string        `json:"permission_mode"`
			Effort         string        `json:"effort"`
			Main           bool          `json:"main"` // becomes the main conversation
			// Purpose is the model purpose it runs under ("assistant" when empty; a plugin's /agent asks for its own);
			// MachineID one of the hub's machines instead of the assistant's (docs/protocol.md「助手」).
			Purpose   string `json:"purpose"`
			MachineID string `json:"machine_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
		defer cancel()
		if in.Purpose != "" {
			if _, err := h.modelPurposeDef(ctx, in.Purpose); err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Errorf("purpose %q: %w", in.Purpose, err))
				return
			}
		}
		if in.MachineID != "" && h.localMachine(in.MachineID) == nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("no machine %q", in.MachineID))
			return
		}
		start := func() (*proto.Session, error) {
			return h.startAssistantAt(ctx, in.MachineID, in.Prompt, in.Images, in.Launcher, in.PermissionMode, in.Effort, in.Purpose)
		}
		if in.Main { // the main conversation is the assistant's, on its machine
			start = func() (*proto.Session, error) {
				return h.startMain(ctx, in.Prompt, in.Images, in.Launcher, in.PermissionMode, in.Effort)
			}
		}
		s, err := start()
		if err != nil {
			callErr(w, err)
			return
		}
		h.asst().ConversationStarted(ctx, assistantwire.ConversationStarted{MachineID: s.MachineID, SessionID: s.SessionID, Started: time.Now()})
		writeJSON(w, map[string]any{"session": s})
	})
}

func (h *Hub) startAssistant(ctx context.Context, prompt string, images []proto.Image, launcher, mode, effort string, purposes ...string) (*proto.Session, error) {
	purpose := ""
	if len(purposes) > 0 {
		purpose = purposes[0]
	}
	return h.startAssistantAt(ctx, "", prompt, images, launcher, mode, effort, purpose)
}

// startAssistantAt starts an assistant conversation on machineID ("" is the assistant's machine) under the model
// purpose ("" is "assistant").
func (h *Hub) startAssistantAt(ctx context.Context, machineID, prompt string, images []proto.Image, launcher, mode, effort, purpose string) (*proto.Session, error) {
	if machineID == "" {
		machineID = h.assistantConfig(ctx).MachineID
	}
	m := h.localMachine(machineID)
	if m == nil {
		return nil, errors.New("assistant machine is not configured")
	}
	return h.startAssistantOn(ctx, m, prompt, images, launcher, mode, effort, purpose)
}

// startAssistantOn starts a new conversation on m, the assistant's machine (also the assistant's device.call
// assistant.start): the directory prepared, the model configuration of purpose ("" is "assistant") applied, then the
// node's assistant.send.
func (h *Hub) startAssistantOn(ctx context.Context, m *Machine, prompt string, images []proto.Image, launcher, mode, effort, purpose string) (*proto.Session, error) {
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := h.prepareAssistant(ctx, m, &prep.Cwd); err != nil {
		return nil, err
	}
	// Headless claude (node/assistant.go): no herdr tab, streamed tokens, any launcher per message.
	var res struct {
		SessionID string `json:"session_id"`
	}
	params := map[string]any{"cwd": prep.Cwd, "launcher": launcher, "text": prompt, "images": images, "permission_mode": mode, "effort": effort}
	if purpose == "" {
		purpose = "assistant"
	}
	if err := h.applyModel(ctx, purpose, params); err != nil {
		return nil, err
	}
	used, _ := params["launcher"].(string)
	if err := m.call(ctx, "assistant.send", params, &res); err != nil {
		return nil, err
	}
	s := proto.Session{SessionID: res.SessionID, Cwd: prep.Cwd, Status: proto.StatusWorking, Launcher: used,
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
	m := h.localMachine(cfg.MachineID)
	if m == nil {
		return
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := h.prepareAssistant(ctx, m, &prep.Cwd); err != nil {
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
	m := h.localMachine(h.assistantConfig(ctx).MachineID)
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
// transcripts crowd it out of the list and the Persona tab has no 主对话 (2026-11-01: the main one, quiet for two days, was gone).
func (h *Hub) keptSessions(ctx context.Context, machine string) []string {
	cfg := h.assistantConfig(ctx)
	out := []string{}
	if cfg.Main != "" && cfg.MachineID == machine {
		out = append(out, cfg.Main)
	}
	for _, f := range cfg.Former { // archived, still to be found
		if f.MachineID == machine && !slices.Contains(out, f.SessionID) {
			out = append(out, f.SessionID)
		}
	}
	// Scheduled tasks' own conversations too (the assistant reports them): a weekly one would drop out of the list in
	// between, and with it the app's link to it and the check that its last run is still going.
	tasks, _ := h.assistantTasks()
	for _, t := range tasks {
		if t.RunSessionID != "" && t.RunMachineID == machine && !slices.Contains(out, t.RunSessionID) {
			out = append(out, t.RunSessionID)
		}
	}
	return out
}

// keepMain tells the assistant's node, and the assistant, the main conversation changed.
func (h *Hub) keepMain(ctx context.Context) {
	h.assistantUpdated()
	cfg := h.assistantConfig(ctx)
	m := h.localMachine(cfg.MachineID)
	if m == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := m.call(cctx, "node.configure", map[string]any{"keep_sessions": h.keptSessions(cctx, m.cfg.ID)}, nil); err != nil {
		h.logf("assistant: keep main: %v", err)
	}
}

// runtimeAssistantLauncher: launcher is one an agent runtime on machine m offers to run the assistant (launchers.list's
// assistant), not a login-shell launcher to probe.
func (h *Hub) runtimeAssistantLauncher(ctx context.Context, m *Machine, launcher string) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	if err := m.call(ctx, "launchers.list", map[string]any{}, &res); err != nil {
		return false
	}
	for _, l := range res.Launchers {
		if l.Name == launcher && l.Assistant {
			return true
		}
	}
	return false
}

// The main conversation is the hub's to know (settings assistant, above); the assistant titles the others
// (internal/assistant, titles.go).

// mainTitle is the main conversation's title on the lists the apps get: it has no title of its own (conversation_rename
// refuses it, the apps show the persona there), so neither its first message (the node's title for a transcript
// without one) nor a model's name nor a rename shows up as one (#1313).
const mainTitle = "Persona"

var errRenameMain = errors.New("主对话没有标题可以改")

// mainConversation is the main conversation's session id when machine is the assistant's and list holds an assistant
// conversation, else "" (the setting is only read when it can matter: decorated runs on every push).
func (h *Hub) mainConversation(machine string, list []proto.Session, dir string) string {
	if !slices.ContainsFunc(list, func(s proto.Session) bool { return isAssistant(s, dir) }) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if cfg := h.assistantConfigLegacy(ctx); cfg.MachineID == machine {
		return cfg.Main
	}
	return ""
}

// isMain: id is the main conversation (the assistant's machine is not checked: session ids are unique).
func (h *Hub) isMain(ctx context.Context, id string) bool {
	return id != "" && h.assistantConfigLegacy(ctx).Main == id
}

// renameSession is POST /api/v1/sessions/{mid}/{sid}/rename ({"title", "tab_name"}): the node renames the tab and /
// or the session; an assistant conversation's new title is also the user's from now on (the assistant keeps it).
func (h *Hub) renameSession(w http.ResponseWriter, r *http.Request, m *Machine) {
	p := map[string]any{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil && !errors.Is(err, errEOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sid := r.PathValue("sid")
	p["session_id"] = sid
	title, _ := p["title"].(string)
	s, _ := m.session(sid)
	conv := isAssistant(s, m.assistantCwd())
	if t, _ := p["title"].(string); conv && strings.TrimSpace(t) != "" && h.isMain(r.Context(), sid) {
		writeErr(w, http.StatusBadRequest, errRenameMain) // the apps show the persona there (#1313)
		return
	}
	if conv && s.Agent == "codex" {
		delete(p, "title") // only the assistant's title for Codex (its conversation_rename does the same)
	}
	out := json.RawMessage(`{"ok":true}`)
	if p["title"] != nil || p["tab_name"] != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := m.call(ctx, "session.rename", p, &out); err != nil {
			callErr(w, err)
			return
		}
	}
	if conv && strings.TrimSpace(title) != "" {
		h.asst().ConversationRenamed(r.Context(), assistantwire.ConversationRenamed{MachineID: m.cfg.ID, SessionID: sid, Title: title})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(out)
}

// conversationName is how the Feishu bot calls an assistant conversation (the assistant has the same for its tools).
func conversationName(s proto.Session, main string) string {
	if s.SessionID == main {
		return "主对话"
	}
	// Not named yet: the node's title is then the directory's name ("assistant") or Claude Code's default.
	if t := strings.TrimSpace(s.Title); t != "" && t != "Claude Code" && t != path.Base(s.Cwd) {
		return t
	}
	return "新对话"
}
