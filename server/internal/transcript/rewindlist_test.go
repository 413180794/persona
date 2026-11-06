package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the app's prompt sits in Claude Code's /rewind list (#5397): a slash command written as system lines is one of
// the user's messages in the app and not in Claude's list; one written as a user line (older Claude Code) is in both.
func TestAsListedLeavesOutLocalCommands(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-06T09:00:00Z","message":{"role":"user","content":"again"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-06T09:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"one"}]}}`,
		// An older Claude Code: the command and its output as user lines (Claude lists the command).
		`{"type":"user","uuid":"o1","parentUuid":"a1","timestamp":"2026-11-06T09:00:30Z","message":{"role":"user","content":"<command-name>/model</command-name>\n            <command-message>model</command-message>\n            <command-args></command-args>"},"version":"2.1.30"}`,
		`{"type":"user","uuid":"o2","parentUuid":"o1","timestamp":"2026-11-06T09:00:30Z","message":{"role":"user","content":"<local-command-stdout>Kept model as Model 5</local-command-stdout>"},"version":"2.1.30"}`,
		`{"type":"user","uuid":"u2","parentUuid":"o2","timestamp":"2026-11-06T09:01:00Z","message":{"role":"user","content":"again"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-11-06T09:01:05Z","message":{"id":"m2","role":"assistant","content":[{"type":"text","text":"two"}]}}`,
		// Newer: /rename as system lines (not listed).
		`{"type":"system","subtype":"local_command","uuid":"c1","parentUuid":"a2","timestamp":"2026-11-06T09:02:00Z","content":"<command-name>/rename</command-name>\n            <command-message>rename</command-message>\n            <command-args>again</command-args>","level":"info","isMeta":false,"version":"2.1.309"}`,
		`{"type":"system","subtype":"local_command","uuid":"c2","parentUuid":"c1","timestamp":"2026-11-06T09:02:00Z","content":"<local-command-stdout>Session renamed to: again</local-command-stdout>","level":"info","isMeta":false,"commandRun":{"command":"rename","args":"again"},"version":"2.1.309"}`,
		`{"type":"user","uuid":"u3","parentUuid":"c2","timestamp":"2026-11-06T09:03:00Z","message":{"role":"user","content":"again"}}`,
	}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	// The app: again, /model, again, /rename again, again.
	for _, c := range []struct {
		name               string
		prompt             string
		occurrence, after  int
		wantOcc, wantAfter int
		ok                 bool
	}{
		{"the last one", "again", 1, 0, 1, 0, true},
		{"before /rename", "again", 2, 2, 2, 1, true},
		{"the first", "again", 3, 4, 3, 3, true},
		{"/model is listed", "/model", 1, 3, 1, 2, true},
		{"/rename is not", "/rename again", 1, 1, 0, 0, false},
		{"a count off", "again", 2, 1, 0, 0, false},
		{"not there", "again", 4, 5, 0, 0, false},
	} {
		occ, after, ok := f.AsListed(c.prompt, c.occurrence, c.after)
		if ok != c.ok || occ != c.wantOcc || after != c.wantAfter {
			t.Errorf("%s: %d, %d, %v; want %d, %d, %v", c.name, occ, after, ok, c.wantOcc, c.wantAfter, c.ok)
		}
	}
}
