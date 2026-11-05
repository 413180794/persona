package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
)

// `personad ask` and `personad notify` (internal/scriptcli) reach the phone through the node's socket like the hook
// does (docs/script-cli.md). ask is an interaction of the same shapes the phone already draws: a question card when it
// has options, an allow / deny permission card when it has not. notify is a hub "notify" request (the path
// POST /api/v1/notify takes). Both are attributed to the session of the pane the caller runs in, when the caller says
// which pane that is (HookPane) and a hook has told the node what runs there.

// Ask results (AskReply.Status).
const (
	AskAnswered    = "answered"    // an option was chosen (Answer), or the card was allowed
	AskDenied      = "denied"      // the card was denied
	AskDismissed   = "dismissed"   // handed back to the computer, or answered without an answer
	AskTimeout     = "timeout"     // nobody answered in time
	AskUnavailable = "unavailable" // there is no hub to ask, or asking it failed
	AskInvalid     = "invalid"     // the request is wrong (Error says how)
)

// Limits of an ask: the phone draws a short list of short labels.
const (
	askOptionsMost  = 12
	askOptionMost   = 100 // runes
	askQuestionMost = 1000
	// AskTimeoutMost bounds how long one ask waits for the phone.
	AskTimeoutMost = 24 * time.Hour
	// scriptAgent / scriptAgentLabel: who asks, as a runtime's cards say it (docs/runtimes.md): the apps draw "脚本 在问你"
	// and offer no "always allow" on it.
	scriptAgent      = "script"
	scriptAgentLabel = "脚本"
	scriptAskTool    = "personad_ask"
	// notifyTimeout bounds the hub's answer to a notification.
	notifyTimeout = 15 * time.Second
	// The hub's limits of a notification's texts (hub/notify_api.go); longer ones are cut here, not refused.
	notifyTitleMost = 120
	notifyBodyMost  = 1000
)

// AskRequest is `personad ask`'s question.
type AskRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"` // none: allow / deny
	// TimeoutSeconds: 0 is HookTimeout.
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Cwd            string `json:"cwd,omitempty"` // where the caller runs: names the card when no session does
}

// Normalize trims the question and the options (empty and repeated ones go) and says what is wrong with the request.
func (a *AskRequest) Normalize() error {
	a.Question = strings.TrimSpace(a.Question)
	switch {
	case a.Question == "":
		return errors.New("the question is empty")
	case utf8.RuneCountInString(a.Question) > askQuestionMost:
		return fmt.Errorf("the question is longer than %d characters", askQuestionMost)
	case a.TimeoutSeconds < 0 || time.Duration(a.TimeoutSeconds)*time.Second > AskTimeoutMost:
		return fmt.Errorf("the timeout must be between 1 second and %s", AskTimeoutMost)
	}
	var opts []string
	seen := map[string]bool{}
	for _, o := range a.Options {
		o = strings.TrimSpace(o)
		if o == "" || seen[o] {
			continue
		}
		if utf8.RuneCountInString(o) > askOptionMost {
			return fmt.Errorf("an option is longer than %d characters", askOptionMost)
		}
		seen[o] = true
		opts = append(opts, o)
	}
	if len(opts) > askOptionsMost {
		return fmt.Errorf("more than %d options", askOptionsMost)
	}
	a.Options = opts
	return nil
}

// Timeout is how long the ask waits for the phone.
func (a AskRequest) Timeout() time.Duration {
	if a.TimeoutSeconds <= 0 {
		return HookTimeout
	}
	return time.Duration(a.TimeoutSeconds) * time.Second
}

// AskReply is the node's answer to an ask.
type AskReply struct {
	Status string `json:"status"`
	Answer string `json:"answer,omitempty"` // the option chosen (or what was typed on the card)
	Error  string `json:"error,omitempty"`
	// Retry: the node is going away under the question (a hub or node restart); ask the next one with the same request.
	Retry bool `json:"retry,omitempty"`
}

// NotifyRequest is `personad notify`'s notification.
type NotifyRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Silent bool   `json:"silent,omitempty"` // no sound
	Error  bool   `json:"error,omitempty"`  // time-sensitive, marked ⚠️
}

// NotifyReply is the node's answer to a notification: Sent is the hub having taken it (not the phone showing it).
type NotifyReply struct {
	Sent   bool   `json:"sent"`
	Reason string `json:"reason,omitempty"` // why not, when the hub did not send it (muted)
	Error  string `json:"error,omitempty"`  // why the hub could not be asked
}

// scriptSession is the session that runs in the caller's pane and its title; "" when no hook has told the node.
func (n *Node) scriptSession(pane, cwd string) (id, title string) {
	if pane != "" {
		n.mu.Lock()
		id = n.panes[pane]
		n.mu.Unlock()
	}
	if id != "" {
		if p := n.transcriptPath(id); p != "" {
			if st, err := os.Stat(p); err == nil {
				title = n.meta(p, st).title
			}
		}
	}
	if title == "" && cwd != "" {
		title = filepath.Base(cwd)
	}
	if title == "" {
		title = scriptAgentLabel
	}
	return id, title
}

// scriptInteractionID is the same for every attempt of one ask, so one that reconnects after a restart finds its card.
func scriptInteractionID(attempt string) string {
	if attempt == "" {
		return "ix_" + newUUID()[:12]
	}
	sum := sha256.Sum256([]byte("script-ask\x00" + attempt))
	return "ix_" + hex.EncodeToString(sum[:])[:16]
}

// handleAsk puts the question in front of the phone and waits.
func (n *Node) handleAsk(ctx context.Context, req sockRequest) AskReply {
	var a AskRequest
	if err := json.Unmarshal(req.Input, &a); err != nil {
		return AskReply{Status: AskInvalid, Error: "bad request"}
	}
	if err := a.Normalize(); err != nil {
		return AskReply{Status: AskInvalid, Error: err.Error()}
	}
	if n.peer == nil {
		return AskReply{Status: AskUnavailable, Error: "this node is not connected to a hub"}
	}
	sid, title := n.scriptSession(req.Pane, a.Cwd)
	now := time.Now()
	ix := proto.Interaction{
		ID: scriptInteractionID(req.Attempt), SessionID: sid, SessionTitle: title, Cwd: a.Cwd,
		CreatedAt: now.UTC(), ExpiresAt: now.Add(a.Timeout()).UTC(), ToolName: scriptAskTool,
		Resumable: req.Attempt != "", Agent: scriptAgent, AgentLabel: scriptAgentLabel,
	}
	if len(a.Options) > 0 {
		ix.Kind = proto.IxQuestion
		q := proto.Question{Question: a.Question, Header: scriptAgentLabel}
		for _, o := range a.Options {
			q.Options = append(q.Options, proto.QuestionOption{Label: o})
		}
		ix.Questions = []proto.Question{q}
	} else {
		ix.Kind, ix.Summary = proto.IxPermission, a.Question
	}
	res, ok, err := n.ask(ctx, ix)
	switch {
	case errors.Is(err, errNodeGone):
		return AskReply{Retry: true}
	case err != nil:
		return AskReply{Status: AskUnavailable, Error: err.Error()}
	case !ok && !time.Now().Before(ix.ExpiresAt.Add(-500*time.Millisecond)):
		// The hub's timer and ours end at the same moment: n.ask cannot tell which one it was.
		return AskReply{Status: AskTimeout}
	case !ok && res.Dismiss:
		return AskReply{Status: AskDismissed}
	case !ok:
		return AskReply{Status: AskUnavailable, Error: "the hub did not take the question"}
	}
	if ix.Kind == proto.IxPermission {
		switch {
		case res.Allow == nil:
			return AskReply{Status: AskDismissed}
		case *res.Allow:
			return AskReply{Status: AskAnswered}
		}
		return AskReply{Status: AskDenied}
	}
	answer := strings.TrimSpace(res.Answers[a.Question])
	if answer == "" && len(res.Answers) == 1 {
		for _, v := range res.Answers {
			answer = strings.TrimSpace(v)
		}
	}
	if answer == "" {
		return AskReply{Status: AskDismissed}
	}
	return AskReply{Status: AskAnswered, Answer: answer}
}

// cutRunes cuts s to at most n characters, ending in … when it was cut.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// handleNotify has the hub alert the user's devices (hub "notify" request, the path POST /api/v1/notify takes).
func (n *Node) handleNotify(ctx context.Context, req sockRequest) NotifyReply {
	var nr NotifyRequest
	if err := json.Unmarshal(req.Input, &nr); err != nil {
		return NotifyReply{Error: "bad request"}
	}
	nr.Title, nr.Body = strings.TrimSpace(nr.Title), strings.TrimSpace(nr.Body)
	if nr.Title == "" && nr.Body == "" {
		return NotifyReply{Error: "the notification is empty"}
	}
	if n.peer == nil {
		return NotifyReply{Error: "this node is not connected to a hub"}
	}
	level := "active"
	if nr.Error {
		level = "time-sensitive"
		nr.Title = "⚠️ " + nr.Title
	}
	params := map[string]any{
		"title": cutRunes(strings.TrimSpace(nr.Title), notifyTitleMost), "body": cutRunes(nr.Body, notifyBodyMost),
		"level": level, "silent": nr.Silent,
	}
	if sid, _ := n.scriptSession(req.Pane, ""); sid != "" {
		params["session_id"] = sid // a tap opens that session; a muted session gets nothing
	}
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	var res struct {
		Sent   bool   `json:"sent"`
		Reason string `json:"reason"`
	}
	if err := n.peer.Call(ctx, "notify", params, &res); err != nil {
		return NotifyReply{Error: err.Error()}
	}
	return NotifyReply{Sent: res.Sent, Reason: res.Reason}
}

// Dialer sends one request line to the node's socket and returns its answer line (DialSocket).
type Dialer func(ctx context.Context, req any) ([]byte, error)

// AskNode asks the node to put a question in front of the phone and returns the answer. Like Hook, when the node goes
// away while the phone is deciding it reconnects to the next one with the same request (dialResumable). The error is
// ErrNoNode when nothing listens on the socket, else a transport failure; both mean the phone could not be asked.
func AskNode(ctx context.Context, dial Dialer, a AskRequest, pane string) (AskReply, error) {
	in, _ := json.Marshal(a)
	line, err := dialResumable(ctx, dial, sockRequest{Kind: "ask", Input: in, Pane: pane, Attempt: newUUID()})
	if err != nil {
		return AskReply{}, err
	}
	var rep AskReply
	_ = json.Unmarshal(line, &rep)
	return rep, nil
}

// NotifyNode asks the node to send a notification. The error is ErrNoNode when nothing listens on the socket, else a
// transport failure; a failure on the node's or the hub's side is NotifyReply.Error.
func NotifyNode(ctx context.Context, dial Dialer, nr NotifyRequest, pane string) (NotifyReply, error) {
	in, _ := json.Marshal(nr)
	line, err := dial(ctx, sockRequest{Kind: "notify", Input: in, Pane: pane})
	if err != nil {
		return NotifyReply{}, err
	}
	var rep NotifyReply
	if err := json.Unmarshal(line, &rep); err != nil {
		return NotifyReply{}, fmt.Errorf("bad answer from the Persona node: %w", err)
	}
	return rep, nil
}
