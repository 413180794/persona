package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/orcart"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// orcaHost is the Orca desktop app (docs/plan-orca-support.md「Orca 来源」), through its runtime's unix socket. A pane
// is one of Orca's terminals, its id Orca's pane key "<tabId>:<leafId>" — what worktree.ps names agents by and what
// Orca puts in the terminal's processes as ORCA_PANE_KEY; the terminal's handle (what the runtime's terminal.* calls
// take) is looked up from it. Spaces are worktrees, slots their tabs.
//
// Phase 1: listing, state (Orca's own hooks, polled), plain-text reads, typing, keys, prompts, process → session,
// closing. No colors, no history, no live stream, no starting or renaming. The socket pushes nothing: no Watcher, the
// node polls every second.
type orcaHost struct {
	c *orcart.Client
	// scan lists the agent processes running in Orca's terminals, by pane key (scanOrcaProcs).
	scan func() (map[string][]orcaProc, error)

	mu       sync.Mutex
	handles  map[string]string // pane key -> terminal handle, from the last terminal.list
	procs    map[string][]orcaProc
	procsErr error
	procsAt  time.Time
	failedAt time.Time // when the last terminal.list failed (failErr), for Agents right after a failed Snapshot
	failErr  error
}

var (
	_ host.Host        = (*orcaHost)(nil)
	_ host.PlainScreen = (*orcaHost)(nil)
	_ host.Organizer   = (*orcaHost)(nil)
)

func newOrcaHost(c *orcart.Client) *orcaHost { return &orcaHost{c: c, scan: orcaScan} }

// orcaScan is how a new Orca host lists agent processes (tests swap in a fake process table).
var orcaScan = scanOrcaProcs

// Orca's pane variables in the processes of its terminals: the pane key, and the older name it falls back on.
const (
	orcaPaneEnv       = "ORCA_PANE_KEY"
	orcaLegacyPaneEnv = "ORCA_AGENT_PANE"
)

// orcaProcsMaxAge: how long one scan of the processes serves the listing (it polls every second; a scan is one ps).
const orcaProcsMaxAge = 1500 * time.Millisecond

func (h *orcaHost) Kind() string      { return host.Orca }
func (h *orcaHost) PaneEnv() string   { return orcaPaneEnv }
func (h *orcaHost) PlainScreen() bool { return true }

func (h *orcaHost) Version(ctx context.Context) string { return h.c.Version(ctx) }

// orcaErr marks Orca not running as host.ErrNotRunning: it then has no terminals.
func orcaErr(err error) error {
	if errors.Is(err, orcart.ErrNotRunning) {
		return host.Mark(err, host.ErrNotRunning)
	}
	return err
}

// terminals are this machine's terminals in Orca (it also lists terminals on SSH targets, which are not here), with
// the layout's tabs. A list that failed a moment ago fails again at once: the node asks for Agents right after a failed
// Snapshot, and a wedged Orca must not cost it twice.
func (h *orcaHost) terminals(ctx context.Context) (*orcart.TerminalList, error) {
	h.mu.Lock()
	if h.failErr != nil && time.Since(h.failedAt) < time.Second {
		err := h.failErr
		h.mu.Unlock()
		return nil, err
	}
	h.mu.Unlock()
	l, err := h.c.Terminals(ctx)
	if err != nil {
		err = orcaErr(err)
		h.mu.Lock()
		h.failedAt, h.failErr = time.Now(), err
		h.mu.Unlock()
		return nil, err
	}
	terms := l.Terminals[:0]
	handles := map[string]string{}
	for _, t := range l.Terminals {
		if t.Local() && !t.Orphaned && t.TabID != "" && t.LeafID != "" {
			terms = append(terms, t)
			handles[t.PaneKey()] = t.Handle
		}
	}
	l.Terminals = terms
	h.mu.Lock()
	h.failErr, h.handles = nil, handles
	h.mu.Unlock()
	return l, nil
}

// view is Orca's state at one moment: its terminals and the agents its hooks report, by pane key.
type orcaView struct {
	list  *orcart.TerminalList
	trees []orcart.Worktree
	rows  map[string]orcart.AgentRow
	procs map[string][]orcaProc
	// procsKnown: the process scan worked (else the agent rows and Orca's agentIdentity are all there is).
	procsKnown bool
}

func (h *orcaHost) look(ctx context.Context, procsAge time.Duration) (*orcaView, error) {
	l, err := h.terminals(ctx)
	if err != nil {
		return nil, err
	}
	v := &orcaView{list: l, rows: map[string]orcart.AgentRow{}}
	// Without worktree.ps the panes are still listed, their states unknown.
	if trees, err := h.c.Worktrees(ctx); err == nil {
		v.trees = trees
		for _, w := range trees {
			for _, a := range w.Agents {
				v.rows[a.PaneKey] = a
			}
		}
	}
	procs, err := h.agentProcs(procsAge)
	v.procs, v.procsKnown = procs, err == nil
	return v, nil
}

func (h *orcaHost) Snapshot(ctx context.Context) (*host.Layout, error) {
	v, err := h.look(ctx, orcaProcsMaxAge)
	if err != nil {
		return nil, err
	}
	l := &host.Layout{}
	spaces := map[string]int{} // worktree id -> index in l.Spaces
	addSpace := func(id, path, branch string) {
		if _, ok := spaces[id]; ok || id == "" {
			return
		}
		spaces[id] = len(l.Spaces)
		l.Spaces = append(l.Spaces, host.Space{ID: id, Label: orcaSpaceLabel(branch, path), Number: len(l.Spaces) + 1})
	}
	// Orca's sidebar order (worktree.ps), then any worktree only the terminals name.
	for _, w := range v.trees {
		if slices.ContainsFunc(v.list.Terminals, func(t orcart.Terminal) bool { return t.WorktreeID == w.WorktreeID }) {
			addSpace(w.WorktreeID, w.Path, w.Branch)
		}
	}
	for _, t := range v.list.Terminals {
		addSpace(t.WorktreeID, t.WorktreePath, t.Branch)
	}
	perSpace := map[string]int{}
	for _, tab := range v.list.Tabs {
		if _, ok := spaces[tab.WorktreeID]; !ok || slices.ContainsFunc(l.Slots, func(s host.Slot) bool { return s.ID == tab.ID }) {
			continue
		}
		perSpace[tab.WorktreeID]++
		label := stripSpinner(tab.Title)
		if label == "" {
			label = strconv.Itoa(perSpace[tab.WorktreeID])
		}
		l.Slots = append(l.Slots, host.Slot{ID: tab.ID, SpaceID: tab.WorktreeID, Label: label, Number: perSpace[tab.WorktreeID]})
	}
	for _, t := range v.list.Terminals {
		a := v.agent(t)
		l.Panes = append(l.Panes, a)
		if a.Agent != "" {
			l.Agents = append(l.Agents, a)
		}
	}
	return l, nil
}

// orcaSpaceLabel names a worktree the way Orca's sidebar does: its branch, else its folder.
func orcaSpaceLabel(branch, path string) string {
	if b := strings.TrimPrefix(branch, "refs/heads/"); b != "" {
		return b
	}
	return firstOf(filepath.Base(path))
}

func (h *orcaHost) Agents(ctx context.Context) ([]host.Agent, error) {
	v, err := h.look(ctx, orcaProcsMaxAge)
	if err != nil {
		return nil, err
	}
	var out []host.Agent
	for _, t := range v.list.Terminals {
		if a := v.agent(t); a.Agent != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

func (h *orcaHost) Agent(ctx context.Context, pane host.PaneRef) (*host.Agent, error) {
	v, err := h.look(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, t := range v.list.Terminals {
		if t.PaneKey() == pane.ID {
			a := v.agent(t)
			return &a, nil
		}
	}
	return nil, fmt.Errorf("%w %s", errNoOrcaTerminal, pane.ID)
}

// orcaAgents names the agents Orca reports (agentType, agentIdentity) the way the node does.
var orcaAgents = map[string]string{"claude": "claude", "codex": "codex"}

// orcaStatus maps Orca's agent states to the node's: waiting is Orca's word for an agent waiting on the user, as
// blocked is.
var orcaStatus = map[string]string{
	"working": proto.StatusWorking, "blocked": proto.StatusBlocked, "waiting": proto.StatusBlocked, "done": proto.StatusDone,
}

// agent is a terminal as an agent pane. Its agent is the claude or codex process running in it (by its ORCA_PANE_KEY):
// Orca keeps an agent's row (and its identity on the terminal) after the agent exits, so neither alone says one runs
// there. Only when the processes could not be listed do the row and the identity decide. Its state is the row's main
// agent's own (a claude whose turn ended while its background subagents run is done, as herdr would say), when the row
// is of that agent.
func (v *orcaView) agent(t orcart.Terminal) host.Agent {
	key := t.PaneKey()
	a := host.Agent{
		PaneID: host.PaneRef{Kind: host.Orca, ID: key}.String(), WorkspaceID: t.WorktreeID, TabID: t.TabID,
		Cwd: t.WorktreePath, Title: stripSpinner(t.Title), RawTitle: t.Title,
	}
	row, hasRow := v.rows[key]
	if v.procsKnown {
		if ps := v.procs[key]; len(ps) > 0 {
			a.Agent = ps[0].agent
			a.ForegroundCwd = ps[0].cwd
		}
	} else if k := orcaAgents[t.AgentIdentity]; k != "" && hasRow && row.HasLivePty && orcaAgents[row.AgentType] == k {
		a.Agent = k
	}
	if a.Agent == "" {
		return a
	}
	a.AgentStatus = proto.StatusUnknown
	if hasRow && orcaAgents[row.AgentType] == a.Agent {
		if s := orcaStatus[row.OwnState()]; s != "" {
			a.AgentStatus = s
		}
	}
	return a
}

// handle is the runtime's handle of a pane's terminal: from the last listing, else from a new one.
func (h *orcaHost) handle(ctx context.Context, paneKey string, fresh bool) (string, error) {
	if !fresh {
		h.mu.Lock()
		hd, ok := h.handles[paneKey]
		h.mu.Unlock()
		if ok {
			return hd, nil
		}
	}
	if _, err := h.terminals(ctx); err != nil {
		return "", err
	}
	h.mu.Lock()
	hd, ok := h.handles[paneKey]
	h.mu.Unlock()
	if !ok {
		return "", host.Mark(fmt.Errorf("%w %s", errNoOrcaTerminal, paneKey), host.ErrNotDelivered)
	}
	return hd, nil
}

// errNoOrcaTerminal: Orca lists no terminal with that pane key (closed, or Orca restarted).
var errNoOrcaTerminal = errors.New("orca: no terminal")

// onTerminal runs fn with the pane's terminal handle; a handle Orca no longer knows (the terminal restarted under a
// new one) is looked up again once.
func (h *orcaHost) onTerminal(ctx context.Context, paneKey string, fn func(handle string) error) error {
	hd, err := h.handle(ctx, paneKey, false)
	if err != nil {
		return err
	}
	err = fn(hd)
	if orcart.IsCode(err, "terminal_handle_stale") {
		if hd, err = h.handle(ctx, paneKey, true); err != nil {
			return err
		}
		err = fn(hd)
	}
	return err
}

// Read is terminal.read of the visible screen as plain text: Orca reads no colors here, and the screen read is the one
// that never switches source, so "recent" is the screen too. Lines cuts it to its last rows.
func (h *orcaHost) Read(ctx context.Context, pane host.PaneRef, o host.ReadOpts) (string, error) {
	if o.ANSI {
		return "", fmt.Errorf("%w: Orca reads screens as plain text only", host.ErrUnsupported)
	}
	var text string
	err := h.onTerminal(ctx, pane.ID, func(hd string) error {
		s, err := h.c.Read(ctx, hd)
		if err != nil {
			return err
		}
		text = s.Text()
		return nil
	})
	if err != nil {
		return "", orcaErr(err)
	}
	if o.Lines > 0 {
		if rows := strings.Split(strings.TrimRight(text, "\n"), "\n"); len(rows) > o.Lines {
			text = strings.Join(rows[len(rows)-o.Lines:], "\n") + "\n"
		}
	}
	return text, nil
}

// SendText types text as is (terminal.send without enter writes the bytes).
func (h *orcaHost) SendText(ctx context.Context, pane host.PaneRef, text string) error {
	return orcaInputErr(h.onTerminal(ctx, pane.ID, func(hd string) error { return h.c.Send(ctx, hd, text) }))
}

// orcaKeys are the bytes a terminal program gets for Jarvis's key names (Orca's terminal.send writes raw bytes).
var orcaKeys = map[string]string{
	"enter": "\r", "return": "\r", "esc": "\x1b", "escape": "\x1b", "tab": "\t", "shift+tab": "\x1b[Z", "backtab": "\x1b[Z",
	"backspace": "\x7f", "delete": "\x1b[3~", "space": " ",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~",
	"alt+enter": "\x1b\r", "shift+enter": "\x1b\r", "ctrl+j": "\n",
}

// orcaKeyBytes is a key name's bytes: a named key, ctrl+<letter> (its control byte), or a single printable character.
func orcaKeyBytes(k string) (string, bool) {
	if b, ok := orcaKeys[strings.ToLower(k)]; ok {
		return b, true
	}
	if l, ok := strings.CutPrefix(strings.ToLower(k), "ctrl+"); ok && len(l) == 1 && l[0] >= 'a' && l[0] <= 'z' {
		return string(rune(l[0] - 'a' + 1)), true
	}
	if r := []rune(k); len(r) == 1 && r[0] >= ' ' && r[0] != 0x7f {
		return k, true
	}
	return "", false
}

// orcaKeyGap: keys go one write each, a moment apart, so an escape is not read as the start of the next key's
// sequence (esc then shift+tab would arrive as alt+escape-sequence).
const orcaKeyGap = 40 * time.Millisecond

// SendKeys presses the keys one by one; a key with no bytes fails before anything is pressed.
func (h *orcaHost) SendKeys(ctx context.Context, pane host.PaneRef, keys ...string) error {
	seq := make([]string, 0, len(keys))
	for _, k := range keys {
		b, ok := orcaKeyBytes(k)
		if !ok {
			return host.Mark(fmt.Errorf("orca: unknown key %q", k), host.ErrNotDelivered)
		}
		seq = append(seq, b)
	}
	for i, b := range seq {
		if i > 0 {
			time.Sleep(orcaKeyGap)
		}
		err := h.onTerminal(ctx, pane.ID, func(hd string) error { return h.c.Send(ctx, hd, b) })
		if err != nil {
			if i > 0 {
				return orcaErr(err) // some keys were pressed: not safe to send again
			}
			return orcaInputErr(err)
		}
	}
	return nil
}

// Prompt is terminal.send as an agent prompt: Orca pastes the text into the agent and submits it. The pane's processes
// are looked at first: once the agent has exited, the shell under it would run the text as a command, so that is
// refused with nothing typed.
func (h *orcaHost) Prompt(ctx context.Context, pane host.PaneRef, text string) error {
	procs, err := h.agentProcs(0)
	if err == nil && len(procs[pane.ID]) == 0 {
		return host.Mark(fmt.Errorf("orca: no agent runs in %s any more", pane.ID), host.ErrNotDelivered)
	}
	return orcaInputErr(h.onTerminal(ctx, pane.ID, func(hd string) error { return h.c.Prompt(ctx, hd, text) }))
}

// orcaInputErr marks the refusals that typed nothing (host.ErrNotDelivered): Orca not running or incompatible, a
// terminal that is gone or not writable, a request it rejected before writing, a send it turned down (accepted false;
// "permission": the agent waits on a dialog, also host.ErrDialogOpen).
func orcaInputErr(err error) error {
	var r *orcart.Refusal
	switch {
	case err == nil:
		return nil
	case errors.Is(err, host.ErrNotDelivered):
		return err
	case errors.As(err, &r):
		if r.Reason == "permission" {
			return host.Mark(host.Mark(err, host.ErrNotDelivered), host.ErrDialogOpen)
		}
		return host.Mark(err, host.ErrNotDelivered)
	case errors.Is(err, orcart.ErrNotRunning), errors.Is(err, orcart.ErrIncompatible), orcart.Refused(err):
		return host.Mark(orcaErr(err), host.ErrNotDelivered)
	}
	return err
}

// errOrcaRename: Orca owns its worktrees' and tabs' names; Jarvis does not rename them.
var errOrcaRename = fmt.Errorf("%w: Orca 里的工作区和标签页暂时不能从手机改名", host.ErrUnsupported)

func (h *orcaHost) RenameSpace(context.Context, string, string) error { return errOrcaRename }
func (h *orcaHost) RenameSlot(context.Context, string, string) error  { return errOrcaRename }

// Close is terminal.close: Orca closes the pane (its tab when it was the only one). A terminal already gone counts as
// closed.
func (h *orcaHost) Close(ctx context.Context, pane host.PaneRef, _, _ string) error {
	err := h.onTerminal(ctx, pane.ID, func(hd string) error { return h.c.Close(ctx, hd) })
	if errors.Is(err, errNoOrcaTerminal) || orcart.IsCode(err, "terminal_handle_stale") {
		return nil // no terminal with that pane key any more
	}
	return orcaErr(err)
}

// PaneProcesses are the agent processes whose ORCA_PANE_KEY is the pane, claude first.
func (h *orcaHost) PaneProcesses(ctx context.Context, pane host.PaneRef) ([]int, error) {
	procs, err := h.agentProcs(orcaProcsMaxAge)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, p := range procs[pane.ID] {
		pids = append(pids, p.pid)
	}
	return pids, nil
}

// agentProcs is a scan of the agent processes no older than maxAge.
func (h *orcaHost) agentProcs(maxAge time.Duration) (map[string][]orcaProc, error) {
	h.mu.Lock()
	if h.procsAt.IsZero() || time.Since(h.procsAt) > maxAge {
		h.mu.Unlock()
		procs, err := h.scan()
		h.mu.Lock()
		h.procs, h.procsErr, h.procsAt = procs, err, time.Now()
	}
	defer h.mu.Unlock()
	return h.procs, h.procsErr
}

// orcaProc is an agent process in one of Orca's terminals.
type orcaProc struct {
	pid   int
	agent string // claude | codex
	cwd   string
}

var (
	orcaPaneRe       = paneEnvRe(orcaPaneEnv)
	orcaLegacyPaneRe = paneEnvRe(orcaLegacyPaneEnv)
)

// orcaPaneOf is the pane key in a process's environment listing ("" when none): ORCA_PANE_KEY, else
// ORCA_AGENT_PANE. Nothing else is taken from it (it also holds Orca's hook token).
func orcaPaneOf(env string) string {
	if m := orcaPaneRe.FindStringSubmatch(env); m != nil {
		return m[1]
	}
	if m := orcaLegacyPaneRe.FindStringSubmatch(env); m != nil {
		return m[1]
	}
	return ""
}

// agentOfCmd: the command line runs Claude Code or Codex.
func agentOfCmd(argv []string) string {
	if isClaudeCmd(argv) {
		return "claude"
	}
	if len(argv) == 0 {
		return ""
	}
	switch filepath.Base(argv[0]) {
	case "codex":
		return "codex"
	case "node", "bun":
		if len(argv) > 1 && filepath.Base(argv[1]) == "codex" || len(argv) > 1 && strings.Contains(argv[1], "/codex/") {
			return "codex"
		}
	}
	return ""
}

// scanOrcaProcs lists this user's claude and codex processes running in Orca's terminals, by pane key, claude first
// and lowest pid first within an agent (a wrapper before the program it starts). Linux reads /proc; macOS asks ps once.
func scanOrcaProcs() (map[string][]orcaProc, error) {
	out := map[string][]orcaProc{}
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			cmd, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
			if err != nil {
				continue
			}
			agent := agentOfCmd(strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00"))
			if agent == "" {
				continue
			}
			env, _ := os.ReadFile("/proc/" + e.Name() + "/environ")
			if pane := orcaPaneOf(string(env)); pane != "" {
				cwd, _ := os.Readlink("/proc/" + e.Name() + "/cwd")
				out[pane] = append(out[pane], orcaProc{pid: pid, agent: agent, cwd: cwd})
			}
		}
		sortOrcaProcs(out)
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "ps", "-E", "-ww", "-o", "pid=,command=", "-U", strconv.Itoa(os.Getuid())).Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parseOrcaPS(string(b)), nil
}

// parseOrcaPS reads `ps -E -o pid=,command=` output (the command line followed by the environment).
func parseOrcaPS(s string) map[string][]orcaProc {
	out := map[string][]orcaProc{}
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		agent := agentOfCmd(f[1:])
		if agent == "" {
			continue
		}
		if pane := orcaPaneOf(line); pane != "" {
			out[pane] = append(out[pane], orcaProc{pid: pid, agent: agent})
		}
	}
	sortOrcaProcs(out)
	return out
}

func sortOrcaProcs(m map[string][]orcaProc) {
	for _, ps := range m {
		slices.SortFunc(ps, func(a, b orcaProc) int {
			if (a.agent == "claude") != (b.agent == "claude") {
				if a.agent == "claude" {
					return -1
				}
				return 1
			}
			return a.pid - b.pid
		})
	}
}
