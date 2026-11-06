package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bookkeeping files (a person's hashed identity hints, a connector's cursor, the Moments verdicts) sit in the data
// dir next to what is searched, but neither Search nor Find may return a line of them: they reach the model through
// kb_search and mean nothing to it.
func TestSearchAndFindSkipBookkeeping(t *testing.T) {
	e := testEngine(t, nil)
	e.HandleKey = []byte("0123456789abcdef0123456789abcdef")
	st := e.Store
	if err := st.PutPerson(&Person{ID: "mail-a", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if !e.addHandles("mail-a", []Handle{{Kind: HandleEmail, Value: handleTestEmail}}) {
		t.Fatal("addHandles")
	}
	if err := st.PutProject(&Project{Slug: "p1", Name: "Plan"}); err != nil {
		t.Fatal(err)
	}
	private := map[string]string{
		filepath.Join("cursors", "imessage.json"):               `{"cursor":"zqxcursortoken"}`,
		filepath.Join(KindProject, "p1", "moments-judged.json"): `{"zqxjudgedpost":true}`,
	}
	for rel, body := range private {
		p := filepath.Join(st.Dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if raw, err := os.ReadFile(st.Path(KindPerson, "mail-a", "handles.json")); err != nil || !strings.Contains(string(raw), "email:") {
		t.Fatalf("handles.json not written: %q %v", raw, err)
	}
	skipped := func(path string) bool {
		base := filepath.Base(path)
		return base == "handles.json" || base == "moments-judged.json" || strings.HasPrefix(filepath.ToSlash(path), "cursors/")
	}
	for _, q := range []string{"email:", "zqxcursortoken", "zqxjudgedpost"} {
		for _, h := range e.Search(q, true, 10) {
			if skipped(h.Path) {
				t.Errorf("Search(%q) returned %s: %q", q, h.Path, h.Line)
			}
		}
		for _, r := range e.Find(q, 10) {
			for _, h := range r.Hits {
				if skipped(h.Path) {
					t.Errorf("Find(%q) returned %s: %q", q, h.Path, h.Line)
				}
			}
		}
	}
	// What is not bookkeeping is still found: the person's own meta and the project.
	if hits := e.Search("Alice", false, 10); len(hits) == 0 {
		t.Error("Search no longer finds the person")
	}
	if res := e.Find("Plan", 10); len(res) == 0 {
		t.Error("Find no longer finds the project")
	}
}
