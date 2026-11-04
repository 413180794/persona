package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAccountSelectionAcrossTransports(t *testing.T) {
	for _, account := range []int{0, 1, 2} {
		t.Run(strconv.Itoa(account), func(t *testing.T) {
			want := strconv.Itoa(max(account, 1))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("x-lark-connector-account"); got != want {
					t.Errorf("%s account = %q, want %q", r.URL.Path, got, want)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing agent authorization")
				}
				if r.URL.Path == "/agent/v1/events" {
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.CloseNow()
					_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"conversation.upserted","feedId":"test-feed"}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[]}`))
			}))
			defer srv.Close()
			c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test-token", Account: account})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, _, err := c.Conversations(ctx, "unread", "", 1); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Send(ctx, "test-feed", "test message", ""); err != nil {
				t.Fatal(err)
			}
			if err := c.Media(ctx, httptest.NewRecorder(), "/media/test", nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := c.Raw(ctx, httptest.NewRecorder(), "GET", "/me", nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			gotEvent := false
			_ = c.followEvents(ctx, func(ev Event) { gotEvent = ev.Type == "conversation.upserted" }, func() {})
			if !gotEvent {
				t.Fatal("selected account event was not received")
			}
		})
	}
}

func TestRawAccountOverridePreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ol_account") != "1" || r.Header.Get("x-lark-connector-account") != "" {
			t.Error("explicit query account was overridden by the default header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "test-token", Account: 2})
	if err := c.Raw(context.Background(), httptest.NewRecorder(), "GET", "/me", url.Values{"ol_account": {"1"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAccountConfigValidation(t *testing.T) {
	for _, account := range []int{-1, 1000000000} {
		if (Config{Account: account}).Validate() == nil {
			t.Errorf("invalid account %d accepted", account)
		}
	}
	for _, account := range []int{0, 1, 2, 999999999} {
		if err := (Config{Account: account}).Validate(); err != nil {
			t.Errorf("valid account %d: %v", account, err)
		}
	}
	for _, body := range []string{`{"account":"2"}`, `{"account":1.5}`} {
		var cfg Config
		if json.Unmarshal([]byte(body), &cfg) == nil {
			t.Errorf("noninteger account accepted: %s", body)
		}
	}
}

func TestForAccount(t *testing.T) {
	c := New(Config{BaseURL: "http://lark-connector.invalid/agent/v1", Token: "test-token", Account: 2})
	if c.ForAccount(0) != c || c.ForAccount(2) != c {
		t.Fatal("the configured account is the client itself")
	}
	one := c.ForAccount(1)
	if one.Account() != 1 || c.ForAccount(1) != one || one.ForAccount(2) != c || one.ForAccount(3) != c.ForAccount(3) {
		t.Fatal("derived clients are kept per account and lead back to the root")
	}
	if one.ForAccount(0) != c {
		t.Fatal("zero is lark.account from any derived client")
	}
}

func TestParseAccount(t *testing.T) {
	for s, want := range map[string]int{"1": 1, "2": 2, "999999999": 999999999} {
		if got, ok := ParseAccount(s); !ok || got != want {
			t.Errorf("%q = %d %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "0", "-1", "01", "+1", "1.0", "x", "1000000000"} {
		if _, ok := ParseAccount(s); ok {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestAccountsAndMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account := r.Header.Get("x-lark-connector-account")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"activeId":"` + account + `","accounts":[
			{"id":"1","name":"一号","userId":"u1","signedIn":true,"primary":true,"unreadCount":2},
			{"id":"2","name":"二号","userId":"u2","signedIn":true,"primary":false,"unreadCount":0,"avatarUrl":"/agent/v1/media/x?ol_account=2"},
			{"id":"3","signedIn":true,"quarantined":true,"primary":false,"unreadCount":0},
			{"id":"bad","signedIn":true,"primary":false,"unreadCount":0}]}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Token: "test-token"})
	list, err := c.Accounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != 1 || !list[0].Primary || list[0].Unread != 2 || list[1].Avatar != "/api/v1/lark/media/x?ol_account=2" ||
		list[2].SignedIn || !list[2].Quarantined {
		t.Fatalf("accounts = %+v", list)
	}
	// The primary account comes first in the list whichever account is asked about: Me is the one asked about.
	for _, id := range []int{1, 2} {
		me, err := c.ForAccount(id).Me(context.Background())
		if err != nil || me.UserID != "u"+strconv.Itoa(id) {
			t.Fatalf("account %d: me = %+v, %v", id, me, err)
		}
	}
}

func TestAccountsBeforeMultiAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"no route"}}`))
	}))
	defer srv.Close()
	list, err := New(Config{BaseURL: srv.URL, Token: "test-token"}).Accounts(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != 1 || !list[0].SignedIn {
		t.Fatalf("accounts = %+v, %v", list, err)
	}
}
