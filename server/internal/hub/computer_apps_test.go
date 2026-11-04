package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCallApps(t *testing.T) {
	for _, c := range []struct {
		name, app, script, steps string
		want                     string
		whole                    bool
	}{
		{"computer_state", "", "", "", wholeScreen, true},
		{"computer_state", "Calculator", "", "", "Calculator", true},
		{"computer_run", "", `const a = await cua.getApp("Calculator"); const b = await cua.getApp('Notes');`, "", "Calculator,Notes", true},
		{"computer_run", "", `const a = await cua.getApp(name);`, "", "", false},
		{"computer_run", "", `const a = await cua.getApp("Calculator"); const b = await cua.getApp(other);`, "", "Calculator", false},
		{"computer_run", "", `return 1`, "", "", false},
		{"computer_run", "", "", `[{"click": {"app": "Calculator", "name": "6"}}, {"click": {"app": "Calculator", "name": "7"}}]`, "Calculator", true},
		{"computer_run", "Notes", "", `[{"tool": "press_key", "args": {"key": "return"}}]`, "Notes", true},
		{"computer_run", "", "", `[{"tool": "press_key", "args": {"key": "return"}}]`, "", false},
	} {
		apps, whole := callApps(c.name, c.app, c.script, json.RawMessage(c.steps))
		if strings.Join(apps, ",") != c.want || whole != c.whole {
			t.Errorf("%s %q %q %q: %v %v, want %q %v", c.name, c.app, c.script, c.steps, apps, whole, c.want, c.whole)
		}
	}
}

// answerCard waits for the phone card the call raises and answers it.
func answerCard(t *testing.T, h *Hub, allow, always bool) proto.Interaction {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p := h.pendingInteractions(); len(p) > 0 {
			if err := h.resolve(p[0].ID, proto.Resolution{Allow: &allow, Always: always}); err != nil {
				t.Fatal(err)
			}
			return p[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no card")
	return proto.Interaction{}
}

// The first time an app is operated on a Mac the phone is asked; 一直允许 keeps it (by bundle id) until removed. A
// terminal is asked about every time, with no 一直允许, and so is a script whose apps cannot all be read.
func TestComputerAppsAreAskedOnce(t *testing.T) {
	h, _ := pluginTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	_, seen := computerMac(t, h, "mac")
	if err := h.plugins.reg.SetEnabled(ctx, cuaPluginID, true, "test"); err != nil {
		t.Fatal(err)
	}
	if err := h.plugins.reg.SetConfig(ctx, cuaPluginID, json.RawMessage(`{"machines": ["mac"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	run := func(steps string) chan proto.ToolResult {
		out := make(chan proto.ToolResult, 1)
		go func() {
			out <- h.callComputerTool(ctx, "conv1234", "computer_run", json.RawMessage(`{"machine": "mac", "steps": `+steps+`}`))
		}()
		return out
	}
	calc := `[{"click": {"app": "Calculator", "name": "6"}}]`

	done := run(calc)
	card := answerCard(t, h, true, true)
	if !strings.Contains(card.Summary, "「Calculator」") || card.AlwaysLabel != "以后操作「Calculator」不再问" {
		t.Fatalf("asked about Calculator, with 一直允许: %+v", card)
	}
	if res := <-done; res.IsError {
		t.Fatalf("%+v", res)
	}
	short, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if res := h.callComputerTool(short, "conv1234", "computer_run", json.RawMessage(`{"machine": "mac", "steps": `+calc+`}`)); res.IsError {
		t.Fatalf("always allowed: not asked again: %+v", res)
	}
	code, out := call(t, mux, "GET", "/api/v1/machines/mac/computer/apps", h.cfg.Token, "")
	apps, _ := out["apps"].([]any)
	if code != 200 || len(apps) != 1 || apps[0].(map[string]any)["bundle_id"] != "com.apple.calculator" || apps[0].(map[string]any)["name"] != "Calculator" {
		t.Fatalf("kept by bundle id: %d %v", code, out)
	}

	term := `[{"click": {"app": "Terminal", "name": "x"}}]`
	for range 2 {
		done = run(term)
		if card = answerCard(t, h, true, true); card.AlwaysLabel != "" {
			t.Fatalf("a terminal: asked every time, never kept: %+v", card)
		}
		<-done
	}
	go func() { _ = answerCard(t, h, false, false) }()
	if res := h.callComputerTool(ctx, "conv1234", "computer_run", json.RawMessage(`{"machine": "mac", "script": "const a = await cua.getApp(name); return 1"}`)); !res.IsError ||
		!strings.Contains(res.Content[0].Text, "declined") {
		t.Fatalf("a script naming its apps by expression: asked, declined: %+v", res)
	}

	if code, out := call(t, mux, "DELETE", "/api/v1/machines/mac/computer/apps/com.apple.calculator", h.cfg.Token, ""); code != 200 || len(out["apps"].([]any)) != 0 {
		t.Fatalf("removed: %d %v", code, out)
	}
	done = run(calc)
	answerCard(t, h, true, false)
	<-done

	if err := h.plugins.reg.SetConfig(ctx, cuaPluginID, json.RawMessage(`{"machines": ["mac"], "app_approval": false}`), "test"); err != nil {
		t.Fatal(err)
	}
	_, before := seen()
	if res := h.callComputerTool(short, "conv1234", "computer_run", json.RawMessage(`{"machine": "mac", "steps": `+term+`}`)); res.IsError {
		t.Fatalf("turned off: nothing asked: %+v", res)
	}
	if _, after := seen(); len(after) != len(before)+1 {
		t.Fatal("turned off: no list_apps either, only the call")
	}
}
