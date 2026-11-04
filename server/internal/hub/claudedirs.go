package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Claude Code config directories per machine (node/claudedirs.go): the configured ones (hub.json
// machines[].claude_config_dirs) plus those the node found in running claudes, remembered here so a profile's
// sessions stay listed after the node restarts with none of its claudes running.

func claudeDirsKey(machine string) string { return "claude_dirs/" + machine }

// claudeDirsBlockedKey: the patterns of directories the user keeps out of Jarvis on that machine
// (proto.ClaudeDirBlocked): not looked in, not remembered, their sessions not listed.
func claudeDirsBlockedKey(machine string) string { return "claude_dirs_blocked/" + machine }

func (m *Machine) blockedClaudeDirs(ctx context.Context) []string {
	var out []string
	if m.hub.store != nil {
		_, _ = m.hub.store.Setting(ctx, claudeDirsBlockedKey(m.cfg.ID), &out)
	}
	return proto.CleanClaudeDirPatterns(out)
}

// nodeHome is the machine's home directory, for "~" in patterns ("" before the node first said).
func (m *Machine) nodeHome() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.info.Home
}

// claudeDirs: the directories the node is told to look in — configured, then remembered — without blocked ones.
func (m *Machine) claudeDirs(ctx context.Context) []string {
	dirs := slices.Clone(m.cfg.ClaudeConfigDirs)
	// Where Persona's own claudes keep their sessions is looked in too, even before one of them runs.
	if d := m.cfg.ClaudeStartDir; d != "" && !slices.Contains(dirs, d) {
		dirs = append(dirs, d)
	}
	if m.hub.store != nil {
		var seen []string
		if ok, err := m.hub.store.Setting(ctx, claudeDirsKey(m.cfg.ID), &seen); ok && err == nil {
			for _, d := range seen {
				if !slices.Contains(dirs, d) {
					dirs = append(dirs, d)
				}
			}
		}
	}
	blocked, home := m.blockedClaudeDirs(ctx), m.nodeHome()
	return slices.DeleteFunc(dirs, func(d string) bool { return proto.ClaudeDirBlocked(d, home, blocked) })
}

// botmux is hub.json's machines[].botmux: the machine's node lists botmux's sessions unless it says false.
func (m *Machine) botmux() bool { return m.cfg.Botmux == nil || *m.cfg.Botmux }

// configureNode hands the node its config directories and whether it lists botmux's sessions, before the first
// session list. Older nodes do not know node.configure; they keep looking in ~/.claude only.
func (m *Machine) configureNode(ctx context.Context, peer *rpc.Peer) {
	_ = peer.Call(ctx, "node.configure", map[string]any{"machine_id": m.cfg.ID, "codex": m.codexConfig(ctx),
		"claude_dirs": m.claudeDirs(ctx), "claude_dirs_blocked": m.blockedClaudeDirs(ctx),
		"keep_sessions": m.hub.keptSessions(ctx, m.cfg.ID), "botmux": m.botmux(), "claude_start_dir": m.cfg.ClaudeStartDir,
		"herdr_autostart": m.cfg.HerdrAutostart == nil || *m.cfg.HerdrAutostart}, nil)
	m.sendManaged(ctx, func(ctx context.Context, method string, params, out any) error {
		return peer.Call(ctx, method, params, out)
	})
	m.sendEchoes(ctx, peer)
}

// rememberClaudeDirs stores the directories the node found (claude.dirs event), added to what was known; blocked ones
// are not (an older node reports them all the same).
func (m *Machine) rememberClaudeDirs(params json.RawMessage) {
	var p struct {
		Dirs []string `json:"dirs"`
	}
	if json.Unmarshal(params, &p) != nil || m.hub.store == nil {
		return
	}
	ctx := context.Background()
	var seen []string
	_, _ = m.hub.store.Setting(ctx, claudeDirsKey(m.cfg.ID), &seen)
	blocked, home := m.blockedClaudeDirs(ctx), m.nodeHome()
	changed := false
	for _, d := range p.Dirs {
		if d != "" && !slices.Contains(seen, d) && !proto.ClaudeDirBlocked(d, home, blocked) {
			seen = append(seen, d)
			changed = true
		}
	}
	if changed {
		if err := m.hub.store.PutSetting(ctx, claudeDirsKey(m.cfg.ID), seen); err != nil {
			m.hub.logf("[%s] remember claude dirs: %v", m.cfg.ID, err)
		}
	}
}

var errBadPattern = errors.New("bad pattern")

// setBlockedClaudeDirs stores the machine's blocked patterns, forgets the remembered directories they block, and
// tells the node: it drops them at once and sends the session list without their sessions.
func (m *Machine) setBlockedClaudeDirs(ctx context.Context, patterns []string) error {
	if m.hub.store == nil {
		return errors.New("no database")
	}
	patterns = proto.CleanClaudeDirPatterns(patterns)
	for _, p := range patterns {
		if !proto.ValidClaudeDirPattern(p) {
			return fmt.Errorf("%w %q: a path or glob starting with / or ~/", errBadPattern, p)
		}
	}
	if err := m.hub.store.PutSetting(ctx, claudeDirsBlockedKey(m.cfg.ID), patterns); err != nil {
		return err
	}
	var seen []string
	if ok, _ := m.hub.store.Setting(ctx, claudeDirsKey(m.cfg.ID), &seen); ok {
		home := m.nodeHome()
		kept := slices.DeleteFunc(slices.Clone(seen), func(d string) bool { return proto.ClaudeDirBlocked(d, home, patterns) })
		if len(kept) != len(seen) {
			if err := m.hub.store.PutSetting(ctx, claudeDirsKey(m.cfg.ID), kept); err != nil {
				return err
			}
		}
	}
	m.hub.logf("[%s] blocked claude dirs: %v", m.cfg.ID, patterns)
	m.reconfigureClaudeDirs(ctx)
	return nil
}

// reconfigureClaudeDirs hands an online node its directories and blocked patterns again and takes the session list
// it then has (the apps get it in the next machines / sessions broadcast). Offline, the next connection does it.
func (m *Machine) reconfigureClaudeDirs(ctx context.Context) {
	m.mu.Lock()
	online := m.peer != nil
	m.mu.Unlock()
	if !online {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	// botmux too: this can come between the connection and its own node.configure, and the session list that follows
	// must not be the node's first word on it (an older hub's, node/hostbotmux.go useBotmux).
	if err := m.call(ctx, "node.configure", map[string]any{"claude_dirs": m.claudeDirs(ctx),
		"claude_dirs_blocked": m.blockedClaudeDirs(ctx), "botmux": m.botmux()}, nil); err != nil {
		m.hub.logf("[%s] configure claude dirs: %v", m.cfg.ID, err)
		return
	}
	var list struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if err := m.call(ctx, "sessions.list", map[string]any{}, &list); err == nil {
		m.setSessions(list.Sessions)
	}
}

// claudeDirsState is what GET /api/v1/machines/{mid}/claude-dirs answers: the machine's config directories (blocked
// ones marked) and the blocked patterns.
type claudeDirsState struct {
	Dirs    []proto.ClaudeDir `json:"dirs"`
	Blocked []string          `json:"blocked"`
	Home    string            `json:"home,omitempty"`
	// Live: the list is the node's own (every directory it knows, with counts); false: the hub's record (the
	// machine is offline or its node is older).
	Live bool `json:"live"`
}

func (m *Machine) claudeDirsState(ctx context.Context) claudeDirsState {
	blocked, home := m.blockedClaudeDirs(ctx), m.nodeHome()
	// JSON serializes a nil slice as null; the contract is a JSON array (the web settings page calls .filter on it),
	// so an empty blocklist must be [] like Dirs below.
	blockedJSON := blocked
	if blockedJSON == nil {
		blockedJSON = []string{}
	}
	st := claudeDirsState{Blocked: blockedJSON, Home: home, Dirs: []proto.ClaudeDir{}}
	var res struct {
		Dirs []proto.ClaudeDir `json:"dirs"`
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.call(cctx, "claude.dirs.list", map[string]any{}, &res); err == nil && res.Dirs != nil {
		st.Dirs, st.Live = res.Dirs, true
	}
	has := func(d string) bool {
		return slices.ContainsFunc(st.Dirs, func(c proto.ClaudeDir) bool { return c.Path == d })
	}

	// What the node did not list: configured ones it was not told about (blocked), and, from an older or offline
	// node, the remembered ones.
	var seen []string
	if m.hub.store != nil {
		_, _ = m.hub.store.Setting(ctx, claudeDirsKey(m.cfg.ID), &seen)
	}
	add := func(d, source string) {
		if d != "" && !has(d) {
			st.Dirs = append(st.Dirs, proto.ClaudeDir{Path: d, Source: source, Blocked: proto.ClaudeDirBlocked(d, home, blocked)})
		}
	}
	for _, d := range m.cfg.ClaudeConfigDirs {
		add(d, "config")
	}
	if !st.Live {
		for _, d := range seen {
			add(d, "found")
		}
	}
	// The hub says what blocks what (the node only whether): the pages show the rule and lift it.
	for i := range st.Dirs {
		d := &st.Dirs[i]
		if slices.Contains(m.cfg.ClaudeConfigDirs, d.Path) {
			d.Source = "config"
		}
		if d.Default {
			d.Blocked, d.BlockedBy = false, ""
			continue
		}
		// The node's word stands (it also matches the real path); the hub's own record has only the hub's.
		d.BlockedBy = proto.ClaudeDirBlockedBy(d.Path, home, blocked)
		d.Blocked = d.BlockedBy != "" || (st.Live && d.Blocked)
	}
	return st
}

func (h *Hub) getClaudeDirs(w http.ResponseWriter, r *http.Request, m *Machine) {
	writeJSON(w, m.claudeDirsState(r.Context()))
}

func (h *Hub) putClaudeDirsBlocked(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Blocked []string `json:"blocked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad body: %w", err))
		return
	}
	if err := m.setBlockedClaudeDirs(r.Context(), in.Blocked); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, errBadPattern) {
			code = http.StatusBadRequest
		}
		writeErr(w, code, err)
		return
	}
	writeJSON(w, m.claudeDirsState(r.Context()))
}
