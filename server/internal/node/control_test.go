package node

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// foreground is pane.process_info's answer with these programs in front (name, argv0).
func foreground(procs ...[2]string) map[string]any {
	fg := []any{}
	for i, p := range procs {
		fg = append(fg, map[string]any{"pid": 100 + i, "name": p[0], "argv0": p[1]})
	}
	return map[string]any{"process_info": map[string]any{"foreground_processes": fg}}
}

// An agent herdr does not know (or no longer knows) gets the text typed only while something other than a shell runs
// in front of the pane (#1185). Once the agent exits the shell is in front: herdr lists it, so "any process" lets the
// text through, and the enter after a paste runs it all. Nothing is typed then, and the check failing types nothing.
func TestPromptFallbackRefusesAShellInFront(t *testing.T) {
	for _, tc := range []struct {
		name  string
		info  any
		shell bool // refused because only a shell is in front (the phone says the agent exited)
	}{
		{"login shell", foreground([2]string{"zsh", "-zsh"}), true},
		{"shell by path", foreground([2]string{"bash", "/bin/bash"}), true},
		{"a shell running a shell", foreground([2]string{"zsh", "-zsh"}, [2]string{"fish", "fish"}), true},
		{"no processes", foreground(), true},
		{"process lookup failed", herdrFail{Code: "internal", Message: "process lookup failed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, _ := herdrNode(t)
			f.reply("agent.prompt", herdrFail{Code: "agent_not_found", Message: "agent exited"})
			f.reply("pane.process_info", tc.info)
			const pane, text = "w1:p1", "touch /tmp/ran-in-shell\nsecond line"
			err := n.prompt(context.Background(), pane, text)
			if !errors.Is(err, host.ErrNotDelivered) || errors.Is(err, errShellOnly) != tc.shell {
				t.Fatalf("prompt = %v, want not delivered (shell only: %v)", err, tc.shell)
			}
			assertTrace(t, f.trace(), []string{
				call("agent.prompt", map[string]any{"target": pane, "text": text}),
				call("pane.process_info", map[string]any{"pane_id": pane}),
			})
		})
	}
}

// What the phone gets for it: a refusal that nothing was sent, saying the agent exited.
func TestSendToAnExitedAgentIsRefused(t *testing.T) {
	n, f, _ := herdrNode(t)
	seedAgents(t, n, f, agentOf("codex", sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp/c", ""))
	f.reply("agent.prompt", herdrFail{Code: "agent_not_found", Message: "no agent in pane"})
	f.reply("pane.process_info", foreground([2]string{"zsh", "-zsh"}))
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "rm -rf build"})
	code, data := refusalOf(t, err)
	if code != notRunning || data["delivered"] != deliveredNone || !strings.Contains(err.Error(), "已经退出") {
		t.Fatalf("refusal %q %v: %v", code, data, err)
	}
	if typed := f.trace("pane.send_text"); len(typed) > 0 {
		t.Fatalf("typed into the shell: %v", typed)
	}
}

// An agent herdr does not detect (TRAEX) in front, its shell behind it: the text goes as a paste (an escape in it
// cannot end the paste early), then enter.
func TestPromptFallbackPastesSafelyBeforeEnter(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("agent.prompt", herdrFail{Code: "agent_not_found", Message: "unrecognized agent"})
	f.reply("pane.process_info", foreground([2]string{"zsh", "-zsh"}, [2]string{"node", "traex"}))
	const pane = "w1:p1"
	const text = "line one\n\x1b[201~echo unsafe\n\x1b[200~line two"
	if err := n.prompt(context.Background(), pane, text); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		call("agent.prompt", map[string]any{"target": pane, "text": text}),
		call("pane.process_info", map[string]any{"pane_id": pane}),
		call("pane.send_text", map[string]any{"pane_id": pane,
			"text": "\x1b[200~line one\n<ESC>[201~echo unsafe\n<ESC>[200~line two\x1b[201~"}),
		sendKeys(pane, "enter"),
	})
}

func TestFindBlock(t *testing.T) {
	all := []string{"a", "b", "c", "a", "b", "d"}
	if got := findBlock(all, []string{"a", "b"}); got != 3 {
		t.Fatalf("last match: %d", got)
	}
	if got := findBlock(all, []string{"c", "a "}); got != 2 {
		t.Fatalf("trailing spaces: %d", got)
	}
	if got := findBlock(all, []string{"x"}); got != -1 {
		t.Fatalf("missing: %d", got)
	}
}

func TestNearestBlockPicksTheOneWhereWeAre(t *testing.T) {
	// the same screen pushed three times; the phone has the last 4 lines → its block starts near 6
	all := []string{"x", "a", "b", "y", "a", "b", "z", "a", "b", "w"}
	if got := nearestBlock(all, []string{"a", "b"}, len(all)-4); got != 7 {
		t.Fatalf("nearest to 6: %d", got)
	}
	if got := nearestBlock(all, []string{"a", "b"}, 2); got != 1 {
		t.Fatalf("nearest to 2: %d", got)
	}
}
