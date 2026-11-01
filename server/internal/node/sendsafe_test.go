package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
)

func screenFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "screen", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakePane is a herdr socket serving one pane: reads return screens[state], send_keys moves to the next state.
type fakePane struct {
	mu      sync.Mutex
	screens []string
	state   int
	keys    [][]string
	// clearAfter: after this many reads the pane moves to the next screen by itself (the TUI catching up).
	clearAfter int
	reads      int
}

func (f *fakePane) serve(t *testing.T) *herdr.Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "hp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			var req struct {
				ID     string         `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.Unmarshal(line, &req)
			f.mu.Lock()
			var result any = map[string]any{}
			switch req.Method {
			case "pane.read":
				result = map[string]any{"read": map[string]any{"text": f.screens[f.state]}}
				if f.reads++; f.clearAfter > 0 && f.reads >= f.clearAfter && f.state+1 < len(f.screens) {
					f.state++
				}
			case "pane.send_keys":
				var keys []string
				for _, k := range req.Params["keys"].([]any) {
					keys = append(keys, k.(string))
				}
				f.keys = append(f.keys, keys)
				if f.state+1 < len(f.screens) {
					f.state++
				}
			}
			f.mu.Unlock()
			b, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
			_, _ = c.Write(append(b, '\n'))
			c.Close()
		}
	}()
	return &herdr.Client{Socket: sock}
}

func refusalOf(t *testing.T, err error) (string, map[string]any) {
	t.Helper()
	var re *rpc.Error
	if !errors.As(err, &re) {
		t.Fatalf("not a refusal: %v", err)
	}
	data := map[string]any{}
	_ = json.Unmarshal(re.Data, &data)
	return re.Code, data
}

func TestCheckPaneRefusesDraftAndClearsWhenAsked(t *testing.T) {
	typed := screenFixture(t, "claude-2.1.312-typed-162.ansi")
	f := &fakePane{screens: []string{typed, screenFixture(t, "claude-2.1.312-cleared-162.ansi")}}
	n := &Node{hosts: herdrHosts(f.serve(t))}
	ctx := context.Background()

	code, data := refusalOf(t, n.checkPane(ctx, "w1:p1", "claude", ""))
	if code != "input_draft" || data["delivered"] != "none" || data["clearable"] != "fix the flaky test" {
		t.Fatalf("refusal %s %v", code, data)
	}
	// The user saw another draft: nothing is cleared.
	if code, _ := refusalOf(t, n.checkPane(ctx, "w1:p1", "claude", "something else")); code != "input_draft" || len(f.keys) != 0 {
		t.Fatalf("cleared a draft the user did not see: %s %v", code, f.keys)
	}
	if err := n.checkPane(ctx, "w1:p1", "claude", "fix the flaky test"); err != nil {
		t.Fatalf("after clearing: %v", err)
	}
	if len(f.keys) != 1 || f.keys[0][0] != "ctrl+k" || f.keys[0][1] != "ctrl+u" {
		t.Fatalf("keys %v", f.keys)
	}
}

// Two quick sends: the second found the first still in the box (typed, Enter not yet taken) and was refused as a
// draft, with an offer to clear it — the user's own first message. Now the sends to a session take turns and a box
// showing what was just typed is waited out.
func TestCheckPaneWaitsOutWhatWasJustTyped(t *testing.T) {
	typed := screenFixture(t, "claude-2.1.312-typed-162.ansi") // the box shows "fix the flaky test"
	f := &fakePane{screens: []string{typed, screenFixture(t, "claude-2.1.312-empty-162.ansi")}, clearAfter: 3}
	n := &Node{hosts: herdrHosts(f.serve(t))}
	n.typing.typed("w1:p1", "fix the flaky test\nand the second line of the message")
	if err := n.checkPane(context.Background(), "w1:p1", "claude", ""); err != nil {
		t.Fatalf("our own message taken for a draft: %v", err)
	}
	// Someone else's draft in another pane is still a draft.
	g := &fakePane{screens: []string{typed}}
	m := &Node{hosts: herdrHosts(g.serve(t))}
	m.typing.typed("w1:p2", "fix the flaky test")
	if code, _ := refusalOf(t, m.checkPane(context.Background(), "w1:p1", "claude", "")); code != "input_draft" {
		t.Fatalf("a draft in another pane: %s", code)
	}
	if !n.typing.ours("w1:p1", "[Pasted text #1 +500 lines] ") || n.typing.ours("w1:p9", "fix") {
		t.Fatal("paste placeholder / unknown pane")
	}
}

func TestSendsToOneSessionTakeTurns(t *testing.T) {
	var l typingLocks
	unlock := l.lock("s1")
	other := l.lock("s2") // another session does not wait
	other()
	got := make(chan struct{})
	go func() { defer l.lock("s1")(); close(got) }()
	select {
	case <-got:
		t.Fatal("a second send to the session did not wait")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("the second send never ran")
	}
}

func TestCheckPaneRefusesDialog(t *testing.T) {
	for _, name := range []string{"claude-2.1.312-trust-162.ansi", "claude-2.1.312-model-picker-162.ansi"} {
		f := &fakePane{screens: []string{screenFixture(t, name)}}
		n := &Node{hosts: herdrHosts(f.serve(t))}
		if code, _ := refusalOf(t, n.checkPane(context.Background(), "w1:p1", "claude", "")); code != "dialog_open" {
			t.Fatalf("%s: %s", name, code)
		}
	}
	f := &fakePane{screens: []string{screenFixture(t, "claude-2.1.312-empty-162.ansi")}}
	if err := (&Node{hosts: herdrHosts(f.serve(t))}).checkPane(context.Background(), "w1:p1", "claude", ""); err != nil {
		t.Fatalf("empty box: %v", err)
	}
	codex := &fakePane{screens: []string{"Allow command?\n\n› 1. Yes, proceed\n  2. No, and tell Codex what to do differently\n"}}
	if code, _ := refusalOf(t, (&Node{hosts: herdrHosts(codex.serve(t))}).checkPane(context.Background(), "w1:p1", "codex", "")); code != "dialog_open" {
		t.Fatal(code)
	}
}

func TestSentLogOnce(t *testing.T) {
	var l sentLog
	var typed atomic.Int32
	deliver := func() error { typed.Add(1); return nil }
	if err := l.once("m1", deliver); err != nil || l.once("m1", deliver) != nil || typed.Load() != 1 {
		t.Fatalf("a delivered message is typed once, got %d", typed.Load())
	}
	// Refused: nothing typed, so the retry runs.
	refuse := func() error { return refusal("input_draft", "x", nil) }
	_ = l.once("m2", refuse)
	if err := l.once("m2", deliver); err != nil || typed.Load() != 2 {
		t.Fatalf("retry after a refusal: %v %d", err, typed.Load())
	}
	// Failed while typing: it may be there, the retry reports the same instead of typing again.
	_ = l.once("m3", func() error { return uncertain(errors.New("socket closed")) })
	if err := l.once("m3", deliver); err == nil || typed.Load() != 2 {
		t.Fatalf("retry after an uncertain send: %v %d", err, typed.Load())
	}
	// A retry while the first is still typing waits for it.
	release := make(chan struct{})
	go func() { _ = l.once("m4", func() error { <-release; typed.Add(1); return nil }) }()
	time.Sleep(20 * time.Millisecond)
	done := make(chan error)
	go func() { done <- l.once("m4", deliver) }()
	close(release)
	if err := <-done; err != nil || typed.Load() != 3 {
		t.Fatalf("concurrent retry: %v %d", err, typed.Load())
	}
	// No id: every call sends.
	_ = l.once("", deliver)
	_ = l.once("", deliver)
	if typed.Load() != 5 {
		t.Fatal(typed.Load())
	}
}

func TestDialogKeysCheckRevision(t *testing.T) {
	trust := screenFixture(t, "claude-2.1.312-trust-162.txt")
	f := &fakePane{screens: []string{trust}}
	n := &Node{hosts: herdrHosts(f.serve(t)), agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}}}
	ctx := context.Background()
	rev := screenDialogRev(t, trust)
	keys, pre, err := n.dialogKeys(ctx, "s1", rev, "2")
	if err != nil || strings.Join(keys, ",") != "down,enter" || pre != nil {
		t.Fatalf("keys %v %v %v", keys, pre, err)
	}
	// The cursor moved on the computer: same dialog, keys from where it is now.
	moved := strings.Replace(strings.Replace(trust, " ❯ No, exit", "   No, exit", 1), "   Yes, I trust this folder", " ❯ Yes, I trust this folder", 1)
	f.screens[0] = moved
	if keys, _, err := n.dialogKeys(ctx, "s1", rev, "2"); err != nil || strings.Join(keys, ",") != "enter" {
		t.Fatalf("after the cursor moved: %v %v", keys, err)
	}
	// Answered at the computer: the screen moved on, nothing is pressed.
	f.screens[0] = screenFixture(t, "claude-2.1.312-empty-162.txt")
	if _, _, err := n.dialogKeys(ctx, "s1", rev, "2"); err == nil {
		t.Fatal("answered into a changed screen")
	} else if code, _ := refusalOf(t, err); code != "prompt_changed" {
		t.Fatal(code)
	}
}

func screenDialogRev(t *testing.T, s string) string {
	t.Helper()
	d := screen.ParseDialog(s)
	if d == nil {
		t.Fatal("no dialog")
	}
	return d.Rev
}

// A prompt left in the input box (seen once with an image: `[Image #1]` and the text, the agent done) gets one Enter;
// a box that empties, or shows someone else's text, gets none.
func TestEnsureSubmittedPressesEnterOnceForAStuckPrompt(t *testing.T) {
	defer func(g, p time.Duration) { submitGrace, submitPoll = g, p }(submitGrace, submitPoll)
	submitGrace, submitPoll = 200*time.Millisecond, 20*time.Millisecond
	typed := screenFixture(t, "claude-2.1.312-typed-162.ansi") // the box shows "fix the flaky test"
	empty := screenFixture(t, "claude-2.1.312-empty-162.ansi")
	ctx := context.Background()

	stuck := &fakePane{screens: []string{typed, empty}}
	n := &Node{hosts: herdrHosts(stuck.serve(t))}
	n.ensureSubmitted(ctx, "w1:p1", "fix the flaky test\n\n<attached_images>\n/tmp/a.jpg\n</attached_images>")
	if len(stuck.keys) != 1 || stuck.keys[0][0] != "enter" {
		t.Fatalf("stuck prompt: keys %v", stuck.keys)
	}

	taken := &fakePane{screens: []string{typed, empty}, clearAfter: 2} // Claude takes it a moment later
	m := &Node{hosts: herdrHosts(taken.serve(t))}
	m.ensureSubmitted(ctx, "w1:p1", "fix the flaky test")
	if len(taken.keys) != 0 {
		t.Fatalf("pressed enter on a prompt Claude took: %v", taken.keys)
	}

	other := &fakePane{screens: []string{typed}}
	o := &Node{hosts: herdrHosts(other.serve(t))}
	o.ensureSubmitted(ctx, "w1:p1", "something else entirely")
	if len(other.keys) != 0 {
		t.Fatalf("pressed enter on someone else's draft: %v", other.keys)
	}
}

func TestStillPromptedIgnoresChips(t *testing.T) {
	for _, c := range []struct {
		box, prompted string
		want          bool
	}{
		{"[Image #1]What two colors <attached_images> </attached_images>", "What two colors are in this picture?\n\n<attached_images>\n/x.jpg\n</attached_images>", true},
		{"[Pasted text #1 +40 lines]", "a long paste", false},
		{"fix the flaky test", "fix the flaky test", true},
		{"fix", "fix the flaky test", false},
		{"", "anything", false},
	} {
		if got := stillPrompted(c.box, c.prompted); got != c.want {
			t.Errorf("stillPrompted(%q, %q) = %v", c.box, c.prompted, got)
		}
	}
}
