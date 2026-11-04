package hub

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

func TestChatPinAccountBoundRoutes(t *testing.T) {
	for _, mode := range []string{"list", "changed", "wrong-owner", "locate", "add", "bad-body", "logout", "changed-after-write", "changed-before-write"} {
		t.Run(mode, func(t *testing.T) {
			accounts, reads, writes := 0, 0, 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/accounts":
					accounts++
					account := "one"
					if (mode == "changed" && accounts > 1) || (mode == "changed-after-write" && accounts > 2) || (mode == "changed-before-write" && accounts > 1) {
						account = "two"
					}
					fmt.Fprintf(w, `{"activeId":"1","accounts":[{"id":"1","signedIn":%t,"userId":%q}]}`, mode != "logout", account)
				case "/conversations/123/pins":
					reads++
					fmt.Fprint(w, `{"items":[]}`)
				case "/messages/20":
					fmt.Fprint(w, `{"item":{"id":"20","feedId":"123","position":"8","capabilities":{"pin":true}}}`)
				case "/conversations/123/messages":
					fmt.Fprint(w, `{"items":[{"id":"20"}]}`)
				case "/messages/20/pin":
					writes++
					fmt.Fprint(w, `{"pinned":true}`)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			}))
			defer up.Close()
			h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "synthetic"})}
			mux := http.NewServeMux()
			h.larkRoutes(mux)
			method, path, body := "GET", "/api/v1/lark/conversations/123/pins", ""
			if mode == "wrong-owner" {
				path += "?account_id=old"
			}
			if mode == "locate" {
				path = "/api/v1/lark/conversations/123/pin-messages/20?account_id=one"
			}
			if mode == "add" || mode == "bad-body" || mode == "changed-after-write" || mode == "changed-before-write" {
				method, body = "POST", `{"account_id":"one","message_id":"20"}`
			}
			if mode == "bad-body" {
				body = `{"account_id":"one","message_id":"20","extra":true}`
			}
			code, out := do(t, mux, method, path, body)
			switch mode {
			case "list":
				if code != 200 || out["account_id"] != "one" || fmt.Sprint(out["items"]) != "[]" {
					t.Fatalf("bad page %d %v", code, out)
				}
			case "changed-after-write", "changed-before-write":
				want := 0
				if mode == "changed-after-write" {
					want = 1
				}
				if code != 409 || writes != want || out["pinned"] != nil {
					t.Fatalf("account mutation %d %v, writes %d", code, out, writes)
				}
			case "changed", "wrong-owner":
				if code != 409 || out["items"] != nil || out["code"] != "lark_account_changed" {
					t.Fatalf("leaked page %d %v", code, out)
				}
			case "locate":
				if code != 200 || out["target"] == nil {
					t.Fatalf("target %d %v", code, out)
				}
			case "add":
				if code != 200 || writes != 1 || out["pinned"] != true {
					t.Fatalf("mutation %d %v", code, out)
				}
			case "bad-body":
				if code != 400 || writes != 0 || accounts != 0 {
					t.Fatalf("bad body was accepted: %d", code)
				}
			case "logout":
				if code != 502 || out["code"] != "lark_not_logged_in" || reads != 0 {
					t.Fatalf("login %d %v", code, out)
				}
			}
			if mode == "wrong-owner" && reads != 0 {
				t.Fatal("read other account's pins")
			}
		})
	}
}

func TestChatPinsNoStoreEvenOnFailure(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer up.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "synthetic"})}
	mux := http.NewServeMux()
	h.larkRoutes(mux)
	for _, path := range []string{"/api/v1/lark/conversations/123/pins", "/api/v1/lark/conversations/123/pin-messages/20"} {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("pin response could be cached")
		}
	}
}

func TestChatPinWriteRequiresFullRole(t *testing.T) {
	path := "/api/v1/lark/conversations/123/pins"
	if allowed(principal{Role: roleAnswer}, "POST", path) {
		t.Fatal("answer-only device changed Pins")
	}
	if !allowed(principal{Role: roleFull}, "POST", path) {
		t.Fatal("full device cannot change Pins")
	}
}
