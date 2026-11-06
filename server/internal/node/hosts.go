package node

import (
	"context"
	"fmt"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// The node drives every terminal through the host its pane belongs to (internal/host). Panes are kept as PaneRef
// strings everywhere in the node (n.panes, agents, the hub's database): a bare id is a herdr pane.

// paneHost is the host of a stored pane and the pane's ref in it.
func (n *Node) paneHost(pane string) (host.Host, host.PaneRef, error) {
	h, ref := n.hosts.Pane(pane)
	if h == nil {
		return nil, ref, fmt.Errorf("no %s host on this machine", ref.Kind)
	}
	return h, ref, nil
}

// paneCap is the pane's host as capability C (Launcher, Organizer…), with the pane's ref.
func paneCap[C any](n *Node, pane string) (C, host.PaneRef, error) {
	var none C
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return none, ref, err
	}
	c, ok := h.(C)
	if !ok {
		return none, ref, unsupported(h)
	}
	return c, ref, nil
}

// readPane reads a pane's screen as plain text.
func (n *Node) readPane(ctx context.Context, pane, source string, lines int) (string, error) {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return "", err
	}
	return h.Read(ctx, ref, host.ReadOpts{Source: source, Lines: lines})
}

// readPaneANSI reads a pane's screen with its colors.
func (n *Node) readPaneANSI(ctx context.Context, pane, source string, lines int) (string, error) {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return "", err
	}
	return h.Read(ctx, ref, host.ReadOpts{Source: source, Lines: lines, ANSI: true})
}

// plainScreen: the pane's host reads screens as plain text only (host.PlainScreen), so colored reads fail there.
func (n *Node) plainScreen(pane string) bool {
	h, _ := n.hosts.Pane(pane)
	p, ok := h.(host.PlainScreen)
	return ok && p.PlainScreen()
}

// canTypeText: the pane's host can type text without submitting it (host.TextTyper).
func (n *Node) canTypeText(pane string) bool {
	h, _ := n.hosts.Pane(pane)
	t, ok := h.(host.TextTyper)
	return !ok || t.CanTypeText()
}

func (n *Node) sendText(ctx context.Context, pane, text string) error {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return err
	}
	return h.SendText(ctx, ref, text)
}

func (n *Node) sendKeys(ctx context.Context, pane string, keys ...string) error {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return err
	}
	return h.SendKeys(ctx, ref, keys...)
}

// hostPrompt is the host's own prompt (no fallback; prompt types when the host knows no agent there).
func (n *Node) hostPrompt(ctx context.Context, pane, text string) error {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return err
	}
	return h.Prompt(ctx, ref, text)
}

// defaultHost is where new sessions start and where spaces without a host kind live: the set's default host (#2576;
// host kinds are engine names), classic herdr for herdr + Herdr App, its full-capability host. nil for the empty set,
// and for a default that is no terminal (Paseo).
func (n *Node) defaultHost() host.Host {
	kind := n.defaultKind()
	if kind == "" {
		return nil
	}
	return n.hosts.Get(kind)
}

// defaultEngineName names the host pane-less operations run on, for user-facing messages: herdr + Herdr App starts and
// resumes on classic herdr, so it says herdr, not "herdr + Herdr App".
func (n *Node) defaultEngineName() string {
	if name := proto.EngineNames[n.defaultKind()]; name != "" {
		return name
	}
	return n.engine()
}

// hostCap is the default host as capability C; with no host enabled, noEngineRefusal.
func hostCap[C any](n *Node) (C, error) {
	var none C
	kind := n.defaultKind()
	if kind == "" {
		return none, noEngineRefusal()
	}
	h := n.hosts.Get(kind)
	if h == nil {
		return none, fmt.Errorf("no %s host on this machine", kind)
	}
	c, ok := h.(C)
	if !ok {
		return none, unsupported(h)
	}
	return c, nil
}

func (n *Node) paneProcesses(ctx context.Context, pane string) ([]int, error) {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return nil, err
	}
	return h.PaneProcesses(ctx, ref)
}

// paneAgent is the host's view of the agent in a pane.
func (n *Node) paneAgent(ctx context.Context, pane string) (*host.Agent, error) {
	h, ref, err := n.paneHost(pane)
	if err != nil {
		return nil, err
	}
	return h.Agent(ctx, ref)
}

// renameSlot renames a slot through the launcher's host (a slot it just made).
func renameSlot(ctx context.Context, launch host.Launcher, slot, name string) error {
	org, ok := launch.(host.Organizer)
	if !ok {
		return fmt.Errorf("%w: cannot rename %s", host.ErrUnsupported, slot)
	}
	return org.RenameSlot(ctx, slot, name)
}

// unsupported: the host lacks the capability asked for ("unsupported: …", errors.Is host.ErrUnsupported).
func unsupported(h host.Host) error {
	return fmt.Errorf("%w: %s cannot do this", host.ErrUnsupported, h.Kind())
}
