package node

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

// Ultracode is independent of effort in Claude Code 2.1.284 and later. Only the
// slash command changes it: neither launch flags nor settings carry a choice.
// The record is an observation of one process, never a preference for a resume.
type ultracodeRecord struct {
	Since  time.Time  `json:"since"`
	Value  *bool      `json:"value,omitempty"`
	At     time.Time  `json:"at,omitempty"`
	LostAt *time.Time `json:"lost_at,omitempty"`
	// LostDismissed also cancels a delayed history scan after dismissal or reopening.
	LostDismissed bool `json:"lost_dismissed,omitempty"`
}

type ultracodeState struct {
	persistMu sync.Mutex // serializes writes of this process observation
	ultracodeRecord
	pending        *bool
	note           string
	rejectedModel  string
	applying       bool
	retryAt        time.Time
	awaitingHook   bool
	awaitingResume bool
}

func ultraBool(v bool) *bool { return &v }

var ultracodeVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+].*)?$`)

func ultracodeVersionNote(version string) string {
	version = strings.TrimSpace(version)
	if !ultracodeVersion.MatchString(version) {
		return ""
	} // unknown: try the command
	if versionAtLeast(strings.TrimPrefix(strings.SplitN(version, "+", 2)[0], "v"), [3]int{2, 1, 284}) {
		return ""
	}
	return "请升级到 Claude Code 2.1.284 或更新版本，旧版不支持独立的 ultracode 开关"
}

func ultracodeCommand(on bool) string {
	if on {
		return "/effort ultracode on"
	}
	return "/effort ultracode off"
}

// ultracodeOutcome only reads the verdict after the command, never an old line
// above it. A nil value is unknown, including refusals, not an assumed off.
func ultracodeOutcome(shown string, on bool) (value *bool, note string, modelRejected bool) {
	flat := strings.Join(strings.Fields(screen.Plain(shown)), " ")
	i := strings.LastIndex(flat, ultracodeCommand(on))
	if i < 0 {
		return nil, "", false
	}
	flat = flat[i+len(ultracodeCommand(on)):]
	switch {
	case strings.Contains(flat, "Ultracode needs dynamic workflows enabled"):
		return nil, "需要先在 Claude Code 设置中开启 dynamic workflows（工作流）", false
	case strings.Contains(flat, "Ultracode isn't available on "):
		model := strings.SplitN(strings.SplitN(flat, "Ultracode isn't available on ", 2)[1], ". ", 2)[0]
		return nil, "模型 " + strings.TrimSpace(model) + " 不支持 ultracode，请切换模型后重试", true
	case strings.Contains(flat, "CLAUDE_CODE_EFFORT_LEVEL=") && strings.Contains(flat, "overrides"):
		if err := effortOutcome(flat, "ultracode"); err != nil {
			return nil, err.Error(), false
		}
	case strings.Contains(flat, "Invalid argument"), strings.Contains(flat, "Usage: /effort"), strings.Contains(flat, "Usage: /ultracode"):
		return nil, "Claude Code 未接受 ultracode 开关，请确认版本为 2.1.284 或更新版本", false
	}
	if strings.Contains(flat, "Ultracode on (this session only):") {
		return ultraBool(true), "", false
	}
	if strings.Contains(flat, "Ultracode off.") {
		return ultraBool(false), "", false
	}
	return nil, "", false
}

func (n *Node) ultracodeMeta(id string) quickMeta {
	var m quickMeta
	if p := n.transcriptPath(id); p != "" {
		if st, err := os.Stat(p); err == nil {
			m = n.meta(p, st)
		}
	}
	m.model = n.shownModel(id, m.model)
	return m
}

func (n *Node) bgUltracode(path string) (*bool, time.Time) {
	n.mu.Lock()
	sc := n.bgScans[path]
	n.mu.Unlock()
	if sc == nil {
		return nil, time.Time{}
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.pending {
		return nil, time.Time{}
	}
	return sc.meta.Ultracode, sc.meta.UltracodeAt
}

// ultracodeLocked hydrates a persisted process observation. Caller holds n.mu.
func (n *Node) ultracodeLocked(id string) *ultracodeState {
	if n.modes == nil {
		n.modes = map[string]sessionModes{}
	}
	m := n.modes[id]
	if m.Ultracode == nil {
		m.Ultracode = &ultracodeState{}
		if n.state != nil {
			if st, ok := n.state.get(id); ok && st.Ultracode != nil {
				m.Ultracode.ultracodeRecord = *st.Ultracode
			}
		}
		n.modes[id] = m
	}
	return m.Ultracode
}

func (u *ultracodeState) observe(m quickMeta) {
	if m.ultracode != nil && !m.ultracodeAt.Before(u.Since) && m.ultracodeAt.After(u.At) {
		u.Value, u.At = m.ultracode, m.ultracodeAt
		u.note, u.rejectedModel = "", ""
		if *u.Value {
			u.LostAt = nil
			u.LostDismissed = true
		}
	}
	if u.rejectedModel != "" && m.model != "" && m.model != u.rejectedModel {
		u.note, u.rejectedModel = "", ""
	}
}

func (n *Node) persistUltracode(id string) {
	if n.state == nil {
		return
	}
	n.mu.Lock()
	u := n.ultracodeLocked(id)
	n.mu.Unlock()
	u.persistMu.Lock()
	defer u.persistMu.Unlock()
	n.mu.Lock()
	record := u.ultracodeRecord
	n.mu.Unlock()
	st, _ := n.state.get(id)
	st.Ultracode = &record
	n.state.put(id, st)
}

// beginUltracodeProcess invalidates every older observation. The hook for a
// process started here completes its boundary without dropping its start choice.
func (n *Node) beginUltracodeProcess(id string, resume, own bool, now time.Time) {
	m := n.ultracodeMeta(id)
	n.mu.Lock()
	u := n.ultracodeLocked(id)
	u.observe(m)
	if !own && u.awaitingHook && resume == u.awaitingResume && time.Since(u.Since) < n.startWait() {
		u.awaitingHook = false
		// Keep the launch boundary: the hook may reach node after a slash verdict.
		n.mu.Unlock()
		return
	}
	lost := u.LostAt
	previousSince, previousAt := u.Since, u.At
	if u.Value != nil && *u.Value {
		t := now.UTC()
		lost = &t
	}
	u.ultracodeRecord = ultracodeRecord{Since: now.UTC(), LostAt: lost}
	if resume {
		u.Value, u.At = ultraBool(false), now.UTC()
	}
	u.pending, u.note, u.rejectedModel = nil, "", ""
	u.applying = false
	u.awaitingHook, u.awaitingResume = own, resume
	n.mu.Unlock()
	n.persistUltracode(id)
	if resume && m.bgWait && lost == nil {
		n.recoverUltracodeLost(id, previousSince, previousAt, now.UTC())
	}
}

func (n *Node) ultracodeHook(req sockRequest, in hookInput) {
	if req.Resume || in.AgentID != "" || in.HookEventName != "SessionStart" ||
		(in.Source != "startup" && in.Source != "resume") || !validID(in.SessionID) {
		return
	}
	n.rememberTranscript(in)
	if n.isAssistantSession(in.SessionID) {
		return
	}
	n.beginUltracodeProcess(in.SessionID, in.Source == "resume", false, time.Now())
}

func (n *Node) ultracodeRow(s *proto.Session) {
	if s.Agent != "claude" || n.isAssistantSession(s.SessionID) {
		return
	}
	m := n.ultracodeMeta(s.SessionID)
	if s.Model != "" {
		m.model = s.Model
	}
	n.mu.Lock()
	u := n.ultracodeLocked(s.SessionID)
	before := u.ultracodeRecord
	u.observe(m)
	changed := before != u.ultracodeRecord
	s.Ultracode, s.UltracodeNote, s.UltracodePending, s.UltracodeLostAt = u.Value, u.note, u.pending != nil, u.LostAt
	if note := ultracodeVersionNote(m.version); note != "" {
		s.UltracodeNote = note
	}
	run := u.pending != nil && !u.applying && !s.Starting && s.PaneID != "" &&
		(s.Status == proto.StatusIdle || s.Status == proto.StatusDone) && time.Now().After(u.retryAt)
	if run {
		u.applying = true
	}
	n.mu.Unlock()
	if changed {
		n.persistUltracode(s.SessionID)
	}
	if run {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			unlock := n.typing.lock(s.SessionID)
			_ = n.applyPendingUltracode(ctx, s.SessionID, s.PaneID)
			unlock()
			n.notifySessions()
		}()
	}
}

func writeUltracodeKey(w *keyWriter, s *proto.Session) {
	if s.Ultracode == nil && s.UltracodeNote == "" && !s.UltracodePending && s.UltracodeLostAt == nil {
		return
	}
	w.b.WriteString("ultracode:")
	switch {
	case s.Ultracode == nil:
		w.str("unknown", '/')
	case *s.Ultracode:
		w.str("on", '/')
	default:
		w.str("off", '/')
	}
	w.quote(s.UltracodeNote, '/')
	if s.UltracodePending {
		w.b.WriteByte('p')
	}
	if s.UltracodeLostAt != nil {
		w.int(s.UltracodeLostAt.UnixNano(), '/')
	}
	w.b.WriteByte('\n')
}

// setUltracode queues a busy agent's request; a resting agent takes it now.
func (n *Node) setUltracode(ctx context.Context, id string, on *bool, dismiss bool) error {
	if n.isAssistantSession(id) {
		return fmt.Errorf("主对话不支持 ultracode，请在 Agent 会话中使用")
	}
	if on == nil && !dismiss {
		return fmt.Errorf("ultracode or dismiss_lost is required")
	}
	if on == nil {
		n.mu.Lock()
		n.ultracodeLocked(id).LostAt = nil
		n.ultracodeLocked(id).LostDismissed = true
		n.mu.Unlock()
		n.persistUltracode(id)
		n.notifySessions()
		return nil
	}
	// Wait outside the input lock: an async start needs it for its first prompt.
	if err := n.starts.wait(ctx, id); err != nil {
		return err
	}
	defer n.typing.lock(id)()
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return fmt.Errorf("这个 Agent 没在运行：发一条消息恢复它以后再改")
	}
	if a, ok := n.agent(id); !ok || a.Agent != "claude" {
		return fmt.Errorf("只有 Claude Code Agent 支持 ultracode")
	}
	m := n.ultracodeMeta(id)
	if note := ultracodeVersionNote(m.version); note != "" {
		return fmt.Errorf("%s", note)
	}
	n.mu.Lock()
	u := n.ultracodeLocked(id)
	u.observe(m)
	if u.rejectedModel != "" && *on {
		note := u.note
		n.mu.Unlock()
		return fmt.Errorf("%s", note)
	}
	u.pending, u.note = ultraBool(*on), ""
	if dismiss {
		u.LostDismissed = true
		u.LostAt = nil
	}
	n.mu.Unlock()
	if dismiss {
		n.persistUltracode(id)
	}
	err = n.applyPendingUltracode(ctx, id, pane)
	n.notifySessions()
	return err
}

func (n *Node) ultracodeBusy(id string) bool {
	// agent applies claudeStatus: a stale host "done" cannot hide an open turn,
	// live hook counters or a working title. A closed turn clears stale spinners.
	if a, ok := n.agent(id); ok && (a.AgentStatus == proto.StatusWorking || a.AgentStatus == proto.StatusBlocked) {
		return true
	}
	m := n.ultracodeMeta(id)
	// A dynamic workflow may still run after the foreground turn stops.
	for _, w := range n.workflows(id, m.wf, m.wfScript) {
		if w.Running {
			return true
		}
	}
	return m.bgWait
}

// applyPendingUltracode requires the input lock, or a start before it exposes its
// input to sends. A draft/dialog leaves the request queued and is never cleared.
func (n *Node) applyPendingUltracode(ctx context.Context, id, pane string) error {
	n.mu.Lock()
	u := n.ultracodeLocked(id)
	wanted := u.pending
	generation := u.Since
	u.applying = wanted != nil
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		if u.Since.Equal(generation) {
			u.applying = false
		}
		n.mu.Unlock()
	}()
	if wanted == nil {
		return nil
	}
	if n.ultracodeBusy(id) {
		n.mu.Lock()
		u.retryAt = time.Now().Add(3 * time.Second)
		n.mu.Unlock()
		return nil
	}
	if err := n.checkPane(ctx, pane, "claude", ""); err != nil {
		n.mu.Lock()
		if u.Since.Equal(generation) {
			u.note, u.retryAt = err.Error(), time.Now().Add(3*time.Second)
		}
		n.mu.Unlock()
		return err
	}
	m := n.ultracodeMeta(id)
	if note := ultracodeVersionNote(m.version); note != "" {
		n.mu.Lock()
		if u.Since.Equal(generation) {
			u.pending, u.note = nil, note
		}
		n.mu.Unlock()
		return fmt.Errorf("%s", note)
	}
	before, err := n.readPane(ctx, pane, "visible", 60)
	if err != nil {
		return err
	}
	n.mu.Lock()
	sameProcess := u.Since.Equal(generation)
	n.mu.Unlock()
	if !sameProcess {
		return fmt.Errorf("Claude Code 已恢复，请重新设置 ultracode")
	}
	cmd := ultracodeCommand(*wanted)
	started := time.Now().UTC()
	if err := n.prompt(ctx, pane, cmd); err != nil {
		n.mu.Lock()
		if u.Since.Equal(generation) {
			u.pending, u.note = nil, err.Error()
		}
		n.mu.Unlock()
		if r := deliveryRefusal(err); r != nil {
			return r
		}
		return uncertain(err)
	}
	n.typing.typed(pane, cmd)
	var value *bool
	var note string
	var rejected bool
	for range 15 {
		select {
		case <-ctx.Done():
			note = "未能确认 ultracode 状态，请在终端里确认"
		case <-time.After(200 * time.Millisecond):
		}
		if ctx.Err() != nil {
			break
		}
		shown, err := n.readPane(ctx, pane, "visible", 60)
		if err != nil || shown == before {
			continue
		}
		value, note, rejected = ultracodeOutcome(shown, *wanted)
		if value != nil || note != "" {
			break
		}
	}
	if value == nil && note == "" {
		note = "未能确认 ultracode 状态，请在终端里确认"
	}
	if value != nil && *value != *wanted {
		note = "ultracode 的实际状态与请求不一致，请在终端里确认"
	}
	n.mu.Lock()
	if !u.Since.Equal(generation) {
		n.mu.Unlock()
		return fmt.Errorf("Claude Code 已恢复，未采用旧进程的 ultracode 结果")
	}
	u.pending, u.note = nil, note
	if value != nil {
		u.Value, u.At = value, started
		if *value {
			u.LostDismissed = true
			u.LostAt = nil
		}
	}
	if rejected {
		u.rejectedModel = m.model
		if u.rejectedModel == "" {
			u.rejectedModel = "unknown"
		}
	}
	n.mu.Unlock()
	n.persistUltracode(id)
	if note != "" {
		return fmt.Errorf("%s", note)
	}
	return nil
}

func (n *Node) checkUltracodeStart(ctx context.Context, on *bool, agent string) error {
	if on == nil {
		return nil
	}
	if agent != "claude" {
		return fmt.Errorf("只有 Claude Code Agent 支持 ultracode")
	}
	if note := ultracodeVersionNote(n.claudeV.get(ctx)); note != "" {
		return fmt.Errorf("%s", note)
	}
	return nil
}

func (n *Node) startUltracode(ctx context.Context, id, pane string, on *bool) error {
	if on == nil {
		return nil
	}
	n.mu.Lock()
	n.ultracodeLocked(id).pending = ultraBool(*on)
	n.ultracodeLocked(id).applying = true
	n.mu.Unlock()
	return n.applyPendingUltracode(ctx, id, pane)
}
