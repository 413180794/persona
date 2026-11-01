package orcart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Terminal is one terminal of terminal.list (the fields Jarvis reads).
type Terminal struct {
	// Handle is the runtime's id for the terminal ("term_<uuid>"), what terminal.read / send / close take.
	Handle string `json:"handle"`
	// TabID and LeafID place it: a tab of a worktree, split into leaves (panes). PaneKey joins them.
	TabID        string `json:"tabId"`
	LeafID       string `json:"leafId"`
	WorktreeID   string `json:"worktreeId"`
	WorktreePath string `json:"worktreePath"`
	Branch       string `json:"branch"`
	Title        string `json:"title"`
	Connected    bool   `json:"connected"`
	Writable     bool   `json:"writable"`
	Orphaned     bool   `json:"orphaned"`
	// ExecutionHostID is where the terminal runs: "local" for this machine (others are SSH targets).
	ExecutionHostID string `json:"executionHostId"`
	// AgentIdentity is the agent Orca sees in the terminal ("claude", "codex"…), "" for none.
	AgentIdentity string `json:"agentIdentity"`
	LastOutputAt  int64  `json:"lastOutputAt"`
}

// PaneKey is the terminal's pane as Orca names it in worktree.ps and in the processes it runs (ORCA_PANE_KEY).
func (t Terminal) PaneKey() string { return t.TabID + ":" + t.LeafID }

// Local: the terminal runs on this machine.
func (t Terminal) Local() bool { return t.ExecutionHostID == "" || t.ExecutionHostID == "local" }

// Tab is one tab as Orca's layout shows it.
type Tab struct {
	ID         string
	Title      string
	WorktreeID string
}

// TerminalList is terminal.list: the terminals, and the tabs of each worktree's layout in order.
type TerminalList struct {
	Terminals []Terminal
	Tabs      []Tab
}

// Terminals is terminal.list.
func (c *Client) Terminals(ctx context.Context) (*TerminalList, error) {
	var res struct {
		Terminals     []Terminal `json:"terminals"`
		VisualLayouts []struct {
			WorktreeID string          `json:"worktreeId"`
			Root       json.RawMessage `json:"root"`
		} `json:"visualLayouts"`
	}
	params := map[string]any{"includeVisualLayouts": true, "includePreview": false}
	if err := c.Call(ctx, ListTimeout, "terminal.list", params, &res); err != nil {
		return nil, err
	}
	out := &TerminalList{Terminals: res.Terminals}
	for _, l := range res.VisualLayouts {
		var root any
		if json.Unmarshal(l.Root, &root) == nil {
			collectTabs(root, l.WorktreeID, &out.Tabs)
		}
	}
	return out, nil
}

// collectTabs walks a layout tree (groups split into groups, each with tabs) for its tabs, in order.
func collectTabs(v any, worktree string, out *[]Tab) {
	switch v := v.(type) {
	case map[string]any:
		if tabs, ok := v["tabs"].([]any); ok {
			for _, t := range tabs {
				if m, ok := t.(map[string]any); ok {
					id, _ := m["tabId"].(string)
					title, _ := m["title"].(string)
					if id != "" {
						*out = append(*out, Tab{ID: id, Title: title, WorktreeID: worktree})
					}
				}
			}
		}
		for k, c := range v {
			if k != "tabs" {
				collectTabs(c, worktree, out)
			}
		}
	case []any:
		for _, c := range v {
			collectTabs(c, worktree, out)
		}
	}
}

// Worktree is one worktree of worktree.ps.
type Worktree struct {
	WorktreeID  string `json:"worktreeId"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	DisplayName string `json:"displayName"`
	Repo        string `json:"repo"`
	// Agents are the agents Orca's own hooks reported in the worktree's panes.
	Agents []AgentRow `json:"agents"`
}

// AgentRow is one agent of worktree.ps: its pane, kind and state from Orca's hooks.
type AgentRow struct {
	PaneKey   string `json:"paneKey"`
	AgentType string `json:"agentType"`
	// State: working | blocked | waiting | done, folding in the agent's children (a settled agent with live subagents
	// reads working); MainAgent.State is the agent's own, when Orca has it.
	State     string `json:"state"`
	MainAgent *struct {
		State string `json:"state"`
	} `json:"mainAgent"`
	HasLivePty bool  `json:"hasLivePty"`
	UpdatedAt  int64 `json:"updatedAt"`
}

// OwnState is the pane's agent's own state: its main agent's when known, else the row's (Orca's mainAgentState).
func (a AgentRow) OwnState() string {
	if a.MainAgent != nil && a.MainAgent.State != "" {
		return a.MainAgent.State
	}
	return a.State
}

// Worktrees is worktree.ps.
func (c *Client) Worktrees(ctx context.Context) ([]Worktree, error) {
	var res struct {
		Worktrees []Worktree `json:"worktrees"`
	}
	err := c.Call(ctx, ListTimeout, "worktree.ps", nil, &res)
	return res.Worktrees, err
}

// Screen is terminal.read with screen: the visible screen as plain text.
type Screen struct {
	Lines []string
	// Wrapped[i]: line i continues on the next one (a soft wrap).
	Wrapped []bool
	// Source is what was read ("screen"; "stream" is the output's tail, not the screen).
	Source string
}

// Text is the screen as text, soft-wrapped rows joined, one line per row, ending in a newline.
func (s Screen) Text() string {
	var b strings.Builder
	for i, l := range s.Lines {
		b.WriteString(l)
		if i < len(s.Wrapped) && s.Wrapped[i] && i+1 < len(s.Lines) {
			continue
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ErrNoScreen: Orca has no screen for the terminal right now (terminal.read's source "screen-unavailable").
var ErrNoScreen = errors.New("orca has no screen for the terminal")

// Read is terminal.read {screen: true}: the terminal's visible screen, plain text.
func (c *Client) Read(ctx context.Context, handle string) (*Screen, error) {
	var res struct {
		Terminal struct {
			Tail     []string `json:"tail"`
			TailRows []struct {
				Wrapped bool `json:"wrapped"`
			} `json:"tailRows"`
			Source string `json:"source"`
		} `json:"terminal"`
	}
	if err := c.Call(ctx, CallTimeout, "terminal.read", map[string]any{"terminal": handle, "screen": true}, &res); err != nil {
		return nil, err
	}
	if res.Terminal.Source == "screen-unavailable" {
		return nil, fmt.Errorf("orca terminal.read: %w", ErrNoScreen)
	}
	s := &Screen{Lines: res.Terminal.Tail, Source: res.Terminal.Source}
	for _, r := range res.Terminal.TailRows {
		s.Wrapped = append(s.Wrapped, r.Wrapped)
	}
	return s, nil
}

// desktop is the client Jarvis says it is in terminal.send: input from the user at their desktop (the only client
// type Orca pastes agent prompts for).
var desktop = map[string]any{"type": "desktop"}

// ErrRefused: Orca accepted the request but wrote nothing (terminal.send's accepted false); Reason says why.
var ErrRefused = errors.New("orca refused the input")

// Refusal is a terminal.send Orca turned down with nothing written: Reason "no-agent" (no prompt-ready agent in
// front) or "permission" (the agent waits on a permission dialog).
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "orca: input refused: " + r.Reason }
func (r *Refusal) Unwrap() error { return ErrRefused }

// send is terminal.send; a refusal (nothing written) is a *Refusal.
func (c *Client) send(ctx context.Context, params map[string]any) error {
	params["client"] = desktop
	var res struct {
		Send struct {
			Accepted      *bool  `json:"accepted"`
			BytesWritten  int    `json:"bytesWritten"`
			RefusedReason string `json:"refusedReason"`
		} `json:"send"`
	}
	if err := c.Call(ctx, SendTimeout, "terminal.send", params, &res); err != nil {
		return err
	}
	if a := res.Send.Accepted; a != nil && !*a {
		reason := res.Send.RefusedReason
		if reason == "" {
			reason = "not accepted"
		}
		return &Refusal{Reason: reason}
	}
	return nil
}

// Send is terminal.send of raw text, without enter: written to the terminal as is (key bytes too), no paste.
func (c *Client) Send(ctx context.Context, handle, text string) error {
	return c.send(ctx, map[string]any{"terminal": handle, "text": text})
}

// Prompt is terminal.send of text and enter as an agent prompt: Orca pastes it (bracketed) into the agent it
// recognises in front and submits it.
func (c *Client) Prompt(ctx context.Context, handle, text string) error {
	return c.send(ctx, map[string]any{"terminal": handle, "text": text, "enter": true, "agentPrompt": true})
}

// Close is terminal.close: Orca closes the terminal's pane (its tab when it was the only one).
func (c *Client) Close(ctx context.Context, handle string) error {
	return c.Call(ctx, CloseTimeout, "terminal.close", map[string]any{"terminal": handle}, nil)
}

// Probe is status.get without the compatibility gate: the runtime's status (nil when it did not answer) and whether
// this client can use it (ErrIncompatible says why not).
func (c *Client) Probe(ctx context.Context) (*Status, error) {
	var st Status
	err := c.call(ctx, CallTimeout, "status.get", nil, &st, false)
	if !gaveUp(ctx) {
		c.note("status.get", err)
	}
	if err != nil {
		return nil, err
	}
	return &st, st.compatible()
}

// Version is the running Orca's app version, "" when it does not answer or speaks a protocol this client does not.
func (c *Client) Version(ctx context.Context) string {
	st, err := c.Probe(ctx)
	if err != nil {
		return ""
	}
	return st.AppVersion
}
