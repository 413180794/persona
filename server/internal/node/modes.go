package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Permission mode and effort level of a Claude session, chosen from the app (docs/ios.md「思考强度和权限模式」).
//
// Shown: the transcript records the mode on every prompt and the effort on every reply; a value set from the app
// wins until the transcript shows a newer one (a TUI only writes the mode down with the next prompt).
// Set: at start as --permission-mode / --effort; later, headless conversations take control requests on stdin and
// TUI sessions are driven like a person would: shift+tab cycles the mode, and the /effort picker's "s" sets the
// level for this session only (`/effort <level>` would also save it as the default for new sessions).

var effortLevels = proto.EffortLevels

var permissionModes = proto.PermissionModes

// tuiModeCycle is the order shift+tab walks through (Claude Code 2.1.x); bypassPermissions is only in the cycle
// of sessions started with it.
var tuiModeCycle = []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions"}

// sessionModes is a mode / effort the app set, with when, until the transcript catches up.
type sessionModes struct {
	Mode, Effort     string
	ModeAt, EffortAt time.Time
}

func validModes(mode, effort string) error {
	if mode != "" && !slices.Contains(permissionModes, mode) {
		return fmt.Errorf("unknown permission mode %q", mode)
	}
	if effort != "" && !slices.Contains(effortLevels, effort) {
		return fmt.Errorf("unknown effort level %q", effort)
	}
	return nil
}

// modeFlags are the claude flags starting a session with a mode / effort (values already validated).
func modeFlags(mode, effort string) string {
	var b strings.Builder
	if mode != "" {
		b.WriteString(" --permission-mode " + mode)
	}
	if effort != "" {
		b.WriteString(" --effort " + effort)
	}
	return b.String()
}

func (n *Node) noteModes(id, mode, effort string) {
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	m := n.modes[id]
	if mode != "" {
		m.Mode, m.ModeAt = mode, now
	}
	if effort != "" {
		m.Effort, m.EffortAt = effort, now
	}
	n.modes[id] = m
}

// currentModes is the session's mode and effort: from its transcript, or what the app set since.
func (n *Node) currentModes(id string, m quickMeta) (mode, effort string) {
	mode, effort = m.mode, m.effort
	if mode != "" && !slices.Contains(permissionModes, mode) && n.isAssistantSession(id) {
		mode = modeFromCodex(mode, m.sandbox) // a Codex rollout records its approval policy and sandbox
	}
	n.mu.Lock()
	set, ok := n.modes[id]
	n.mu.Unlock()
	if ok {
		if set.Mode != "" && set.ModeAt.After(m.modeAt) {
			mode = set.Mode
		}
		if set.Effort != "" && set.EffortAt.After(m.effortAt) {
			effort = set.Effort
		}
	}
	return mode, effort
}

// modeFromCodex reads the app's mode back from what an assistant's Codex turn ran with (codexPolicy in reverse), so
// it survives a node restart. acceptEdits runs like default and reads back as default.
func modeFromCodex(approval, sandbox string) string {
	for _, mode := range []string{"default", "auto", "plan", proto.ModeBypass} {
		if a, s := codexPolicy(mode); a == approval && s == sandbox {
			return mode
		}
	}
	return ""
}

// modesOf is currentModes for a session id, reading its transcript meta when there is one.
func (n *Node) modesOf(id string) (mode, effort string) {
	var m quickMeta
	if p := n.transcriptPath(id); p != "" {
		if st, err := os.Stat(p); err == nil {
			m = n.meta(p, st)
		}
	}
	return n.currentModes(id, m)
}

// setModes changes a running session's permission mode and / or effort level ("" leaves one as it is).
func (n *Node) setModes(ctx context.Context, id, mode, effort string) error {
	if err := validModes(mode, effort); err != nil {
		return err
	}
	if n.isAssistantSession(id) {
		n.noteModes(id, mode, effort)
		n.mu.Lock()
		p := n.procs[id]
		n.mu.Unlock()
		if p != nil && p.alive() {
			n.syncAssistantModes(p)
		} // else: applied when the next message starts the process
		n.notifySessions()
		return nil
	}
	if n.codexPath(id) != "" || n.traexPath(id) != "" {
		return fmt.Errorf("Codex / TRAEX 的会话不支持在手机上改权限模式和思考强度")
	}
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return fmt.Errorf("这个 Agent 没在运行：发一条消息恢复它以后再改")
	}
	// The /effort picker makes herdr report the agent as blocked; the hub would turn that into a "needs you" push.
	n.mu.Lock()
	n.driving[id] = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.driving, id)
		n.mu.Unlock()
	}()
	if mode != "" {
		if err := n.tuiSetMode(ctx, pane, mode); err != nil {
			return err
		}
		n.noteModes(id, mode, "")
	}
	if effort != "" {
		if a, ok := n.agent(id); ok && (a.AgentStatus == proto.StatusWorking || a.AgentStatus == proto.StatusBlocked) {
			return fmt.Errorf("Agent 正在工作，这一轮结束后再改思考强度")
		}
		if err := n.tuiSetEffort(ctx, pane, effort); err != nil {
			return err
		}
		n.noteModes(id, "", effort)
	}
	n.notifySessions()
	return nil
}

// tuiMode reads the permission mode off the TUI's footer: the lines under the input box's bottom rule.
func tuiMode(screen string) string {
	lines := strings.Split(screen, "\n")
	rule := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "──") {
			rule = i
			break
		}
	}
	if rule < 0 {
		return ""
	}
	for _, l := range lines[rule+1:] {
		switch {
		case strings.Contains(l, "accept edits on"):
			return "acceptEdits"
		case strings.Contains(l, "plan mode on"):
			return "plan"
		case strings.Contains(l, "auto mode on"):
			return "auto"
		case strings.Contains(l, "bypass permissions on"):
			return "bypassPermissions"
		case strings.Contains(l, "manual mode on"), strings.Contains(l, "? for shortcuts"):
			return "default"
		}
	}
	return ""
}

// tuiSetMode presses shift+tab until the footer shows the wanted mode.
func (n *Node) tuiSetMode(ctx context.Context, pane, mode string) error {
	for range len(tuiModeCycle) + 1 {
		screen, err := n.readPane(ctx, pane, "visible", 60)
		if err != nil {
			return err
		}
		cur := tuiMode(screen)
		if cur == mode {
			return nil
		}
		if cur == "" {
			return fmt.Errorf("终端里没看到 Claude Code 的输入框（可能有对话框挡着）")
		}
		if err := n.sendKeys(ctx, pane, "shift+tab"); err != nil {
			return err
		}
		time.Sleep(350 * time.Millisecond)
	}
	if mode == "bypassPermissions" {
		return fmt.Errorf("「全部放行」只能在新开 Agent 时选，运行中切不过去")
	}
	return fmt.Errorf("切换权限模式没成功：终端里循环一圈也没出现 %s", mode)
}

// tuiPromptEmpty reports whether nothing is typed in the TUI's input box (the hint text on a fresh session counts as
// empty), so the /effort command is not appended to someone's draft.
func tuiPromptEmpty(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if rest, ok := strings.CutPrefix(t, "❯"); ok {
			rest = strings.TrimSpace(rest)
			return rest == "" || strings.HasPrefix(rest, `Try "`)
		}
	}
	return false
}

// tuiSetEffort opens the /effort picker, moves to the level and confirms it for this session. Two pickers exist: the
// older list ("for this session only", "s" picks it for the session) and, from Claude Code 2.1.2xx, a slider
// ("←/→ to adjust · Enter to confirm") whose leftmost stop is low and that confirms with Enter.
func (n *Node) tuiSetEffort(ctx context.Context, pane, effort string) error {
	screen, err := n.readPane(ctx, pane, "visible", 60)
	if err != nil {
		return err
	}
	if !tuiPromptEmpty(screen) {
		return fmt.Errorf("终端的输入框里有还没发出的文字，先在电脑上处理掉再改思考强度")
	}
	if !n.canTypeText(pane) { // a host that types only whole submissions (herdr-core): the command is one
		if err := n.hostPrompt(ctx, pane, "/effort"); err != nil {
			return err
		}
	} else {
		if err := n.sendText(ctx, pane, "/effort"); err != nil {
			return err
		}
		time.Sleep(300 * time.Millisecond)
		if err := n.sendKeys(ctx, pane, "enter"); err != nil {
			return err
		}
	}
	picker := ""
	for range 15 {
		time.Sleep(200 * time.Millisecond)
		if s, err := n.readPane(ctx, pane, "visible", 60); err == nil {
			if picker = effortPicker(s); picker != "" {
				break
			}
		}
	}
	if picker == "" {
		_ = n.sendKeys(ctx, pane, "esc")
		return fmt.Errorf("没打开思考强度的选择器（这个 Claude Code 版本可能不支持 /effort）")
	}
	// From the left end (low) one stop right per level; the slider has stops past max (ultracode), the list does not.
	keys := []string{"left", "left", "left", "left", "left", "left"}
	for range slices.Index(effortLevels, effort) {
		keys = append(keys, "right")
	}
	if err := n.sendKeys(ctx, pane, keys...); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	confirm := "s"
	if picker == "slider" {
		confirm = "enter"
	}
	if err := n.sendKeys(ctx, pane, confirm); err != nil {
		return err
	}
	for range 15 {
		time.Sleep(200 * time.Millisecond)
		s, err := n.readPane(ctx, pane, "visible", 60)
		if err == nil && effortPicker(s) == "" {
			return effortOutcome(s, effort)
		}
	}
	_ = n.sendKeys(ctx, pane, "esc")
	return fmt.Errorf("思考强度的选择器没有关闭，已取消")
}

// effortPicker says which /effort picker is on screen: "list", "slider" or "" (none).
func effortPicker(screen string) string {
	switch {
	case strings.Contains(screen, "for this session only"):
		return "list"
	case strings.Contains(screen, "to adjust") && strings.Contains(screen, "Enter to confirm") && strings.Contains(screen, "Effort"):
		return "slider"
	}
	return ""
}

// effortOutcome reads what Claude Code printed once the picker closed. An effort set in the environment
// (CLAUDE_CODE_EFFORT_LEVEL: the env block of Claude's settings.json, or a launcher) wins over the session's: say so
// instead of claiming it changed.
func effortOutcome(screen, effort string) error {
	flat := strings.Join(strings.Fields(screen), " ")
	if i := strings.LastIndex(flat, "CLAUDE_CODE_EFFORT_LEVEL="); i >= 0 {
		v := strings.TrimRight(strings.Fields(flat[i+len("CLAUDE_CODE_EFFORT_LEVEL="):] + " ")[0], ".,;:")
		if v != effort && strings.Contains(flat[i:], "overrides") {
			return fmt.Errorf("这台电脑设了 CLAUDE_CODE_EFFORT_LEVEL=%s（Claude 的 settings.json 的 env 或启动器里），它优先于会话里改的值，要改得先去掉它", v)
		}
	}
	if strings.Contains(flat, "effort level to "+effort) || strings.Contains(flat, "● "+effort+" ") || strings.Contains(flat, "◈ "+effort+" ") {
		return nil
	}
	return fmt.Errorf("思考强度可能没改成 %s，请在终端里确认", effort)
}

// syncAssistantModes sends a headless conversation's wanted mode and effort as control requests (they apply from
// the next request on, mid-turn included), once per change.
func (n *Node) syncAssistantModes(p *assistantProc) {
	if p.agent == "codex" {
		return // Codex gets them with each turn (codexPolicy), and the node answers approvals by the mode it reads then
	}
	mode, effort := n.modesOf(p.id)
	p.mu.Lock()
	var reqs []map[string]any
	if mode != "" && mode != p.mode {
		reqs = append(reqs, map[string]any{"subtype": "set_permission_mode", "mode": mode})
		p.mode = mode
	}
	if effort != "" && effort != p.effort {
		reqs = append(reqs, map[string]any{"subtype": "apply_flag_settings", "settings": map[string]any{"effortLevel": effort}})
		p.effort = effort
	}
	p.mu.Unlock()
	for _, r := range reqs {
		p.reqSeq.Add(1)
		line, _ := json.Marshal(map[string]any{"type": "control_request", "request_id": fmt.Sprintf("jarvis-%d", p.reqSeq.Load()), "request": r})
		if err := p.write(line); err != nil {
			n.logger.Printf("assistant %s: control request: %v", p.id[:8], err)
		}
	}
}
