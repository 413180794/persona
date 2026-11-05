package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The node's launcher repair runs only its fixed update, and says so for a launcher or an action it does not cover.
func TestLauncherRepair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ZDOTDIR", home)
	n := &Node{dir: t.TempDir()}
	old := claudeUpdateScript
	t.Cleanup(func() { claudeUpdateScript = old })

	if r, err := n.launcherRepair(context.Background(), "claude", "rm -rf /"); err != nil || !r.Unsupported || r.OK {
		t.Fatalf("unknown action: %+v %v", r, err)
	}
	claudeUpdateScript = "echo Successfully updated"
	if r, err := n.launcherRepair(context.Background(), "claude", launcherRepairUpgrade); err != nil || !r.OK || r.Output != "Successfully updated" {
		t.Fatalf("update: %+v %v", r, err)
	}
	claudeUpdateScript = "echo no network >&2; exit 3"
	if r, err := n.launcherRepair(context.Background(), "claude", launcherRepairUpgrade); err != nil || r.OK || r.Output != "no network" {
		t.Fatalf("failed update: %+v %v", r, err)
	}
}

// What the user's shell startup prints is not the repair's output: Ubuntu's sudo hint from the rc file (a hub machine
// with a fresh HOME, #3689) still leaves just the update's own line.
func TestLauncherRepairDropsRCNoise(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ZDOTDIR", home)
	hint := "cat <<'EOF'\nTo run a command as administrator (user \"root\"), use \"sudo <command>\".\nSee \"man sudo_root\" for details.\n\nEOF\n"
	for _, rc := range []string{".bashrc", ".zshrc"} {
		if err := os.WriteFile(filepath.Join(home, rc), []byte(hint), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n := &Node{dir: t.TempDir()}
	old := claudeUpdateScript
	t.Cleanup(func() { claudeUpdateScript = old })
	claudeUpdateScript = "echo Successfully updated"
	if r, err := n.launcherRepair(context.Background(), "claude", launcherRepairUpgrade); err != nil || !r.OK || r.Output != "Successfully updated" {
		t.Fatalf("update: %+v %v", r, err)
	}
}

// Without a terminal an interactive bash warns about job control; those lines are dropped, the command's own stay.
func TestDropShellStartupNoise(t *testing.T) {
	out := "bash: cannot set terminal process group (-1): Inappropriate ioctl for device\n" +
		"bash: no job control in this shell\n" +
		"/usr/bin/bash: no job control in this shell\r\n" +
		"To run a command as administrator (user \"root\"), use \"sudo <command>\".\n" +
		"See \"man sudo_root\" for details.\n" +
		"Successfully updated\n" +
		"note: no job control in this shell, said the script\n" +
		"exit\n" +
		"no job control in this shell\n" +
		"exit\n"
	want := "Successfully updated\nnote: no job control in this shell, said the script\nexit\nno job control in this shell"
	if got := dropShellStartupNoise(out); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
