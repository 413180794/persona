package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeSetting is one Claude Code /config option the app may change. Only these keys are read or written;
// everything else in ~/.claude/settings.json is preserved as is.
type ClaudeSetting struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Detail  string   `json:"detail"`
	Type    string   `json:"type"` // bool | enum | string
	Options []string `json:"options,omitempty"`
	Default any      `json:"default"`
	Value   any      `json:"value"` // nil when unset (Claude Code uses the default)
}

var claudeSettings = []ClaudeSetting{
	{Key: "awaySummaryEnabled", Title: "离开回顾（recap）", Detail: "离开 5 分钟以上回来时，Claude 先写一段进展回顾", Type: "bool", Default: true},
	{Key: "alwaysThinkingEnabled", Title: "总是深度思考", Detail: "每轮都开启 extended thinking", Type: "bool", Default: false},
	{Key: "effortLevel", Title: "思考强度", Detail: "越高越慢、越仔细", Type: "enum", Options: []string{"low", "medium", "high", "xhigh", "max"}, Default: "high"},
	{Key: "language", Title: "回复语言", Detail: "Claude 回复用的语言，比如「中文」", Type: "string", Default: ""},
	{Key: "promptSuggestionEnabled", Title: "输入建议", Detail: "在输入框里预测下一句", Type: "bool", Default: true},
	{Key: "spinnerTipsEnabled", Title: "等待时的小提示", Detail: "工作中在状态行显示使用技巧", Type: "bool", Default: true},
	{Key: "prefersReducedMotion", Title: "减少动效", Detail: "终端里少用动画", Type: "bool", Default: false},
	{Key: "autoUpdatesChannel", Title: "更新渠道", Detail: "Claude Code 自动更新跟随的版本线", Type: "enum", Options: []string{"latest", "stable"}, Default: "latest"},
}

func (n *Node) claudeSettingsPath() string {
	p := filepath.Join(n.home, ".claude", "settings.json")
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real // the file may be a symlink into a dotfiles repo; edit the target
	}
	return p
}

func (n *Node) readClaudeSettings() (map[string]any, error) {
	raw := map[string]any{}
	b, err := os.ReadFile(n.claudeSettingsPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("~/.claude/settings.json is not valid JSON: %w", err)
		}
	}
	return raw, nil
}

// getClaudeSettings returns the editable options with their current values.
func (n *Node) getClaudeSettings() ([]ClaudeSetting, error) {
	raw, err := n.readClaudeSettings()
	if err != nil {
		return nil, err
	}
	out := make([]ClaudeSetting, len(claudeSettings))
	for i, s := range claudeSettings {
		s.Value = raw[s.Key]
		out[i] = s
	}
	return out, nil
}

// setClaudeSetting validates and writes one option (value nil removes it, i.e. back to the default).
func (n *Node) setClaudeSetting(key string, value any) error {
	var def *ClaudeSetting
	for i := range claudeSettings {
		if claudeSettings[i].Key == key {
			def = &claudeSettings[i]
		}
	}
	if def == nil {
		return fmt.Errorf("setting %q is not editable from the app", key)
	}
	if value != nil {
		switch def.Type {
		case "bool":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be true or false", key)
			}
		case "enum":
			s, _ := value.(string)
			valid := false
			for _, o := range def.Options {
				valid = valid || o == s
			}
			if !valid {
				return fmt.Errorf("%s must be one of %v", key, def.Options)
			}
		case "string":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	raw, err := n.readClaudeSettings()
	if err != nil {
		return err
	}
	if value == nil || value == "" {
		delete(raw, key)
	} else {
		raw[key] = value
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	path := n.claudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".jarvis.tmp"
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(tmp, append(b, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
