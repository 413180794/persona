package hub

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

func TestLarkConversationSnapshotAccountBoundary(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		for _, mode := range []string{"same", "changed", "expected-mismatch", "signed-out"} {
			t.Run(fmt.Sprintf("%t/%s", pinned, mode), func(t *testing.T) {
				accounts, feeds := 0, 0
				up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/accounts" {
						accounts++
						user := "selected"
						if mode == "changed" && accounts > 1 {
							user = "different"
						}
						fmt.Fprintf(w, `{"activeId":"2","accounts":[{"id":"1","primary":true,"signedIn":true,"userId":"wrong"},{"id":"2","signedIn":%t,"userId":%q}]}`, mode != "signed-out", user)
						return
					}
					feeds++
					fmt.Fprint(w, `{"items":[]}`)
				}))
				defer up.Close()
				h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "synthetic"})}
				path := "/api/v1/lark/conversations?account_bound=1"
				if pinned {
					path = "/api/v1/lark/pinned?account_bound=1"
				}
				if mode == "expected-mismatch" {
					path += "&account_id=old"
				}
				mux := http.NewServeMux()
				h.larkRoutes(mux)
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
				if recorder.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("account-bound responses must not be cached by an intermediary")
				}
				accounts, feeds = 0, 0
				code, out := do(t, mux, "GET", path, "")
				if mode == "same" {
					if code != 200 || out["account_id"] != "selected" || fmt.Sprint(out["conversations"]) != "[]" {
						t.Fatalf("invalid account snapshot: %d %v", code, out)
					}
				} else {
					expected := http.StatusConflict
					if mode == "signed-out" {
						expected = http.StatusBadGateway
					}
					if code != expected || out["conversations"] != nil || out["account_id"] != nil {
						t.Fatalf("cross-account data published: %d %v", code, out)
					}
					if mode != "changed" && feeds != 0 {
						t.Fatal("read feeds despite an invalid account")
					}
				}
			})
		}
	}
}

func TestLarkConversationSnapshotKeepsLegacyReadsUnbound(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts" {
			t.Error("legacy reads must not acquire a new account dependency")
		}
		fmt.Fprint(w, `{"items":[]}`)
	}))
	defer up.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "synthetic"})}
	mux := http.NewServeMux()
	h.larkRoutes(mux)
	for _, path := range []string{"/api/v1/lark/pinned", "/api/v1/lark/conversations"} {
		code, out := do(t, mux, "GET", path, "")
		if code != 200 || out["account_id"] != nil {
			t.Fatalf("legacy contract changed: %d %v", code, out)
		}
	}
}
