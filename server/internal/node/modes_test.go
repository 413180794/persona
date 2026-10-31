package node

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestTuiMode(t *testing.T) {
	const box = "────\n❯ \n────\n"
	cases := map[string]string{
		box + "  ⏸ manual mode on · ? for shortcuts · ← for agents": "default",
		box + "  ? for shortcuts":                               "default",
		box + "  ⏵⏵ accept edits on (shift+tab to cycle)":       "acceptEdits",
		box + "  ⏸ plan mode on (shift+tab to cycle)\n\n":       "plan",
		box + "  ⏵⏵ auto mode on (shift+tab to cycle)":          "auto",
		box + "  ⏵⏵ bypass permissions on (shift+tab to cycle)": "bypassPermissions",
		// Chat text mentioning a mode, far above the footer, does not count.
		"I turned plan mode on for you\n" + box + "\n\n\n\n\n": "",
	}
	for screen, want := range cases {
		if got := tuiMode(screen); got != want {
			t.Errorf("tuiMode(%q) = %q, want %q", screen, got, want)
		}
	}
}

func TestTuiPromptEmpty(t *testing.T) {
	for screen, want := range map[string]bool{
		"────\n❯ \n────\n  ? for shortcuts":                                         true,
		"────\n❯ Try \"create a util logging.py that...\"\n────\n  ? for shortcuts": true,
		"────\n❯ half-typed draft\n────\n  ? for shortcuts":                         false,
		"no prompt here": false,
	} {
		if got := tuiPromptEmpty(screen); got != want {
			t.Errorf("tuiPromptEmpty(%q) = %v", screen, got)
		}
	}
}

func TestValidModesAndFlags(t *testing.T) {
	if err := validModes("plan", "xhigh"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"yolo", ""}, {"", "ultra"}, {"plan; rm -rf ~", ""}} {
		if validModes(bad[0], bad[1]) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := modeFlags("acceptEdits", "low"); got != " --permission-mode acceptEdits --effort low" {
		t.Errorf("flags %q", got)
	}
	if modeFlags("", "") != "" {
		t.Error("empty flags")
	}
}

func TestCurrentModesPrefersNewer(t *testing.T) {
	n := &Node{modes: map[string]sessionModes{}}
	old := time.Now().Add(-time.Minute)
	m := quickMeta{mode: "default", modeAt: old, effort: "high", effortAt: old}
	if mode, effort := n.currentModes("s", m); mode != "default" || effort != "high" {
		t.Fatalf("transcript only: %s %s", mode, effort)
	}
	n.noteModes("s", "plan", "") // set from the app after the transcript's last word
	if mode, effort := n.currentModes("s", m); mode != "plan" || effort != "high" {
		t.Fatalf("app wins: %s %s", mode, effort)
	}
	m.mode, m.modeAt = "auto", time.Now().Add(time.Second) // then the terminal changed it again
	if mode, _ := n.currentModes("s", m); mode != "auto" {
		t.Fatalf("newer transcript wins: %s", mode)
	}
}

// Claude Code 2.1.206's /effort is a slider (←/→, Enter), not the list with "for this session only".
func TestEffortPickerAndOutcome(t *testing.T) {
	b, err := os.ReadFile("testdata/claude-2.1.206-effort-slider-46.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := effortPicker(string(b)); got != "slider" {
		t.Fatalf("slider: %q", got)
	}
	if got := effortPicker("Choose effort\n  ● high  for this session only (s)"); got != "list" {
		t.Fatalf("list: %q", got)
	}
	if got := effortPicker("❯ \n  ⏸ manual mode on"); got != "" {
		t.Fatalf("none: %q", got)
	}
	// A launcher's CLAUDE_CODE_EFFORT_LEVEL wins over the session's: told, not claimed as done.
	env := "❯ /effort\n  ⎿  CLAUDE_CODE_EFFORT_LEVEL=max overrides\n     this session — clear it and xhigh takes\n     over"
	if err := effortOutcome(env, "xhigh"); err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_EFFORT_LEVEL=max") {
		t.Fatalf("env override: %v", err)
	}
	if err := effortOutcome("  ⎿  Set effort level to high (this session)", "high"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := effortOutcome("❯ \n", "low"); err == nil {
		t.Fatal("nothing said: should not count as changed")
	}
}
