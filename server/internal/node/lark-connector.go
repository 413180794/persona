package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
)

// The assistant can lead the user's personal agents and check lark-connector hub attention/approvals through lark-connector's
// agent hub control CLI (`lark-connector-agent`, docs/assistant.md「lark-connector agent hub」): personal agents (agent_sessions /
// agent_search / agent_messages / agent_send), hub agents/attention (hub_agents / hub_attention), and approval
// requests waiting there (approvals_list). When a CLI with `mcp-server --control` is on this machine, the Claude
// assistant gets it as a second MCP server, `lark-connector-agent mcp-server --control`. The CLI reads its own config
// (~/.config/lark-connector-agent/config.json, 0600); Persona never reads or passes its tokens.
//
//   - reads run without asking (lark-connectorReadTools);
//   - agent_send goes through the conversation's permission mode (asks on the phone by default; runs without asking
//     under bypassPermissions);
//   - Herdr/Orca tools (herdr_*, orca_*) are disallowed because Persona's own mcp__jarvis__agents_* tools manage panes
//     and terminals with hub safety checks;
//   - approval decisions are never Persona's to make: approval_decide is disallowed outright, whatever the CLI's own
//     approvals.enabled says. Pending approvals are only listed and forwarded to the user.

// lark-connectorServer is the MCP server name the assistant sees the tools under (mcp__lark-connector__*).
const lark-connectorServer = "lark-connector"

// lark-connectorReadTools only read; they skip the phone.
var lark-connectorReadTools = []string{
	"hub_agents", "hub_attention", "approvals_list",
	"agent_sessions", "agent_search", "agent_messages",
}

// lark-connectorDeniedTools are never callable by the assistant: approval_decide (approvals are only forwarded to the user)
// and Herdr/Orca tools (Persona's own mcp__jarvis__agents_* tools manage panes and terminals with hub checks).
var lark-connectorDeniedTools = []string{
	"approval_decide",
	"herdr_prompt", "herdr_rename", "herdr_answer",
	"orca_read", "orca_history", "orca_wait", "orca_send",
}

var (
	lark-connectorControlMu    sync.Mutex
	lark-connectorControlCache = map[string]lark-connectorControlEntry{}
)

type lark-connectorControlEntry struct {
	modTime time.Time
	size    int64
	ok      bool
}

// lark-connectorAgentBin finds the CLI: $PERSONA_lark-connector_AGENT (falling back to $JARVIS_lark-connector_AGENT via
// identity.LookupEnv; an explicit empty value or "0"/"off"/"false" disables the integration), then PATH, then ~/bin
// (where the wrapper usually lives and a service's PATH often misses). It also probes `<bin> --help` to ensure the
// installed CLI supports `mcp-server --control`. Returns "" when disabled, not installed, or lacking `--control`.
func lark-connectorAgentBin() string {
	if raw, ok := identity.LookupEnv("PERSONA_lark-connector_AGENT"); ok {
		p := strings.TrimSpace(raw)
		if p == "" || p == "0" || strings.EqualFold(p, "off") || strings.EqualFold(p, "false") {
			return ""
		}
		if haslark-connectorControl(p) {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath("lark-connector-agent"); err == nil && haslark-connectorControl(p) {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, "bin", "lark-connector-agent"); haslark-connectorControl(p) {
			return p
		}
	}
	return ""
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// haslark-connectorControl checks that p is an executable whose `--help` output mentions both `mcp-server` and `--control`,
// so a plain released `lark-connector-agent` without control support is not wired in by accident. Cached by path + mtime.
func haslark-connectorControl(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return false
	}
	lark-connectorControlMu.Lock()
	if e, ok := lark-connectorControlCache[p]; ok && e.modTime.Equal(st.ModTime()) && e.size == st.Size() {
		lark-connectorControlMu.Unlock()
		return e.ok
	}
	lark-connectorControlMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, p, "--help").CombinedOutput()
	s := string(out)
	ok := strings.Contains(s, "mcp-server") && strings.Contains(s, "--control")

	lark-connectorControlMu.Lock()
	lark-connectorControlCache[p] = lark-connectorControlEntry{modTime: st.ModTime(), size: st.Size(), ok: ok}
	lark-connectorControlMu.Unlock()
	return ok
}

// lark-connectorMCP is the mcpServers entry for the CLI at bin.
func lark-connectorMCP(bin string) map[string]any {
	return map[string]any{"command": bin, "args": []string{"mcp-server", "--control"}}
}

// assistantToolFlags are the --allowedTools / --disallowedTools values for the assistant, starting from baseAllowed
// (n.assistantTools()), with or without the lark-connector control tools.
func assistantToolFlags(baseAllowed string, lark-connector bool) (allowed, disallowed string) {
	if !lark-connector {
		return baseAllowed, ""
	}
	allow := []string{baseAllowed}
	for _, t := range lark-connectorReadTools {
		allow = append(allow, "mcp__"+lark-connectorServer+"__"+t)
	}
	var deny []string
	for _, t := range lark-connectorDeniedTools {
		deny = append(deny, "mcp__"+lark-connectorServer+"__"+t)
	}
	return strings.Join(allow, ","), strings.Join(deny, ",")
}
