package node

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
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
