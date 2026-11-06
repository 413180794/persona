package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The click steps of a run_steps call, both ways of writing a step, and which are in the foreground.
func TestClicksCountsTheSteps(t *testing.T) {
	var args map[string]any
	_ = json.Unmarshal([]byte(`{"steps": [
	  {"set_value": {"role": "textfield", "value": "6"}},
	  {"click": {"role": "button", "name": "×"}},
	  {"tool": "double_click", "args": {"x": 3, "y": 4, "delivery_mode": "foreground"}},
	  {"right_click": {"element_token": "s0000002a:11", "delivery_mode": "background"}},
	  {"tool": "type_text", "args": {"text": "7"}},
	  {"expect": [{"text": "42"}]}
	]}`), &args)
	if all, fg := clicks("run_steps", args); all != 3 || fg != 1 {
		t.Fatalf("all %d, foreground %d", all, fg)
	}
	if all, _ := clicks("run_script", map[string]any{"script": "click()"}); all != 0 {
		t.Fatal("a script's clicks are not counted")
	}
	if all, _ := clicks("run_steps", map[string]any{"steps": "not a list"}); all != 0 {
		t.Fatal("steps that are not a list")
	}
}

// "Click sound": none plays nothing; foreground only for a foreground click; all for any; once a call.
func TestTheClickSoundAsTheSettingSays(t *testing.T) {
	dir := t.TempDir()
	played := filepath.Join(dir, "played")
	bin := filepath.Join(dir, "afplay")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$1\" >> "+played+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath, oldOS := afplayPath, installOS
	afplayPath, installOS = bin, "darwin"
	t.Cleanup(func() { afplayPath, installOS = oldPath, oldOS })
	background := map[string]any{"steps": []any{map[string]any{"click": map[string]any{"name": "AC"}}, map[string]any{"click": map[string]any{"name": "6"}}}}
	foreground := map[string]any{"steps": []any{map[string]any{"tool": "click", "args": map[string]any{"x": 1.0, "y": 2.0, "delivery_mode": "foreground"}}}}

	for _, c := range []struct {
		mode string
		args map[string]any
		want bool
	}{
		{"", background, false}, {"none", foreground, false}, {"foreground", background, false},
		{"foreground", foreground, true}, {"all", background, true}, {"all", map[string]any{"steps": []any{}}, false},
	} {
		if got := playClicks(c.mode, "run_steps", c.args); got != c.want {
			t.Errorf("%s %v: played %v", c.mode, c.args, got)
		}
	}
	got := ""
	for deadline := time.Now().Add(3 * time.Second); strings.Count(got, "\n") < 2 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(played)
		got = string(b)
	}
	if got != clickSoundFile+"\n"+clickSoundFile+"\n" {
		t.Fatalf("played: %q", got)
	}
	installOS = "linux"
	if playClicks("all", "run_steps", background) {
		t.Fatal("only on a Mac")
	}
}

// "Show the agent's cursor": off restarts a daemon that draws it with --no-overlay, on again restarts it without; a daemon
// already that way, or none running, is left alone; on never undoes a --no-overlay this node did not ask for.
func TestTheAgentCursorRestartsTheDaemonOnlyToChangeIt(t *testing.T) {
	dir := t.TempDir()
	running, calls := filepath.Join(dir, "running"), filepath.Join(dir, "calls")
	cua := filepath.Join(dir, "cua-driver")
	open := filepath.Join(dir, "open")
	write := func(path, script string) {
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Cua: stop ends the daemon; status says whether it runs. open: the app starts it, with what it was given.
	write(cua, "#!/bin/sh\necho \"cua $*\" >> "+calls+"\ncase \"$1\" in\nstop) rm -f "+running+" ;;\n"+
		"status) [ -f "+running+" ] && echo 'Cua Driver daemon is running' || echo 'Cua Driver daemon is not running' ;;\nesac\n")
	write(open, "#!/bin/sh\necho \"open $*\" >> "+calls+"\nshift 3\n[ \"$1\" = --args ] && shift\n[ $# -eq 0 ] && set -- serve\n"+
		"echo \"/Applications/CuaDriver.app/Contents/MacOS/cua-driver $*\" > "+running+"\n")
	oldBin, oldOpen, oldArgs, oldOS := cuaBin, openPath, cuaDaemonArgs, installOS
	cuaBin, openPath, installOS = func() string { return cua }, open, "darwin"
	cuaDaemonArgs = func(context.Context) (string, bool) {
		b, err := os.ReadFile(running)
		return strings.TrimSpace(string(b)), err == nil
	}
	t.Cleanup(func() {
		cuaBin, openPath, cuaDaemonArgs, installOS = oldBin, oldOpen, oldArgs, oldOS
		cursorOff.Store(false)
	})
	ctx := context.Background()
	set := func(on bool) bool {
		t.Helper()
		out, err := setCursor(ctx, json.RawMessage(`{"on": `+map[bool]string{true: "true", false: "false"}[on]+`}`))
		if err != nil {
			t.Fatal(err)
		}
		return out.(map[string]any)["restarted"].(bool)
	}
	daemon := func() string { b, _ := os.ReadFile(running); return strings.TrimSpace(string(b)) }

	write(running, "/Applications/CuaDriver.app/Contents/MacOS/cua-driver serve\n")
	if set(true) {
		t.Fatal("on, and never turned off here: left alone")
	}
	if !set(false) || !strings.HasSuffix(daemon(), "cua-driver serve --no-overlay") {
		t.Fatalf("off: restarted without the overlay: %q", daemon())
	}
	if c, _ := os.ReadFile(calls); !strings.Contains(string(c), "cua stop") || !strings.Contains(string(c), "open -g -b "+cuaBundle+" --args serve --no-overlay") {
		t.Fatalf("stopped, then opened with --no-overlay: %s", c)
	}
	if set(false) {
		t.Fatal("off again: already that way")
	}
	if !set(true) || strings.Contains(daemon(), "--no-overlay") {
		t.Fatalf("on again: restarted with it: %q", daemon())
	}

	// A daemon the user started with --no-overlay themselves stays that way.
	write(running, "/Applications/CuaDriver.app/Contents/MacOS/cua-driver serve --no-overlay\n")
	if set(true) || !strings.Contains(daemon(), "--no-overlay") {
		t.Fatal("not this node's to undo")
	}
	// None running: nothing to restart, and the next start is without the overlay.
	_ = os.Remove(running)
	if set(false) || !cursorOff.Load() {
		t.Fatal("none running")
	}
	startCua(ctx, cua)
	if !strings.Contains(daemon(), "--no-overlay") {
		t.Fatalf("started later, the way it was asked: %q", daemon())
	}
}
