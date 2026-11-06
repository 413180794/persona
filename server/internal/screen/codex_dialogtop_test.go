package screen

import (
	"strings"
	"testing"
)

// #3214: Codex draws no rule under the conversation, so dialogTop found no edge there and the whole screen was read as
// the dialog: an answer's numbered list became options (the hub's card offered the prose as buttons) and the
// recordings' DialogHead was the welcome box or an answer. Codex starts each row of the conversation at the left
// edge — the agent's and status rows with •, the user's with ›, events with ✔ ✗ ■ — and ends a turn with
// "Worked for 2s • 9:41 PM"; its dialogs are indented under them, with the cursor's › at the edge only on an option.
const codexAnswer = "• I'll do three things:\n  1. Fix the race in the watcher\n  2. Add a regression test\n  3. Push the branch"

func TestCodexAnswersNumberedListIsNotADialog(t *testing.T) {
	// A finished turn (the issue's screen), and the same with the input box under it as Codex draws it.
	for _, s := range []string{
		codexAnswer + "\n\n  Worked for 2s • 9:41 PM\n",
		"› plan it\n\n" + codexAnswer + "\n\n  Worked for 2s • 9:41 PM\n\n\n› Ask Codex to do anything\n\n  gpt-6-astra medium · ~/project\n",
	} {
		if d := ParseDialog(s); d != nil {
			t.Errorf("an answer read as a dialog: %q %+v\n%s", d.Question, d.Options, s)
		}
		if Choosing(s) {
			t.Errorf("Choosing on a finished answer:\n%s", s)
		}
		if h := DialogHead(s); h != "" {
			t.Errorf("DialogHead on a finished answer: %q", h)
		}
	}
}

func TestCodexDialogUnderANumberedAnswerHasOnlyItsOwnOptions(t *testing.T) {
	for _, name := range []string{"codex-0.163.0-exec-approval-120.txt", "codex-0.153.4-exec-approval-120.txt"} {
		approval := fixture(t, name)
		for _, swap := range []string{
			"• I’ll run the exact command with escalated permissions.", // a • row ("Running …") stays between
			"• Running curl -sI https://example.com | head -1",         // the list right above the dialog
		} {
			s := strings.Replace(approval, swap, codexAnswer, 1)
			if s == approval {
				t.Fatalf("%s no longer holds %q", name, swap)
			}
			d := ParseDialog(s)
			if d == nil || len(d.Options) != 3 || !strings.HasPrefix(d.Options[0].Label, "Yes, proceed") ||
				!strings.HasPrefix(d.Options[2].Label, "No, and tell Codex") {
				t.Fatalf("%s, %q swapped for a numbered answer: %+v", name, swap, d)
			}
			if strings.Contains(d.Question, "three things") {
				t.Errorf("%s: the question is the answer's: %q", name, d.Question)
			}
			if h := DialogHead(s); h != "Would you like to run the following command?" {
				t.Errorf("%s: DialogHead %q", name, h)
			}
		}
	}
}

// The recordings: the dialog starts under the conversation, not at the top of the screen; what is read off the
// question overlays is unchanged, and a screen without a dialog still has none.
func TestCodexRecordingsDialogTop(t *testing.T) {
	for _, c := range []struct{ name, first, head string }{
		{"codex-0.163.0-plan-question-120.txt", "Question 1/2 (2 unanswered)", "Which color should the banner be?"},
		{"codex-0.163.0-plan-question-2-120.txt", "", "How tall should it be?"},
		{"codex-0.153.4-plan-question-120.txt", "", "Which color should the banner be?"},
		{"codex-0.153.4-exec-approval-120.txt", "Would you like to run the following command?", "Would you like to run the following command?"},
		{"codex-0.163.0-exec-approval-120.txt", "Would you like to run the following command?", "Would you like to run the following command?"},
		{"codex-0.163.0-async-expanded-120.txt", "Which color should the banner be?", "Which color should the banner be?"},
	} {
		s := fixture(t, c.name)
		lines := strings.Split(s, "\n")
		top := dialogTop(lines)
		if top <= 1 {
			t.Errorf("%s: dialogTop %d (the whole screen)", c.name, top)
		}
		var first string
		for _, l := range lines[top:] {
			if t := strings.TrimSpace(l); t != "" {
				first = t
				break
			}
		}
		if c.first != "" && first != c.first {
			t.Errorf("%s: the dialog starts at %q", c.name, first)
		}
		if h := DialogHead(s); h != c.head {
			t.Errorf("%s: DialogHead %q, want %q", c.name, h, c.head)
		}
	}
	for _, name := range []string{"codex-0.163.0-empty-120.txt", "codex-0.153.4-empty-120.txt", "codex-0.163.0-plan-answered-120.txt",
		"codex-0.163.0-async-collapsed-120.txt", "codex-0.163.0-slash-popup-120.txt", "codex-0.153.4-draft-120.txt"} {
		if d := ParseDialog(fixture(t, name)); d != nil {
			t.Errorf("%s: a dialog %+v", name, d)
		}
	}
}
