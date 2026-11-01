package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chyroc/jarvis/server/internal/proto"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
)

// Typing into someone's terminal is the riskiest thing Jarvis does. Before a message goes into a running agent's
// pane the node reads the screen and refuses — nothing typed — when
//   - a dialog has the keyboard (permission prompt, picker, rewind): the text would pick an option (dialog_open);
//   - Claude's input box holds a draft typed at the computer, or bash mode: the message would be appended to it and
//     sent together, or run as a command (input_draft, with the box's text). The app may ask to clear that exact
//     draft and send (clear_draft).
// Sends carry the app's client_msg_id: a retry of one already delivered (the response was lost) is acknowledged
// without typing it again. See docs/protocol.md「发消息」.

// dialogKeys checks that the session's screen still shows the dialog a card was made from (same revision) and
// returns the keys for option, worked out from the screen as it is now (an unnumbered list moves from where the cursor
// is): keys nil when no option is named (the caller's keys stand). A changed or gone dialog refuses with
// prompt_changed, nothing pressed.
func (n *Node) dialogKeys(ctx context.Context, id, rev, option string) (keys, pre []string, err error) {
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	text, err := n.readPane(ctx, pane, "visible", screen.DialogLines)
	if err != nil {
		return nil, nil, err
	}
	d := screen.ParseDialog(text)
	if d == nil || d.Rev != rev {
		return nil, nil, refusal(screen.PromptChanged, "终端上的对话框已经变了（可能在电脑上答过了），这次什么都没按：看看新的再答。", nil)
	}
	if option == "" {
		return nil, nil, nil
	}
	o := d.Option(option)
	if o == nil {
		return nil, nil, fmt.Errorf("the dialog has no option %q", option)
	}
	if o.Kind == screen.KindTypeSomething {
		return []string{"enter"}, o.Keys, nil
	}
	return o.Keys, nil, nil
}

// sendOpts are the optional fields of session.send.
type sendOpts struct {
	ClientMsgID string `json:"client_msg_id"`
	// ClearDraft is the draft the user saw in a refusal and chose to clear: cleared only if the box still shows it.
	ClearDraft string `json:"clear_draft"`
}

// Delivery states a failed send reports: "none" when nothing reached the pane (safe to resend), "unknown" when the
// failure came after typing started.
const (
	deliveredNone    = "none"
	deliveredUnknown = "unknown"
)

func refusal(code, message string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["delivered"] = deliveredNone
	b, _ := json.Marshal(data)
	return &rpc.Error{Code: code, Message: message, Data: b}
}

// checkPane refuses a send into a pane that is not ready for typed text, after clearing the draft the user agreed
// to clear. kind is the agent ("claude", "codex", TRAEX).
func (n *Node) checkPane(ctx context.Context, pane, kind, clearDraft string) error {
	// Codex / TRAEX: their own screen (codexask.go) — a draft in the box, an approval or a question overlay refuses;
	// the transcript pager and 0.163's question pane are only shown, and step aside.
	if kind == "codex" || kind == traexAgent {
		return n.codexReady(ctx, pane, false)
	}
	// Claude in a host that reads screens without colors (host.PlainScreen: a draft and the grey placeholder look the
	// same there), any other agent: only the numbered approvals and pickers.
	if kind != "claude" || n.plainScreen(pane) {
		text, err := n.readPane(ctx, pane, "visible", 60)
		if err == nil && screen.Choosing(text) {
			return refusal(screen.DialogOpen, dialogMessage, nil)
		}
		return nil
	}
	read := func() string {
		s, err := n.readPaneANSI(ctx, pane, "visible", 60)
		if err != nil {
			s, _ = n.readPane(ctx, pane, "visible", 60) // no colors: any text in the box counts as a draft
		}
		return s
	}
	code, in := screen.CheckClaude(read())
	// What we typed a moment ago may still show while Claude takes it: wait for the box to empty rather than call our
	// own message a draft.
	for wait := time.Now().Add(2 * time.Second); code == screen.InputDraft && n.typing.ours(pane, in.Text) && time.Now().Before(wait); {
		time.Sleep(100 * time.Millisecond)
		code, in = screen.CheckClaude(read())
	}
	if code == screen.InputDraft && clearDraft != "" && in.Clearable == clearDraft {
		if err := n.sendKeys(ctx, pane, screen.ClearKeys...); err != nil {
			return refusal(code, draftMessage(in.Text), map[string]any{"draft": in.Text})
		}
		time.Sleep(150 * time.Millisecond)
		code, in = screen.CheckClaude(read())
	}
	switch code {
	case screen.DialogOpen:
		return refusal(code, dialogMessage, nil)
	case screen.InputDraft:
		data := map[string]any{"draft": in.Text}
		if in.Clearable != "" {
			data["clearable"] = in.Clearable
		}
		return refusal(code, draftMessage(in.Text), data)
	}
	return nil
}

// Refusals of a host that takes input on its own terms (herdr-core): it refused the text with nothing typed, or it
// cannot type text without submitting it.
const (
	notDelivered        = "not_delivered"
	notDeliveredMessage = "电脑上的终端这次没有收下这条消息，什么都没输入：稍后再发。"
	cannotType          = "cannot_type"
	cannotTypeMessage   = "电脑上的这个终端只能整条发送（粘贴后回车），不能只输入文字，这次什么都没按：在电脑上回答，或者直接发消息。"
)

// deliveryRefusal is the refusal for a host's error that typed nothing (host.ErrNotDelivered), nil for any other.
func deliveryRefusal(err error) error {
	if !errors.Is(err, host.ErrNotDelivered) {
		return nil
	}
	if errors.Is(err, host.ErrDialogOpen) {
		return refusal(screen.DialogOpen, dialogMessage, nil)
	}
	return refusal(notDelivered, notDeliveredMessage, nil)
}

// typingRefusal refuses, before anything is pressed, keys that type text into a host that cannot type without
// submitting (host.TextTyper) — unless it is only text and enter, which goes as a prompt. Dialog answers with keys to
// press first (a free-text option) are refused here, not after the field was opened. Only a session the node already
// knows is checked: herdr's path makes no extra call.
func (n *Node) typingRefusal(id, text string, keys, pre []string) error {
	if text == "" {
		return nil
	}
	a, ok := n.agent(id)
	if !ok || n.canTypeText(a.PaneID) {
		return nil
	}
	if len(pre) == 0 && len(keys) == 1 && keys[0] == "enter" {
		return nil
	}
	return refusal(cannotType, cannotTypeMessage, nil)
}

const dialogMessage = "电脑上的终端正开着一个对话框（确认、选择列表等），这条消息没有发出去：先回答或关掉它再发。"

func draftMessage(draft string) string {
	if r := []rune(draft); len(r) > 200 {
		draft = string(r[:200]) + "…"
	}
	if draft == "" {
		return "电脑上的输入框里有没发出去的内容，这条消息没有发出去，免得拼在一起。"
	}
	return "电脑上的输入框里有没发出去的内容：「" + draft + "」。这条消息没有发出去，免得拼在一起。"
}

// typingLocks makes the sends to one session take turns, and remembers what was last typed into each pane.
type typingLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	last  map[string]typedText
}

type typedText struct {
	text string
	at   time.Time
}

// lock waits for the session's earlier sends and returns the unlock.
func (t *typingLocks) lock(id string) func() {
	t.mu.Lock()
	if t.locks == nil {
		t.locks = map[string]*sync.Mutex{}
	}
	l := t.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		t.locks[id] = l
	}
	t.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (t *typingLocks) typed(pane, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]typedText{}
	}
	t.last[pane] = typedText{text, time.Now()}
}

// ours: the box shows (the start of) what was typed into the pane in the last few seconds, or Claude's placeholder
// for a long paste.
func (t *typingLocks) ours(pane, box string) bool {
	t.mu.Lock()
	last, ok := t.last[pane]
	t.mu.Unlock()
	if !ok || time.Since(last.at) > 5*time.Second {
		return false
	}
	b := strings.Join(strings.Fields(box), "")
	if b == "" {
		return false
	}
	return strings.HasPrefix(strings.Join(strings.Fields(last.text), ""), b) || pastePlaceholder.MatchString(box)
}

var pastePlaceholder = regexp.MustCompile(`^\s*\[Pasted text #\d+`)

// How long a prompt may sit in Claude's input box before ensureSubmitted presses Enter for it.
var submitGrace = 2 * time.Second

// ensureSubmitted presses Enter once when what was just prompted is still in Claude's input box after submitGrace.
// Seen with images (2026-10-31 and 11-01, Claude Code 2.1.206, a photo from the phone): the text and its
// `[Image #n]` chip sat in the box, the agent `done`, until Enter was pressed on the computer; 42 tries in an
// isolated herdr did not repeat it, so the cause is unknown. Only the box after the grace counts: right after the
// paste it can show something else for a moment (the first version gave up on that and missed one). A box that
// emptied by then (Claude took it, or queued it while working) or shows anything else is left alone.
func (n *Node) ensureSubmitted(ctx context.Context, pane, text string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(submitGrace):
	}
	s, err := n.readPaneANSI(ctx, pane, "visible", 60)
	if err != nil {
		return
	}
	in := screen.ClaudeInput(s)
	if !in.Box || !in.Draft || !stillPrompted(in.Text, text) {
		return
	}
	if n.logger != nil {
		n.logger.Printf("prompt still in the input box after %s, pressing enter", submitGrace)
	}
	_ = n.sendKeys(ctx, pane, "enter")
}

var boxChips = regexp.MustCompile(`\[(Image|Pasted text) #\d+[^\]]*\]`)

// stillPrompted: the input box shows the start of what was prompted. Claude turns an image path into an `[Image #n]`
// chip and a long paste into `[Pasted text #n …]`, so chips are dropped and only the first words are compared.
func stillPrompted(box, prompted string) bool {
	squash := func(s string) []rune {
		return []rune(strings.Join(strings.Fields(boxChips.ReplaceAllString(s, "")), ""))
	}
	b, p := squash(box), squash(strings.SplitN(prompted, "\n", 2)[0])
	if len(p) > 12 {
		p = p[:12]
	}
	return len(p) > 0 && len(b) >= len(p) && string(b[:len(p)]) == string(p)
}

// sentLog remembers the client_msg_id of recent sends (10 minutes) so a retry is not typed twice.
type sentLog struct {
	mu   sync.Mutex
	sent map[string]*sendRecord
}

type sendRecord struct {
	at   time.Time
	done chan struct{}
	err  error
}

const sentTTL = 10 * time.Minute

// once runs send for a client_msg_id at most once: a retry waits for a send in flight and returns its outcome, or
// succeeds at once after a delivered one. Refused sends (nothing typed) are forgotten, so their retry runs again.
func (l *sentLog) once(id string, send func() error) error {
	if id == "" {
		return send()
	}
	l.mu.Lock()
	if l.sent == nil {
		l.sent = map[string]*sendRecord{}
	}
	for k, r := range l.sent {
		if time.Since(r.at) > sentTTL {
			delete(l.sent, k)
		}
	}
	if r := l.sent[id]; r != nil {
		l.mu.Unlock()
		<-r.done
		return r.err
	}
	r := &sendRecord{at: time.Now(), done: make(chan struct{})}
	l.sent[id] = r
	l.mu.Unlock()
	r.err = send()
	if r.err != nil && !maybeDelivered(r.err) {
		l.mu.Lock()
		delete(l.sent, id)
		l.mu.Unlock()
	}
	close(r.done)
	return r.err
}

// uncertain marks a failure while typing into the pane: the message may have arrived.
func uncertain(err error) error {
	if err == nil {
		return nil
	}
	b, _ := json.Marshal(map[string]any{"delivered": deliveredUnknown})
	return &rpc.Error{Message: err.Error(), Data: b}
}

func maybeDelivered(err error) bool {
	e, ok := err.(*rpc.Error)
	if !ok {
		return false
	}
	var d struct {
		Delivered string `json:"delivered"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return d.Delivered == deliveredUnknown
}

// Refusals of a send to a session that is not running on an engine that cannot start one (herdr-core, Orca): herdr
// resumes it in a new pane instead.
const (
	notRunning    = "not_running"
	notInFront    = "agent_not_in_front"
	inFrontReason = "Claude 不在前台（可能被挂起），没有发送：在电脑上把它切回前台再发。"
)

// notRunningRefusal refuses, with nothing typed, a send to a session that is not live when the machine's engine
// cannot start sessions (no host.Launcher), instead of trying to resume it there: when the pane it was last seen in is
// still there, its agent is not in front (suspended, in the background); otherwise it is gone. nil when the engine can
// resume it (herdr: unchanged).
func (n *Node) notRunningRefusal(ctx context.Context, id string) error {
	h := n.defaultHost()
	if h == nil {
		return nil
	}
	if _, ok := h.(host.Launcher); ok {
		return nil
	}
	n.mu.Lock()
	pane := n.lastPanes[id]
	n.mu.Unlock()
	if pane != "" {
		if ph, ref := n.hosts.Pane(pane); ph != nil {
			if _, err := ph.Agent(ctx, ref); err == nil {
				return refusal(notInFront, inFrontReason, nil)
			}
		}
	}
	name := proto.EngineNames[n.engine()]
	return refusal(notRunning, fmt.Sprintf("这个会话现在没有在运行（可能被挂起或已退出），%s 里的会话要在电脑上重新打开，这次没有发送。", name), nil)
}
