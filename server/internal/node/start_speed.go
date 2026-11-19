package node

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A new Agent's speed (session.start's speed, proto.AgentSpeeds; docs/integrations/launchers.md "Speed"). Codex runs a
// faster one as its native service tier, `-c service_tier=<tier>` beside its effort (codexExecutionArgs), once its model
// catalog says the model offers that tier: Codex itself runs an unadvertised one without a word. Claude Code turns on
// fast mode, `"fastMode": true` in the --settings file it starts with, its only switch for it at startup. Any other
// agent, and a start that runs nowhere Persona sets these (a runtime's, an app's, a resume), is refused before anything
// starts: a speed that costs more is never dropped silently. Standard asks for nothing and changes nothing.

func speedRefusal(ctx context.Context, code string, args i18n.Args) error {
	return refusal(code, i18n.T(i18n.From(ctx), "rpc-errors."+code, args), nil)
}

// checkStartSpeed refuses session.start's speed where the start would not run it: before a starting row or a tab.
func (n *Node) checkStartSpeed(ctx context.Context, speed, launcher, agentType, host, resume string, work bool) error {
	if !proto.ValidAgentSpeed(speed) {
		return speedRefusal(ctx, "agent_speed_invalid", i18n.Args{"speed": speed})
	}
	if !proto.FasterSpeed(speed) {
		return nil
	}
	if resume != "" || work || isDesktopLauncher(launcher) || isCodexAppLauncher(launcher) {
		return speedRefusal(ctx, "agent_speed_start_only", nil)
	}
	if _, runtime := engineRuntimes[cmp.Or(host, n.defaultKind())]; runtime {
		return speedRefusal(ctx, "agent_speed_start_only", nil)
	}
	if h, _ := n.startRuntimeIn(withStartHost(ctx, host), launcher); h != nil {
		return speedRefusal(ctx, "agent_speed_start_only", nil)
	}
	return n.speedAgentRefusal(ctx, launcher, n.speedAgent(ctx, launcher, agentType), speed)
}

// speedAgent is the agent a start of launcher runs, as n.start dispatches it: a Codex alias the login shell lists is
// Codex whatever its name.
func (n *Node) speedAgent(ctx context.Context, launcher, agentType string) string {
	kind := n.startAgentType(launcher, agentType)
	if work := workAgentKind(launcher, kind); work != "" {
		return work
	}
	if kind == "claude" && agentType == "" && !proto.IsExternalLauncher(launcher) {
		if _, codex := n.codexLauncher(ctx, launcher); codex {
			return "codex"
		}
	}
	return kind
}

func (n *Node) speedAgentRefusal(ctx context.Context, launcher, agent, speed string) error {
	if ok, _ := proto.AgentTakesSpeed(agent, speed); ok {
		return nil
	}
	return speedRefusal(ctx, "agent_speed_unsupported", i18n.Args{"launcher": cmp.Or(launcher, "claude"), "agent": cmp.Or(agent, "?"), "speed": speed})
}

// codexSpeedTier is execution's speed for a new Codex of launcher, checked against its model catalog: the service tier
// to start with ("" none), or the refusal of a model that does not offer it. A catalog that cannot be read, or a
// model it does not list (a gateway's own id), leaves it to Codex.
func (n *Node) codexSpeedTier(ctx context.Context, launcher string, e modelExecution) (string, error) {
	tier := proto.CodexServiceTier(e.Speed)
	if tier == "" {
		return "", nil
	}
	list, err := n.launcherModels(ctx, launcher, "codex", false)
	if err != nil {
		n.logf("start %s: speed %s not checked against the model list: %v", launcher, e.Speed, err)
		return tier, nil
	}
	i := slices.IndexFunc(list.Models, func(m proto.AgentModel) bool {
		if e.Model == "" {
			return m.Default
		}
		return m.ID == e.Model
	})
	if i < 0 {
		return tier, nil
	}
	m := list.Models[i]
	offered := make([]string, 0, len(m.ServiceTiers))
	for _, t := range m.ServiceTiers {
		if t.ID == tier {
			return tier, nil
		}
		offered = append(offered, t.Name)
	}
	if len(offered) == 0 {
		offered = append(offered, "Standard")
	}
	return "", speedRefusal(ctx, "agent_speed_model", i18n.Args{"model": m.ID, "speed": e.Speed, "tiers": strings.Join(offered, ", ")})
}

// claudeFastSettings is settings (the --settings file a Claude start passes, "" for none) with fast mode on: a copy in
// the node's directory named by its content, so a start's resume command (herdrresume.go) keeps a file that stays.
func (n *Node) claudeFastSettings(settings string) (string, error) {
	values := map[string]any{}
	if settings != "" {
		b, err := os.ReadFile(settings)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &values); err != nil {
				return "", err
			}
		}
	}
	values["fastMode"] = true
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	path := filepath.Join(n.dir, "claude-fast", hex.EncodeToString(sum[:8])+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == string(b) {
		return path, nil
	}
	return path, writeFileAtomic(path, b)
}
