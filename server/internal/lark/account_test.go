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
			if err := c.Send(ctx, "test-feed", "test message"); err != nil {
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
