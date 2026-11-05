package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The filters a platform's extension would give (a hide_agents setting): its Codex runs by originator, its Claude
// runs by the skill directory they are given, and the first lines of its prompts for anything without a mark.
var platformFilters = []proto.AgentFilter{
	{Extension: "bots", ID: "codex", Agents: []string{"codex"}, Originators: []string{"codex-bot"}},
	{Extension: "bots", ID: "claude", Agents: []string{"claude"}, AddedDirs: []string{"~/.bot/skill-bundles"}},
	{Extension: "bots", ID: "prompts", FirstMessagePrefixes: []string{"You are running as an agent in a bot workspace chat", "## Bot Channel Identity\n"}},
}

func writeClaudeTranscript(t *testing.T, dir, id, prompt string, added []string, age time.Duration) {
	t.Helper()
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	var dirs []string
	for _, d := range added {
		dirs = append(dirs, q(d))
	}
	body := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-11-05T07:31:12.327Z"}` + "\n" +
		`{"type":"user","uuid":"u1","timestamp":"2026-11-05T07:31:12.394Z","cwd":"/src/app","message":{"role":"user","content":[{"type":"text","text":` + q(prompt) + `}]}}` + "\n" +
		`{"type":"attachment","uuid":"e1","attachment":{"type":"environment","snapshot":{"workingDirectory":"/src/app","additionalWorkingDirectories":[` + strings.Join(dirs, ",") + `]}}}` + "\n" +
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-05T07:31:20Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(body+strings.Repeat(" ", 4096)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	_ = os.Chtimes(p, at, at)
}

// A platform's Claude runs leave the list, by the directory they are given or the first line of their prompt; the
// user's own sessions stay, one that talks about the platform and one given another directory too. Hundreds of newer
// platform runs do not crowd the user's out of the newest 80, and without filters everything is listed again.
func TestAgentFiltersLeaveClaudeRunsOut(t *testing.T) {
	n, _ := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	dir := filepath.Join(n.home, ".claude", "projects", "-src-app")
	_ = os.MkdirAll(dir, 0o700)
	bundle := filepath.Join(n.home, ".bot", "skill-bundles", "v5", "abc")
	const mine, mineAboutBot, mineAdded, chat, noMark = "11111111-0000-4000-8000-000000000001", "11111111-0000-4000-8000-000000000002",
		"11111111-0000-4000-8000-000000000003", "22222222-0000-4000-8000-000000000001", "22222222-0000-4000-8000-000000000002"
	writeClaudeTranscript(t, dir, mine, "fix the login page", nil, 3*time.Hour)
	writeClaudeTranscript(t, dir, mineAboutBot, "You are running as an agent? Look at why the bot plugin fails", nil, 3*time.Hour)
	writeClaudeTranscript(t, dir, mineAdded, "compare with the other repo", []string{filepath.Join(n.home, "code", "other")}, 3*time.Hour)
	writeClaudeTranscript(t, dir, chat, "## Bot Channel Identity\n\n- workspace_id: w1", []string{bundle}, time.Hour)
	writeClaudeTranscript(t, dir, noMark, "You are running as an agent in a bot workspace chat.\n\nChat ID: c1", nil, time.Hour)
	for i := 0; i < 90; i++ {
		writeClaudeTranscript(t, dir, fmt.Sprintf("33333333-0000-4000-8000-%012d", i), "run the alarm check", []string{bundle}, time.Duration(i+1)*time.Minute)
	}
	listed := func() map[string]bool {
		out := map[string]bool{}
		for _, s := range n.scanOffline() {
			out[s.SessionID] = true
		}
		return out
	}
	if got := listed(); len(got) != 80 || got[mine] {
		t.Fatalf("without filters the newest 80 are listed and the platform's runs push the user's out: %d, mine %v", len(got), got[mine])
	}
	n.setAgentFilters(platformFilters)
	got := listed()
	for _, id := range []string{mine, mineAboutBot, mineAdded} {
		if !got[id] {
			t.Errorf("the user's own %s is listed", id)
		}
	}
	if len(got) != 3 {
		t.Errorf("only the user's own three are listed, got %d: %v", len(got), got)
	}
	n.setAgentFilters([]proto.AgentFilter{})
	if got := listed(); len(got) != 80 {
		t.Fatalf("filters dropped: all listed again, got %d", len(got))
	}
}

// A platform's Codex runs (and their subagents) leave the list by originator, one without it by its prompt; the user's
// own (Persona's, the CLI's, the desktop App's) stay, and so does a kept one.
func TestAgentFiltersLeaveCodexRunsOut(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "05")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	prompt := func(text string) string {
		return fmt.Sprintf(`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":%q}]}}}`, text)
	}
	cases := []struct {
		id, meta, prompt string
		listed           bool
	}{
		{"0f000000-0000-4000-8000-0000000000a1", `{"originator":"codex-bot","source":"vscode","cwd":"/home/me"}`, "## Bot Channel Identity\n\n- workspace_id: w1", false},
		{"0f000000-0000-4000-8000-0000000000a2", `{"originator":"codex-bot","source":{"subagent":{"thread_spawn":{"parent_thread_id":"0f000000-0000-4000-8000-0000000000a1"}}}}`, "look at the logs", false},
		{"0f000000-0000-4000-8000-0000000000a3", `{"originator":"codex-bot","source":"vscode","cwd":"/home/me"}`, "", false},
		{"0f000000-0000-4000-8000-0000000000a4", `{"originator":"some-bridge","source":"vscode","cwd":"/home/me"}`, "You are running as an agent in a bot workspace chat.", false},
		{"0f000000-0000-4000-8000-0000000000b1", `{"originator":"jarvis","source":"vscode","cwd":"/work/app"}`, "fix the build", true},
		{"0f000000-0000-4000-8000-0000000000b2", `{"originator":"codex-tui","source":"cli","cwd":"/work/mine"}`, "why does the bot start so many codex runs", true},
		{"0f000000-0000-4000-8000-0000000000b3", `{"originator":"Codex Desktop","source":"vscode","cwd":"/work/app"}`, "", true},
		{"0f000000-0000-4000-8000-0000000000b4", `{"originator":"codex-bot","source":"vscode","cwd":"/home/me"}`, "kept by the hub", true},
	}
	for _, c := range cases {
		body := `{"type":"session_meta","payload":` + c.meta + "}\n"
		if c.prompt != "" {
			body += prompt(c.prompt) + "\n"
		}
		p := filepath.Join(dir, "rollout-2026-11-05T01-00-00-"+c.id+".jsonl")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if id := n.registerCodex(home, c.id, p); strings.HasSuffix(c.id, "b4") {
			n.setKept([]string{id}) // the session id, not the thread's
		}
	}
	n.setAgentFilters(platformFilters)
	listed := map[string]bool{}
	for _, s := range n.codexSessions(map[string]bool{}) {
		listed[s.SourceThreadID] = true
	}
	for _, c := range cases {
		if listed[c.id] != c.listed {
			t.Errorf("%s %q listed=%v, want %v", c.meta, c.prompt, listed[c.id], c.listed)
		}
	}
}

// The head is read once a session has started; a file that has no prompt yet is read again as it grows, so a run is
// left out from its first prompt on.
func TestSessionMarksWaitForTheFirstPrompt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(p, []byte(`{"type":"session_meta","payload":{"originator":"codex-bot"}}`+"\n"), 0o600)
	if m := sessionMarks(p); m.Originator != "codex-bot" || m.First != "" {
		t.Fatalf("%+v", m)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"hello there"}]}}}`)
	_ = f.Close()
	if m := sessionMarks(p); m.First != "hello there" {
		t.Fatalf("read again once it grew: %+v", m)
	}
}
