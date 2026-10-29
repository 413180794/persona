package screen

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCheckClaudeFixtures(t *testing.T) {
	for _, c := range []struct {
		name, code, clearable string
	}{
		{"claude-2.1.312-empty-162.ansi", "", ""},   // the grey tip is not a draft
		{"claude-2.1.312-cleared-162.ansi", "", ""}, // after ctrl+k ctrl+u
		{"claude-2.1.312-typed-162.ansi", InputDraft, "fix the flaky test"},
		{"claude-2.1.312-multiline-162.ansi", InputDraft, ""}, // two rows: the clear keys take one
		{"claude-2.1.312-wrapped-162.ansi", InputDraft, ""},
		{"claude-2.1.312-bash-162.ansi", InputDraft, ""}, // bash mode: the text would run as a command
		{"claude-2.1.312-trust-162.ansi", DialogOpen, ""},
		{"claude-2.1.312-model-picker-162.ansi", DialogOpen, ""},
		{"claude-rewind-2.1.304-confirm-apple-restore-120.txt", DialogOpen, ""},
	} {
		code, in := CheckClaude(fixture(t, c.name))
		if code != c.code || in.Clearable != c.clearable {
			t.Errorf("%s: code %q clearable %q (text %q), want %q %q", c.name, code, in.Clearable, in.Text, c.code, c.clearable)
		}
	}
}

// Without colors any text is a draft (a grey suggestion and typed words read the same), but for the exact
// new-session tip row, which is how Claude draws it under NO_COLOR.
func TestPlainTextCannotTellSuggestion(t *testing.T) {
	if code, _ := CheckClaude(fixture(t, "claude-2.1.312-empty-162.txt")); code != "" {
		t.Fatalf("plain tip: %q", code)
	}
	if code, _ := CheckClaude("────────────────────\n❯ fix the build\n────────────────────\n footer"); code != InputDraft {
		t.Fatalf("plain suggestion-or-draft: %q", code)
	}
	if code, in := CheckClaude(fixture(t, "claude-2.1.312-typed-162.txt")); code != InputDraft || in.Text != "fix the flaky test" {
		t.Fatalf("plain typed: %q %q", code, in.Text)
	}
}

func TestSuggestionWithCursor(t *testing.T) {
	rule := "\x1b[38;2;136;136;136m" + "────────────────────" + "\x1b[0m"
	// Claude's own cursor sits inverse on the first grey character.
	grey := rule + "\n❯ \x1b[7mf\x1b[0m\x1b[2mix the build\x1b[0m\n" + rule + "\n  footer"
	if code, _ := CheckClaude(grey); code != "" {
		t.Fatalf("suggestion under the cursor: %q", code)
	}
	typedCursor := rule + "\n❯ hello\x1b[7m \x1b[0m\n" + rule + "\n  footer"
	if code, _ := CheckClaude(typedCursor); code != InputDraft {
		t.Fatalf("typed text with the cursor after it: %q", code)
	}
	emptyCursor := rule + "\n❯ \x1b[7m \x1b[0m\n" + rule + "\n  footer"
	if code, _ := CheckClaude(emptyCursor); code != "" {
		t.Fatalf("empty box with the cursor: %q", code)
	}
}

func TestChoosingIgnoresHistory(t *testing.T) {
	screen := "❯ 1. an old message that starts like an option\n⏺ done\n────────────────────\n❯ \n────────────────────\n  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt"
	if Choosing(screen) {
		t.Fatal("a numbered line above the input box is history, and esc to interrupt is no dialog")
	}
}

func TestSGRColorIsNotDim(t *testing.T) {
	runs := sgrRuns("\x1b[38;2;2;2;2mx\x1b[2my\x1b[22mz")
	if len(runs) != 3 || runs[0].dim || !runs[1].dim || runs[2].dim {
		t.Fatalf("%+v", runs)
	}
}
