package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFakelark-connectorBin(t *testing.T, dir, name, helpOut string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	script := "#!/bin/sh\necho " + shellQuote(helpOut) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestAssistantToolFlagsWithoutlark-connector(t *testing.T) {
	allowed, disallowed := assistantToolFlags("mcp__jarvis,Bash(sh /x/office.sh *)", false)
	if allowed != "mcp__jarvis,Bash(sh /x/office.sh *)" || disallowed != "" {
		t.Fatalf("got %q / %q", allowed, disallowed)
	}
}

func TestAssistantToolFlagslark-connector(t *testing.T) {
	allowed, disallowed := assistantToolFlags("mcp__jarvis", true)
	allowSet := map[string]bool{}
	for _, a := range strings.Split(allowed, ",") {
		allowSet[a] = true
	}
	for _, want := range []string{"mcp__jarvis", "mcp__lark-connector__agent_sessions", "mcp__lark-connector__approvals_list", "mcp__lark-connector__hub_attention"} {
		if !allowSet[want] {
			t.Errorf("%s not allowed: %s", want, allowed)
		}
	}
	// Acting and overlapping tools must not skip the phone.
	for _, act := range []string{"herdr_prompt", "herdr_rename", "herdr_answer", "orca_read", "orca_history", "orca_wait", "orca_send", "agent_send", "approval_decide"} {
		if allowSet["mcp__lark-connector__"+act] {
			t.Errorf("%s is auto-allowed", act)
		}
	}
	denySet := map[string]bool{}
	for _, d := range strings.Split(disallowed, ",") {
		denySet[d] = true
	}
	for _, wantDeny := range []string{
		"mcp__lark-connector__approval_decide",
		"mcp__lark-connector__herdr_prompt", "mcp__lark-connector__herdr_rename", "mcp__lark-connector__herdr_answer",
		"mcp__lark-connector__orca_read", "mcp__lark-connector__orca_history", "mcp__lark-connector__orca_wait", "mcp__lark-connector__orca_send",
	} {
		if !denySet[wantDeny] {
			t.Errorf("%s not disallowed: %s", wantDeny, disallowed)
		}
	}
	if denySet["mcp__lark-connector__agent_send"] {
		t.Errorf("agent_send should not be disallowed (uses conversation permission mode)")
	}
}

func Testlark-connectorMCP(t *testing.T) {
	m := lark-connectorMCP("/x/lark-connector-agent")
	args, _ := m["args"].([]string)
	if m["command"] != "/x/lark-connector-agent" || strings.Join(args, " ") != "mcp-server --control" {
		t.Fatalf("got %v", m)
	}
	if _, ok := m["env"]; ok {
		t.Fatal("no env: the CLI reads its own config, Persona passes no tokens")
	}
}

func Testlark-connectorAgentBin(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakelark-connectorBin(t, dir, "lark-connector-agent", "Usage: lark-connector-agent mcp-server --control")
	oldBin := writeFakelark-connectorBin(t, t.TempDir(), "lark-connector-agent", "Usage: lark-connector-agent mcp-server")

	// PERSONA_lark-connector_AGENT takes precedence over JARVIS_lark-connector_AGENT.
	t.Setenv("PERSONA_lark-connector_AGENT", bin)
	t.Setenv("JARVIS_lark-connector_AGENT", filepath.Join(dir, "missing"))
	if got := lark-connectorAgentBin(); got != bin {
		t.Fatalf("PERSONA_lark-connector_AGENT override: got %q, want %q", got, bin)
	}

	// Explicit empty PERSONA_lark-connector_AGENT disables discovery even when PATH and JARVIS_lark-connector_AGENT have it.
	t.Setenv("PERSONA_lark-connector_AGENT", "")
	t.Setenv("JARVIS_lark-connector_AGENT", bin)
	t.Setenv("PATH", dir)
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("explicit empty PERSONA_lark-connector_AGENT should disable, got %q", got)
	}

	// Explicit "0" or "off" also disables discovery.
	t.Setenv("PERSONA_lark-connector_AGENT", "off")
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("PERSONA_lark-connector_AGENT=off should disable, got %q", got)
	}

	// Legacy JARVIS_lark-connector_AGENT fallback when PERSONA_lark-connector_AGENT is unset.
	os.Unsetenv("PERSONA_lark-connector_AGENT")
	t.Setenv("JARVIS_lark-connector_AGENT", bin)
	if got := lark-connectorAgentBin(); got != bin {
		t.Fatalf("JARVIS_lark-connector_AGENT fallback: got %q, want %q", got, bin)
	}

	// A binary without `mcp-server --control` in --help is rejected.
	t.Setenv("JARVIS_lark-connector_AGENT", oldBin)
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("old binary without --control should be rejected, got %q", got)
	}

	// Missing override returns "".
	t.Setenv("JARVIS_lark-connector_AGENT", filepath.Join(dir, "missing"))
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("missing override: got %q", got)
	}

	// Explicit empty JARVIS_lark-connector_AGENT when PERSONA_lark-connector_AGENT is unset disables PATH fallback.
	t.Setenv("JARVIS_lark-connector_AGENT", "")
	t.Setenv("PATH", dir)
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("explicit empty JARVIS_lark-connector_AGENT should disable, got %q", got)
	}

	// Unset both env vars: falls back to PATH.
	os.Unsetenv("JARVIS_lark-connector_AGENT")
	t.Setenv("PATH", dir)
	if got := lark-connectorAgentBin(); got != bin {
		t.Fatalf("PATH: got %q, want %q", got, bin)
	}

	// Old binary on PATH is ignored.
	t.Setenv("PATH", filepath.Dir(oldBin))
	t.Setenv("HOME", t.TempDir())
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("old binary on PATH: got %q", got)
	}

	//Absent everywhere returns "".
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got := lark-connectorAgentBin(); got != "" {
		t.Fatalf("absent: got %q", got)
	}
}

func TestClaudeAssistantMCPAndToolsWiring(t *testing.T) {
	n, _ := testNode(t)
	dir := t.TempDir()
	bin := writeFakelark-connectorBin(t, dir, "lark-connector-agent", "Usage: lark-connector-agent mcp-server --control")
	t.Setenv("PERSONA_lark-connector_AGENT", bin)

	mcpJSON, flags := n.claudeAssistantMCPAndTools("sess-123")
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(mcpJSON), &cfg); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}
	if _, ok := cfg.MCPServers["jarvis"]; !ok {
		t.Fatal("missing jarvis mcp server")
	}
	ol, ok := cfg.MCPServers["lark-connector"]
	if !ok {
		t.Fatal("missing lark-connector mcp server in --mcp-config")
	}
	if ol["command"] != bin {
		t.Fatalf("lark-connector command = %v, want %q", ol["command"], bin)
	}
	disallowedIdx := slices.Index(flags, "--disallowedTools")
	if disallowedIdx < 0 || disallowedIdx+1 >= len(flags) {
		t.Fatalf("missing --disallowedTools in flags: %v", flags)
	}
	disallowed := strings.Split(flags[disallowedIdx+1], ",")
	if !slices.Contains(disallowed, "mcp__lark-connector__approval_decide") {
		t.Fatalf("--disallowedTools %v does not contain mcp__lark-connector__approval_decide", disallowed)
	}

	// When disabled via PERSONA_lark-connector_AGENT="", lark-connector server and --disallowedTools are omitted.
	t.Setenv("PERSONA_lark-connector_AGENT", "")
	mcpJSONOff, flagsOff := n.claudeAssistantMCPAndTools("sess-123")
	var cfgOff struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(mcpJSONOff), &cfgOff); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfgOff.MCPServers["lark-connector"]; ok {
		t.Fatal("lark-connector server should not be present when disabled")
	}
	if slices.Contains(flagsOff, "--disallowedTools") {
		t.Fatalf("unexpected --disallowedTools when disabled: %v", flagsOff)
	}
}
