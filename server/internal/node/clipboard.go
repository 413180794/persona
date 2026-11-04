package node

// The Mac's clipboard for the app's VNC (#1343): RFB's clipboard carries only Latin-1, so text in anything else (and
// text typed through a Chinese input method, pasted with ⌘V) goes through here, pbcopy and pbpaste. Text only.

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var pbcopyPath, pbpastePath = "/usr/bin/pbcopy", "/usr/bin/pbpaste"

// clipboardMost is the most text that goes either way.
const clipboardMost = 1 << 20

// macClipboard is computer.clipboard: {"set": text} puts text on the clipboard; {} reads it ({"text": …}).
func macClipboard(ctx context.Context, params json.RawMessage) (any, error) {
	if installOS != "darwin" {
		return nil, errors.New("只有 Mac 有这个剪贴板")
	}
	var p struct {
		Set *string `json:"set"`
	}
	_ = json.Unmarshal(params, &p)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if p.Set != nil {
		if len(*p.Set) > clipboardMost {
			return nil, errors.New("太长了")
		}
		cmd := exec.CommandContext(ctx, pbcopyPath)
		cmd.Stdin = strings.NewReader(*p.Set)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, errors.New("没放进剪贴板：" + strings.TrimSpace(string(out)))
		}
		return map[string]any{"ok": true}, nil
	}
	out, err := exec.CommandContext(ctx, pbpastePath).Output()
	if err != nil {
		return nil, errors.New("读不了剪贴板")
	}
	if len(out) > clipboardMost {
		out = out[:clipboardMost]
	}
	return map[string]any{"text": string(out)}, nil
}
