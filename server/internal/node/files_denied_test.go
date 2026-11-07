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

// Who gets Full Disk Access depends on what started the node (#5944): TCC holds an ssh-started node's reads to
// sshd-keygen-wrapper, a LaunchAgent's to its own program.
func TestExplainDeniedSaysWhoToAllow(t *testing.T) {
	denied := &fs.PathError{Op: "open", Path: "/Users/me/Documents/a.md", Err: fs.ErrPermission}
	for via, want := range map[string]string{
		"ssh":     "sshd-keygen-wrapper",
		"launchd": "加上 /Users/me/.persona/bin/personad 并打开它（Persona 的常驻 node 由 launchd 启动",
		"":        "运行 Persona node 的终端或 App",
	} {
		err := explainDeniedFor("/Users/me/Documents/a.md", "/Users/me", via, "/Users/me/.persona/bin/personad", denied)
		if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "「文稿」") || !errors.Is(err, fs.ErrPermission) {
			t.Errorf("%q: %v", via, err)
		}
		if via != "ssh" && strings.Contains(err.Error(), "sshd-keygen-wrapper") {
			t.Errorf("%q names ssh: %v", via, err)
		}
	}
	if err := explainDeniedFor("/Users/me/Documents/a.md", "/Users/me", "launchd", "", denied); !strings.Contains(err.Error(), "加上 personad") {
		t.Errorf("no executable: %v", err)
	}
	if err := explainDeniedFor("/Users/me/code/a.md", "/Users/me", "launchd", "/x", denied); err != denied {
		t.Errorf("outside the protected folders: %v", err)
	}
}
