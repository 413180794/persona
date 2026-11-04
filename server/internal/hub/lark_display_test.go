package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

func TestLarkDisplayScopedPatchAndConflicts(t *testing.T) {
	for _, status := range []int{200, 403, 404, 409} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			patches := 0
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-lark-connector-account") != "7" || r.Header.Get("Authorization") != "Bearer test-token" || r.URL.Query().Has("ol_account") {
					t.Error("wrong selected account")
				}
				switch r.URL.Path {
				case "/accounts":
					fmt.Fprint(w, `{"activeId":"7","accounts":[{"id":"1","userId":"primary","primary":true,"signedIn":true},{"id":"7","userId":"selected","signedIn":true}]}`)
				case "/settings/local/message-display":
					patches++
					var body map[string]map[string]bool
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != "PATCH" || len(body) != 2 || len(body["patch"]) != 1 || body["patch"]["antiRecall"] != true || len(body["expected"]) != 1 || body["expected"]["antiRecall"] {
						t.Error("wrong patch")
					}
					w.WriteHeader(status)
					if status != 200 {
						fmt.Fprint(w, `{"error":{"code":"SETTINGS_CONFLICT","message":"changed"}}`)
						return
					}
					fmt.Fprint(w, `{"antiRecall":true,"showEditHistory":false,"dynamicSignatureScript":"never return this"}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer up.Close()
			h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "test-token", Account: 7})}
			mux := http.NewServeMux()
			h.larkDisplayRoutes(larkMux{mux, h})
			request := func(account string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/lark/message-display?ol_account=7", strings.NewReader(`{"account_id":"`+account+`","key":"anti_recall","value":true,"expected":false}`)))
				return w
			}
			w := request("selected")
			if w.Code != status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "Script") || strings.Contains(w.Body.String(), "never return") {
				t.Fatal("leaked unrelated settings")
			}
			if status == 200 && !strings.Contains(w.Body.String(), `"account_id":"selected"`) {
				t.Fatal(w.Body)
			}
			if got := request("primary"); got.Code != 409 {
				t.Fatalf("wrong identity allowed: %d", got.Code)
			}
			if patches != 1 {
				t.Fatalf("unexpected write/retry count %d", patches)
			}
		})
	}
}

func TestLarkDisplayRejectsInvalidAndReadOnly(t *testing.T) {
	h := &Hub{}
	mux := http.NewServeMux()
	h.larkDisplayRoutes(larkMux{mux, h})
	for _, body := range []string{`{}`, `{"account_id":"a","key":"standIn","value":true,"expected":false}`, `{"account_id":"a","key":"anti_recall","value":true}`, `{"account_id":"a","key":"anti_recall","value":true,"expected":false,"script":"x"}`, `{"account_id":"a","key":"anti_recall","value":true,"expected":false} {}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/lark/message-display", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	}
	if allowed(principal{Role: roleAnswer}, "POST", "/api/v1/lark/message-display") {
		t.Fatal("read-only device allowed to change preferences")
	}
}

func TestLarkDisplayPreservesSignedOutCode(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts" {
			t.Errorf("read or wrote settings without a signed-in account: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"activeId":"7","accounts":[{"id":"7","userId":"selected","signedIn":false}]}`)
	}))
	defer up.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL, Token: "test-token", Account: 7})}
	mux := http.NewServeMux()
	h.larkDisplayRoutes(larkMux{mux, h})
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/lark/message-display", strings.NewReader(`{"account_id":"selected","key":"anti_recall","value":true,"expected":false}`))
		mux.ServeHTTP(w, r)
		var out map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 502 || out["code"] != larkNotLoggedIn {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
}
