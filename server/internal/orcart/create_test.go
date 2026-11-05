package orcart_test

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
)

func TestCreateTerminal(t *testing.T) {
	fake := orcarttest.New(t)
	c := &orcart.Client{MetaPath: fake.MetaPath}

	created, err := c.CreateTerminal(context.Background(), "id:wt-1", "my-tab", "mut-1")
	if err != nil {
		t.Fatalf("CreateTerminal failed: %v", err)
	}
	if created.Handle == "" {
		t.Errorf("expected non-empty handle, got empty")
	}
	if created.WorktreeID != "wt-1" {
		t.Errorf("expected WorktreeID wt-1, got %s", created.WorktreeID)
	}
	if created.Title != "my-tab" {
		t.Errorf("expected Title my-tab, got %s", created.Title)
	}
	if !created.IsLocal() {
		t.Errorf("expected terminal to be local")
	}
}

func TestRenameAndPaneTitle(t *testing.T) {
	fake := orcarttest.New(t)
	c := &orcart.Client{MetaPath: fake.MetaPath}

	created, err := c.CreateTerminal(context.Background(), "id:wt-1", "old-name", "mut-1")
	if err != nil {
		t.Fatalf("CreateTerminal failed: %v", err)
	}

	target := created.Target()
	if err := c.RenameTerminal(context.Background(), target, "new-tab-name"); err != nil {
		t.Fatalf("RenameTerminal failed: %v", err)
	}

	if err := c.SetPaneTitle(context.Background(), target, "pane-name"); err != nil {
		t.Fatalf("SetPaneTitle failed: %v", err)
	}
}
