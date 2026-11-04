package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatPinsProjectionAndPagination(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations/123/pins" || r.URL.Query().Get("cursor") != "50" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprint(w, `{"items":[{"id":"10","messageId":"20","message":{"id":"20","feedId":"123","position":"7","localState":"synced","capabilities":{"pin":false,"unpin":true},"senderAvatarUrl":"/agent/v1/media/avatar","olk":{"version":1,"state":"decrypted","publicParts":[{"kind":"text","text":"cover"}],"bodyParts":[{"kind":"text","text":"body"}]} }},{"id":"11","messageId":"21"},{"id":"10","messageId":"20"}],"hasMore":true,"nextCursor":"100","total":5}`)
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "synthetic"})
	p, err := c.ChatPins(context.Background(), "123", "50", 50)
	if err != nil || len(p.Items) != 2 || p.NextCursor != "100" || p.Total != 5 {
		t.Fatalf("page: %+v, %v", p, err)
	}
	m := p.Items[0].Message
	if m == nil || m.OLK == nil || m.SenderAvatar != "/api/v1/lark/media/avatar" || m.CanPin == nil || *m.CanPin || m.CanUnpin == nil || !*m.CanUnpin {
		t.Fatalf("projection lost: %+v", m)
	}
	if p.Items[1].Message != nil {
		t.Fatal("missing message was invented")
	}
}

func TestChatPinsRejectsIncompletePagesAndCrossFeedMessage(t *testing.T) {
	for _, tc := range []struct {
		body string
		fail bool
	}{
		{`{"items":[],"hasMore":true}`, true},
		{`{"items":[],"hasMore":true,"nextCursor":"50"}`, true},
		{`{"items":[],"hasMore":true,"nextCursor":"bad"}`, true},
		{`{"items":[{"id":"1"}]}`, true},
		{`{"items":null}`, false},
		{`{"items":[{"id":"1","messageId":"2","message":{"id":"2","feedId":"other"}}]}`, false},
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
		c := New(Config{BaseURL: up.URL, Token: "synthetic"})
		p, err := c.ChatPins(context.Background(), "123", "50", 50)
		up.Close()
		if (err != nil) != tc.fail {
			t.Fatalf("%s: %+v %v", tc.body, p, err)
		}
		if !tc.fail {
			if p.Items == nil {
				t.Fatal("empty pins encoded as null")
			}
			for _, p := range p.Items {
				if p.Message != nil {
					t.Fatal("another chat's body escaped")
				}
			}
		}
	}
}

func TestPinErrorsKeepMissingRouteAndResourceDistinct(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code, want string
	}{
		{404, "", "lark_lark-connector_upgrade_required"}, {405, "METHOD_NOT_ALLOWED", "lark_lark-connector_upgrade_required"},
		{404, "MESSAGE_NOT_FOUND", "lark_pin_not_found"}, {403, "FORBIDDEN", "lark_pin_forbidden"},
		{502, "INTERNAL", "lark_pins_unavailable"},
	} {
		err := pinReadError(&doError{Status: tc.status, Code: tc.code, Message: "private marker"})
		var e *ChatPinError
		if !errors.As(err, &e) || e.Code != tc.want || strings.Contains(e.Message, "private marker") {
			t.Fatalf("unsafe error %v", err)
		}
	}
	if !errors.Is(pinReadError(ErrNotLoggedIn), ErrNotLoggedIn) {
		t.Fatal("login error changed into upgrade")
	}
}

func TestChatPinMutationVerifiesIdentityCapabilityAndMatchingPin(t *testing.T) {
	for _, mode := range []string{"add", "remove", "remove-recalled", "forbidden", "unknown-capability", "changed-account", "wrong-feed", "wrong-pin", "lost-receipt"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/messages/20":
					feed, caps := "123", `,"capabilities":{"pin":true,"unpin":true}`
					if mode == "wrong-feed" {
						feed = "different"
					}
					if mode == "remove-recalled" {
						caps += `,"isRecalled":true,"isDeleted":true`
					}
					if mode == "forbidden" {
						caps = `,"capabilities":{"pin":false}`
					}
					if mode == "unknown-capability" {
						caps = ""
					}
					fmt.Fprintf(w, `{"item":{"id":"20","feedId":%q,"localState":"synced"%s}}`, feed, caps)
				case "/conversations/123/pins":
					id := "20"
					if mode == "wrong-pin" {
						id = "99"
					}
					fmt.Fprintf(w, `{"items":[{"id":"10","messageId":%q}]}`, id)
				case "/accounts":
					id := "account"
					if mode == "changed-account" {
						id = "different"
					}
					fmt.Fprintf(w, `{"activeId":"1","accounts":[{"id":"1","signedIn":true,"userId":%q}]}`, id)
				case "/messages/20/pin":
					writes++
					if mode == "remove" || mode == "remove-recalled" {
						var body map[string]string
						_ = json.NewDecoder(r.Body).Decode(&body)
						if r.Method != "DELETE" || body["pinId"] != "10" {
							t.Fatal("wrong unpin request")
						}
						fmt.Fprint(w, `{"pinned":false}`)
					} else if mode == "lost-receipt" {
						fmt.Fprint(w, `{}`)
					} else {
						fmt.Fprint(w, `{"pinned":true}`)
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer up.Close()
			c := New(Config{BaseURL: up.URL, Token: "synthetic"})
			err := c.ChangeChatPin(context.Background(), "account", "123", "20", "10", (mode == "remove" || mode == "remove-recalled") || mode == "wrong-pin")
			if mode == "add" || (mode == "remove" || mode == "remove-recalled") {
				if err != nil || writes != 1 {
					t.Fatalf("mutation %d %v", writes, err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid mutation accepted")
			}
			want := 0
			if mode == "lost-receipt" {
				want = 1
				var e *ChatPinError
				if !errors.As(err, &e) || e.Code != "lark_pin_result_unknown" {
					t.Fatal(err)
				}
			}
			if writes != want {
				t.Fatalf("writes=%d, want %d", writes, want)
			}
		})
	}
}

func TestResolveChatPinUsesChatOrThreadAndRequiresTarget(t *testing.T) {
	for _, mode := range []string{"chat", "reply", "topic-root", "missing"} {
		t.Run(mode, func(t *testing.T) {
			reads := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/messages/20":
					extra := ""
					if mode == "reply" {
						extra = `,"threadId":"10","threadMessageType":"reply"`
					}
					fmt.Fprintf(w, `{"item":{"id":"20","feedId":"123","position":"8"%s}}`, extra)
				case "/messages/10":
					fmt.Fprint(w, `{"item":{"id":"10","feedId":"123"}}`)
				case "/messages/20/thread":
					fmt.Fprint(w, `{"items":[]}`)
				case "/messages/10/thread":
					reads++
					if reads == 1 {
						fmt.Fprint(w, `{"items":[{"id":"30","position":"9"}],"hasOlder":true,"nextOlderCursor":"5"}`)
					} else {
						if r.URL.Query().Get("beforePosition") != "5" {
							t.Fatal("thread cursor lost")
						}
						fmt.Fprint(w, `{"items":[{"id":"20","position":"8"}]}`)
					}
				case "/conversations/123/messages":
					if r.URL.Query().Get("aroundPosition") != "8" {
						t.Fatal("did not locate around original")
					}
					id := "20"
					if mode == "missing" {
						id = "other"
					}
					fmt.Fprintf(w, `{"items":[{"id":%q}]}`, id)
				default:
					t.Errorf("unexpected %s", r.URL)
				}
			}))
			defer up.Close()
			c := New(Config{BaseURL: up.URL, Token: "synthetic"})
			target, err := c.ResolveChatPin(context.Background(), "123", "20", mode == "topic-root")
			if mode == "missing" {
				if err == nil {
					t.Fatal("missing target accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (target.Root != nil) != (mode != "chat") {
				t.Fatal("wrong navigation target")
			}
			if mode == "reply" && (reads != 2 || len(target.Page.Messages) != 2) {
				t.Fatal("older reply was not located")
			}
		})
	}
}

func TestChatPinEventsIgnoreReadAcknowledgements(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"type":"pins.updated","feedId":"123"}`))
	if !ok || len(ev.FeedIDs) != 1 || ev.FeedIDs[0] != "123" {
		t.Fatal("pin update lost")
	}
	if _, ok := parseEvent([]byte(`{"type":"pins.read-state.updated","feedId":"123"}`)); ok {
		t.Fatal("list read would trigger a read-event loop")
	}
}
