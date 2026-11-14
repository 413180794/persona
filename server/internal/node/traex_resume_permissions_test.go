package node

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

func assertTraexHerdrResumeCommand(t *testing.T, argv, want []string) {
	t.Helper()
	if len(argv) != 2 || argv[0] != "eval" || !resumeSayable(argv[1]) {
		t.Fatalf("invalid herdr resume argv: %q", argv)
	}
	words := shellWords(argv[1])
	i := slices.Index(words, want[0])
	if i < 0 || !slices.Equal(words[i:], want) {
		t.Fatalf("resume command = %q, want root options before resume: %q", words, want)
	}
}

func assertTraexHerdrResumeReport(t *testing.T, f *fakeHerdr, want []string) {
	t.Helper()
	var reports int
	// Startup registration and background backfill can overlap; every report must retain the saved policy.
	for _, c := range f.calls() {
		if c.Method != "pane.report_agent_session" {
			continue
		}
		reports++
		if c.Params["agent"] != traexAgent || c.Params["agent_session_id"] != want[len(want)-1] {
			t.Fatalf("resume report lost the TRAE session: %v", c.Params)
		}
		raw, ok := c.Params["resume_argv"].([]any)
		if !ok {
			t.Fatalf("resume report has no command: %v", c.Params)
		}
		var argv []string
		for _, arg := range raw {
			argv = append(argv, str(arg))
		}
		assertTraexHerdrResumeCommand(t, argv, want)
	}
	if reports == 0 {
		t.Fatal("no TRAE resume command was reported")
	}
}

func TestTraexHerdrResumeArgvPreservesSavedPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, launcher, mode string
		args                 []string
		managed              bool
	}{
		{"readonly", traexAgent, "plan", []string{"--sandbox", "read-only", "--ask-for-approval", "never"}, false},
		{"bypass", traexAgent, proto.ModeBypass, []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}, false},
		{"default", traexAgent, "", nil, false},
		{"alias-readonly", "review_wrapper", "plan", []string{"--sandbox", "read-only", "--ask-for-approval", "never"}, false},
		{"managed-readonly", "managed-traex", "plan", []string{"--sandbox", "read-only", "--ask-for-approval", "never"}, true},
		{"managed-bypass", "managed-traex", proto.ModeBypass, []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, home := testNode(t)
			n.dir, n.bin = filepath.Join(home, "persona dir"), filepath.Join(home, "persona bin", "personad")
			n.state.put(threadNew, sessionState{Launcher: tc.launcher, Cwd: home, AppMode: tc.mode, AppEffort: "max"})
			prefix := []string{tc.launcher}
			if tc.managed {
				n.setManaged([]proto.ManagedLauncher{{Name: tc.launcher, Agent: traexAgent}})
				prefix = []string{n.bin, "launch", tc.launcher}
			}
			argv, ok := n.agentResumeArgv(agentLaunch{Agent: traexAgent, Launcher: tc.launcher}, threadNew)
			if !ok {
				t.Fatal("no restore command for saved TRAE permissions")
			}
			want := append(slices.Clone(prefix), tc.args...)
			want = append(want, "resume", threadNew)
			assertTraexHerdrResumeCommand(t, argv, want)
		})
	}
}

func TestTraexHerdrResumeBackfillAndNewServerPreservePermissions(t *testing.T) {
	for _, tc := range []struct {
		mode string
		args []string
	}{
		{"plan", []string{"--sandbox", "read-only", "--ask-for-approval", "never"}},
		{proto.ModeBypass, []string{"--sandbox", "danger-full-access", "--ask-for-approval", "never"}},
		{"", nil},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			n, f, home := herdrNode(t)
			n.state.put(threadNew, sessionState{Pane: "w1:p2", Launcher: traexAgent, Cwd: home, AppMode: tc.mode})
			agents := []host.Agent{{Agent: traexAgent, PaneID: "w1:p2", AgentSession: &host.AgentSession{Kind: "id", Value: threadNew}}}
			want := append([]string{traexAgent}, tc.args...)
			want = append(want, "resume", threadNew)

			// No launch metadata survived in memory: reconstruct the command from persisted session state.
			n.backfillResume(agents)
			waitCalls(t, f, "pane.report_agent_session", 1)
			waitReports(n, "w1:p2")
			assertTraexHerdrResumeReport(t, f, want)

			f.reset()
			n.backfillResume(agents)
			waitReports(n, "w1:p2")
			if got := f.trace("pane.report_agent_session"); len(got) != 0 {
				t.Fatalf("reported again to the same server: %v", got)
			}

			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(f.sock, later, later); err != nil {
				t.Fatal(err)
			}
			n.backfillResume(agents)
			waitCalls(t, f, "pane.report_agent_session", 1)
			waitReports(n, "w1:p2")
			assertTraexHerdrResumeReport(t, f, want)
		})
	}
}
