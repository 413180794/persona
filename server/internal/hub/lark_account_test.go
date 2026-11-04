package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/lark"
)

func TestInboxLoadsEveryAccount(t *testing.T) {
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
	for _, account := range []int{1, 2} {
		h := newTestHub(t)
		h.store = database
		h.lark = lark.New(lark.Config{Account: account})
		h.inbox.init()
		h.loadInbox(ctx)
		seen := map[string]bool{}
		for _, row := range h.inboxList(false) {
			seen[row.ID] = true
		}
		if !seen["account-test-mail"] || !seen["account-test-legacy"] || !seen["account-test-second"] {
			t.Fatalf("lark.account %d: every account's items should load, got %v", account, seen)
		}
	}
}

func TestInboxAccountIsolation(t *testing.T) {
	now := time.Now()
	b := newInbox()
	conv := []lark.Conversation{{ID: "same-feed", Unread: 1, UpdatedAt: now}}
	ids := map[string]bool{}
	for _, account := range []int{1, 2} {
		rows := b.applyLarkFor(larkInboxAccount{ID: account, Label: "飞书 · 测试"}, conv,
			func(lark.Conversation, int) ([]lark.Message, error) {
				return []lark.Message{{ID: "test-message", Position: "1", CreatedAt: now}}, nil
			}, now, testUserZone)
		if len(rows) != 1 || rows[0].Lark.AccountID != account || rows[0].AccountName != "飞书 · 测试" ||
			rows[0].Lark.OLAccount != map[int]int{1: 0, 2: 2}[account] {
			t.Fatalf("account %d: one item with its account, got %+v", account, rows)
		}
		if ids[rows[0].ID] {
			t.Fatal("inbox IDs collide across accounts")
		}
		ids[rows[0].ID] = true
	}
	if !ids["lark:same-feed:1"] || !ids["lark:account:2:same-feed:1"] {
		t.Fatalf("item ids = %v", ids)
	}
	// Account 2 read the chat; account 1 has it unread still: only account 2's item settles.
	settled := b.settleLarkFor(2, nil, func(string, bool) (*lark.Message, error) { return nil, nil }, now)
	if len(settled) != 1 || settled[0].Lark.AccountID != 2 {
		t.Fatalf("settled = %+v", settled)
	}
	if larkItemAccount(&InboxItem{Lark: &InboxLark{}}) != 1 || larkItemAccount(&InboxItem{}) != 0 {
		t.Fatal("legacy rows are account 1, mail has none")
	}
}

// twoAccountlark-connector answers like an lark-connector with two signed-in accounts and records which account each request named.
func twoAccountlark-connector(t *testing.T, seen func(account, path string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account := r.Header.Get("x-lark-connector-account")
		if q := r.URL.Query().Get("ol_account"); q != "" {
			account = q
		}
		seen(account, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/agent/v1/accounts":
			_, _ = w.Write([]byte(`{"activeId":"` + account + `","accounts":[
				{"id":"1","name":"一号","userId":"u1","signedIn":true,"primary":true,"unreadCount":3,"active":` + strconv.FormatBool(account == "1") + `},
				{"id":"2","name":"二号","userId":"u2","signedIn":true,"primary":false,"unreadCount":1,"avatarUrl":"/agent/v1/media/a?ol_account=2"},
				{"id":"3","name":"三号","signedIn":false,"primary":false,"unreadCount":0}]}`))
		case "/agent/v1/conversations":
			if r.URL.Query().Get("scope") != "unread" {
				_, _ = w.Write([]byte(`{"items":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"feedId":"feed-` + account + `","name":"会话` + account + `","unreadCount":1,"updatedAt":"` +
				time.Now().UTC().Format(time.RFC3339) + `"}]}`))
		case "/agent/v1/conversations/feed-" + account + "/messages":
			_, _ = w.Write([]byte(`{"items":[{"messageId":"m-` + account + `","position":"5","senderName":"同事","text":"你好","createdAt":"` +
				time.Now().UTC().Format(time.RFC3339) + `"}]}`))
		default:
			_, _ = w.Write([]byte(`{"items":[]}`))
		}
	}))
}

func TestLarkRoutesFollowAccount(t *testing.T) {
	var got []string
	upstream := twoAccountlark-connector(t, func(account, path string) { got = append(got, account+" "+path) })
	defer upstream.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"})}
	mux := http.NewServeMux()
	h.larkRoutes(mux)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	for _, c := range []struct{ path, account string }{
		{"/api/v1/lark/conversations?scope=unread", "1"},
		{"/api/v1/lark/conversations?scope=unread&ol_account=2", "2"},
		{"/api/v1/lark/media/key?ol_account=2", "2"},
		{"/api/v1/lark/raw/me?ol_account=2", "2"},
	} {
		got = nil
		if w := get(c.path); w.Code != http.StatusOK || len(got) != 1 || !strings.HasPrefix(got[0], c.account+" ") {
			t.Fatalf("%s: status %d, upstream %v; want account %s", c.path, w.Code, got, c.account)
		}
	}
	for _, bad := range []string{"0", "-1", "two", "02", "1000000000"} {
		got = nil
		if w := get("/api/v1/lark/conversations?ol_account=" + bad); w.Code != http.StatusBadRequest || len(got) != 0 {
			t.Fatalf("ol_account=%s: status %d, upstream %v", bad, w.Code, got)
		}
	}
	w := get("/api/v1/lark/accounts")
	var out struct {
		Accounts []lark.Account `json:"accounts"`
		Default  int            `json:"default"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != http.StatusOK {
		t.Fatalf("accounts: %d %s", w.Code, w.Body)
	}
	if out.Default != 1 || len(out.Accounts) != 3 || out.Accounts[1].Name != "二号" || out.Accounts[1].Avatar != "/api/v1/lark/media/a?ol_account=2" ||
		!out.Accounts[0].Primary || out.Accounts[2].SignedIn {
		t.Fatalf("accounts = %+v", out)
	}
}

func TestCollectLarkEveryAccount(t *testing.T) {
	upstream := twoAccountlark-connector(t, func(string, string) {})
	defer upstream.Close()
	for _, def := range []int{1, 2} {
		h := newTestHub(t)
		h.lark = lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token", Account: def})
		h.inbox.init()
		h.inbox.larkAccount = def
		if ids := h.larkSignedIn(context.Background()); len(ids) != 2 || ids[0].ID != def {
			t.Fatalf("lark.account %d: signed in = %+v", def, ids)
		}
		items, err := h.collectLark(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byFeed := map[string]*InboxItem{}
		for _, it := range items {
			byFeed[it.Lark.FeedID] = it
		}
		one, two := byFeed["feed-1"], byFeed["feed-2"]
		if len(items) != 2 || one == nil || two == nil || one.Lark.AccountID != 1 || two.Lark.AccountID != 2 ||
			one.AccountName != "飞书 · 一号" || two.AccountName != "飞书 · 二号" || byFeed["feed-3"] != nil ||
			one.Lark.OLAccount != map[int]int{1: 0, 2: 1}[def] || two.Lark.OLAccount != map[int]int{1: 2, 2: 0}[def] {
			t.Fatalf("lark.account %d: items = %+v", def, items)
		}
	}
}

func TestLarkAccountLabel(t *testing.T) {
	if larkAccountLabel(lark.Account{Name: "一号"}, false) != "飞书" || larkAccountLabel(lark.Account{}, true) != "飞书" ||
		larkAccountLabel(lark.Account{Name: "一号"}, true) != "飞书 · 一号" {
		t.Fatal("source line of a Feishu item")
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

func TestLarkMessagesToolAccount(t *testing.T) {
	var got []string
	upstream := twoAccountlark-connector(t, func(account, path string) { got = append(got, account+" "+path) })
	defer upstream.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"})}
	for _, c := range []struct{ args, account string }{
		{`{"chat_id":"feed-1"}`, "1"},
		{`{"chat_id":"feed-2","account":2}`, "2"},
	} {
		got = nil
		out, err := h.runLarkTool(context.Background(), "lark_messages", json.RawMessage(c.args))
		if err != nil || !strings.Contains(out, "同事") || len(got) == 0 {
			t.Fatalf("%s: %q, %v, upstream %v", c.args, out, err, got)
		}
		for _, call := range got {
			if !strings.HasPrefix(call, c.account+" ") {
				t.Fatalf("%s: upstream %v, want account %s", c.args, got, c.account)
			}
		}
	}
}
