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

	fc, err := n.readFile("~/a.md", "")
	if err != nil || fc.Text != "# 标题\nhello" || fc.Name != "a.md" {
		t.Fatalf("md = %+v, %v", fc, err)
	}
	fc, err = n.readFile("a.md", dir)
	if err != nil || fc.Text == "" {
		t.Fatalf("relative = %+v, %v", fc, err)
	}
	fc, err = n.readFile(filepath.Join(dir, "b.bin"), "")
	if err != nil || fc.Data == "" || fc.Text != "" {
		t.Fatalf("binary = %+v, %v", fc, err)
	}
	fc, err = n.readFile(dir, "")
	if err != nil || !fc.IsDir || len(fc.Entries) != 3 {
		t.Fatalf("dir = %+v, %v", fc, err)
	}
	if _, err := n.readFile("/Users/x/Library/Mobile Documents/com~apple~CloudDocs/a", ""); err == nil || !strings.Contains(err.Error(), "iCloud") {
		t.Fatalf("iCloud path should be refused: %v", err)
	}
	if got := trimPartialRune([]byte("汉")[:2]); len(got) != 0 {
		t.Fatalf("partial rune kept: %v", got)
	}
}
