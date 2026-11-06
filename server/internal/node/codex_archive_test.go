package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestCodexInventoryArchiveRestoreAndStaleRegistry(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	writeRollout(t, n.home, remoteRaw, "/work/project", time.Now(), time.Now())
	writeRollout(t, n.home, remoteOther, "/other/project", time.Now(), time.Now())
	active := rolloutPath(home, remoteRaw)
	archived := filepath.Join(home, "archived_sessions", filepath.Base(active))
	if err := os.MkdirAll(filepath.Dir(archived), 0700); err != nil {
		t.Fatal(err)
	}
	assertIDs := func(want ...string) {
		t.Helper()
		rows := n.codexSessions(map[string]bool{})
		got := map[string]bool{}
		for _, s := range rows {
			got[s.SourceThreadID] = true
		}
		if len(rows) != len(want) {
			t.Fatalf("inventory=%v, want %v", got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("missing %s: %v", id, got)
			}
		}
	}
	assertIDs(remoteRaw, remoteOther)
	if err := os.Rename(active, archived); err != nil {
		t.Fatal(err)
	}
	// Both the rollout scan and the registry still remember the old path.
	assertIDs(remoteOther)
	expireRollouts(home, true)
	assertIDs(remoteOther)
	if got := rolloutPath(home, remoteRaw); got != archived {
		t.Fatalf("archive must remain readable: %s", got)
	}
	if err := os.Rename(archived, active); err != nil {
		t.Fatal(err)
	}
	assertIDs(remoteRaw, remoteOther)
	if err := os.Remove(active); err != nil {
		t.Fatal(err)
	}
	assertIDs(remoteOther)
}

func TestCodexInventoryArchiveOverridesLoadedCache(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	writeRollout(t, n.home, remoteRaw, "/work/project", time.Now(), time.Now())
	active := rolloutPath(home, remoteRaw)
	_, c := connectRemoteFixture(t, n, home, "one", remoteRaw, remoteOther)
	rows := n.codexSessions(map[string]bool{})
	if len(rows) != 2 {
		t.Fatalf("history and live must deduplicate: %+v", rows)
	}
	for _, s := range rows {
		if s.SourceKind != "codex_app_server" {
			t.Fatalf("live source lost: %+v", s)
		}
	}
	archived := filepath.Join(home, "archived_sessions", filepath.Base(active))
	if err := os.MkdirAll(filepath.Dir(archived), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(active, archived); err != nil {
		t.Fatal(err)
	}
	rows = n.codexSessions(map[string]bool{})
	if len(rows) != 1 || rows[0].SourceThreadID != remoteOther {
		t.Fatalf("archived loaded cache revived: %+v", rows)
	}
	// A valid loaded thread without a rollout stays; after its owner disconnects it is not a ghost row.
	c.close()
	if rows = n.codexSessions(map[string]bool{}); len(rows) != 0 {
		t.Fatalf("stale owner inventory: %+v", rows)
	}
	if rows = n.codexSessions(map[string]bool{remoteOther: true}); len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestCodexArchivedChildDoesNotRemoveParent(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	// Exercise source-scoped identities on every OS, including symlinked macOS temporary homes.
	n.configureCodex(proto.CodexConfig{LegacyHome: filepath.Join(n.home, "other-codex")})
	writeRollout(t, n.home, remoteRaw, "/work/project", time.Now(), time.Now())
	p := filepath.Join(home, "archived_sessions", "rollout-test-"+remoteOther+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"type":"session_meta","payload":{"originator":"Codex Desktop","source":"vscode","parent_thread_id":"`+remoteRaw+`","cwd":"/work/project"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n.registerCodex(home, remoteOther, p)
	n.configureCodex(proto.CodexConfig{Homes: []string{home}})
	rows := n.codexSessions(map[string]bool{})
	if len(rows) != 1 || rows[0].SourceThreadID != remoteRaw {
		t.Fatalf("valid parent lost: %+v", rows)
	}
	parentID := rows[0].SessionID
	activeChild := filepath.Join(filepath.Dir(rolloutPath(home, remoteRaw)), filepath.Base(p))
	if err := os.Rename(p, activeChild); err != nil {
		t.Fatal(err)
	}
	parent := rolloutPath(home, remoteRaw)
	if err := os.Rename(parent, filepath.Join(home, "archived_sessions", filepath.Base(parent))); err != nil {
		t.Fatal(err)
	}
	rows = n.codexSessions(map[string]bool{})
	if len(rows) != 1 || rows[0].SourceThreadID != remoteOther || rows[0].ParentSessionID != parentID {
		t.Fatalf("valid child of archived parent lost: %+v", rows)
	}
}

func TestCodexArchivedRolloutThroughHomeSymlink(t *testing.T) {
	n := remoteTestNode(t)
	home := canonicalCodexHome(t.TempDir(), n.home)
	alias := filepath.Join(t.TempDir(), "codex")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(alias, "archived_sessions", "rollout-test-"+remoteRaw+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n.registerCodex(alias, remoteRaw, p)
	if rows := n.codexSessions(map[string]bool{}); len(rows) != 0 {
		t.Fatalf("alias revived archive: %+v", rows)
	}
}
