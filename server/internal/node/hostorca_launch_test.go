package node

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
)

func TestOrcaCreateTerminalInRegisteredWorktree(t *testing.T) {
	n, _, o, _, home := orcaNode(t)
	repoDir := filepath.Join(home, "my-project")
	mkdir(t, repoDir)

	o.SetWorktrees(orcarttest.Worktree("wt-proj", repoDir, "refs/heads/main"))

	res, err := request(t, n, "session.start", map[string]any{
		"session_id": "0e000000-0000-4000-8000-000000000001",
		"cwd":        repoDir,
		"async":      true,
	})
	if err != nil {
		t.Fatalf("session.start failed: %v", err)
	}
	session, ok := res["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected session map in result, got %v", res)
	}
	if sid, _ := session["session_id"].(string); sid != "0e000000-0000-4000-8000-000000000001" {
		t.Errorf("expected session ID 0e000000-0000-4000-8000-000000000001, got %v", session["session_id"])
	}

	// Verify terminal.create was called in fake Orca
	deadline := time.Now().Add(5 * time.Second)
	for len(o.Trace("terminal.create")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for terminal.create; calls: %v", o.Trace())
		}
		time.Sleep(20 * time.Millisecond)
	}

	createCalls := o.Trace("terminal.create")
	if len(createCalls) == 0 {
		t.Fatalf("expected terminal.create call, got none")
	}
	if !strings.Contains(createCalls[0], `"worktree":"id:wt-proj"`) {
		t.Errorf("expected terminal.create for wt-proj, got %s", createCalls[0])
	}
}

func TestOrcaCreateTerminalInSubdirectory(t *testing.T) {
	n, _, o, _, home := orcaNode(t)
	repoDir := filepath.Join(home, "monorepo")
	subDir := filepath.Join(repoDir, "services", "api")
	mkdir(t, subDir)

	o.SetWorktrees(orcarttest.Worktree("wt-mono", repoDir, "refs/heads/main"))

	res, err := request(t, n, "session.start", map[string]any{
		"session_id": "0e000000-0000-4000-8000-000000000002",
		"cwd":        subDir,
		"async":      true,
	})
	if err != nil {
		t.Fatalf("session.start in subDir failed: %v", err)
	}
	session, ok := res["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected session map in result, got %v", res)
	}
	if cwd, _ := session["cwd"].(string); cwd != subDir {
		t.Errorf("expected Cwd %s, got %v", subDir, session["cwd"])
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(o.Trace("terminal.create")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for terminal.create; calls: %v", o.Trace())
		}
		time.Sleep(20 * time.Millisecond)
	}

	createCalls := o.Trace("terminal.create")
	if !strings.Contains(createCalls[0], `"worktree":"id:wt-mono"`) {
		t.Errorf("expected terminal.create for wt-mono, got %s", createCalls[0])
	}

	// Verify the launched shell command executed cd -- subDir
	for len(o.Writes()) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for terminal write; writes: %v", o.Writes())
		}
		time.Sleep(20 * time.Millisecond)
	}
	w := o.Writes()[0]
	if !strings.Contains(w.Text, "cd -- "+shellQuote(subDir)) {
		t.Errorf("expected cd to %s, got %s", subDir, w.Text)
	}
}

func TestOrcaFallbackToAvailableWorktree(t *testing.T) {
	n, _, o, _, home := orcaNode(t)
	otherDir := filepath.Join(home, "unregistered")
	mkdir(t, otherDir)

	// Orca has a worktree open for another project
	wtDir := filepath.Join(home, "open-project")
	mkdir(t, wtDir)
	o.SetWorktrees(orcarttest.Worktree("wt-open", wtDir, "refs/heads/main"))

	launch, err := hostCap[host.Launcher](n)
	if err != nil {
		t.Fatalf("expected host.Launcher, got error: %v", err)
	}

	created, err := launch.CreateSpace(context.Background(), otherDir, "test")
	if err != nil {
		t.Fatalf("expected fallback to wt-open, got err: %v", err)
	}
	if created.SpaceID != "wt-open" {
		t.Errorf("expected container SpaceID wt-open, got %s", created.SpaceID)
	}
}

func TestOrcaRefusesWhenNoWorktrees(t *testing.T) {
	n, _, o, _, home := orcaNode(t)
	unregDir := filepath.Join(home, "unregistered")
	mkdir(t, unregDir)

	// No worktrees in Orca at all
	o.SetWorktrees()

	launch, err := hostCap[host.Launcher](n)
	if err != nil {
		t.Fatalf("expected host.Launcher, got error: %v", err)
	}

	_, err = launch.CreateSpace(context.Background(), unregDir, "test")
	if err == nil {
		t.Fatalf("expected refusal when no worktrees in Orca, got nil")
	}
	if !strings.Contains(err.Error(), "orca_not_registered") && !strings.Contains(err.Error(), "没有任何打开的项目") {
		t.Errorf("expected orca_not_registered refusal, got %v", err)
	}
}

func TestOrcaRefusesOfflineResume(t *testing.T) {
	n, _, _, _, home := orcaNode(t)
	projDir := filepath.Join(home, "my-project")
	mkdir(t, projDir)

	// Attempt session.start with resume_session_id
	_, err := request(t, n, "session.start", map[string]any{
		"session_id":        "0e000000-0000-4000-8000-000000000003",
		"resume_session_id": "0e000000-0000-4000-8000-000000000003",
		"cwd":               projDir,
	})
	if err == nil {
		t.Fatalf("expected refusal for offline resume in Orca, got nil")
	}
	if !strings.Contains(err.Error(), "orca_resume") && !strings.Contains(err.Error(), "请在电脑上恢复") {
		t.Errorf("expected orca_resume refusal, got: %v", err)
	}
}

func TestOrcaRunSingleUseGrant(t *testing.T) {
	fake := orcarttest.New(t)
	fake.SetWorktrees(orcarttest.Worktree("wt-1", "/tmp/repo", "refs/heads/main"))
	c := &orcart.Client{MetaPath: fake.MetaPath}
	h := newOrcaHost(c)

	created, err := h.CreateSpace(context.Background(), "/tmp/repo", "tab1")
	if err != nil {
		t.Fatalf("CreateSpace failed: %v", err)
	}

	// First Run should succeed
	if err := h.Run(context.Background(), created.Pane, "echo hello"); err != nil {
		t.Fatalf("first Run failed: %v", err)
	}

	// Second Run should be rejected (token already consumed)
	if err := h.Run(context.Background(), created.Pane, "echo twice"); err == nil {
		t.Fatalf("second Run expected error, got nil")
	}
}
