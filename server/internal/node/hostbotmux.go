package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// botmuxHost is botmux, the Feishu topic ↔ AI CLI bridge (docs/engines.md「botmux」): each topic is a session of a
// coding CLI that botmux runs (in tmux) and answers in Feishu. It is not an engine to choose: a node on a machine with
// botmux registers it next to the engine's hosts (nodeHosts). A pane is a botmux session, its id the botmux session
// id, which is also the Claude session id botmux starts claude with. Phase 1: listing and state (one `botmux
// observe` for every daemon's sessions) and replies, which go to botmux's own delivery (POST /api/trigger on its
// dashboard: queued while a turn runs, a parked session resumed by botmux) — never typed into the pane, never a
// `claude --resume` of Persona's. No screens, keys, closing or renaming: those stay in Feishu and on the computer.
type botmuxHost struct {
	// run runs the botmux CLI (tests: a fake).
	run func(ctx context.Context, args ...string) ([]byte, error)
	// client posts to the dashboard.
	client *http.Client

	mu     sync.Mutex
	at     time.Time // when rows were observed
	rows   []botmuxRow
	err    error
	base   string // the dashboard on loopback, "" until asked
	token  string // its token: an admin credential, kept in memory only and never logged
	logged bool   // a failed observe was logged since the last good one
	logf   func(format string, args ...any)
	dryRun bool // botmux renders the turn and dispatches nothing (the live test)
}

var (
	_ host.Host        = (*botmuxHost)(nil)
	_ host.PlainScreen = (*botmuxHost)(nil)
	_ host.TextTyper   = (*botmuxHost)(nil)
)

// botmuxPaneEnv is the variable botmux sets in the processes of a session: its id.
const botmuxPaneEnv = "BOTMUX_SESSION_ID"

// botmuxReuse: an observe answers every refresh for this long. botmux pushes nothing, so the node polls every second;
// one observe (all daemons at once, a Node.js CLI) took 0.3–0.9 s with ~500 sessions. A delivered message looks again
// at once.
const botmuxReuse = 5 * time.Second

// botmuxBin is the botmux CLI, "" when not installed (tests: "", so the hosts of an engine stay as they were).
var botmuxBin = findBotmux

// findBotmux: botmux on PATH, else the launcher botmux's installer writes, ~/.botmux/bin/botmux — the resident node
// (launchd) has only the system's PATH.
func findBotmux() string {
	if p, err := exec.LookPath("botmux"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".botmux", "bin", "botmux")
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// nodeHosts are the hosts a node drives: its engine's, and botmux's when botmux is installed and wanted (useBotmux).
func nodeHosts(engine string, botmux bool, logf func(format string, args ...any)) []host.Host {
	hs := engineHosts(engine, logf)
	if !botmux {
		return hs
	}
	if bin := botmuxBin(); bin != "" {
		hs = append(hs, newBotmux(bin, logf))
	}
	return hs
}

// newBotmux makes the botmux host (tests: one on a fake CLI).
var newBotmux = func(bin string, logf func(format string, args ...any)) host.Host { return newBotmuxHost(bin, logf) }

// useBotmux lists this machine's botmux sessions, or stops (hub.json machines[].botmux): the hub's word in
// node.configure, or on as before when the hub lists sessions without having said (fallback: a hub older than the
// setting). Until one of them, botmux is not a host, so a machine turned off never runs botmux at all, not even while
// the node starts (and no list goes to the hub meanwhile: botmuxPending). Off, botmux is no host: no new `botmux
// observe`, no `botmux dashboard current` (one observe under way finishes, its answer dropped), and its sessions are
// out of the next list pushed, within a second; on, they come with the next refresh. A fallback never overrides the
// hub's word.
func (n *Node) useBotmux(on, fallback bool) {
	n.eng.mu.Lock()
	if n.eng.botmux != nil && (fallback || *n.eng.botmux == on) {
		n.eng.mu.Unlock()
		return
	}
	n.eng.botmux = &on
	n.eng.botmuxSaid.Store(true)
	// The engine's hosts stay as they are (their watchers hold them); only botmux comes or goes.
	all := n.hosts.All()
	hs := slices.DeleteFunc(slices.Clone(all), func(h host.Host) bool { return h.Kind() == host.Botmux })
	removed := !on && len(hs) < len(all)
	switch bin := botmuxBin(); {
	case on && len(hs) < len(all): // registered already
	case on && bin != "":
		n.hosts.Set(append(hs, newBotmux(bin, n.logf))...)
	case removed:
		n.hosts.Set(hs...)
	}
	n.eng.mu.Unlock()
	if !removed {
		return
	}
	// Its sessions go now, not when the hosts next answer: a refresh nobody answers (herdr not running) keeps the
	// agents as they were. A refresh still asking botmux drops its answer (refreshHosts), so they do not come back.
	n.mu.Lock()
	n.engEpoch++
	delete(n.hostAgents, host.Botmux)
	for id, a := range n.agents {
		if host.ParsePane(a.PaneID).Kind == host.Botmux {
			delete(n.agents, id)
		}
	}
	n.mu.Unlock()
}

// botmuxPending: botmux is installed and the hub has not said yet whether its sessions are listed. No list goes to
// the hub until it has (publish): one without botmux's sessions would take them off the hub's list for the seconds
// before the word (node.info comes first), and their coming back would read as new turns. The hub lists the sessions
// right after its word.
func (n *Node) botmuxPending() bool {
	return !n.eng.botmuxSaid.Load() && botmuxBin() != ""
}

func newBotmuxHost(bin string, logf func(format string, args ...any)) *botmuxHost {
	return &botmuxHost{
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, bin, args...).Output()
		},
		// A redirect is the dashboard's login page: the token is stale (Prompt asks for the current one).
		client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		logf: logf,
	}
}

func (h *botmuxHost) Kind() string      { return host.Botmux }
func (h *botmuxHost) PaneEnv() string   { return botmuxPaneEnv }
func (h *botmuxHost) PlainScreen() bool { return true }
func (h *botmuxHost) CanTypeText() bool { return false }

func (h *botmuxHost) Version(context.Context) string { return "" }

// botmuxRow is one session of `botmux observe --include-raw` (schema 1): the canonical fields, and from the raw row
// the title and the attention botmux raised (`botmux send --attention`).
type botmuxRow struct {
	Identity struct {
		SessionID string `json:"sessionId"`
		LarkAppID string `json:"larkAppId"`
	} `json:"identity"`
	CLI struct {
		ID string `json:"id"`
	} `json:"cli"`
	Backend struct {
		Type string `json:"type"`
	} `json:"backend"`
	Liveness         string `json:"liveness"`
	Turn             string `json:"turn"`
	Closed           any    `json:"closed"` // true, false or "unknown"
	TUIPromptActive  bool   `json:"tuiPromptActive"`
	LastActivityAt   int64  `json:"lastActivityAt"` // epoch ms
	WorkingDirectory string `json:"workingDirectory"`
	Raw              struct {
		Title          string `json:"title"`
		AgentAttention *struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		} `json:"agentAttention"`
		// FeishuThreadLink opens the session's topic in Feishu (a raw field: shown when present).
		FeishuThreadLink string `json:"feishuThreadLink"`
	} `json:"raw"`
}

// observe is every daemon's sessions, reused for botmuxReuse. botmux not installed or no daemon running is
// host.ErrNotRunning: no sessions. Any other failure (a timeout, a CLI error) is not: the node keeps the sessions it
// listed last, so a reply in the gap does not take them for not running (and resume one outside botmux).
func (h *botmuxHost) observe(parent context.Context) ([]botmuxRow, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.at.IsZero() && time.Since(h.at) < botmuxReuse {
		return h.rows, h.err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second) // 0.3 s as a rule; seconds on a machine under load
	defer cancel()
	h.rows, h.err = nil, nil
	out, err := h.run(ctx, "observe", "--include-raw")
	var doc struct {
		SchemaVersion int `json:"schemaVersion"`
		Daemons       []struct {
			Probe struct {
				Status string `json:"status"`
			} `json:"probe"`
			Sessions []botmuxRow `json:"sessions"`
		} `json:"daemons"`
	}
	switch {
	case errors.Is(err, exec.ErrNotFound):
		h.err = host.Mark(fmt.Errorf("botmux observe: %w", err), host.ErrNotRunning)
	case err != nil:
		h.err = fmt.Errorf("botmux observe: %w", err)
	case json.Unmarshal(out, &doc) != nil || doc.SchemaVersion != 1:
		h.err = fmt.Errorf("botmux observe: unexpected output (schema %d)", doc.SchemaVersion)
	default:
		online := false
		for _, d := range doc.Daemons {
			if d.Probe.Status == "ok" {
				online = true
				h.rows = append(h.rows, d.Sessions...)
			}
		}
		if !online {
			h.err = host.Mark(errors.New("botmux: no daemon is running"), host.ErrNotRunning)
		}
	}
	h.at = time.Now()
	if parent.Err() != nil {
		h.at = time.Time{} // the caller gave up: says nothing of botmux
	}
	if h.err != nil && !h.logged && h.logf != nil {
		h.logf("botmux: %v", h.err)
	}
	h.logged = h.err != nil
	return h.rows, h.err
}

// botmuxTurns are botmux's turn states as the node's; the others (limited, stalled, interrupted: the turn ended
// without an answer) count as done, for the user to look at.
var botmuxTurns = map[string]string{
	"working": proto.StatusWorking, "starting": proto.StatusWorking, "analyzing": proto.StatusWorking,
	"idle": proto.StatusDone, "unknown": proto.StatusUnknown,
}

// agent is a session as an agent pane, false for the ones botmux does not drive here: closed ones, ones of a CLI
// other than Claude Code (phase 1), ones botmux runs in herdr (herdr's own panes already). A parked one is offline, so
// that a reply to it goes to botmux, which resumes it; Agents lists it only while its transcript is listed offline.
func (r botmuxRow) agent() (host.Agent, bool) {
	if r.CLI.ID != "claude-code" || r.Backend.Type == "herdr" || r.Closed != false || !validID(r.Identity.SessionID) {
		return host.Agent{}, false
	}
	a := host.Agent{
		Agent: "claude", AgentSession: &host.AgentSession{Kind: "id", Value: r.Identity.SessionID},
		Name: botmuxTitle(r.Raw.Title), Cwd: r.WorkingDirectory, WorkspaceID: dirSpace + r.WorkingDirectory,
		PaneID: host.PaneRef{Kind: host.Botmux, ID: r.Identity.SessionID}.String(),
	}
	switch {
	case r.Liveness == "not_running":
		a.AgentStatus = proto.StatusOffline
	case r.Liveness != "alive":
		return host.Agent{}, false
	case r.TUIPromptActive || (r.Raw.AgentAttention != nil && r.Raw.AgentAttention.Kind != ""):
		a.AgentStatus = proto.StatusBlocked
		if r.Raw.AgentAttention != nil {
			a.Attention = botmuxTitle(r.Raw.AgentAttention.Reason)
		}
	default:
		a.AgentStatus = botmuxTurns[r.Turn]
		if a.AgentStatus == "" {
			a.AgentStatus = proto.StatusDone
		}
	}
	a.Title = a.Name
	if strings.HasPrefix(r.Raw.FeishuThreadLink, "https://") {
		a.ExternalURL = r.Raw.FeishuThreadLink
	}
	return a, true
}

// botmuxTitle is a session's title on one line (botmux keeps the first words of the topic until it is renamed).
func botmuxTitle(s string) string { return strings.Join(strings.Fields(s), " ") }

func (h *botmuxHost) Agents(ctx context.Context) ([]host.Agent, error) {
	rows, err := h.observe(ctx)
	if err != nil {
		return nil, err
	}
	var out []host.Agent
	for _, r := range rows {
		a, ok := r.agent()
		if ok && (a.AgentStatus != proto.StatusOffline || time.Since(time.UnixMilli(r.LastActivityAt)) <= offlineWindow) {
			out = append(out, a)
		}
	}
	return out, nil
}

// Snapshot: a space per working directory (as herdr-core's terminals the Herdr app did not place), no slots.
func (h *botmuxHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	agents, err := h.Agents(ctx)
	if err != nil {
		return nil, err
	}
	l := &host.Layout{Agents: agents, Panes: agents}
	seen := map[string]bool{}
	for _, a := range agents {
		if !seen[a.WorkspaceID] {
			seen[a.WorkspaceID] = true
			l.Spaces = append(l.Spaces, host.Space{ID: a.WorkspaceID, Label: filepath.Base(a.Cwd), Number: len(l.Spaces) + 1})
		}
	}
	return l, nil
}

// row is a session's row by id, listed or parked for longer (paneFor finds it to reply to).
func (h *botmuxHost) row(ctx context.Context, id string) (botmuxRow, host.Agent, error) {
	rows, err := h.observe(ctx)
	if err != nil {
		return botmuxRow{}, host.Agent{}, err
	}
	for _, r := range rows {
		if r.Identity.SessionID == id {
			if a, ok := r.agent(); ok {
				return r, a, nil
			}
		}
	}
	return botmuxRow{}, host.Agent{}, fmt.Errorf("botmux: no session %s", id)
}

func (h *botmuxHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	_, a, err := h.row(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// errBotmuxScreen: botmux sessions are read through their transcripts; their terminals stay botmux's.
var errBotmuxScreen = fmt.Errorf("%w: botmux 会话的终端不能从手机读或按键，只能发消息", host.ErrUnsupported)

func (h *botmuxHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) {
	return "", errBotmuxScreen
}
func (h *botmuxHost) SendText(context.Context, host.PaneRef, string) error    { return errBotmuxScreen }
func (h *botmuxHost) SendKeys(context.Context, host.PaneRef, ...string) error { return errBotmuxScreen }

// PaneProcesses: none needed, the session id is botmux's (Agent.AgentSession).
func (h *botmuxHost) PaneProcesses(context.Context, host.PaneRef) ([]int, error) { return nil, nil }

// Prompt hands text to botmux as a turn of the session (POST /api/trigger, target kind turn with the session id,
// fire and forget): botmux queues it behind a running turn, resumes a parked session, and answers in the Feishu topic
// as for a message there. A refusal (4xx, or 503: the bot's daemon is not up) or a dashboard that cannot be reached
// delivered nothing; anything else may have.
func (h *botmuxHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	r, _, err := h.row(ctx, pane.ID)
	if err != nil {
		return host.Mark(err, host.ErrNotDelivered)
	}
	req := map[string]any{
		"source":      map[string]any{"type": "webhook", "connectorId": "persona"},
		"target":      map[string]any{"kind": "turn", "botId": r.Identity.LarkAppID, "sessionId": r.Identity.SessionID},
		"instruction": text,
		"envelope":    map[string]any{"format": "text", "sourceName": "Persona", "trusted": false},
	}
	if h.dryRun {
		req["options"] = map[string]any{"dryRun": true}
	}
	body, _ := json.Marshal(req)
	for attempt := 0; ; attempt++ {
		status, msg, err := h.trigger(ctx, body, attempt > 0)
		stale := err != nil || status == http.StatusUnauthorized || status == http.StatusForbidden ||
			(status >= 300 && status < 400)
		switch {
		case stale && attempt == 0:
			continue // the dashboard moved or its token was rotated: ask botmux for the current one
		case err != nil:
			return host.Mark(err, host.ErrNotDelivered)
		case status >= 200 && status < 300:
			h.mu.Lock()
			h.at = time.Time{} // its state changes now: look again at the next refresh
			h.mu.Unlock()
			return nil
		case status < 500 || status == http.StatusServiceUnavailable:
			return host.Mark(fmt.Errorf("botmux refused the message (%d): %s", status, msg), host.ErrNotDelivered)
		}
		return fmt.Errorf("botmux: delivering the message failed (%d): %s", status, msg)
	}
}

// trigger posts body to the dashboard's /api/trigger; fresh asks botmux for the dashboard again. err: nothing was
// delivered (no dashboard, no connection).
func (h *botmuxHost) trigger(ctx context.Context, body []byte, fresh bool) (int, string, error) {
	base, token, err := h.dashboard(ctx, fresh)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/trigger", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "botmux_dashboard_token", Value: token})
	resp, err := h.client.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return 0, "", fmt.Errorf("botmux: the dashboard does not answer: %w", err)
		}
		return http.StatusBadGateway, err.Error(), nil // it may have arrived
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var res struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(b, &res)
	msg := strings.TrimSpace(res.Error + " " + res.Message)
	if resp.StatusCode < 300 && !res.OK {
		return http.StatusBadGateway, msg, nil // accepted, then failed: it may have run
	}
	return resp.StatusCode, msg, nil
}

// dashboard is botmux's dashboard on loopback and its token, from `botmux dashboard current` (which makes the first
// token when there is none), kept until fresh.
func (h *botmuxHost) dashboard(ctx context.Context, fresh bool) (string, string, error) {
	h.mu.Lock()
	base, token := h.base, h.token
	h.mu.Unlock()
	if base != "" && !fresh {
		return base, token, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := h.run(ctx, "dashboard", "current")
	if err != nil {
		return "", "", fmt.Errorf("botmux dashboard current: %w", err)
	}
	for _, f := range strings.Fields(string(out)) {
		u, err := url.Parse(f)
		if err != nil || u.Scheme != "http" || u.Query().Get("t") == "" || u.Port() == "" {
			continue
		}
		base = "http://" + net.JoinHostPort("127.0.0.1", u.Port())
		token = u.Query().Get("t")
		h.mu.Lock()
		h.base, h.token = base, token
		h.mu.Unlock()
		return base, token, nil
	}
	return "", "", errors.New("botmux dashboard current: no dashboard URL in its output")
}
