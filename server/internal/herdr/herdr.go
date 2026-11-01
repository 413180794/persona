// Package herdr drives herdr's terminals and agents: over its unix socket API (one JSON line each way), or the
// herdr CLI when the socket is not there (older herdr, another session) — the CLI is a thin client of the same API.
package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	Bin string
	// Socket is herdr's API socket; empty: the CLI only.
	Socket string
	// Log gets one line when calls to herdr start failing (at most every 30 s while they do, with how many failed
	// meanwhile) and one when they work again; nil: silent.
	Log func(format string, args ...any)

	seq   atomic.Int64
	trips episode
}

// episode throttles logging of transport failures (herdr down, socket gone): not one line per poll.
type episode struct {
	mu       sync.Mutex
	failing  bool
	since    time.Time
	logged   time.Time
	failures int
}

func (c *Client) note(method string, err error) {
	if c.Log == nil {
		return
	}
	e := &c.trips
	e.mu.Lock()
	defer e.mu.Unlock()
	var he *Error
	if err == nil || errors.As(err, &he) { // an answer from herdr, even an error one, means it is reachable
		if e.failing {
			c.Log("herdr: reachable again after %s (%d failed calls)", time.Since(e.since).Round(time.Second), e.failures)
		}
		e.failing, e.failures = false, 0
		return
	}
	e.failures++
	if !e.failing {
		e.failing, e.since = true, time.Now()
	}
	if time.Since(e.logged) >= 30*time.Second {
		c.Log("herdr: %s failed: %v (%d failed calls since %s)", method, err, e.failures, e.since.Format("15:04:05"))
		e.logged = time.Now()
	}
}

func New() *Client { return &Client{Bin: "herdr", Socket: SocketPath()} }

// SocketPath is where herdr listens: $HERDR_SOCKET_PATH, else herdr.sock in its config directory.
func SocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "herdr", "herdr.sock")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock")
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

const (
	rpcTimeout = 10 * time.Second
	// maxLine bounds one response line: a whole snapshot or a 1000-line scrollback read is a few MB at most.
	maxLine = 64 << 20
)

// errNoSocket: herdr's socket is not reachable, the CLI may still be (it can find a server we cannot).
var errNoSocket = errors.New("herdr socket unavailable")

// call runs one API method and decodes its result into out. cli is the same call as herdr CLI arguments, used when
// the socket is not reachable or does not know the method (an older herdr); nil means there is no CLI equivalent.
func (c *Client) call(ctx context.Context, method string, params any, out any, cli ...string) error {
	err := c.rpc(ctx, method, params, out)
	if err == nil || len(cli) == 0 || !fallback(err) {
		return err
	}
	return c.run(ctx, out, cli...)
}

// fallback: errors the CLI might not have (no socket here, a method this herdr does not know).
func fallback(err error) bool {
	var he *Error
	if errors.As(err, &he) {
		return he.Code == "invalid_request" && strings.Contains(he.Message, "unknown variant")
	}
	return errors.Is(err, errNoSocket)
}

func (c *Client) rpc(ctx context.Context, method string, params any, out any) error {
	err := c.rpcOnce(ctx, method, params, out)
	if ctx.Err() == nil { // a caller giving up is not herdr failing
		c.note(method, err)
	}
	return err
}

func (c *Client) rpcOnce(ctx context.Context, method string, params any, out any) error {
	if c.Socket == "" {
		return errNoSocket
	}
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return fmt.Errorf("%w: %v", errNoSocket, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) }) // cancelled callers do not wait
	defer stop()
	if params == nil {
		params = struct{}{}
	}
	id := fmt.Sprintf("jarvis:%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	line, err := readLine(bufio.NewReaderSize(conn, 64<<10), maxLine)
	if err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	return decodeResponse(method, line, out)
}

func decodeResponse(method string, line []byte, out any) error {
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return fmt.Errorf("herdr %s: decode: %v", method, err)
	}
	if env.Error != nil {
		return env.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// readLine reads one newline-terminated line of at most max bytes.
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > max {
			return nil, fmt.Errorf("response line over %d bytes", max)
		}
		switch {
		case err == nil:
			return bytes.TrimRight(buf, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case len(buf) > 0 && errors.Is(err, io.EOF):
			return buf, nil
		default:
			return nil, err
		}
	}
}

// run executes herdr with args and decodes `.result` into out.
func (c *Client) run(ctx context.Context, out any, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		var env struct {
			Error *Error `json:"error"`
		}
		if json.Unmarshal(stderr.Bytes(), &env) == nil && env.Error != nil {
			return env.Error
		}
		return fmt.Errorf("herdr %s: %v: %s", strings.Join(args[:min(2, len(args))], " "), err, strings.TrimSpace(stderr.String()))
	}
	if out == nil {
		return nil
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		return fmt.Errorf("herdr %s: decode: %v", args[0], err)
	}
	return json.Unmarshal(env.Result, out)
}

func IsNotFound(err error) bool {
	var he *Error
	return errors.As(err, &he) && strings.HasSuffix(he.Code, "not_found")
}

// IsBlocked: herdr refused an agent prompt because it takes the agent to be waiting on the user (agent_blocked).
func IsBlocked(err error) bool {
	var he *Error
	return errors.As(err, &he) && he.Code == "agent_blocked"
}

// AgentSession is what an agent integration reported about its session ({kind: "id", value: <session id>}).
type AgentSession struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Agent struct {
	Agent         string        `json:"agent"`
	AgentSession  *AgentSession `json:"agent_session"`
	Name          string        `json:"name"`
	AgentStatus   string        `json:"agent_status"`
	Cwd           string        `json:"cwd"`
	ForegroundCwd string        `json:"foreground_cwd"`
	PaneID        string        `json:"pane_id"`
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	Title         string        `json:"terminal_title_stripped"`
	// RawTitle keeps what the stripped title drops, e.g. the spinner a busy TUI puts in front ("⠸ fix the build").
	RawTitle string `json:"terminal_title"`
}

// PaneProcesses returns the pids of a pane's foreground processes, claude first.
func (c *Client) PaneProcesses(ctx context.Context, pane string) ([]int, error) {
	var out struct {
		ProcessInfo struct {
			Foreground []struct {
				PID   int    `json:"pid"`
				Name  string `json:"name"`
				Argv0 string `json:"argv0"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	// pane_id, never target: given a target herdr answers for the focused pane.
	if err := c.call(ctx, "pane.process_info", map[string]any{"pane_id": pane}, &out, "pane", "process-info", "--pane", pane); err != nil {
		return nil, err
	}
	var claude, other []int
	for _, p := range out.ProcessInfo.Foreground {
		// Claude Code renames its process to its version ("2.1.312"); argv0 still says claude.
		if strings.Contains(p.Name, "claude") || strings.Contains(p.Argv0, "claude") {
			claude = append(claude, p.PID)
		} else {
			other = append(other, p.PID)
		}
	}
	return append(claude, other...), nil
}

func (a Agent) SessionID() string {
	if a.AgentSession != nil && a.AgentSession.Kind == "id" {
		return a.AgentSession.Value
	}
	return ""
}

func (c *Client) Version(ctx context.Context) string {
	var pong struct {
		Version string `json:"version"`
	}
	if c.rpc(ctx, "ping", nil, &pong) == nil && pong.Version != "" {
		return pong.Version
	}
	out, err := exec.CommandContext(ctx, c.Bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "herdr"))
}

func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	var res struct {
		Agents []Agent `json:"agents"`
	}
	err := c.call(ctx, "agent.list", nil, &res, "agent", "list")
	return res.Agents, err
}

func (c *Client) AgentGet(ctx context.Context, target string) (*Agent, error) {
	var res struct {
		Agent Agent `json:"agent"`
	}
	if err := c.call(ctx, "agent.get", map[string]any{"target": target}, &res, "agent", "get", target); err != nil {
		return nil, err
	}
	return &res.Agent, nil
}

type Workspace struct {
	ID     string `json:"workspace_id"`
	Label  string `json:"label"`
	Number int    `json:"number"`
}

type Tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
}

// Snapshot is the whole live layout in one call: workspaces, tabs and agents.
type Snapshot struct {
	Workspaces []Workspace `json:"workspaces"`
	Tabs       []Tab       `json:"tabs"`
	Agents     []Agent     `json:"agents"`
	// Panes is every pane, agent or not (agent empty): where agents herdr does not detect, such as TRAEX, are found.
	Panes []Agent `json:"panes"`
}

func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	var res struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.call(ctx, "session.snapshot", nil, &res, "api", "snapshot"); err != nil {
		return nil, err
	}
	return &res.Snapshot, nil
}

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var res struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.call(ctx, "workspace.list", nil, &res, "workspace", "list")
	return res.Workspaces, err
}

type Created struct {
	Workspace *Workspace `json:"workspace"`
	Tab       struct {
		ID string `json:"tab_id"`
	} `json:"tab"`
	RootPane struct {
		ID string `json:"pane_id"`
	} `json:"root_pane"`
}

func (c *Client) WorkspaceCreate(ctx context.Context, cwd, label string) (*Created, error) {
	var res Created
	err := c.call(ctx, "workspace.create", map[string]any{"cwd": cwd, "label": label, "focus": false}, &res,
		"workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	return &res, err
}

func (c *Client) TabCreate(ctx context.Context, workspace, cwd, label string) (*Created, error) {
	var res Created
	err := c.call(ctx, "tab.create", map[string]any{"workspace_id": workspace, "cwd": cwd, "label": label, "focus": false}, &res,
		"tab", "create", "--workspace", workspace, "--cwd", cwd, "--label", label, "--no-focus")
	return &res, err
}

func (c *Client) TabRename(ctx context.Context, tab, label string) error {
	return c.call(ctx, "tab.rename", map[string]any{"tab_id": tab, "label": label}, nil, "tab", "rename", tab, label)
}

func (c *Client) WorkspaceRename(ctx context.Context, workspace, label string) error {
	return c.call(ctx, "workspace.rename", map[string]any{"workspace_id": workspace, "label": label}, nil,
		"workspace", "rename", workspace, label)
}

func (c *Client) TabClose(ctx context.Context, tab string) error {
	return c.call(ctx, "tab.close", map[string]any{"tab_id": tab}, nil, "tab", "close", tab)
}

func (c *Client) PaneClose(ctx context.Context, pane string) error {
	return c.call(ctx, "pane.close", map[string]any{"pane_id": pane}, nil, "pane", "close", pane)
}

// TabPanes counts the panes in a tab (a split tab holds more than the agent's own).
func (c *Client) TabPanes(ctx context.Context, workspace, tab string) (int, error) {
	var res struct {
		Panes []struct {
			TabID string `json:"tab_id"`
		} `json:"panes"`
	}
	if err := c.call(ctx, "pane.list", map[string]any{"workspace_id": workspace}, &res, "pane", "list", "--workspace", workspace); err != nil {
		return 0, err
	}
	n := 0
	for _, p := range res.Panes {
		if p.TabID == tab {
			n++
		}
	}
	return n, nil
}

// PaneRun types command and presses enter.
func (c *Client) PaneRun(ctx context.Context, pane, command string) error {
	return c.call(ctx, "pane.send_input", map[string]any{"pane_id": pane, "text": command, "keys": []string{"Enter"}}, nil,
		"pane", "run", pane, command)
}

// PaneRead reads a pane as plain text. source is visible, recent, recent-unwrapped (the CLI's spelling) or detection.
func (c *Client) PaneRead(ctx context.Context, pane, source string, lines int) (string, error) {
	var res struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	params := map[string]any{"pane_id": pane, "source": strings.ReplaceAll(source, "-", "_"), "lines": lines}
	err := c.rpc(ctx, "pane.read", params, &res)
	if err == nil || !fallback(err) {
		return res.Read.Text, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Bin, "pane", "read", pane, "--source", source, "--lines", fmt.Sprint(lines)).Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane read: %w", err)
	}
	return string(out), nil
}

// PaneReadANSI reads a pane with its colors and styles (SGR escapes kept): what tells grey text from typed text.
func (c *Client) PaneReadANSI(ctx context.Context, pane, source string, lines int) (string, error) {
	var res struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	params := map[string]any{"pane_id": pane, "source": strings.ReplaceAll(source, "-", "_"), "lines": lines,
		"format": "ansi", "strip_ansi": false}
	err := c.rpc(ctx, "pane.read", params, &res)
	if err == nil || !fallback(err) {
		return res.Read.Text, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Bin, "pane", "read", pane, "--source", source, "--lines", fmt.Sprint(lines), "--format", "ansi").Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane read: %w", err)
	}
	return string(out), nil
}

func (c *Client) PaneSendText(ctx context.Context, pane, text string) error {
	return c.call(ctx, "pane.send_text", map[string]any{"pane_id": pane, "text": text}, nil, "pane", "send-text", pane, text)
}

func (c *Client) PaneSendKeys(ctx context.Context, pane string, keys ...string) error {
	return c.call(ctx, "pane.send_keys", map[string]any{"pane_id": pane, "keys": keys}, nil, append([]string{"pane", "send-keys", pane}, keys...)...)
}

func (c *Client) AgentPrompt(ctx context.Context, target, text string) error {
	return c.call(ctx, "agent.prompt", map[string]any{"target": target, "text": text}, nil, "agent", "prompt", target, text)
}

func (c *Client) AgentRename(ctx context.Context, target, name string) error {
	return c.call(ctx, "agent.rename", map[string]any{"target": target, "name": name}, nil, "agent", "rename", target, name)
}

// Subscription is one entry of events.subscribe: {"type": "pane.created"}, or per pane
// {"type": "pane.agent_status_changed", "pane_id": "w1:p2"}.
type Subscription map[string]string

// SubscriptionStarted is the event Subscribe reports first, when herdr has accepted the subscriptions.
const SubscriptionStarted = "subscription_started"

// Event is one pushed event: its kind and payload. herdr names kinds with underscores ("pane_agent_status_changed")
// though it takes subscriptions with dots ("pane.agent_status_changed").
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// Subscribe keeps one events.subscribe stream open, calling fn for each event, until ctx ends or herdr closes the
// stream (it drops subscribers that fall behind), and returns why it stopped. The first event is
// SubscriptionStarted, once herdr accepted the subscriptions. A connection takes one subscribe
// call — a second is silently ignored — so a changed set of subscriptions needs a new call.
func (c *Client) Subscribe(ctx context.Context, subs []Subscription, fn func(Event)) error {
	if c.Socket == "" {
		return errNoSocket
	}
	dctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	conn, err := (&net.Dialer{}).DialContext(dctx, "unix", c.Socket)
	cancel()
	if err != nil {
		return fmt.Errorf("%w: %v", errNoSocket, err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	req, err := json.Marshal(map[string]any{"id": "jarvis:events", "method": "events.subscribe",
		"params": map[string]any{"subscriptions": subs}})
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(rpcTimeout))
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 64<<10)
	line, err := readLine(r, maxLine)
	if err != nil {
		return fmt.Errorf("herdr events.subscribe: %w", err)
	}
	if err := decodeResponse("events.subscribe", line, nil); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	fn(Event{Event: SubscriptionStarted})
	for {
		line, err := readLine(r, maxLine)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("herdr events: %w", err)
		}
		var ev struct {
			Event
			Error *Error `json:"error"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Error != nil { // events_lost: herdr gave up on us
			return ev.Error
		}
		if ev.Event.Event != "" {
			fn(ev.Event)
		}
	}
}
