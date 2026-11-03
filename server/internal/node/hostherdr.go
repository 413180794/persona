package node

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
)

// herdrHost is the herdr host: every method is one herdr client call (Close: the tab's pane count, then a close).
// This file and engine.go are the only places in the node that know the herdr client.
type herdrHost struct{ c *herdr.Client }

// herdrHosts is a registry holding only the herdr host on c.
func herdrHosts(c *herdr.Client) *host.Registry { return host.NewRegistry(&herdrHost{c: c}) }

var (
	_ host.Host       = (*herdrHost)(nil)
	_ host.Watcher    = (*herdrHost)(nil)
	_ host.Launcher   = (*herdrHost)(nil)
	_ host.Organizer  = (*herdrHost)(nil)
	_ host.Streamer   = (*herdrHost)(nil)
	_ host.Controller = (*herdrHost)(nil)
)

// herdrPaneEnv is the variable herdr sets in every pane.
const herdrPaneEnv = "HERDR_PANE_ID"

func (h *herdrHost) Kind() string    { return host.Herdr }
func (h *herdrHost) PaneEnv() string { return herdrPaneEnv }

func (h *herdrHost) Version(ctx context.Context) string { return h.c.Version(ctx) }

func (h *herdrHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	snap, err := h.c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	l := &host.Layout{Agents: fromHerdrAgents(snap.Agents), Panes: fromHerdrAgents(snap.Panes)}
	for _, w := range snap.Workspaces {
		l.Spaces = append(l.Spaces, fromHerdrWorkspace(w))
	}
	for _, t := range snap.Tabs {
		l.Slots = append(l.Slots, host.Slot{ID: t.ID, SpaceID: t.WorkspaceID, Label: t.Label, Number: t.Number})
	}
	return l, nil
}

func (h *herdrHost) Agents(ctx context.Context) ([]host.Agent, error) {
	agents, err := h.c.Agents(ctx)
	return fromHerdrAgents(agents), err
}

func (h *herdrHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	a, err := h.c.AgentGet(ctx, pane.ID)
	if err != nil {
		return nil, err
	}
	out := fromHerdrAgent(*a)
	return &out, nil
}

func (h *herdrHost) Read(ctx context.Context, pane host.PaneRef, o host.ReadOpts) (string, error) {
	if o.ANSI {
		return h.c.PaneReadANSI(ctx, pane.ID, o.Source, o.Lines)
	}
	return h.c.PaneRead(ctx, pane.ID, o.Source, o.Lines)
}

func (h *herdrHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	return h.c.PaneSendText(ctx, pane.ID, text)
}

func (h *herdrHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	return h.c.PaneSendKeys(ctx, pane.ID, keys...)
}

// Prompt is agent.prompt; herdr answers *_not_found for a pane whose agent it does not detect (TRAEX).
func (h *herdrHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	err := h.c.AgentPrompt(ctx, pane.ID, text)
	if herdr.IsNotFound(err) {
		return host.Mark(err, host.ErrNoAgent)
	}
	if herdr.IsBlocked(err) {
		return host.Mark(err, host.ErrAgentBlocked)
	}
	return err
}

func (h *herdrHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	return h.c.PaneProcesses(ctx, pane.ID)
}

func (h *herdrHost) ReportMetadata(ctx context.Context, pane host.PaneRef, source string, tokens map[string]*string) error {
	return h.c.PaneReportMetadata(ctx, pane.ID, source, tokens)
}

// Watch is one events.subscribe: the lifecycle events plus agent status for each pane (herdr only sends those per
// pane). A status change into or out of blocked is urgent.
func (h *herdrHost) Watch(ctx context.Context, panes []host.PaneRef, changed func(urgent bool)) error {
	ids := make([]string, len(panes))
	for i, p := range panes {
		ids[i] = p.ID
	}
	var w waits
	return h.c.Subscribe(ctx, subscriptions(ids), func(e herdr.Event) {
		var d struct {
			PaneID      string `json:"pane_id"`
			AgentStatus string `json:"agent_status"`
		}
		status := e.Event == "pane.agent_status_changed" || e.Event == "pane_agent_status_changed"
		changed(status && json.Unmarshal(e.Data, &d) == nil && w.changed(d.PaneID, d.AgentStatus == "blocked"))
	})
}

// herdrLifecycle are the events that change which sessions there are or how they are named.
var herdrLifecycle = []string{
	"workspace.created", "workspace.renamed", "workspace.closed", "workspace.moved",
	"tab.created", "tab.renamed", "tab.closed", "tab.moved",
	"pane.created", "pane.closed", "pane.exited", "pane.updated", "pane.moved", "pane.agent_detected",
}

// subscriptions: the lifecycle events plus agent status for each pane (herdr only sends those per pane).
func subscriptions(panes []string) []herdr.Subscription {
	subs := make([]herdr.Subscription, 0, len(herdrLifecycle)+len(panes))
	for _, t := range herdrLifecycle {
		subs = append(subs, herdr.Subscription{"type": t})
	}
	for _, p := range panes {
		subs = append(subs, herdr.Subscription{"type": "pane.agent_status_changed", "pane_id": p})
	}
	return subs
}

func (h *herdrHost) Spaces(ctx context.Context) ([]host.Space, error) {
	wss, err := h.c.Workspaces(ctx)
	var out []host.Space
	for _, w := range wss {
		out = append(out, fromHerdrWorkspace(w))
	}
	return out, err
}

func (h *herdrHost) CreateSpace(ctx context.Context, cwd, label string) (*host.Created, error) {
	c, err := h.c.WorkspaceCreate(ctx, cwd, label)
	return fromHerdrCreated(c), err
}

func (h *herdrHost) CreateSlot(ctx context.Context, space, cwd, label string) (*host.Created, error) {
	c, err := h.c.TabCreate(ctx, space, cwd, label)
	return fromHerdrCreated(c), err
}

func (h *herdrHost) Run(ctx context.Context, pane host.PaneRef, command string) error {
	return h.c.PaneRun(ctx, pane.ID, command)
}

func (h *herdrHost) NameAgent(ctx context.Context, pane host.PaneRef, name string) error {
	return h.c.AgentRename(ctx, pane.ID, name)
}

func (h *herdrHost) RenameSpace(ctx context.Context, space, name string) error {
	return h.c.WorkspaceRename(ctx, space, name)
}

func (h *herdrHost) RenameSlot(ctx context.Context, slot, name string) error {
	return h.c.TabRename(ctx, slot, name)
}

// Close closes the pane when its tab is split, else the whole tab (also when the panes cannot be counted).
func (h *herdrHost) Close(ctx context.Context, pane host.PaneRef, space, slot string) error {
	if panes, err := h.c.TabPanes(ctx, space, slot); err == nil && panes > 1 {
		return h.c.PaneClose(ctx, pane.ID)
	}
	return h.c.TabClose(ctx, slot)
}

// observeSupport, controlSupport cache whether this herdr has `terminal session observe` / `control` (herdr 0.9 on
// Unix).
var observeSupport, controlSupport struct {
	once sync.Once
	ok   bool
}

func (h *herdrHost) has(cache *struct {
	once sync.Once
	ok   bool
}, sub string) bool {
	cache.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cache.ok = exec.CommandContext(ctx, h.c.Bin, "terminal", "session", sub, "--help").Run() == nil
	})
	return cache.ok
}

func (h *herdrHost) CanObserve() bool { return h.has(&observeSupport, "observe") }
func (h *herdrHost) CanControl() bool { return h.has(&controlSupport, "control") }

func (h *herdrHost) PaneSize(ctx context.Context, pane host.PaneRef) (int, int, error) {
	return h.c.PaneSize(ctx, pane.ID)
}

func (h *herdrHost) Observe(ctx context.Context, pane host.PaneRef, cols, rows int, fn func(host.Frame)) error {
	return h.c.Observe(ctx, pane.ID, cols, rows, func(f herdr.Frame) { fn(host.Frame(f)) })
}

func (h *herdrHost) Control(ctx context.Context, pane host.PaneRef, cols, rows int, takeover bool, fn func(host.Frame)) (host.Control, error) {
	c, err := h.c.StartControl(ctx, pane.ID, cols, rows, takeover, func(f herdr.Frame) { fn(host.Frame(f)) })
	if err != nil {
		if errors.Is(err, herdr.ErrAttachBusy) {
			err = host.Mark(err, host.ErrAttachBusy)
		}
		return nil, err
	}
	return herdrControl{c}, nil
}

// herdrControl is herdr's control with its errors marked for the host package.
type herdrControl struct{ *herdr.Control }

func (c herdrControl) Err() error {
	err := c.Control.Err()
	if errors.Is(err, herdr.ErrTakenOver) {
		return host.Mark(err, host.ErrTakenOver)
	}
	return err
}

func fromHerdrWorkspace(w herdr.Workspace) host.Space {
	return host.Space{ID: w.ID, Label: w.Label, Number: w.Number}
}

func fromHerdrCreated(c *herdr.Created) *host.Created {
	if c == nil {
		return nil
	}
	out := &host.Created{SlotID: c.Tab.ID, Pane: host.PaneRef{Kind: host.Herdr, ID: c.RootPane.ID}}
	if c.Workspace != nil {
		out.SpaceID = c.Workspace.ID
	}
	return out
}

func fromHerdrAgents(agents []herdr.Agent) []host.Agent {
	if agents == nil {
		return nil
	}
	out := make([]host.Agent, len(agents))
	for i, a := range agents {
		out[i] = fromHerdrAgent(a)
	}
	return out
}

// fromHerdrAgent: herdr's pane ids are herdr PaneRefs as they are (no prefix).
func fromHerdrAgent(a herdr.Agent) host.Agent {
	out := host.Agent{Agent: a.Agent, Name: a.Name, AgentStatus: a.AgentStatus, Cwd: a.Cwd, ForegroundCwd: a.ForegroundCwd,
		PaneID: a.PaneID, TabID: a.TabID, WorkspaceID: a.WorkspaceID, Title: a.Title, RawTitle: a.RawTitle, Tokens: a.Tokens}
	if a.AgentSession != nil {
		out.AgentSession = &host.AgentSession{Kind: a.AgentSession.Kind, Value: a.AgentSession.Value}
	}
	return out
}
