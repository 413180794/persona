package node

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The assistant's own conversations are denied the health space's directory and its Claude project directory, for
// reading and writing; a health conversation is denied nothing; the rules go on the claude command line (#3282).
func TestHealthDenyRules(t *testing.T) {
	n, home := testNode(t)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	assistant := filepath.Join(home, ".persona", "assistant")
	health := filepath.Join(home, ".persona", "health")
	rules := n.healthDenyRules(assistant)
	project := filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(health, "-"))
	for _, want := range []string{"Read(/" + health + "/**)", "Edit(/" + health + "/**)", "Read(/" + project + "/**)", "Edit(/" + project + "/**)"} {
		if !slices.Contains(rules, want) {
			t.Fatalf("missing %s in %q", want, rules)
		}
	}
	for _, r := range rules {
		if strings.Contains(r, "/.persona/assistant") || strings.Contains(r, "-assistant/") {
			t.Fatalf("the assistant's own directory denied: %s", r)
		}
	}
	if got := n.healthDenyRules(health); got != nil {
		t.Fatalf("a health conversation denied %q", got)
	}

	_, flags := n.claudeAssistantMCPAndTools("sess-123", assistant)
	i := slices.Index(flags, "--disallowedTools")
	if i < 0 || !slices.Contains(flags[i+1:], "Read(/"+health+"/**)") || slices.Contains(flags[i+1:], "--disallowedTools") {
		t.Fatalf("flags %q", flags)
	}
	if _, flags := n.claudeAssistantMCPAndTools("sess-123", health); slices.ContainsFunc(flags, func(f string) bool { return strings.HasPrefix(f, "Read(") }) {
		t.Fatalf("a health conversation's flags deny files: %q", flags)
	}
}

// On the Agent SDK the deny rules go in the start's settings, next to the ones a settings file already has.
func TestSDKOptionsCarryTheDenyRules(t *testing.T) {
	settings := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settings, []byte(`{"permissions":{"deny":["Bash(rm:*)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := sdkStart{id: "s1", cwd: "/home/me/.persona/assistant", settings: settings, deny: []string{"Read(//home/me/.persona/health/**)"}}.options("/bin/claude")
	if err != nil {
		t.Fatal(err)
	}
	deny := o["settings"].(map[string]any)["permissions"].(map[string]any)["deny"].([]any)
	if len(deny) != 2 || deny[0] != "Bash(rm:*)" || deny[1] != "Read(//home/me/.persona/health/**)" {
		t.Fatalf("deny %v", deny)
	}
}

// A health conversation may write its reports' records without asking; nothing else gets that rule.
func TestHealthReportRules(t *testing.T) {
	n, home := testNode(t)
	t.Setenv("HOME", home)
	health := filepath.Join(home, ".persona", "health")
	reports := "/" + filepath.Join(health, "reports") + "/**"
	if got := healthReportRules(health); !slices.Equal(got, []string{"Edit(" + reports + ")"}) {
		t.Fatalf("rules %q", got)
	}
	if got := healthReportRules(filepath.Join(home, ".persona", "assistant")); got != nil {
		t.Fatalf("the assistant's own conversations got %q", got)
	}
	if got := healthReportRules("/home/a,b/.persona/health"); got != nil {
		t.Fatalf("a path with a comma got %q", got)
	}
	_, flags := n.claudeAssistantMCPAndTools("sess-123", health)
	i := slices.Index(flags, "--allowedTools")
	if i < 0 || !slices.Contains(strings.Split(flags[i+1], ","), "Edit("+reports+")") {
		t.Fatalf("flags %q", flags)
	}
}
