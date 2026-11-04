package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// jarvis_settings: Persona changes its own settings for the user (docs/assistant.md「操作 Persona 自己」). The
// assistant's own (关于我, its launcher, 主对话派的任务只在主对话里通知) are its to read and change; the hub's (the
// inbox's rules, the blocked Claude config directories, mail accounts…) are the hub's settings app's, which asks the
// user itself (app.tool jarvis_settings). Reads are free; every change is a card on the phone that says what changes
// (before → after) and is applied only when approved.

// ownSetting is one of the assistant's own settings jarvis_settings offers.
type ownSetting struct {
	Setting
	get func(ctx context.Context) (string, error)
	set func(ctx context.Context, v json.RawMessage) (*ownChange, error)
}

// ownChange is a change waiting for the phone: what the card says and how it is made (none: nothing to change).
type ownChange struct {
	summary string
	apply   func(ctx context.Context) (string, error)
}

func (s *Service) ownSettings() []ownSetting {
	getSet := []string{"get", "set"}
	return []ownSetting{
		{Setting: Setting{Key: "assistant.notes", Title: "关于我", Actions: getSet,
			About: "what Persona knows about the user (设置 › 助手 › 关于我), Markdown appended to the assistant's instructions; new conversations read it",
			Value: "a string (Markdown; the whole text)"},
			get: func(ctx context.Context) (string, error) { return orNone(s.notes(ctx)), nil },
			set: s.setNotes},
		{Setting: Setting{Key: "assistant.main_tasks_quiet", Title: "主对话派的任务只在主对话里通知", Actions: getSet,
			About: "on (the default): tasks the main conversation opens (task_plan, agent_start) send no pushes, stay off the Live Activity and " +
				"task badges, and the main conversation is told when one is done, waits for the user or fails instead; off: they notify like any agent. " +
				"Applies to tasks opened from now on",
			Value: "true or false"},
			get: func(ctx context.Context) (string, error) { return fmt.Sprint(s.mainTasksQuiet(ctx)), nil },
			set: s.setMainTasksQuiet},
		{Setting: Setting{Key: "assistant.launcher", Title: "Persona 的启动器", Actions: getSet,
			About: "which launcher (model) new Persona conversations start with by default",
			Value: "a launcher name from this setting's get"},
			get: s.getLauncher,
			set: s.setLauncher},
	}
}

func (s *Service) toolJarvisSettings(ctx context.Context, call ToolCall, args json.RawMessage) proto.ToolResult {
	var a struct {
		Action string          `json:"action"`
		Key    string          `json:"key"`
		Value  json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return proto.TextResult(fmt.Sprintf("invalid arguments: %v", err), true)
	}
	own := s.ownSettings()
	if a.Action == "" || a.Action == "list" {
		text, err := s.listSettings(ctx, own)
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult(text, false)
	}
	for _, o := range own {
		if o.Key == a.Key {
			text, err := s.ownSettingAction(ctx, call, o, a.Action, a.Value)
			if err != nil {
				return proto.TextResult(err.Error(), true)
			}
			return proto.TextResult(text, false)
		}
	}
	return s.appTool(ctx, AppSettings, call, args) // the hub's
}

// listSettings is action list: the assistant's settings and the hub's, by key.
func (s *Service) listSettings(ctx context.Context, own []ownSetting) (string, error) {
	var res struct {
		Settings []Setting `json:"settings"`
	}
	if err := s.appCall(ctx, AppSettings, SettingsList, map[string]any{}, &res); err != nil {
		return "", err
	}
	all := res.Settings
	for _, o := range own {
		all = append(all, o.Setting)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })
	var b strings.Builder
	b.WriteString("Settings you can read and (with the user's approval on the phone) change:\n")
	for _, st := range all {
		fmt.Fprintf(&b, "- %s (%s): %s. Actions: %s.", st.Key, st.Title, st.About, strings.Join(st.Actions, ", "))
		if st.Value != "" {
			fmt.Fprintf(&b, " Value: %s.", st.Value)
		}
		b.WriteByte('\n')
	}
	b.WriteString("Routines and follow-ups have their own tools (schedule_routine, schedule_followup); knowledge base tags are kb_update. " +
		"Not changeable here: tokens, pairing, devices, the machines the hub reaches, permission modes.")
	return b.String(), nil
}

// ownSettingAction reads one of the assistant's settings, or changes it once the user approves on the phone.
func (s *Service) ownSettingAction(ctx context.Context, call ToolCall, o ownSetting, action string, value json.RawMessage) (string, error) {
	switch action {
	case "get":
		return o.get(ctx)
	case "set":
	default:
		return "", fmt.Errorf("%s does not take %q (it takes %s)", o.Key, action, strings.Join(o.Actions, ", "))
	}
	change, err := o.set(ctx, value)
	if err != nil {
		return "", err
	}
	if change.apply == nil {
		return change.summary, nil // nothing to change
	}
	shown, _ := json.Marshal(map[string]string{"setting": o.Title, "action": action})
	now := time.Now().UTC()
	r, err := s.ask(ctx, proto.Interaction{ID: "ix_" + randomHex(6), MachineID: call.MachineID, SessionID: call.SessionID,
		Kind: proto.IxPermission, ToolName: "jarvis_settings", Summary: change.summary, ToolInput: shown, SessionTitle: "Persona",
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)})
	if err != nil {
		return "", err
	}
	if r.Allow == nil || !*r.Allow {
		return "The user declined on the phone: nothing changed.", nil
	}
	return change.apply(ctx)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return s
}

// clipped is a value as the card shows it.
func clipped(s string) string { return clip(oneLine(orNone(s)), 120) }

func (s *Service) setNotes(ctx context.Context, v json.RawMessage) (*ownChange, error) {
	var text string
	if err := json.Unmarshal(v, &text); err != nil {
		return nil, errors.New("value must be a string")
	}
	text = strings.TrimSpace(text)
	before := s.notes(ctx)
	if text == before {
		return &ownChange{summary: "关于我没有变化"}, nil
	}
	if s.store() == nil {
		return nil, errors.New("no database on this hub")
	}
	return &ownChange{summary: fmt.Sprintf("改「关于我」：%s → %s", clipped(before), clipped(text)),
		apply: func(ctx context.Context) (string, error) {
			if _, err := s.updateSettings(ctx, func(v *Settings) error { v.Notes = &text; return nil }); err != nil {
				return "", err
			}
			s.settingsChanged(ctx)
			return "关于我 updated; new conversations read it (this one keeps what it started with).", nil
		}}, nil
}

func (s *Service) setMainTasksQuiet(ctx context.Context, v json.RawMessage) (*ownChange, error) {
	var on bool
	if err := json.Unmarshal(v, &on); err != nil {
		return nil, errors.New("value must be true or false")
	}
	before := s.mainTasksQuiet(ctx)
	if on == before {
		return &ownChange{summary: "「主对话派的任务只在主对话里通知」没有变化"}, nil
	}
	if s.store() == nil {
		return nil, errors.New("no database on this hub")
	}
	word := map[bool]string{true: "开", false: "关"}
	return &ownChange{summary: fmt.Sprintf("「主对话派的任务只在主对话里通知」：%s → %s", word[before], word[on]),
		apply: func(ctx context.Context) (string, error) {
			if _, err := s.updateSettings(ctx, func(v *Settings) error { v.MainTasksQuiet = &on; return nil }); err != nil {
				return "", err
			}
			s.settingsChanged(ctx)
			return fmt.Sprintf("assistant.main_tasks_quiet is %v for tasks opened from now on (existing ones keep theirs).", on), nil
		}}, nil
}

// launchers are the launchers of the assistant's machine, as the apps list them.
func (s *Service) launchers(ctx context.Context) ([]proto.Launcher, error) {
	machine := s.settings(ctx).MachineID
	if _, ok := s.device(ctx, machine); !ok {
		return nil, errors.New("the assistant's machine is not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var res struct {
		Launchers []proto.Launcher `json:"launchers"`
	}
	err := s.appCall(cctx, AppModels, ModelsLaunchers, map[string]any{"machine_id": machine}, &res)
	return res.Launchers, err
}

func (s *Service) getLauncher(ctx context.Context) (string, error) {
	now := s.launcherNow(ctx)
	ls, err := s.launchers(ctx)
	if err != nil {
		return "now: " + now + " (the machine's launchers could not be listed: " + err.Error() + ")", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "now: %s\navailable:\n", now)
	for _, l := range ls {
		fmt.Fprintf(&b, "- %s (model %s)\n", l.Name, orNone(l.Model))
	}
	return b.String(), nil
}

// setLauncher writes the launcher into 模型配置 › 主对话, keeping its model and effort.
func (s *Service) setLauncher(ctx context.Context, v json.RawMessage) (*ownChange, error) {
	var name string
	if err := json.Unmarshal(v, &name); err != nil || strings.TrimSpace(name) == "" {
		return nil, errors.New("value must be a launcher name")
	}
	name = strings.TrimSpace(name)
	before := s.launcherNow(ctx)
	if name == before {
		return &ownChange{summary: "启动器没有变化"}, nil
	}
	ls, err := s.launchers(ctx)
	if err != nil {
		return nil, err
	}
	known := false
	for _, l := range ls {
		known = known || l.Name == name
	}
	if !known {
		return nil, fmt.Errorf("no launcher %q on the assistant's machine (get lists them)", name)
	}
	if s.store() == nil {
		return nil, errors.New("no database on this hub")
	}
	rec, err := s.modelRecord(ctx, "assistant")
	if err != nil {
		return nil, err
	}
	rec.Binding.Provider = "launcher." + name
	return &ownChange{summary: fmt.Sprintf("Persona 的默认启动器：%s → %s", before, name),
		apply: func(ctx context.Context) (string, error) {
			if err := s.saveModelRecord(ctx, "assistant", rec); err != nil {
				return "", err
			}
			return "New Persona conversations start with " + name + ".", nil
		}}, nil
}
