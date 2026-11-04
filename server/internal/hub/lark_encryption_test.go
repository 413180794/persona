package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/lark"
)

func TestLarkNativeEncryptedReceiptSurvivesRestart(t *testing.T) {
	if os.Getenv("JARVIS_TEST_DATABASE_URL") == "" {
		t.Skip("needs isolated Postgres")
	}
	store, err := db.Open(context.Background(), os.Getenv("JARVIS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var ciphertext strings.Builder
	ciphertext.WriteString("下周再聊")
	for _, b := range []byte("OLK3.synthetic") {
		ciphertext.WriteRune(rune(0xe0100 + int(b) - 16))
	}
	clientID := lark.NewClientID()
	var sends atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/olk/native/status" {
			io.WriteString(w, `{"accountId":"123","configured":true,"locked":true,"unifiedPassword":false}`)
			return
		}
		if r.URL.Path != "/messages/555/replies" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var in map[string]string
		if json.NewDecoder(r.Body).Decode(&in) != nil || in["text"] != ciphertext.String() || in["clientId"] != clientID || in["expectedSenderId"] != "123" {
			t.Error("wrong encrypted send")
		}
		sends.Add(1)
		io.WriteString(w, `{"messageId":"999"}`)
	}))
	defer up.Close()
	request := func(account string) *httptest.ResponseRecorder {
		h := newTestHub(t)
		h.store = store
		h.lark = lark.New(lark.Config{BaseURL: up.URL, Token: "ordinary", NativeOLKToken: strings.Repeat("n", 43)})
		mux := http.NewServeMux()
		h.larkEncryptionRoutes(larkMux{mux, h})
		body, _ := json.Marshal(map[string]string{"account_id": account, "ciphertext": ciphertext.String(), "client_id": clientID, "reply_id": "555"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/lark/conversations/789/encrypted-messages", strings.NewReader(string(body))))
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("receipt response must not be cached")
		}
		return w
	}
	first, replay := request("123"), request("123")
	if first.Code != 200 || replay.Code != 200 || first.Body.String() != replay.Body.String() || sends.Load() != 1 {
		t.Fatal("encrypted receipt was not deduplicated")
	}
	if changed := request("456"); changed.Code != 409 || sends.Load() != 1 {
		t.Fatal("reused ID with another account was accepted")
	}
}

func TestLarkNativeEncryptionRoutes(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-lark-connector-account") != "7" || r.Header.Get("x-lark-connector-native-olk-token") != strings.Repeat("n", 43) {
			t.Error("wrong grant/account")
		}
		var body map[string]string
		if r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&body)
			if body["accountId"] != "123" {
				t.Error("missing bound account")
			}
		}
		switch r.URL.Path {
		case "/agent/v1/olk/native/status", "/agent/v1/olk/native/unlock":
			io.WriteString(w, `{"accountId":"123","configured":true,"locked":false,"unifiedPassword":true}`)
		case "/agent/v1/olk/native/replay-covers":
			io.WriteString(w, `{"lines":["下周再聊"]}`)
		case "/agent/v1/olk/native/encrypt":
			w.WriteHeader(423)
			io.WriteString(w, `{"error":{"code":"OLK_VAULT_LOCKED","message":"private upstream context"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	h := &Hub{lark: lark.New(lark.Config{BaseURL: up.URL + "/agent/v1", Token: "regular", NativeOLKToken: strings.Repeat("n", 43), Account: 7})}
	mux := http.NewServeMux()
	h.larkEncryptionRoutes(larkMux{mux, h})
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/lark/encryption", "", 200},
		{"POST", "/api/v1/lark/encryption/unlock", `{"account_id":"123","password":"synthetic-password"}`, 200},
		{"POST", "/api/v1/lark/conversations/789/encryption/covers", `{"account_id":"123"}`, 200},
		{"POST", "/api/v1/lark/conversations/789/encryption/prepare", `{"account_id":"123","text":"secret","cover":"下周再聊"}`, 409},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path+"?ol_account=7", strings.NewReader(tc.body)))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private upstream") {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
}
func TestLarkNativeValidationAndRole(t *testing.T) {
	h := &Hub{}
	mux := http.NewServeMux()
	h.larkEncryptionRoutes(larkMux{mux, h})
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/lark/encryption/unlock", `{"account_id":"123","password":"x","unknown":true}`},
		{"/api/v1/lark/encryption/unlock", `{"account_id":"123","password":"` + strings.Repeat("x", 5000) + `"}`},
		{"/api/v1/lark/encryption/unlock", `{"account_id":"123","password":"x"} {}`},
		{"/api/v1/lark/conversations/no/encryption/covers", `{"account_id":"123"}`},
		{"/api/v1/lark/conversations/789/encryption/prepare", `{"account_id":"123","text":"secret","cover":""}`},
		{"/api/v1/lark/conversations/789/encrypted-messages", `{"account_id":"123","client_id":"` + attachmentClientID + `","ciphertext":"plaintext"}`},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if w.Code != 400 {
			t.Fatalf("invalid request: %d", w.Code)
		}
		if allowed(principal{Role: roleAnswer}, "POST", tc.path) || !allowed(principal{Role: roleFull}, "POST", tc.path) {
			t.Fatal("incorrect write role")
		}
	}
}
