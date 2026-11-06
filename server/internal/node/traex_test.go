package node

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

func TestTraexOwners(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home}
	dir := filepath.Join(n.traexHome(), "thread-writer-locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(thread string, pid int) {
		b := []byte(`{"pid":` + strconv.Itoa(pid) + `,"process_name":"TRAE CLI"}`)
		if err := os.WriteFile(filepath.Join(dir, thread+".owner.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0f000000-0000-4000-8000-0000000000a1", os.Getpid())
	write("0f000000-0000-4000-8000-0000000000a2", 1<<30) // no such process
	got := n.traexOwners()
	if len(got) != 1 || got[os.Getpid()] != "0f000000-0000-4000-8000-0000000000a1" {
		t.Fatalf("owners = %v", got)
	}
}

func TestTraexChoice(t *testing.T) {
	blocked := "Would you like to run the following command?\n\n  $ rm -rf build\n\n› 1. Yes, proceed (y)\n  2. No, and tell Codex what to do differently (esc)\n"
	if !traexChoiceRe.MatchString(blocked) {
		t.Fatal("an approval prompt should read as blocked")
	}
	idle := "◆ M2 已通过 vet/test。\n\n1. 第一步\n2. 第二步\n─────\n❯ Run /review on my current changes\n"
	if traexChoiceRe.MatchString(idle) {
		t.Fatal("a numbered list in a reply and the input prompt are not a choice")
	}
}

func TestPaneOfPIDEnv(t *testing.T) {
	env := []byte("PATH=/bin\x00HERDR_ENV=1\x00HERDR_PANE_ID=w79:p2\x00HERDR_TAB_ID=w79:t2\x00")
	if m := herdrPaneEnvRe.FindSubmatch(env); m == nil || string(m[1]) != "w79:p2" {
		t.Fatalf("linux environ: %q", m)
	}
	ps := []byte("traex HOME=/Users/x HERDR_PANE_ID=w2:p1 TERM=xterm\n")
	if m := herdrPaneEnvRe.FindSubmatch(ps); m == nil || string(m[1]) != "w2:p1" {
		t.Fatalf("ps -E: %q", m)
	}
}

// writeMetaRollout writes a rollout whose first line is a session_meta shaped like a compatible CLI / Codex ones (all values made up).
func writeMetaRollout(t *testing.T, home, id, payload string, mtime time.Time) {
	t.Helper()
	day := filepath.Join(home, "sessions", mtime.Format("2006"), mtime.Format("01"), mtime.Format("02"))
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(day, "rollout-"+mtime.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	line := `{"timestamp":"` + mtime.UTC().Format(time.RFC3339) + `","type":"session_meta","payload":` + payload + "}\n"
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func writeOwner(t *testing.T, n *Node, thread string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(n.traexHome(), "thread-writer-locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, thread+".owner.json")
	if err := os.WriteFile(p, []byte(`{"pid":`+strconv.Itoa(os.Getpid())+`,"process_name":"TRAE CLI"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

const (
	threadMain     = "01a00000-0000-7000-8000-00000000000a"
	threadGuardian = "01a00000-0000-7000-8000-00000000000b"
	threadOld      = "01a00000-0000-7000-8000-00000000000c"
	threadNew      = "01a00000-0000-7000-8000-00000000000d"
)

// The guardian that reviews the compatible CLI's tool calls writes a thread of its own from the same process, with a newer owner
// file than the conversation's; the pane runs the conversation (2026-11-01: the app showed the review).
func TestTraexOwnersPickTheConversation(t *testing.T) {
	now := time.Now()
	setup := func(t *testing.T, owners ...string) *Node {
		n := &Node{home: t.TempDir()}
		writeMetaRollout(t, n.traexHome(), threadMain, `{"id":"`+threadMain+`","session_id":"`+threadMain+`","cwd":"/work/app","source":"cli","thread_source":"user"}`, now.Add(-time.Hour))
		writeMetaRollout(t, n.traexHome(), threadGuardian, `{"id":"`+threadGuardian+`","session_id":"`+threadMain+`","forked_from_id":"`+threadMain+`","cwd":"/work/app","source":{"subagent":{"other":"guardian"}},"thread_source":"subagent"}`, now)
		for i, o := range owners {
			writeOwner(t, n, o, now.Add(time.Duration(i-len(owners))*time.Minute))
		}
		return n
	}
	if got := setup(t, threadMain, threadGuardian).traexOwners()[os.Getpid()]; got != threadMain {
		t.Fatalf("conversation and guardian: got %s", got)
	}
	if got := setup(t, threadGuardian).traexOwners()[os.Getpid()]; got != threadMain {
		t.Fatalf("only the guardian's owner file: got %s, want its conversation", got)
	}

	// a compatible CLI versions whose session_meta says nothing about subagents: the newest thread, as before.
	n := &Node{home: t.TempDir()}
	writeMetaRollout(t, n.traexHome(), threadOld, `{"id":"`+threadOld+`","cwd":"/work/app","source":"exec"}`, now.Add(-time.Hour))
	writeMetaRollout(t, n.traexHome(), threadNew, `{"id":"`+threadNew+`","cwd":"/work/app","source":"exec"}`, now)
	writeOwner(t, n, threadOld, now.Add(-time.Hour))
	writeOwner(t, n, threadNew, now)
	if got := n.traexOwners()[os.Getpid()]; got != threadNew {
		t.Fatalf("legacy threads: got %s", got)
	}
}

// Codex writes its thread_spawn subagents' rollouts next to the conversation, in the same directory and newer.
func TestCodexSessionSkipsSubagents(t *testing.T) {
	now := time.Now()
	n := &Node{home: t.TempDir()}
	writeMetaRollout(t, n.codexHome(), threadMain, `{"id":"`+threadMain+`","session_id":"`+threadMain+`","cwd":"/work/app","source":"cli","thread_source":"user"}`, now.Add(-time.Hour))
	writeMetaRollout(t, n.codexHome(), threadNew, `{"id":"`+threadNew+`","session_id":"`+threadMain+`","parent_thread_id":"`+threadMain+`","cwd":"/work/app","source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+threadMain+`","depth":1}}},"thread_source":"subagent"}`, now)
	if got := n.codexSession(host.Agent{Cwd: "/work/app"}, map[string]host.Agent{}); got != threadMain {
		t.Fatalf("codexSession = %s, want the conversation", got)
	}
	if got := mainThread(n.codexHome(), threadNew); got != threadMain {
		t.Fatalf("a subagent maps to %s", got)
	}
	if got := mainThread(n.codexHome(), threadMain); got != threadMain {
		t.Fatalf("the conversation maps to %s", got)
	}

	// A rollout older than the scan window is still read on demand.
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local)
	writeMetaRollout(t, n.codexHome(), threadOld, `{"id":"`+threadOld+`","session_id":"`+threadMain+`","cwd":"/work/app","source":{"subagent":{"other":"guardian"}}}`, old)
	if got := mainThread(n.codexHome(), threadOld); got != threadMain {
		t.Fatalf("an old subagent maps to %s", got)
	}
}
