package screen

import (
	"strings"
	"testing"
)

// #5396: what a rewind in Claude Code's terminal ("Restore conversation") leaves on the screen, recorded on 2.1.318
// (claude-2.1.318-rewind-result-conversation-119): the prompt gone back over is in the input box as typed, and the
// conversation above is redrawn without it, its last ❯ row the prompt before. The screens it must not be taken for are
// made from it.

const (
	trPrompt = `Use the Edit tool (not Bash) to replace the line "beta" in notes.txt with "gamma". Reply with only: ok3`
	trBefore = `Use the Edit tool (not Bash) to replace the line "alpha" in notes.txt with "beta". Reply with only: ok2`
)

func trScreens(t *testing.T) (ansi, recent string) {
	t.Helper()
	return fixture(t, "claude-2.1.318-rewind-result-conversation-119.ansi"), fixture(t, "claude-2.1.318-rewind-result-conversation-119.txt")
}

// withRowsAboveBox puts rows into a plain screen right above its input box.
func withRowsAboveBox(t *testing.T, plain string, rows ...string) string {
	t.Helper()
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if isRule(strings.TrimRight(lines[i], " ")) {
			for j := i - 1; j >= 0; j-- {
				if isRule(strings.TrimRight(lines[j], " ")) {
					out := append(append(append([]string{}, lines[:j]...), rows...), lines[j:]...)
					return strings.Join(out, "\n")
				}
			}
		}
	}
	t.Fatal("no input box")
	return ""
}

func TestTerminalRewoundOnTheRecordedScreen(t *testing.T) {
	ansi, recent := trScreens(t)
	if !TerminalRewound(ansi, recent, trPrompt, trBefore, false) {
		t.Fatal("the recorded screen after a rewind is not seen as one")
	}
	// Spaces and line breaks do not count (a box that wrapped, a CJK prompt).
	if !TerminalRewound(ansi, recent, strings.ReplaceAll(trPrompt, " the ", "  the\n"), trBefore, false) {
		t.Fatal("spaces count")
	}
}

func TestTerminalRewoundIsNotOtherScreens(t *testing.T) {
	ansi, recent := trScreens(t)
	cases := map[string]struct {
		ansi, recent, prompt, before string
		first                        bool
	}{
		// The words typed again as a new message: the prompt's own ❯ row is still above the box.
		"typed again": {ansi, withRowsAboveBox(t, recent, "❯ "+trPrompt, "● ok3", "✻ Cogitated for 4s"), trPrompt, trBefore, false},
		// ↑ recalls the last prompt into the box: the rule over it says History.
		"history recall": {strings.Replace(ansi, "────────────────────────", "─── History 100/100 ────", 1), recent, trPrompt, trBefore, false},
		// The box holds something else, or nothing.
		"another draft": {ansi, recent, "something else", trBefore, false},
		// Claude's grey suggestion is not a draft.
		"a suggestion": {strings.Replace(ansi, trPrompt, "\x1b[2m"+trPrompt+"\x1b[0m", 1), recent, trPrompt, trBefore, false},
		// Without colors a suggestion cannot be told from a draft.
		"a plain read": {recent, recent, trPrompt, trBefore, false},
		// The prompt before is not on the screen (scrolled out: so may the rewound one be), or is not the one shown.
		"no prompt before": {ansi, strings.Replace(recent, "❯ Use the Edit tool (not Bash) to replace the line \"alpha\"", "  Use the Edit tool (not Bash) to replace the line \"alpha\"", 1), trPrompt, trBefore, false},
		"another before":   {ansi, recent, trPrompt, "Append the line \"alpha\" to notes.txt. Reply with only: done", false},
		// A local command after the prompt before is no prompt of the conversation; another prompt is.
		"another prompt after before": {ansi, withRowsAboveBox(t, recent, "❯ something else", "● ok"), trPrompt, trBefore, false},
		// The same words twice in a row: which one is in the box cannot be told.
		"same as before": {ansi, recent, trPrompt, trPrompt, false},
		// The first prompt, but the conversation above still has the agent's words.
		"first with words above": {ansi, recent, trPrompt, "", true},
		// The /rewind panel is still open.
		"panel open": {fixture(t, "claude-2.1.318-rewind-options-conversation-selected-119.txt"), recent, trPrompt, trBefore, false},
	}
	for name, c := range cases {
		if TerminalRewound(c.ansi, c.recent, c.prompt, c.before, c.first) {
			t.Errorf("%s: taken for a rewind", name)
		}
	}
}

func TestTerminalRewoundPlaceholderDraft(t *testing.T) {
	ansi, recent := trScreens(t)
	draft := "[Pasted text #1 +20 lines]"
	if TerminalRewound(strings.Replace(ansi, trPrompt, draft, 1), recent, draft, trBefore, false) {
		t.Fatal("a paste placeholder taken for the prompt")
	}
}

// The prompt before wraps over several rows in the conversation; the first prompt gone back to leaves nothing of the
// agent's above the box.
func TestTerminalRewoundWrappedAndFirst(t *testing.T) {
	ansi, _ := trScreens(t)
	box := "───────────────────────────────────────\n❯ " + trPrompt + "\n───────────────────────────────────────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)"
	wrapped := strings.Join([]string{
		" ▐▛███▜▌   Claude Code v2.1.318",
		"",
		"❯ Use the Edit tool (not Bash) to replace the",
		"  line \"alpha\" in notes.txt with \"beta\".",
		"  Reply with only: ok2",
		"  Read 1 file",
		"● ok2",
		"✻ Cogitated for 8s",
		"",
		box,
	}, "\n")
	if !TerminalRewound(ansi, wrapped, trPrompt, trBefore, false) {
		t.Fatal("a wrapped prompt before is not found")
	}
	withCommand := strings.Replace(wrapped, "✻ Cogitated for 8s", "✻ Cogitated for 8s\n\n❯ /rename probe\n  ⎿  Session renamed to: probe", 1)
	if !TerminalRewound(ansi, withCommand, trPrompt, trBefore, false) {
		t.Fatal("a local command after the prompt before hides it")
	}
	banner := " ▐▛███▜▌   Claude Code v2.1.318\n▝▜█████▛▘  Opus 5.5\n\n" + box
	if !TerminalRewound(ansi, banner, trPrompt, "", true) {
		t.Fatal("the first prompt gone back to is not seen")
	}
}
