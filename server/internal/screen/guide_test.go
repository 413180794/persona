package screen

import (
	"strings"
	"testing"
)

const teachFixture = "claude-2.1.318-teach-auto-mode-184.txt"

// #5504: Claude Code's auto mode setup guide, as a pane showed it, is a guide; it is still a dialog for the check
// before typing (DialogOpen), and not a notice Enter closes (Enter would start its scan).
func TestGuideTeachAutoMode(t *testing.T) {
	s := fixture(t, teachFixture)
	if got := Guide(s); got != "teach-auto-mode" {
		t.Fatalf("Guide = %q", got)
	}
	if got := DialogHead(s); got != "Teach auto mode about your environment?" {
		t.Fatalf("DialogHead = %q", got)
	}
	if code, _ := CheckClaude(s); code != DialogOpen {
		t.Fatalf("CheckClaude = %q", code)
	}
	if NoticeOnly(s) {
		t.Fatal("taken for a notice Enter closes")
	}
}

// Other dialogs and plain screens are not guides, nor is an answer that quotes the guide's title above the input box.
func TestGuideOnlyTheKnownOnes(t *testing.T) {
	for _, name := range []string{
		"claude-2.1.309-auto-mode-notice-120.txt", "claude-2.1.312-trust-162.txt", "claude-2.1.312-model-picker-162.txt",
		"claude-2.1.316-danger-rm-120.txt", "claude-2.1.316-single-100.txt", "claude-2.1.312-empty-162.txt",
		"claude-2.1.312-typed-162.txt",
	} {
		if g := Guide(fixture(t, name)); g != "" {
			t.Errorf("%s taken for guide %q", name, g)
		}
	}
	quoted := strings.Replace(fixture(t, "claude-2.1.312-empty-162.txt"), "\n", "\n⏺ It asked: Teach auto mode about your environment?\n", 1)
	if g := Guide(quoted); g != "" {
		t.Fatalf("a quoted title taken for guide %q", g)
	}
	// The same title over a footer without Esc to cancel is not one either.
	noEsc := strings.Replace(fixture(t, teachFixture), " · Esc to cancel", "", 1)
	if g := Guide(noEsc); g != "" {
		t.Fatalf("no Esc footer taken for guide %q", g)
	}
}
