package node

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// authNode is a node in the fake home fakeClaudeHome made, its claude at version (cached, as the self-check reads it).
func authNode(t *testing.T, version string) *Node {
	t.Helper()
	home := os.Getenv("HOME")
	n := &Node{home: home, dir: filepath.Join(home, ".persona"), logger: log.New(io.Discard, "", 0)}
	n.claudeV.set(version)
	return n
}

// What `claude auth status --json` prints (2.1.308) for a claude.ai login: the account's email and organization are
// in it and must not leave the machine.
const authClaudeAI = `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "apiProvider": "firstParty",
  "analyticsDisabled": false,
  "configDirectory": "/home/me/.claude",
  "email": "me@example.com",
  "orgId": "org-1",
  "orgName": "Example",
  "subscriptionType": "max"
}`

// Plain claude is asked in the login shell with the --settings a start gives it before the subcommand, and with the
// machine's claude_start_dir as its config directory; behind a banner and escape codes its status is read, and only
// the login's method leaves (no email, no organization).
func TestClaudeAuthStatusSignedIn(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "args")
	fakeClaudeHome(t, `printf '%s\n' "CONFIG=$CLAUDE_CONFIG_DIR" "$@" > `+shellQuote(seen)+`
printf 'Welcome to this machine\n\033]1337;ShellIntegrationVersion=14\007'
cat <<'EOF'
`+authClaudeAI+`
EOF
`)
	n := authNode(t, "2.1.308")
	dir := "/tmp/profile-dir"
	n.claudeStartDir.Store(&dir)
	st, err := n.claudeAuthStatus(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := proto.ClaudeAuthStatus{State: proto.ClaudeLoggedIn, AuthMethod: "claude.ai", APIProvider: "firstParty", Version: "2.1.308"}
	if st != want {
		t.Fatalf("status = %+v", st)
	}
	b, _ := os.ReadFile(seen)
	args := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(args) != 6 || args[0] != "CONFIG=/tmp/profile-dir" || args[1] != "--settings" || args[3] != "auth" || args[4] != "status" || args[5] != "--json" {
		t.Fatalf("claude ran with %q", args)
	}
}

// Signed out: Claude Code exits 1 with the same JSON, and that is not_logged_in, not an error.
func TestClaudeAuthStatusSignedOut(t *testing.T) {
	fakeClaudeHome(t, `echo '{"loggedIn": false, "authMethod": "none", "apiProvider": "firstParty"}'
exit 1
`)
	st, err := authNode(t, "2.1.308").claudeAuthStatus(context.Background(), "claude")
	if err != nil || st.State != proto.ClaudeNotLoggedIn || st.AuthMethod != "none" {
		t.Fatalf("status = %+v (%v)", st, err)
	}
}

// A key in the environment counts as signed in to Claude Code: reported as api_key with the variable's name, which
// the hub takes as unverified until a message goes.
func TestClaudeAuthStatusAPIKey(t *testing.T) {
	fakeClaudeHome(t, `echo '{"loggedIn": true, "authMethod": "api_key", "apiProvider": "firstParty", "apiKeySource": "ANTHROPIC_API_KEY"}'
`)
	st, _ := authNode(t, "2.1.308").claudeAuthStatus(context.Background(), "claude")
	if st.State != proto.ClaudeLoggedIn || st.AuthMethod != "api_key" || st.KeySource != "ANTHROPIC_API_KEY" {
		t.Fatalf("status = %+v", st)
	}
	// Something that looks like a key itself is never passed on as a source.
	if got, ok := parseClaudeAuthStatus(`{"loggedIn": true, "authMethod": "api_key", "apiKeySource": "sk-ant-abcdefghijklmnop"}`); !ok || got.KeySource != "" {
		t.Fatalf("status = %+v", got)
	}
}

// A claude from before `claude auth` is not asked (it could take the words for a prompt): unknown, old_claude.
func TestClaudeAuthStatusOldClaude(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	fakeClaudeHome(t, `touch `+shellQuote(ran)+`
`)
	st, err := authNode(t, "2.1.40").claudeAuthStatus(context.Background(), "claude")
	if err != nil || st.State != proto.ClaudeAuthUnknown || st.Reason != proto.ClaudeAuthOld || st.Version != "2.1.40" {
		t.Fatalf("status = %+v (%v)", st, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("an old claude was run")
	}
}

// What cannot be read is unknown, never a failure: claude not found, an answer that is no status, a run past the
// time limit.
func TestClaudeAuthStatusUnreadable(t *testing.T) {
	fakeClaudeHome(t, `echo "error: unknown option '--json'" >&2
exit 1
`)
	n := authNode(t, "")
	if st, err := n.claudeAuthStatus(context.Background(), "claude"); err != nil || st.State != proto.ClaudeAuthUnknown ||
		st.Reason != proto.ClaudeAuthUnreadable {
		t.Fatalf("status = %+v (%v)", st, err)
	}
	fakeClaudeHome(t, `sleep 5
`)
	old := claudeAuthTimeout
	claudeAuthTimeout = 300 * time.Millisecond
	t.Cleanup(func() { claudeAuthTimeout = old })
	if st, _ := authNode(t, "2.1.308").claudeAuthStatus(context.Background(), "claude"); st.State != proto.ClaudeAuthUnknown ||
		st.Reason != proto.ClaudeAuthTimeout {
		t.Fatalf("status = %+v", st)
	}
	// Without zsh's banners: a missing claude prints nothing to read.
	if _, ok := parseClaudeAuthStatus("zsh:1: command not found: claude\n"); ok {
		t.Fatal("read a status out of command not found")
	}
}

// A managed launcher is asked through `personad launch <name>` with its gateway merged into the --settings, as it
// starts; a word that is no launcher here is refused, not run.
func TestClaudeAuthStatusManagedLauncher(t *testing.T) {
	fakeClaudeHome(t, "exit 1\n")
	seen := filepath.Join(t.TempDir(), "seen")
	bin := filepath.Join(os.Getenv("HOME"), "bin", "personad")
	script := `#!/bin/sh
printf '%s\n' "$@" > ` + shellQuote(seen) + `
while [ $# -gt 0 ]; do [ "$1" = --settings ] && cat "$2" >> ` + shellQuote(seen) + `; shift; done
echo '{"loggedIn": true, "authMethod": "oauth_token", "apiProvider": "firstParty"}'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	n := authNode(t, "2.1.308")
	n.bin = bin
	n.setManaged([]proto.ManagedLauncher{{Name: "gw", BaseURL: "https://gw.example.com", APIKey: "tok-test", Auth: "bearer"}})
	st, err := n.claudeAuthStatus(context.Background(), "gw")
	if err != nil || st.State != proto.ClaudeLoggedIn || st.AuthMethod != "oauth_token" {
		t.Fatalf("status = %+v (%v)", st, err)
	}
	b, _ := os.ReadFile(seen)
	if s := string(b); !strings.HasPrefix(s, "launch\ngw\n--settings\n") || !strings.Contains(s, "auth\nstatus\n--json\n") ||
		!strings.Contains(s, "https://gw.example.com") {
		t.Fatalf("ran with %s", s)
	}
	if _, err := n.claudeAuthStatus(context.Background(), "reboot"); err == nil {
		t.Fatal("an unknown launcher was run")
	}
}
