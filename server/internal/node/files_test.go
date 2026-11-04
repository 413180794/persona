package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 标题\nhello"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o600)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o700)

	fc, err := n.readFile("~/a.md", "", 0, false)
	if err != nil || fc.Text != "# 标题\nhello" || fc.Name != "a.md" {
		t.Fatalf("md = %+v, %v", fc, err)
	}
	fc, err = n.readFile("a.md", dir, 0, false)
	if err != nil || fc.Text == "" {
		t.Fatalf("relative = %+v, %v", fc, err)
	}
	fc, err = n.readFile(filepath.Join(dir, "b.bin"), "", 0, false)
	if err != nil || fc.Data == "" || fc.Text != "" {
		t.Fatalf("binary = %+v, %v", fc, err)
	}
	fc, err = n.readFile(dir, "", 0, false)
	if err != nil || !fc.IsDir || len(fc.Entries) != 3 {
		t.Fatalf("dir = %+v, %v", fc, err)
	}
	if _, err := n.readFile("/Users/x/Library/Mobile Documents/com~apple~CloudDocs/a", "", 0, false); err == nil || !strings.Contains(err.Error(), "iCloud") {
		t.Fatalf("iCloud path should be refused: %v", err)
	}
	if got := trimPartialRune([]byte("汉")[:2]); len(got) != 0 {
		t.Fatalf("partial rune kept: %v", got)
	}
}

// The phone links a path mentioned in a conversation only when it is there on the session's machine (fs.stat).
func TestStatPaths(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	_ = os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600)
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o700)
	got := n.statPaths([]string{"~/a.go", "a.go", "sub", "~/sub/", "nope.go", "/Users/x/Library/Mobile Documents/com~apple~CloudDocs/a", ""}, dir)
	want := []string{"file", "file", "dir", "dir", "missing", "missing", "missing"}
	if len(got) != len(want) {
		t.Fatalf("got %d stats for %d paths", len(got), len(want))
	}
	for i, s := range got {
		if s.Kind != want[i] {
			t.Errorf("%q: %s, want %s", s.Path, s.Kind, want[i])
		}
	}
	if got[0].Path != "~/a.go" || got[0].Size != int64(len("package a\n")) {
		t.Fatalf("the path is echoed as asked, with the size: %+v", got[0])
	}
	// A relative path with no directory to resolve it against is not a guess.
	if s := n.statPaths([]string{"a.go"}, ""); s[0].Kind != "missing" {
		t.Fatalf("relative without cwd: %+v", s[0])
	}
	// At most maxStatPaths answered.
	many := make([]string, maxStatPaths+5)
	for i := range many {
		many[i] = "~/a.go"
	}
	if s := n.statPaths(many, ""); len(s) != maxStatPaths {
		t.Fatalf("%d answered, want %d", len(s), maxStatPaths)
	}
}

// A text file bigger than a page comes in pages that end at a line's end; `next` is where the next one starts.
func TestReadFilePages(t *testing.T) {
	dir := t.TempDir()
	n := &Node{home: dir}
	line := strings.Repeat("x", 99) + "\n" // 100 bytes
	big := strings.Repeat(line, maxFileBytes/100+50)
	_ = os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o600)

	var got strings.Builder
	offset, pages := int64(0), 0
	for {
		fc, err := n.readFile("~/big.txt", "", offset, false)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if fc.Offset != offset || fc.Size != int64(len(big)) {
			t.Fatalf("page %d: offset %d size %d", pages, fc.Offset, fc.Size)
		}
		if fc.Next != 0 && !strings.HasSuffix(fc.Text, "\n") {
			t.Fatalf("page %d ends mid-line: …%q", pages, fc.Text[len(fc.Text)-10:])
		}
		got.WriteString(fc.Text)
		if fc.Next == 0 {
			if fc.Truncated {
				t.Fatal("the last page says truncated")
			}
			break
		}
		if !fc.Truncated || fc.Next <= offset {
			t.Fatalf("page %d: next %d after %d, truncated %v", pages, fc.Next, offset, fc.Truncated)
		}
		offset = fc.Next
	}
	if pages != 2 || got.String() != big {
		t.Fatalf("%d pages, %d of %d bytes back", pages, got.Len(), len(big))
	}
	if _, err := n.readFile("~/big.txt", "", int64(len(big))+1, false); err == nil {
		t.Fatal("an offset past the end should fail")
	}
	// Binary files are not paged: past offset 0 they are refused.
	_ = os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o600)
	if _, err := n.readFile("~/b.bin", "", 2, false); err == nil {
		t.Fatal("a binary file read from an offset should fail")
	}
}
