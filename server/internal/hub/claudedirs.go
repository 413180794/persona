package hub

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/nwverse/persona/server/internal/rpc"
)

// Claude Code config directories per machine (node/claudedirs.go): the configured ones (hub.json
// machines[].claude_config_dirs) plus those the node found in running claudes, remembered here so a profile's
// sessions stay listed after the node restarts with none of its claudes running.

func claudeDirsKey(machine string) string { return "claude_dirs/" + machine }

// configureNode hands the node its config directories, before the first session list. Older nodes do not know
// node.configure; they keep looking in ~/.claude only.
func (m *Machine) configureNode(ctx context.Context, peer *rpc.Peer) {
	dirs := slices.Clone(m.cfg.ClaudeConfigDirs)
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
	_ = peer.Call(ctx, "node.configure", map[string]any{"machine_id": m.cfg.ID, "codex": m.codexConfig(ctx), "claude_dirs": dirs, "keep_sessions": m.hub.keptSessions(ctx, m.cfg.ID)}, nil)
	m.sendManaged(ctx, func(ctx context.Context, method string, params, out any) error {
		return peer.Call(ctx, method, params, out)
	})
}

// rememberClaudeDirs stores the directories the node found (claude.dirs event), added to what was known.
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
	changed := false
	for _, d := range p.Dirs {
		if d != "" && !slices.Contains(seen, d) {
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
