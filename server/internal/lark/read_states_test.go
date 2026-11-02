package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestReadStates(t *testing.T) {
	var calls int
	var pairs []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/agent/v1/messages/read-states" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Pairs []map[string]string `json:"pairs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pairs = body.Pairs
		// m-2 has no receipt (e.g. still sending): lark-connector leaves it out.
		_, _ = io.WriteString(w, `{"items":[
			{"messageId":"m-1","readCount":3,"unreadCount":1,"readUserIds":["u1","u2","u3"],"unreadUserIds":["u4"]},
			{"messageId":"m-3","readCount":0,"unreadCount":4}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "tok"})
	ctx := context.Background()

	got, err := c.ReadStates(ctx, "feed-1", []string{"m-1", "m-2", "m-3"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ReadState{{MessageID: "m-1", ReadCount: 3, UnreadCount: 1}, {MessageID: "m-3", ReadCount: 0, UnreadCount: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadStates = %+v, want %+v", got, want)
	}
	wantPairs := []map[string]string{{"feedId": "feed-1", "messageId": "m-1"}, {"feedId": "feed-1", "messageId": "m-2"}, {"feedId": "feed-1", "messageId": "m-3"}}
	if !reflect.DeepEqual(pairs, wantPairs) {
		t.Fatalf("pairs = %v, want %v", pairs, wantPairs)
	}

	// Nothing to ask: no call, an empty (not nil) list.
	if got, err := c.ReadStates(ctx, "feed-1", nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty: %v, %v", got, err)
	}
	ids := make([]string, MaxReadStates+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("m-%d", i)
	}
	if _, err := c.ReadStates(ctx, "feed-1", ids); err == nil {
		t.Fatal("over the cap: want an error")
	}
	if calls != 1 {
		t.Fatalf("lark-connector called %d times, want 1", calls)
	}
}
