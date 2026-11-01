package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestInboxAccountSwitchPersists(t *testing.T) {
	dsn := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now()
	for _, row := range []InboxItem{
		{ID: "account-test-legacy", UpdatedAt: now, Lark: &InboxLark{FeedID: "same-feed"}},
		{ID: "account-test-second", UpdatedAt: now, Lark: &InboxLark{AccountID: 2, FeedID: "same-feed"}},
		{ID: "account-test-mail", UpdatedAt: now, Source: "mail"},
	} {
		raw, _ := json.Marshal(row)
		if err := database.PutInboxItem(ctx, row.ID, raw, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []int{1, 2, 1} {
		h := newTestHub(t)
		h.store = database
		h.lark = lark.New(lark.Config{Account: account})
		h.inbox.init()
		h.loadInbox(ctx)
		seen := map[string]bool{}
		for _, row := range h.inboxList() {
			seen[row.ID] = true
		}
		if !seen["account-test-mail"] || seen["account-test-legacy"] != (account == 1) || seen["account-test-second"] != (account == 2) {
			t.Fatalf("account %d: persisted inbox selection was incorrect", account)
		}
	}
}

func TestInboxAccountIsolation(t *testing.T) {
	for _, selected := range []int{0, 1, 2} {
		b := newInbox()
		b.larkAccount = selected
		for _, stored := range []int{0, 1, 2} {
			it := &InboxItem{Lark: &InboxLark{AccountID: stored}}
			want := max(stored, 1) == max(selected, 1)
			if b.acceptsAccount(it) != want {
				t.Errorf("selected %d, stored %d: want accepted %v", selected, stored, want)
			}
		}
		if !b.acceptsAccount(&InboxItem{Source: "mail"}) {
			t.Fatal("switching Lark accounts hid a mail item")
		}
	}
	now := time.Now()
	ids := map[string]bool{}
	for _, account := range []int{1, 2} {
		b := newInbox()
		b.larkAccount = account
		rows := b.applyLark([]lark.Conversation{{ID: "same-feed", Unread: 1, UpdatedAt: now}},
			func(lark.Conversation, int) ([]lark.Message, error) {
				return []lark.Message{{ID: "test-message", Position: "1", CreatedAt: now}}, nil
			}, now, testUserZone)
		if len(rows) != 1 || rows[0].Lark.AccountID != account {
			t.Fatalf("account %d: missing account metadata", account)
		}
		if ids[rows[0].ID] {
			t.Fatal("inbox IDs collide across accounts")
		}
		ids[rows[0].ID] = true
	}
}

func TestLoadConfigRejectsInvalidLarkAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".jarvis")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(`{"token":"test-token","lark":{"account":-1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "lark.account") {
		t.Fatalf("want account validation error, got %v", err)
	}
}
