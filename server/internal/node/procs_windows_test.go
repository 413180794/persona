package node

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProcHelper is the child TestListWinProcsReadsTheChild reads: it waits until killed.
func TestProcHelper(t *testing.T) {
	if os.Getenv("PERSONA_PROC_HELPER") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(time.Minute)
}

// listWinProcs reads another process's command line, working directory, start time and only the variables asked for
// (a value with spaces whole, a name in another case), from its PEB.
func TestListWinProcsReadsTheChild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcHelper$", "arg with space")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PERSONA_PROC_HELPER=1", "ORCA_PANE_KEY=tab1:leaf1", `Claude_Config_Dir=C:\Users\A B\.claude`,
		"PERSONA_SECRET=never-kept")
	before := time.Now().Add(-time.Second)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	self := imageName(os.Args[0])
	var got *winProc
	for deadline := time.Now().Add(10 * time.Second); got == nil && time.Now().Before(deadline); {
		procs, parents, err := listWinProcs(func(name string) bool { return name == self },
			"ORCA_PANE_KEY", "CLAUDE_CONFIG_DIR")
		if err != nil {
			t.Fatal(err)
		}
		if parents[cmd.Process.Pid] != os.Getpid() {
			t.Fatalf("parent of the child %d, want %d", parents[cmd.Process.Pid], os.Getpid())
		}
		for i := range procs {
			if procs[i].pid == cmd.Process.Pid && procs[i].env["ORCA_PANE_KEY"] != "" {
				got = &procs[i]
			}
		}
		if got == nil {
			time.Sleep(50 * time.Millisecond) // its environment is there once it has started
		}
	}
	if got == nil {
		t.Fatal("the child is not listed")
	}
	if got.ppid != os.Getpid() || got.name != self {
		t.Errorf("ppid %d name %q", got.ppid, got.name)
	}
	if len(got.argv) != 3 || got.argv[2] != "arg with space" {
		t.Errorf("argv %q", got.argv)
	}
	if !strings.EqualFold(got.cwd, dir) {
		t.Errorf("cwd %q, want %q", got.cwd, dir)
	}
	if got.start.Before(before) || got.start.After(time.Now()) {
		t.Errorf("start %v", got.start)
	}
	want := map[string]string{"ORCA_PANE_KEY": "tab1:leaf1", "CLAUDE_CONFIG_DIR": `C:\Users\A B\.claude`}
	if len(got.env) != len(want) || got.env["ORCA_PANE_KEY"] != want["ORCA_PANE_KEY"] ||
		got.env["CLAUDE_CONFIG_DIR"] != want["CLAUDE_CONFIG_DIR"] {
		t.Errorf("env %q, want %q", got.env, want)
	}
}

func TestNestedAgent(t *testing.T) {
	// 10 claude (pane A) → 11 bash → 12 claude (pane A, started later): nested. 20 claude (pane B) → 21 claude
	// (pane A): another pane's agent is no ancestor. 30 claude (pane A), its parent's pid 10 reused by a claude
	// started after it: not nested.
	parents := map[int]int{10: 1, 11: 10, 12: 11, 20: 1, 21: 20, 30: 10, 1: 0}
	agents := map[int]struct {
		pane  string
		start int64
	}{10: {"A", 100}, 12: {"A", 200}, 20: {"B", 100}, 21: {"A", 200}, 30: {"A", 50}}
	of := func(pid int) (string, int64, bool) { a, ok := agents[pid]; return a.pane, a.start, ok }
	for pid, want := range map[int]bool{10: false, 12: true, 20: false, 21: false, 30: false} {
		if got := nestedAgent(pid, agents[pid].pane, agents[pid].start, parents, of); got != want {
			t.Errorf("pid %d nested %v, want %v", pid, got, want)
		}
	}
	// A cycle in the parents (pids reused) ends.
	if nestedAgent(40, "A", 0, map[int]int{40: 41, 41: 40}, of) {
		t.Error("cycle")
	}
}

func TestPickEnvAndNames(t *testing.T) {
	env := pickEnv("=C:=C:\\work\x00Path=C:\\bin\x00orca_pane_key=t:l\x00HERDR_PANE_ID=w1:p1\x00TOKEN=x\x00",
		[]string{"ORCA_PANE_KEY", "HERDR_PANE_ID"})
	if len(env) != 2 || env["ORCA_PANE_KEY"] != "t:l" || env["HERDR_PANE_ID"] != "w1:p1" {
		t.Errorf("env %q", env)
	}
	if orcaPaneOf(envListing(map[string]string{"ORCA_PANE_KEY": "t:l"})) != "t:l" ||
		orcaPaneOf(envListing(env)) != "" { // inside a herdr nested in Orca's terminal
		t.Error("orcaPaneOf on a picked environment")
	}
	for in, want := range map[string]string{`C:\work\`: `C:\work`, `C:\`: `C:\`, `C:\work`: `C:\work`} {
		if got := trimDirSep(in); got != want {
			t.Errorf("trimDirSep(%q) = %q", in, got)
		}
	}
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{`C:\Users\A B\.local\bin\claude.exe`, "--resume"}, "claude"},
		{[]string{`CLAUDE.EXE`}, "claude"},
		{[]string{`C:\Program Files\nodejs\node.exe`, `C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`}, "claude"},
		{[]string{`C:\Program Files\nodejs\node.exe`, `C:\npm\node_modules\@openai\codex\bin\codex.js`}, "codex"},
		{[]string{`C:\tools\codex.exe`}, "codex"},
		{[]string{`C:\Program Files\nodejs\node.exe`, `server.js`}, ""},
	} {
		if got := agentOfCmd(c.argv); got != c.want {
			t.Errorf("agentOfCmd(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
}

// loginShell never resolves to System32's bash.exe, which runs WSL.
func TestLoginShellIsNotWSL(t *testing.T) {
	sh := loginShellProgram()
	if sh == "" {
		t.Skip("no Git Bash on this machine")
	}
	if sys := os.Getenv("SystemRoot"); sys != "" && strings.HasPrefix(strings.ToLower(sh), strings.ToLower(sys)) {
		t.Errorf("login shell %s", sh)
	}
}
