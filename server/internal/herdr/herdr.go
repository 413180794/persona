// Package herdr wraps the herdr CLI (socket API) used to drive terminals and agents.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	Bin string
}

func New() *Client { return &Client{Bin: "herdr"} }

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

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
				PID  int    `json:"pid"`
				Name string `json:"name"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := c.run(ctx, &out, "pane", "process-info", "--pane", pane); err != nil {
		return nil, err
	}
	var claude, other []int
	for _, p := range out.ProcessInfo.Foreground {
		if strings.Contains(p.Name, "claude") {
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
	err := c.run(ctx, &res, "agent", "list")
	return res.Agents, err
}

func (c *Client) AgentGet(ctx context.Context, target string) (*Agent, error) {
	var res struct {
		Agent Agent `json:"agent"`
	}
	if err := c.run(ctx, &res, "agent", "get", target); err != nil {
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
	if err := c.run(ctx, &res, "api", "snapshot"); err != nil {
		return nil, err
	}
	return &res.Snapshot, nil
}

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var res struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.run(ctx, &res, "workspace", "list")
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
	err := c.run(ctx, &res, "workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	return &res, err
}

func (c *Client) TabCreate(ctx context.Context, workspace, cwd, label string) (*Created, error) {
	var res Created
	err := c.run(ctx, &res, "tab", "create", "--workspace", workspace, "--cwd", cwd, "--label", label, "--no-focus")
	return &res, err
}

func (c *Client) TabRename(ctx context.Context, tab, label string) error {
	return c.run(ctx, nil, "tab", "rename", tab, label)
}

func (c *Client) WorkspaceRename(ctx context.Context, workspace, label string) error {
	return c.run(ctx, nil, "workspace", "rename", workspace, label)
}

func (c *Client) TabClose(ctx context.Context, tab string) error {
	return c.run(ctx, nil, "tab", "close", tab)
}

func (c *Client) PaneClose(ctx context.Context, pane string) error {
	return c.run(ctx, nil, "pane", "close", pane)
}

// TabPanes counts the panes in a tab (a split tab holds more than the agent's own).
func (c *Client) TabPanes(ctx context.Context, workspace, tab string) (int, error) {
	var res struct {
		Panes []struct {
			TabID string `json:"tab_id"`
		} `json:"panes"`
	}
	if err := c.run(ctx, &res, "pane", "list", "--workspace", workspace); err != nil {
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

func (c *Client) PaneRun(ctx context.Context, pane, command string) error {
	return c.run(ctx, nil, "pane", "run", pane, command)
}

func (c *Client) PaneRead(ctx context.Context, pane, source string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Bin, "pane", "read", pane, "--source", source, "--lines", fmt.Sprint(lines)).Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane read: %w", err)
	}
	return string(out), nil
}

func (c *Client) PaneSendText(ctx context.Context, pane, text string) error {
	return c.run(ctx, nil, "pane", "send-text", pane, text)
}

func (c *Client) PaneSendKeys(ctx context.Context, pane string, keys ...string) error {
	return c.run(ctx, nil, append([]string{"pane", "send-keys", pane}, keys...)...)
}

func (c *Client) AgentPrompt(ctx context.Context, target, text string) error {
	return c.run(ctx, nil, "agent", "prompt", target, text)
}

func (c *Client) AgentRename(ctx context.Context, target, name string) error {
	return c.run(ctx, nil, "agent", "rename", target, name)
}
