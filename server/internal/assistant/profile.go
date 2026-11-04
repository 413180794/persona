package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/proto"
)

// The assistant's own settings (settings `assistant`, docs/assistant.md「和 hub 的边界」): the machine its
// conversations run on, its launcher, the user's time zone, 关于我, the main conversation and the ones it replaced,
// and whether tasks from the main conversation are told only there. Only the assistant reads and writes them; hub.json's
// assistant section is their default (the hub's hello). It reports what the hub's own features need of them in every
// snapshot (Profile), and serves GET / PUT /api/v1/assistant, the warm-up and the memory routes.

// settingsKey is the setting the assistant keeps them in (the hub wrote the same JSON until 2026-11-04).
const settingsKey = "assistant"

// assistantDir is the assistant's directory on a machine, relative to its home (node assistant.prepare).
const assistantDir = ".jarvis/assistant"

// settingsFresh is how long the settings read from the database are used before they are read again (the assistant
// writes them itself; another hub sharing the database might too).
const settingsFresh = 5 * time.Second

// Settings are the assistant's settings, as saved.
type Settings struct {
	MachineID string `json:"machine_id"`
	Launcher  string `json:"launcher"`
	// Timezone is the phone's (IANA name): routines and the snapshot read times in it, and the hub its own.
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

// FormerMain is a main conversation that was replaced, and until when it was the main one.
type FormerMain struct {
	MachineID string    `json:"machine_id"`
	SessionID string    `json:"session_id"`
	Until     time.Time `json:"until"`
}

// settingsBook keeps the settings read last (with no database: the only copy).
type settingsBook struct {
	change sync.Mutex // one change at a time
	mu     sync.Mutex
	cached *Settings
	at     time.Time
	gen    uint64 // counts the changes saved: a read begun before one does not replace what it saved
}

// savedSettings are the settings as saved (zero when never written); false when they could not be read.
func (s *Service) savedSettings(ctx context.Context) (Settings, bool) {
	b := &s.prefs
	st := s.store()
	b.mu.Lock()
	if b.cached != nil && (st == nil || time.Since(b.at) < settingsFresh) {
		v := *b.cached
		b.mu.Unlock()
		return v, true
	}
	gen := b.gen
	b.mu.Unlock()
	var v Settings
	if st != nil {
		if _, err := st.Setting(ctx, settingsKey, &v); err != nil {
			s.logf("assistant: settings: %v", err)
			return Settings{}, false
		}
	}
	b.mu.Lock()
	if b.gen == gen { // no change was saved meanwhile (else what this read is older than what is cached)
		b.cached, b.at = &v, time.Now()
	} else if b.cached != nil {
		v = *b.cached
	}
	b.mu.Unlock()
	return v, true
}

// updateSettings changes the saved settings with fn (which may refuse) and saves them.
func (s *Service) updateSettings(ctx context.Context, fn func(*Settings) error) (Settings, error) {
	b := &s.prefs
	b.change.Lock()
	defer b.change.Unlock()
	b.mu.Lock()
	b.at = time.Time{} // read them again
	b.mu.Unlock()
	v, ok := s.savedSettings(ctx)
	if !ok {
		return Settings{}, errors.New("the assistant's settings could not be read")
	}
	if err := fn(&v); err != nil {
		return Settings{}, err
	}
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, settingsKey, v); err != nil {
			return Settings{}, err
		}
	}
	b.mu.Lock()
	b.cached, b.at = &v, time.Now()
	b.gen++
	b.mu.Unlock()
	return v, nil
}

// settings are the settings in effect, what the rest of the package reads (the main conversation, the machine, the time
// zone; isMain and mainTasksQuiet for short): the machine one the hub has (else the hub's default), the launcher the
// assistant's own (else hub.json's, else claude; 模型配置 › 主对话 goes over it: launcherNow).
func (s *Service) settings(ctx context.Context) Settings {
	v, _ := s.savedSettings(ctx)
	return s.effective(ctx, v)
}

func (s *Service) effective(ctx context.Context, v Settings) Settings {
	def := s.hello(ctx).Assistant
	if v.MachineID == "" || !slices.Contains(def.Machines, v.MachineID) {
		v.MachineID = def.MachineID
	}
	if v.Launcher == "" {
		v.Launcher = def.Launcher
	}
	if v.Launcher == "" {
		v.Launcher = "claude"
	}
	return v
}

// notes is what the assistant knows about the user: edited in the app, else hub.json's assistant.notes.
func (s *Service) notes(ctx context.Context) string {
	if n := s.settings(ctx).Notes; n != nil {
		return strings.TrimSpace(*n)
	}
	return strings.TrimSpace(s.hello(ctx).OwnerNotes)
}

func quietOn(v *bool) bool { return v == nil || *v }

// mainTasksQuiet: a task opened from the main conversation now is quiet (main_tasks_quiet, on by default).
func (s *Service) mainTasksQuiet(ctx context.Context) bool {
	return quietOn(s.settings(ctx).MainTasksQuiet)
}

// launcherNow is the launcher new conversations start with: 模型配置 › 主对话's, else the assistant's own.
func (s *Service) launcherNow(ctx context.Context) string {
	if rec, err := s.modelRecord(ctx, "assistant"); err == nil && rec.Binding.Provider != "" {
		return strings.TrimPrefix(rec.Binding.Provider, "launcher.")
	}
	return s.settings(ctx).Launcher
}

// modelRecord is what purpose itself is set to in the hub's model configuration.
type modelRecord struct {
	Binding  ModelBinding `json:"binding"`
	Revision int64        `json:"revision"`
}

func (s *Service) modelRecord(ctx context.Context, purpose string) (modelRecord, error) {
	var r modelRecord
	err := s.appCall(ctx, AppModels, ModelsRecord, map[string]any{"purpose": purpose}, &r)
	return r, err
}

func (s *Service) saveModelRecord(ctx context.Context, purpose string, r modelRecord) error {
	return s.appCall(ctx, AppModels, ModelsSave, map[string]any{"purpose": purpose, "revision": r.Revision, "binding": r.Binding}, nil)
}

// profile is what the hub's features need of the settings (Snapshot.Profile); tasks are the scheduled tasks as
// reported (nil: unreadable, and Keep is left out). nil when the settings could not be read.
func (s *Service) profile(ctx context.Context, tasks []TaskView) *Profile {
	saved, ok := s.savedSettings(ctx)
	if !ok {
		return nil
	}
	v := s.effective(ctx, saved)
	p := &Profile{MachineID: v.MachineID, Main: v.Main, Launcher: v.Launcher, Timezone: v.Timezone}
	if tasks != nil {
		p.Keep = keptSessions(v, tasks)
	}
	return p
}

// keptSessions are the conversations their machines list however long they have been quiet: the main conversation,
// the main ones it replaced, and scheduled tasks' own. Otherwise a busy machine's 80 newest transcripts crowd them out
// of the list and the Persona tab has no 主对话 (2026-11-01: the main one, quiet for two days, was gone), and a weekly
// task's conversation drops out in between, with the app's link to it and the check that its last run still goes.
func keptSessions(v Settings, tasks []TaskView) []SessionRef {
	out := []SessionRef{}
	add := func(machine, id string) {
		if machine != "" && id != "" && !slices.Contains(out, SessionRef{MachineID: machine, SessionID: id}) {
			out = append(out, SessionRef{MachineID: machine, SessionID: id})
		}
	}
	add(v.MachineID, v.Main)
	for _, f := range v.Former { // archived, still to be found
		add(f.MachineID, f.SessionID)
	}
	for _, t := range tasks {
		add(t.RunMachineID, t.RunSessionID)
	}
	return out
}

// settingsChanged: the hub learns them (the snapshot's profile) and the next message reads the time in the new zone.
func (s *Service) settingsChanged(ctx context.Context) {
	s.ProfileChanged(ctx)
	s.publish(ctx)
}

// ---- GET / PUT /api/v1/assistant, the warm-up, the memory ----

// assistantCwd is the assistant's directory on d, or "" while d has not reported its home.
func assistantCwd(d Device) string {
	if d.Home == "" {
		return ""
	}
	if d.DataDir != "" {
		return path.Join(d.DataDir, "assistant")
	}
	return path.Join(d.Home, assistantDir)
}

type assistantView struct {
	Settings
	Cwd            string          `json:"cwd"`
	Conversations  []proto.Session `json:"conversations"`
	NotesEffective string          `json:"notes_effective"`  // what the assistant is told about the user
	MainTasksQuiet bool            `json:"main_tasks_quiet"` // in effect (the setting's own is left out while unset)
}

// view is GET /api/v1/assistant; withArchived lists archived conversations too (flagged `archived`).
func (s *Service) view(ctx context.Context, withArchived bool) assistantView {
	v := s.settings(ctx)
	v.Launcher = s.launcherNow(ctx)
	out := assistantView{Settings: v, Conversations: []proto.Session{}, NotesEffective: s.notes(ctx), MainTasksQuiet: quietOn(v.MainTasksQuiet)}
	if d, ok := s.device(ctx, v.MachineID); ok {
		out.Cwd = assistantCwd(d)
		list, _ := s.sessions(ctx, d.ID)
		for _, c := range list {
			if c.Assistant && (withArchived || !c.Archived) {
				out.Conversations = append(out.Conversations, c)
			}
		}
	}
	return out
}

func (s *Service) profileRoutes() {
	s.handle("GET /api/v1/assistant", func(w http.ResponseWriter, r *http.Request) {
		archived := r.URL.Query().Get("archived")
		apierr.WriteJSON(w, s.view(r.Context(), archived == "1" || archived == "true"))
	})
	s.handle("PUT /api/v1/assistant", s.putSettings)
	// warm: the app opened the 对话 tab; get a process ready so the first message skips claude's startup.
	s.handle("POST /api/v1/assistant/warm", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Launcher string `json:"launcher"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		go s.warm(in.Launcher)
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
	s.handle("GET /api/v1/assistant/memory", func(w http.ResponseWriter, r *http.Request) {
		var res json.RawMessage
		if err := s.memory(r.Context(), map[string]any{"op": "list"}, &res); err != nil {
			apierr.Call(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(res)
	})
	s.handle("PUT /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		if err := s.memory(r.Context(), map[string]any{"op": "write", "name": r.PathValue("name"), "content": in.Content}, nil); err != nil {
			apierr.Call(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
	s.handle("DELETE /api/v1/assistant/memory/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.memory(r.Context(), map[string]any{"op": "delete", "name": r.PathValue("name")}, nil); err != nil {
			apierr.Call(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"ok": true})
	})
}

// putSettings is PUT /api/v1/assistant: what the body sets changes ("-" as main clears it); a launcher is checked
// on the assistant's machine and goes into 模型配置 › 主对话 too.
func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apierr.Write(w, http.StatusBadRequest, err)
		return
	}
	machine := s.settings(ctx).MachineID
	if in.MachineID != "" {
		if _, ok := s.device(ctx, in.MachineID); !ok {
			apierr.Write(w, http.StatusBadRequest, errors.New("unknown machine"))
			return
		}
		machine = in.MachineID
	}
	if in.Launcher != "" {
		if err := s.checkLauncher(ctx, machine, in.Launcher); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
	}
	if in.Timezone != "" {
		if _, err := time.LoadLocation(in.Timezone); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
	}
	if in.Launcher != "" {
		rec, err := s.modelRecord(ctx, "assistant")
		if err == nil {
			rec.Binding = mergeModel(rec.Binding, ModelBinding{Provider: "launcher." + in.Launcher})
			err = s.saveModelRecord(ctx, "assistant", rec)
		}
		if err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
	}
	launcher := s.launcherNow(ctx)
	if _, err := s.updateSettings(ctx, func(v *Settings) error {
		*v = s.effective(ctx, *v) // saved as they are in effect, as the hub saved them
		v.MachineID, v.Launcher = machine, launcher
		if in.Launcher != "" {
			v.Launcher = in.Launcher
		}
		if in.Main == "-" {
			v.Main = ""
		} else if in.Main != "" {
			v.Main = in.Main
		}
		if in.Timezone != "" {
			v.Timezone = in.Timezone
		}
		if in.Notes != nil {
			v.Notes = in.Notes
		}
		if in.MainTasksQuiet != nil {
			v.MainTasksQuiet = in.MainTasksQuiet
		}
		return nil
	}); err != nil {
		apierr.Write(w, http.StatusInternalServerError, err)
		return
	}
	s.settingsChanged(ctx)
	apierr.WriteJSON(w, s.view(ctx, false))
}

// mergeModel puts over's choices on base: another provider starts from nothing (its model and effort are its own).
func mergeModel(base, over ModelBinding) ModelBinding {
	if over.Provider != "" && over.Provider != base.Provider {
		base = ModelBinding{Provider: over.Provider}
	}
	if over.Model != "" {
		base.Model = over.Model
	}
	if over.Effort != "" {
		base.Effort = over.Effort
	}
	return base
}

// checkLauncher: the assistant runs Claude Code (claude -p), Codex (codex app-server) or an agent runtime that says it
// can (its launcher has assistant: docs/runtimes.md「助手」); anything else would fail every message. One that can't
// be probed now goes.
func (s *Service) checkLauncher(ctx context.Context, machine, launcher string) error {
	if launcher == "claude" {
		return nil
	}
	if _, ok := s.device(ctx, machine); !ok || s.runtimeAssistantLauncher(ctx, machine, launcher) {
		return nil
	}
	var res struct {
		Detail proto.LauncherDetail `json:"detail"`
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := s.call(pctx, machine, DeviceLaunchersProbe, map[string]any{"name": launcher}, &res)
	cancel()
	if err == nil && res.Detail.Agent != "" && res.Detail.Agent != "claude" && res.Detail.Agent != "codex" {
		return fmt.Errorf("%s 跑的是 %s，助手只能用启动 Claude Code、Codex 或能当助手的 runtime 的启动器", launcher, res.Detail.Agent)
	}
	return nil
}

// runtimeAssistantLauncher: launcher is one an agent runtime on machine offers to run the assistant (the node's
// launchers.list says assistant), not a login-shell launcher to probe.
func (s *Service) runtimeAssistantLauncher(ctx context.Context, machine, launcher string) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	if err := s.call(ctx, machine, DeviceLaunchersList, map[string]any{}, &res); err != nil {
		return false
	}
	return slices.ContainsFunc(res.Launchers, func(l proto.Launcher) bool { return l.Name == launcher && l.Assistant })
}

// warm gets a process ready on the assistant's machine (node assistant.warm) for the launcher ("" is the one new
// conversations start with).
func (s *Service) warm(launcher string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	machine := s.settings(ctx).MachineID
	if launcher == "" {
		launcher = s.launcherNow(ctx)
	}
	if _, ok := s.device(ctx, machine); !ok {
		return
	}
	cwd, err := s.prepare(ctx, machine)
	if err != nil {
		return
	}
	params := map[string]any{"cwd": cwd, "launcher": launcher}
	if err := s.applyModel(ctx, "assistant", params); err != nil {
		s.logf("warm model: %v", err)
		return
	}
	if err := s.call(ctx, machine, DeviceAssistantWarm, params, nil); err != nil {
		s.logf("warm assistant: %v", err)
	}
}

// memory runs one memory operation (node/memory.go) on the assistant's machine.
func (s *Service) memory(ctx context.Context, op map[string]any, out any) error {
	d, ok := s.device(ctx, s.settings(ctx).MachineID)
	if !ok {
		return errors.New("assistant machine is not configured")
	}
	cwd := assistantCwd(d)
	if cwd == "" {
		return errors.New("assistant machine is offline")
	}
	op["cwd"] = cwd
	return s.call(ctx, d.ID, DeviceAssistantMemory, op, out)
}
