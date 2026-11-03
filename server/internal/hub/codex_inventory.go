package hub

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/nwverse/persona/server/internal/proto"
)

func codexInventoryKey(machine string) string { return "codex_inventory/" + machine }

func (m *Machine) codexConfig(ctx context.Context) proto.CodexConfig {
	var cfg proto.CodexConfig
	if m.hub.store != nil {
		_, _ = m.hub.store.Setting(ctx, codexInventoryKey(m.cfg.ID), &cfg)
	}
	if m.cfg.CodexDiscovery != nil {
		cfg.Discovery = m.cfg.CodexDiscovery
	}
	for _, h := range m.cfg.CodexHomes {
		if !slices.Contains(cfg.Homes, h) {
			cfg.Homes = append(cfg.Homes, h)
		}
	}
	for _, s := range m.cfg.CodexServers {
		i := slices.IndexFunc(cfg.Servers, func(v proto.CodexServer) bool { return v.ID == s.ID })
		if i >= 0 {
			cfg.Servers[i] = s
		} else {
			cfg.Servers = append(cfg.Servers, s)
		}
	}
	return cfg
}

func (m *Machine) rememberCodexInventory(raw json.RawMessage) {
	if m.hub.store == nil {
		return
	}
	var cfg proto.CodexConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	// Updates come from this authenticated node, not public endpoint URLs. The node never reports credentials.
	if err := m.hub.store.PutSetting(ctx, codexInventoryKey(m.cfg.ID), cfg); err != nil {
		m.hub.logf("[%s] remember Codex inventory: %v", m.cfg.ID, err)
	}
}
