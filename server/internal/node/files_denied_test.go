package node

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// macOS refusing the protected folders (TCC) says what to do; other errors and other folders pass through.
func TestExplainDenied(t *testing.T) {
	denied := &fs.PathError{Op: "open", Path: "/Users/me/Desktop/a.png", Err: fs.ErrPermission}
	err := explainDenied("/Users/me/Desktop/a.png", "/Users/me", denied)
	if !strings.Contains(err.Error(), "「桌面」") || !strings.Contains(err.Error(), "完全磁盘访问权限") || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Desktop: %v", err)
	}
	if err := explainDenied("/Users/me/Downloads", "/Users/me", denied); !strings.Contains(err.Error(), "「下载」") {
		t.Fatalf("Downloads itself: %v", err)
	}
	for _, p := range []string{"/Users/me/code/a.png", "/Users/user/Desktop/a.png", "/Users/me"} {
		if err := explainDenied(p, "/Users/me", denied); err != denied {
			t.Fatalf("%s: rewrote %v", p, err)
		}
	}
	missing := &fs.PathError{Op: "open", Path: "/Users/me/Desktop/a.png", Err: fs.ErrNotExist}
	if err := explainDenied("/Users/me/Desktop/a.png", "/Users/me", missing); err != missing {
		t.Fatalf("not found: rewrote %v", err)
	}
}
