package node

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

func TestDirsRecentExcludesAssistantDirectories(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "custom-data")
	t.Setenv("PERSONA_HOME", root)
	n := &Node{home: home, metas: map[string]quickMeta{}, agents: map[string]host.Agent{}}
	newest := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, path := range []string{
		filepath.Join(home, ".persona", "assistant"),
		filepath.Join(home, ".persona", "assistant"),
		filepath.Join(root, "assistant"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		n.metas[fmt.Sprint(i)] = quickMeta{cwd: path, mtime: newest}
		n.agents[fmt.Sprint(i)] = host.Agent{Cwd: path}
	}
	alias := filepath.Join(home, "alias", "assistant")
	if err := os.MkdirAll(filepath.Dir(alias), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "assistant"), alias); err != nil {
		t.Fatal(err)
	}
	n.metas["alias"] = quickMeta{cwd: alias, mtime: newest}

	project := filepath.Join(home, "projects", "app")
	assistantProject := filepath.Join(home, "projects", "assistant")
	liveProject := filepath.Join(home, "projects", "live")
	for _, path := range []string{project, assistantProject, liveProject} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	n.metas["app"] = quickMeta{cwd: project, mtime: newest.Add(-time.Hour)}
	n.metas["app-older"] = quickMeta{cwd: project, mtime: newest.Add(-3 * time.Hour)}
	n.metas["assistant-project"] = quickMeta{cwd: assistantProject, mtime: newest.Add(-2 * time.Hour)}
	n.metas["missing"] = quickMeta{cwd: filepath.Join(home, "missing"), mtime: newest}
	n.agents["live"] = host.Agent{Cwd: liveProject}

	dirs, err := n.dirs("~")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{project, assistantProject, liveProject}
	if len(dirs.Recent) != len(want) {
		t.Fatalf("recent directories = %+v, want %v", dirs.Recent, want)
	}
	for i, path := range want {
		if dirs.Recent[i].Path != path {
			t.Errorf("recent[%d] = %q, want %q", i, dirs.Recent[i].Path, path)
		}
	}
	if !dirs.Recent[0].LastUsed.Equal(newest.Add(-time.Hour)) {
		t.Errorf("project last used = %v", dirs.Recent[0].LastUsed)
	}
	if len(n.metas) != 8 || len(n.agents) != 4 {
		t.Fatal("directory suggestions must preserve existing session metadata and agents")
	}
	if _, err := n.dirs(filepath.Join(root, "assistant")); err != nil {
		t.Fatalf("explicitly browsing the assistant directory: %v", err)
	}
}

func TestRecentDirsAppliesLimitAfterExcludingAssistant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", filepath.Join(home, ".persona"))
	n := &Node{home: home, metas: map[string]quickMeta{}}
	for i := range 16 {
		path := filepath.Join(home, fmt.Sprintf("project-%02d", i))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		n.metas[path] = quickMeta{cwd: path, mtime: time.Unix(int64(i+1), 0)}
	}
	assistant := filepath.Join(home, ".persona", "assistant")
	if err := os.MkdirAll(assistant, 0o700); err != nil {
		t.Fatal(err)
	}
	n.agents = map[string]host.Agent{"assistant": {Cwd: assistant}}
	n.metas[assistant] = quickMeta{cwd: assistant, mtime: time.Unix(100, 0)}
	got := n.recentDirs()
	if len(got) != 15 {
		t.Fatalf("got %d recent directories, want 15", len(got))
	}
	for i, dir := range got {
		want := filepath.Join(home, fmt.Sprintf("project-%02d", 15-i))
		if dir.Path != want {
			t.Errorf("recent[%d] = %q, want %q", i, dir.Path, want)
		}
	}
}
