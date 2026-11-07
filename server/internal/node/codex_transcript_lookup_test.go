package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestOldCodexOpaqueTranscriptReadableWithoutInventory(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "history", true: "native_archive"}[archived], func(t *testing.T) {
			n := remoteTestNode(t) // Empty registry models a restarted node.
			base := t.TempDir()
			old := time.Now().Add(-2 * codexWindow)
			writeRollout(t, base, remoteRaw, "/wanted", old, old,
				`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"retained old transcript"}]}}}`)
			home := canonicalCodexHome(filepath.Join(base, ".codex"), n.home)
			path := rolloutPath(home, remoteRaw)
			if path == "" {
				t.Fatal("fixture not found")
			}
			if archived {
				target := filepath.Join(home, "archived_sessions", filepath.Base(path))
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				path = target
			}
			// A second home has the same raw UUID, but different content.
			other := t.TempDir()
			writeRollout(t, other, remoteRaw, "/wrong", old, old,
				`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"wrong home transcript"}]}}}`)
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(home, alias); err != nil {
				t.Fatal(err)
			}
			n.configureCodex(proto.CodexConfig{Homes: []string{filepath.Join(other, ".codex"), alias}})
			id := codexSourceID(home, remoteRaw, n.codexInventory().LegacyHome)
			if rows := n.codexSessions(map[string]bool{}); len(rows) != 0 {
				t.Fatalf("old inventory: %+v", rows)
			}
			if got := n.transcriptPath(id); got != path {
				t.Fatalf("path = %q, want %q", got, path)
			}
			out, err := request(t, n, "session.items", map[string]any{"session_id": id, "limit": 80})
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "retained old transcript") || strings.Contains(string(body), "wrong home transcript") {
				t.Fatalf("wrong items: %s", body)
			}
			if _, ok := n.codexSource(id); ok {
				t.Fatal("read registered an inventory source")
			}
			if rows := n.codexSessions(map[string]bool{}); len(rows) != 0 {
				t.Fatalf("read revived inventory: %+v", rows)
			}
		})
	}
}

func TestOldCodexOpaqueTranscriptRejectsOtherHomesAndEscapingSymlinks(t *testing.T) {
	n := remoteTestNode(t)
	base := t.TempDir()
	old := time.Now().Add(-2 * codexWindow)
	writeRollout(t, base, remoteRaw, "/private", old, old)
	home := canonicalCodexHome(filepath.Join(base, ".codex"), n.home)
	id := codexSourceID(home, remoteRaw, n.codexInventory().LegacyHome)
	if got := n.transcriptPath(id); got != "" {
		t.Fatalf("unconfigured home: %s", got)
	}
	allowed := canonicalCodexHome(t.TempDir(), n.home)
	n.configureCodex(proto.CodexConfig{Homes: []string{allowed}})
	dir := filepath.Join(allowed, "sessions", "2000", "01", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rolloutPath(home, remoteRaw), filepath.Join(dir, "rollout-old-"+remoteRaw+".jsonl")); err != nil {
		t.Fatal(err)
	}
	allowedID := codexSourceID(allowed, remoteRaw, n.codexInventory().LegacyHome)
	if got := n.transcriptPath(allowedID); got != "" {
		t.Fatalf("escaped home via symlink: %s", got)
	}
}
