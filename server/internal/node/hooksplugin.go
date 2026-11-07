package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Persona's hooks (questions, permission requests and turn ends go to the phone; docs/integrations/claude-integration.md) reach
// the claude it starts as a plugin of their own, loaded for that session only (#1411). A start can give claude any
// number of --plugin-dir, each adding to the rest, but Claude Code keeps only the last --settings: a launcher (an
// alias, or a function in ~/.zshrc, whose words the node cannot read) may pass its own, with the gateway, its token
// and the model in it, and Persona's hooks in a --settings after it dropped all of that ("Not logged in"). A claude
// that does not load plugins (claudeLoadsPlugins, agentplugin.go) still gets them in --settings (launcher_settings.go).

// hooksPluginName is the plugin's name in a session; a session loads one of the two.
const hooksPluginName = "persona-hooks"

// hooksPlugin is the plugin with the hooks of kind's starts: "claude" (the agents Persona starts, the hooks of
// claude-settings.json) or "assistant" (its assistant, assistant-settings.json's).
func (n *Node) hooksPlugin(kind string) string { return filepath.Join(n.dir, kind+"-hooks") }

// hooksPluginFiles is the plugin with hooks (a settings file's "hooks") as files, by path under its directory; its
// version is theirs (a hash), so it changes when they do.
func hooksPluginFiles(hooks any) (map[string][]byte, error) {
	b, err := json.Marshal(hooks)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	manifest := map[string]any{"name": hooksPluginName, "version": "1.0.0-" + hex.EncodeToString(sum[:6]),
		"description": "Persona's hooks for the sessions it starts: questions, permission requests and turn ends go to the phone"}
	mb, err := marshalJSON(manifest)
	if err != nil {
		return nil, err
	}
	hb, err := marshalJSON(map[string]any{"hooks": hooks})
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		filepath.Join(".claude-plugin", "plugin.json"): mb,
		filepath.Join("hooks", "hooks.json"):           hb,
	}, nil
}

// claudeStartSettings is how a claude start of launcher gets Persona's hooks for kind ("claude" / "assistant") and a
// managed launcher's gateway: settings is the --settings to pass and hooks the --plugin-dir, "" for none.
//
// With a claude that loads plugins, the hooks are their plugin and settings is only a managed launcher's env (its
// gateway must beat the user's global settings env; Persona's own launcher has no settings of its own to lose):
// whatever --settings a launcher passes is then the one claude keeps. Otherwise, or while the plugin is not written,
// as before: the hooks' settings file, merged with an alias's own --settings or a managed launcher's env.
func (n *Node) claudeStartSettings(ctx context.Context, launcher, kind, cwd string) (settings, hooks string) {
	ours := filepath.Join(n.dir, kind+"-settings.json")
	plugin := n.hooksPlugin(kind)
	if _, err := os.Stat(filepath.Join(plugin, "hooks", "hooks.json")); err != nil || !n.claudeLoadsPlugins(ctx) {
		return n.claudeSettings(ctx, launcher, ours, cwd), ""
	}
	if m, ok := n.managedLauncher(launcher); ok {
		if path, err := n.mergeManagedSettings(launcher, m, ""); err != nil {
			n.logf("launcher %s: its gateway not written for the start: %v", launcher, err)
		} else {
			settings = path
		}
	}
	return settings, plugin
}
