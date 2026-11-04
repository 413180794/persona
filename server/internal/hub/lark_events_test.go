package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/nwverse/persona/server/internal/lark"
)

func TestLarkEventBatchKeepsSettingsAlongsideMessages(t *testing.T) {
	settings := lark.Event{Type: "local.settings.updated"}
	messages := lark.Event{Type: "message.updated", FeedIDs: []string{"b", "a", "b"}}
	for _, events := range [][]lark.Event{{settings, messages}, {messages, settings}, {settings, messages, settings}} {
		var batch larkEventBatch
		for _, ev := range events {
			batch.add(ev)
		}
		got := batch.drain()
		if got["settings_changed"] != true || !reflect.DeepEqual(got["feed_ids"], []string{"a", "b"}) {
			t.Fatalf("lost invalidation: %#v", got)
		}
		batch.add(lark.Event{Type: "message.updated", FeedIDs: []string{"c"}})
		got = batch.drain()
		if got["settings_changed"] != false || !reflect.DeepEqual(got["feed_ids"], []string{"c"}) {
			t.Fatalf("leaked earlier batch: %#v", got)
		}
		batch.add(settings)
		got = batch.drain()
		if got["settings_changed"] != true || !reflect.DeepEqual(got["feed_ids"], []string{}) {
			t.Fatalf("lost settings-only change: %#v", got)
		}
	}
}

func TestLarkEventsPerAccount(t *testing.T) {
	var mu sync.Mutex
	signedIn := map[string]bool{"1": true, "2": true}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account := r.Header.Get("x-lark-connector-account")
		switch r.URL.Path {
		case "/agent/v1/accounts":
			mu.Lock()
			two := signedIn["2"]
			mu.Unlock()
			_, _ = w.Write([]byte(`{"activeId":"` + account + `","accounts":[{"id":"1","signedIn":true,"primary":true,"unreadCount":0},` +
				`{"id":"2","signedIn":` + strconv.FormatBool(two) + `,"primary":false,"unreadCount":0}]}`))
		case "/agent/v1/events":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"conversation.upserted","feedId":"feed-`+account+`"}`))
			<-r.Context().Done()
		}
	}))
	defer upstream.Close()
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{BaseURL: upstream.URL + "/agent/v1", Token: "test-token"})
	app := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}}
	h.clients[app] = struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.larkFollow(ctx, h.lark)
	following := map[int]context.CancelFunc{}
	h.larkAccountEvents(ctx, following)
	if len(following) != 1 || following[2] == nil {
		t.Fatalf("following %v, want account 2 beside lark.account", following)
	}
	want := map[float64]string{1: "feed-1", 2: "feed-2"}
	deadline := time.After(5 * time.Second)
	for len(want) > 0 {
		select {
		case b := <-app.out:
			var m struct {
				Type      string   `json:"type"`
				Account   float64  `json:"account"`
				OLAccount float64  `json:"ol_account"`
				FeedIDs   []string `json:"feed_ids"`
			}
			if json.Unmarshal(b, &m) != nil || m.Type != "lark" {
				continue
			}
			if feed, ok := want[m.Account]; !ok || len(m.FeedIDs) != 1 || m.FeedIDs[0] != feed || m.OLAccount != map[float64]float64{1: 0, 2: 2}[m.Account] {
				t.Fatalf("event %s", b)
			}
			delete(want, m.Account)
		case <-deadline:
			t.Fatalf("no event for accounts %v", want)
		}
	}
	mu.Lock()
	signedIn["2"] = false
	mu.Unlock()
	h.larkAccounts.at = time.Time{} // as if the list were read a minute ago
	h.larkAccountEvents(ctx, following)
	if len(following) != 0 {
		t.Fatalf("following %v after account 2 signed out", following)
	}
}
