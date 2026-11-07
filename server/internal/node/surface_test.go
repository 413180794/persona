package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/transcript"
)

// The list reads the client a session runs in off its transcript (#6390): Claude's entrypoint, a rollout's originator.
func TestQuickMetaReadsTheSurface(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct{ body, want string }{
		"desktop.jsonl": {`{"type":"user","uuid":"a","timestamp":"2026-01-01T00:00:00Z","cwd":"/w","entrypoint":"claude-desktop","message":{"role":"user","content":"hi"}}` + "\n", transcript.SurfaceDesktop},
		"cli.jsonl":     {`{"type":"user","uuid":"a","timestamp":"2026-01-01T00:00:00Z","cwd":"/w","entrypoint":"cli","message":{"role":"user","content":"hi"}}` + "\n", transcript.SurfaceCLI},
		"sdk.jsonl":     {`{"type":"user","uuid":"a","timestamp":"2026-01-01T00:00:00Z","cwd":"/w","entrypoint":"sdk-cli","message":{"role":"user","content":"hi"}}` + "\n", ""},
		"rollout.jsonl": {`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"t","cwd":"/w","originator":"Codex Desktop","cli_version":"1"}}` + "\n", transcript.SurfaceDesktop},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(p)
		if got := readQuickMeta(p, st).surface; got != c.want {
			t.Errorf("%s: surface %q, want %q", name, got, c.want)
		}
	}
}
