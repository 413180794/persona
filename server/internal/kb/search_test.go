package kb

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSearchLimitsAndOrder(t *testing.T) {
	dir, extra := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "docs/a.md", " ALPHA beta first \nalpha only\nbeta alpha second\n")
	write(dir, "docs/b.json", "beta ALPHA third\n")
	write(extra, "guide.md", "Beta alpha extra\n")
	write(dir, "chats/c/messages/2026-10-01.md", "alpha beta older message\n")
	write(dir, "chats/c/messages/2026-10-02.md", "alpha beta newer message\n")
	write(dir, "moments/posts/001.md", "alpha beta older post\n")
	write(dir, "moments/posts/002.md", "alpha beta newer post\n")
	e := &Engine{Store: &Store{Dir: dir}, ExtraDirs: []string{extra}}
	profiles := []Hit{
		{Path: "docs/a.md", Line: "ALPHA beta first"},
		{Path: "docs/a.md", Line: "beta alpha second"},
		{Path: "docs/b.json", Line: "beta ALPHA third"},
		{Path: filepath.Join(extra, "guide.md"), Line: "Beta alpha extra"},
	}
	posts := []Hit{
		{Path: "moments/posts/002.md", Line: "alpha beta newer post"},
		{Path: "moments/posts/001.md", Line: "alpha beta older post"},
	}
	all := append(append([]Hit{}, profiles...),
		Hit{Path: "chats/c/messages/2026-10-02.md", Line: "alpha beta newer message"},
		Hit{Path: "chats/c/messages/2026-10-01.md", Line: "alpha beta older message"})
	all = append(all, posts...)
	for _, tt := range []struct {
		name, query string
		messages    bool
		limit       int
		want        []Hit
	}{
		{name: "empty", limit: 20},
		{name: "whitespace", query: " \t\n", limit: 20},
		{name: "negative", query: "alpha beta", limit: -1},
		{name: "zero", query: "alpha beta"},
		{name: "one", query: "alpha beta", limit: 1, want: all[:1]},
		{name: "across_files", query: "alpha beta", limit: 3, want: all[:3]},
		{name: "extra_dir", query: "alpha beta", limit: 4, want: profiles},
		{name: "message_limit", query: "alpha beta", messages: true, limit: 5, want: all[:5]},
		{name: "all", query: " \tALpha BeTA\n", messages: true, limit: 20, want: all},
		{name: "without_messages", query: "alpha beta", limit: 20, want: append(append([]Hit{}, profiles...), posts...)},
		{name: "no_match", query: "alpha missing", messages: true, limit: 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.Search(tt.query, tt.messages, tt.limit); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Search(%q, %v, %d) = %+v, want %+v", tt.query, tt.messages, tt.limit, got, tt.want)
			}
		})
	}
}
