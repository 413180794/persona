package node

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOtherAgentStartCommand(t *testing.T) {
	const prompt = "a 'quoted' prompt; $(printf unexpected)"
	cmd := otherAgentCommand("wrapper", "00000000-0000-4000-8000-000000000001", prompt)
	// Execute only a fake shell function: a first prompt must remain a single literal argument.
	out, err := exec.Command("sh", "-c", `wrapper() { printf '%s\n' "$#" "$@"; }; `+cmd).Output()
	if err != nil || string(out) != "3\nresume\n00000000-0000-4000-8000-000000000001\n"+prompt+"\n" {
		t.Fatalf("command arguments = %q (%v)", out, err)
	}
	if strings.Contains(cmd, "--session-id") || strings.Contains(cmd, "--settings") || strings.Contains(cmd, "--mcp-config") {
		t.Fatal("a non-Claude start must not receive Claude flags")
	}
}
