// Package host is what the node needs from a terminal host — where the terminals run (herdr, later Orca): its panes,
// reading their screens, typing into them, opening and closing them. What runs inside a pane (Claude Code, Codex,
// TRAEX) is the agent; telling agents apart, finding their sessions and judging their state stay in the node, on top
// of any host (docs/plan-orca-support.md).
//
// A host implements Host; everything else is an optional capability the node finds with a type assertion.
package host

import (
	"context"
	"errors"
)

// Host kinds. Herdr panes are stored and sent without a prefix (see PaneRef).
const (
	Herdr = "herdr"
	Orca  = "orca" // not implemented yet (docs/plan-orca-support.md)
)

// Kinds are the host kinds a stored pane may be prefixed with; the list is fixed, not what a machine registers, so
// a stored pane means the same everywhere.
var Kinds = []string{Herdr, Orca}

// Host is what every terminal host provides. Pane ids in what it returns (Agent.PaneID, Created.Pane) are PaneRefs
// of its own kind; the PaneRefs it is given are always of its kind.
type Host interface {
	// Kind names the host ("herdr"); it is the prefix of its PaneRefs.
	Kind() string
	// Version is the host's version, "" when unknown or not running.
	Version(ctx context.Context) string
	// Snapshot is the whole layout at once: spaces, slots, the panes running an agent the host detects, every pane.
	Snapshot(ctx context.Context) (*Layout, error)
	// Agents lists only the panes running an agent the host detects (a lighter call than Snapshot; herdr: hosts that
	// cannot snapshot).
	Agents(ctx context.Context) ([]Agent, error)
	// Agent is one pane as an agent pane (its agent and state, which the host may not know: Agent "").
	Agent(ctx context.Context, pane PaneRef) (*Agent, error)
	// Read reads a pane's screen.
	Read(ctx context.Context, pane PaneRef, opts ReadOpts) (string, error)
	// SendText types text into a pane as is (no enter).
	SendText(ctx context.Context, pane PaneRef, text string) error
	// SendKeys presses keys in a pane, by Jarvis's key names (herdr's: "enter", "esc", "shift+tab", "ctrl+u", "up"…);
	// each host translates them.
	SendKeys(ctx context.Context, pane PaneRef, keys ...string) error
	// Prompt submits text to the agent in a pane the way the host does it best (herdr: agent.prompt, a paste and a
	// submit). ErrNoAgent (errors.Is) when the host does not know an agent there: the caller types it instead.
	Prompt(ctx context.Context, pane PaneRef, text string) error
	// PaneProcesses are the pids of a pane's foreground processes, claude first.
	PaneProcesses(ctx context.Context, pane PaneRef) ([]int, error)
	// PaneEnv is the environment variable that names the pane in the processes running in it ("HERDR_PANE_ID"): its
	// value is the host's pane id (PaneRef.ID).
	PaneEnv() string
}

// Watcher pushes changes: the node takes a snapshot when one comes instead of polling every second.
type Watcher interface {
	// Watch calls changed for every change until ctx ends or the host ends the stream, and returns why it stopped.
	// panes are the agent panes whose state changes to report (a different set needs a new Watch); the first call of
	// changed means the stream is live (snapshot then, to cover the gap before it).
	Watch(ctx context.Context, panes []PaneRef, changed func()) error
}

// Launcher opens new panes running a command.
type Launcher interface {
	// Spaces lists the spaces (herdr: workspaces) new panes can go into.
	Spaces(ctx context.Context) ([]Space, error)
	// CreateSpace opens a new space in cwd, with one slot holding one pane.
	CreateSpace(ctx context.Context, cwd, label string) (*Created, error)
	// CreateSlot opens a new slot (herdr: tab) with one pane in a space.
	CreateSlot(ctx context.Context, space, cwd, label string) (*Created, error)
	// Run types command into a pane and presses enter.
	Run(ctx context.Context, pane PaneRef, command string) error
	// NameAgent names the agent running in a pane (shown by the host).
	NameAgent(ctx context.Context, pane PaneRef, name string) error
}

// Organizer renames and closes.
type Organizer interface {
	RenameSpace(ctx context.Context, space, name string) error
	RenameSlot(ctx context.Context, slot, name string) error
	// Close closes a pane in the given space / slot: the whole slot when the pane is its only one.
	Close(ctx context.Context, pane PaneRef, space, slot string) error
}

// Streamer streams a pane's terminal as it changes, at the pane's own size, without resizing it.
type Streamer interface {
	// CanObserve reports whether this host can stream (herdr: a version with `terminal session observe`).
	CanObserve() bool
	// PaneSize is the grid the pane's programs see.
	PaneSize(ctx context.Context, pane PaneRef) (cols, rows int, err error)
	// Observe streams frames to fn until ctx ends or the host stops, and returns why.
	Observe(ctx context.Context, pane PaneRef, cols, rows int, fn func(Frame)) error
}

// Controller lets a client drive a pane's terminal at its own size (input, resize, scroll, click).
type Controller interface {
	CanControl() bool
	// Control takes the pane's terminal at cols × rows, streaming frames to fn. ErrAttachBusy (errors.Is) when another
	// client holds it and takeover is false.
	Control(ctx context.Context, pane PaneRef, cols, rows int, takeover bool, fn func(Frame)) (Control, error)
}

// Control is a terminal being driven (Controller.Control).
type Control interface {
	Input(text string) error
	Resize(cols, rows int) error
	Scroll(lines int, up bool, column, row int) error
	Click(column, row int) error
	// Done closes when the control ended (Close, the host ending it); Err then says why (ErrTakenOver: someone took it).
	Done() <-chan struct{}
	Err() error
	Close()
}

// Errors a host marks its own errors with (errors.Is); the error text stays the host's.
var (
	// ErrNoAgent: Prompt found no agent the host knows in the pane.
	ErrNoAgent = errors.New("no agent in the pane")
	// ErrAttachBusy: another client holds the terminal (Controller.Control without takeover).
	ErrAttachBusy = errors.New("attach_busy")
	// ErrTakenOver: the terminal being driven was taken by another client.
	ErrTakenOver = errors.New("taken_over")
	// ErrUnsupported: the host cannot do this (a capability it lacks). Errors carrying it start with "unsupported",
	// the prefix the hub falls back on.
	ErrUnsupported = errors.New("unsupported")
)

// Mark makes err also match kind with errors.Is, keeping its text and what it already matches.
func Mark(err, kind error) error {
	if err == nil {
		return nil
	}
	return marked{err, kind}
}

type marked struct{ err, kind error }

func (m marked) Error() string   { return m.err.Error() }
func (m marked) Unwrap() []error { return []error{m.err, m.kind} }

// ReadOpts says what of a pane's screen to read.
type ReadOpts struct {
	// Source: "visible" (the screen), "recent" (back into the scrollback, wrapped as shown) or "recent-unwrapped".
	Source string
	// Lines: at most this many lines, from the end.
	Lines int
	// ANSI keeps colors and styles (SGR escapes; rows end in \r\n).
	ANSI bool
}

// AgentSession is what an agent integration in the host reported about the pane's session (Kind "id": Value is the
// session id).
type AgentSession struct {
	Kind  string
	Value string
}

// Agent is one pane as the node sees it: where it is, and the agent running in it with its state and session when the
// host (or the node) knows them.
type Agent struct {
	// Agent is the kind of agent: "claude", "codex", "traex"…; "" when nothing the host detects runs there.
	Agent        string
	AgentSession *AgentSession
	// Name is the agent's name in the host (NameAgent).
	Name string
	// AgentStatus is the host's state for the agent (proto.Status*), "" when it has none.
	AgentStatus   string
	Cwd           string
	ForegroundCwd string
	// PaneID is the pane as a PaneRef string (String): a herdr pane id as is.
	PaneID string
	// TabID, WorkspaceID: the host's slot and space ids.
	TabID       string
	WorkspaceID string
	// Title is the terminal title without escapes; RawTitle as set (with the spinner a busy TUI puts in front).
	Title    string
	RawTitle string
}

// SessionID is the session id the host reported, "" when none.
func (a Agent) SessionID() string {
	if a.AgentSession != nil && a.AgentSession.Kind == "id" {
		return a.AgentSession.Value
	}
	return ""
}

// Space is a group of slots (herdr: a workspace).
type Space struct {
	ID     string
	Label  string
	Number int
}

// Slot holds panes inside a space (herdr: a tab).
type Slot struct {
	ID      string
	SpaceID string
	Label   string
	Number  int
}

// Layout is a host's whole state at one moment.
type Layout struct {
	Spaces []Space
	Slots  []Slot
	// Agents are the panes running an agent the host detects.
	Agents []Agent
	// Panes is every pane, agent or not (Agent ""): where agents the host does not detect (TRAEX) are found.
	Panes []Agent
}

// Created is what a Launcher opened.
type Created struct {
	SpaceID string // set by CreateSpace
	SlotID  string
	Pane    PaneRef
}

// Frame is one frame of a terminal stream: the grid redrawn as VT sequences — the whole screen when Full, else only
// what changed. Data is base64.
type Frame struct {
	Seq    int64
	Full   bool
	Width  int
	Height int
	Data   string
}
