package link

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPokeEventNameIsPerLinkFile(t *testing.T) {
	dir := t.TempDir()
	a := PokeEventName(filepath.Join(dir, "a", "link.json"), PokeCheck)
	b := PokeEventName(filepath.Join(dir, "b", "link.json"), PokeCheck)
	if a == b {
		t.Fatalf("two data directories share %q", a)
	}
	if !strings.HasPrefix(a, `Local\PersonaLink.`) || !strings.HasSuffix(a, ".recheck") {
		t.Fatalf("name %q", a)
	}
	if got := PokeEventName(filepath.Join(dir, "a", "link.json"), PokeRedial); !strings.HasSuffix(got, ".reconnect") || strings.TrimSuffix(got, ".reconnect") != strings.TrimSuffix(a, ".recheck") {
		t.Fatalf("redial %q does not pair with check %q", got, a)
	}
	// A relative path and the same file spelled absolutely are one link.json.
	t.Chdir(dir)
	if rel := PokeEventName(filepath.Join("a", "link.json"), PokeCheck); rel != a {
		t.Fatalf("relative %q, absolute %q", rel, a)
	}
}
