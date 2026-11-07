package node

// The plugin cua's small settings (docs/extensions/computer-use.md "d) Small settings", #1507): a sound on the Mac when the assistant
// clicks (none, foreground clicks only, or all of them), so whoever sits at it hears it working, and whether Cua
// Driver draws the agent's cursor.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

var (
	// clickSoundFile is played (afplay, not waited for) for a call of the hub's with a click in it.
	clickSoundFile = "/System/Library/Sounds/Tink.aiff"
	afplayPath     = "/usr/bin/afplay"
)

// clickTools are Cua Driver's clicking tools; `delivery_mode: foreground` fronts the window and moves the real
// pointer, the default (background) posts to the app and moves nothing.
var clickTools = []string{"click", "double_click", "right_click"}

// clicks counts the click steps of a run_steps call: all of them, and those delivered in the foreground. A step is
// `{"click": {…}}` or `{"tool": "click", "args": {…}}`. A run_script's clicks are inside its program and not counted.
func clicks(tool string, args map[string]any) (all, foreground int) {
	if tool != "run_steps" {
		return 0, 0
	}
	steps, _ := args["steps"].([]any)
	for _, s := range steps {
		step, _ := s.(map[string]any)
		name, _ := step["tool"].(string)
		a, _ := step["args"].(map[string]any)
		if name == "" {
			for _, t := range clickTools {
				if v, ok := step[t]; ok {
					name = t
					a, _ = v.(map[string]any)
					break
				}
			}
		}
		if !isClickTool(name) {
			continue
		}
		all++
		if a["delivery_mode"] == "foreground" {
			foreground++
		}
	}
	return all, foreground
}

func isClickTool(name string) bool {
	for _, t := range clickTools {
		if name == t {
			return true
		}
	}
	return false
}

// playClicks plays the click sound once for a call that clicks, as the plugin's click_sound says: "foreground" for
// foreground clicks only, "all" for any. Once a call: its steps run inside Cua Driver in one go.
func playClicks(mode, tool string, args map[string]any) bool {
	if installOS != "darwin" || (mode != "foreground" && mode != "all") {
		return false
	}
	all, foreground := clicks(tool, args)
	if (mode == "all" && all == 0) || (mode == "foreground" && foreground == 0) {
		return false
	}
	cmd := exec.Command(afplayPath, clickSoundFile)
	if cmd.Start() != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}

// cursorOff: this node turned Cua Driver's agent cursor off (computer.cursor), so the daemon is (re)started with
// --no-overlay; turning it on again only undoes that, never a --no-overlay the user started it with.
var cursorOff atomic.Bool

// cuaDaemonArgs is the running Cua Driver daemon's command line (`ps`), or false when none runs.
var cuaDaemonArgs = func(ctx context.Context) (string, bool) {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "args=").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "/cua-driver serve") {
			return line, true
		}
	}
	return "", false
}

// setCursor (computer.cursor): Cua Driver draws the agent's cursor, or not. The overlay is the daemon's, set when it
// starts, so a running daemon the other way is stopped and started again (interrupting whatever it was doing); one
// that does not run starts the right way when next needed (startCua).
func setCursor(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		On bool `json:"on"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.On && !cursorOff.Load() {
		return map[string]any{"restarted": false}, nil // never turned off here
	}
	cursorOff.Store(!p.On)
	bin := cuaBin()
	if installOS != "darwin" || bin == "" {
		return map[string]any{"restarted": false}, nil
	}
	args, running := cuaDaemonArgs(ctx)
	if !running || strings.Contains(args, "--no-overlay") == !p.On {
		return map[string]any{"restarted": false}, nil
	}
	_, _ = cuaRun(ctx, bin, "stop")
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if _, still := cuaDaemonArgs(ctx); !still {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	startCua(ctx, bin)
	return map[string]any{"restarted": true}, nil
}
