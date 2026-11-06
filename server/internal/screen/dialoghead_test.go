package screen

import "testing"

// #2553: the first line of the dialog holding the terminal, for "Needs You"'s reason.
func TestDialogHead(t *testing.T) {
	for name, want := range map[string]string{
		"claude-2.1.309-auto-mode-notice-120.txt":   "We're changing auto mode to no longer charge for classifier requests in Claude Code.",
		"claude-2.1.312-trust-162.txt":              "Accessing workspace:",
		"claude-2.1.312-model-picker-162.txt":       "Select model",
		"claude-2.1.316-danger-rm-120.txt":          "Bash command",
		"claude-2.1.303-questions-preview-198.txt":  "文案可以吗？",
		"claude-2.1.316-single-100.txt":             "示例：先发哪个环境？",
		"claude-2.1.316-multiselect-100.txt":        "示例清单里哪些现在就做？",
		"claude-2.1.316-multiselect-review-100.txt": "Ready to submit your answers?",
		"claude-2.1.312-empty-162.txt":              "",
		"claude-2.1.312-suggestion-171.txt":         "",
		"claude-2.1.312-typed-162.txt":              "",
	} {
		if got := DialogHead(fixture(t, name)); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// Only a notice that Enter closes is one: confirmations, permissions, pickers and questions never are.
func TestNoticeOnly(t *testing.T) {
	if !NoticeOnly(fixture(t, "claude-2.1.309-auto-mode-notice-120.txt")) {
		t.Fatal("the auto mode notice")
	}
	for _, name := range []string{"claude-2.1.312-trust-162.txt", "claude-2.1.312-model-picker-162.txt",
		"claude-2.1.316-danger-rm-120.txt", "claude-2.1.303-questions-preview-198.txt", "claude-2.1.316-multiselect-100.txt",
		"claude-2.1.312-empty-162.txt", "claude-rewind-2.1.304-confirm-apple-restore-120.txt"} {
		if NoticeOnly(fixture(t, name)) {
			t.Errorf("%s taken for a notice", name)
		}
	}
	// An «Enter to continue» footer over options is still a choice.
	if NoticeOnly("────────\n Pick one\n ❯ 1. Yes\n   2. No\n\n Enter to continue · Esc to cancel\n") {
		t.Fatal("options under an Enter to continue footer")
	}
	// The same with the cursor at the left edge, as AskUserQuestion draws it: not the user's prompt in the history.
	if NoticeOnly("────────\n Pick one\n   1. Yes\n❯ 2. No\n\n Enter to continue · Esc to cancel\n") {
		t.Fatal("options with a cursor at the left edge")
	}
	// The footer must be all there is: «Enter to continue · Tab to amend» offers more than closing.
	if NoticeOnly("────────\n Note\n\n Enter to continue · Tab to amend\n") {
		t.Fatal("a footer with more keys")
	}
}
