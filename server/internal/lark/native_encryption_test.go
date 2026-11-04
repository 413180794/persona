package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func syntheticOLK() string {
	var s strings.Builder
	s.WriteString("下周再聊")
	for _, b := range []byte("OLK3.synthetic-ciphertext") {
		s.WriteRune(rune(0xe0100 + int(b) - 16))
	}
	return s.String()
}
func TestNativeOLKGrantAndWire(t *testing.T) {
	grant := strings.Repeat("n", 43)
	writes := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer regular" || r.Header.Get("x-lark-connector-account") != "7" {
			t.Error("wrong identity")
		}
		if strings.Contains(r.URL.Path, "/olk/native/") && r.Header.Get("x-lark-connector-native-olk-token") != grant {
			t.Error("missing separate grant")
		}
		var body map[string]string
		if r.Method == "POST" {
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid body")
			}
		}
		switch r.URL.Path {
		case "/agent/v1/olk/native/status", "/agent/v1/olk/native/unlock":
			if r.Method == "POST" && (body["accountId"] != "123" || body["password"] != "synthetic-password") {
				t.Error("wrong unlock")
			}
			io.WriteString(w, `{"accountId":"123","configured":true,"locked":false,"unifiedPassword":false}`)
		case "/agent/v1/olk/native/replay-covers":
			if body["accountId"] != "123" || body["feedId"] != "789" {
				t.Error("wrong covers")
			}
			io.WriteString(w, `{"lines":["下周再聊"]}`)
		case "/agent/v1/olk/native/encrypt":
			if body["accountId"] != "123" || body["text"] != "private" || body["cover"] != "下周再聊" {
				t.Error("wrong preparation")
			}
			json.NewEncoder(w).Encode(NativeOLKPrepared{AccountID: "123", FeedID: "789", Messages: []NativeOLKCiphertext{{Text: syntheticOLK()}}})
		case "/agent/v1/conversations/789/messages":
			writes++
			if body["text"] != syntheticOLK() || body["expectedSenderId"] != "123" || body["clientId"] != "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c" {
				t.Error("wrong send")
			}
			if r.Header.Get("x-lark-connector-native-olk-token") != "" {
				t.Error("native grant leaked into generic send")
			}
			io.WriteString(w, `{"messageId":"999"}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL + "/agent/v1", Account: 7, Token: "regular", NativeOLKToken: grant})
	ctx := context.Background()
	if _, err := c.NativeOLKStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NativeOLKUnlock(ctx, "123", "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	if lines, err := c.NativeOLKCovers(ctx, "123", "789"); err != nil || len(lines) != 1 {
		t.Fatalf("covers: %v", err)
	}
	if _, err := c.NativeOLKEncrypt(ctx, "123", "789", "private", "下周再聊"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if id, err := c.SendOLKCiphertext(ctx, "123", "789", syntheticOLK(), "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c", ""); err != nil || id != "999" {
			t.Fatalf("send: %v", err)
		}
	}
	if _, err := c.SendOLKCiphertext(ctx, "456", "789", syntheticOLK(), "0b6a3c1e-2f4d-4e8a-9c7b-5d1e3f2a4b6c", ""); err == nil {
		t.Fatal("changed account sent")
	}
	if writes != 2 {
		t.Fatal("unexpected write")
	}
}
func TestNativeOLKNeverFallsBackOrReflectsSecrets(t *testing.T) {
	for _, body := range []string{
		`{"accountId":"123","feedId":"789","messages":[{"text":"private plaintext"}]}`,
		`{"accountId":"456","feedId":"789","messages":[]}`,
		`{"accountId":"123","feedId":"999","messages":[]}`,
		`{"accountId":"123","feedId":"789","messages":[]}`,
		`not JSON synthetic-password`,
	} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer up.Close()
			c := New(Config{BaseURL: up.URL, Token: "ordinary", NativeOLKToken: strings.Repeat("n", 43)})
			out, err := c.NativeOLKEncrypt(context.Background(), "123", "789", "private", "下周再聊")
			if err == nil || len(out.Messages) != 0 || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "synthetic-password") {
				t.Fatal("unsafe result")
			}
		})
	}
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"code":"OLK_VAULT_INVALID_PASSWORD","message":"synthetic-password"}}`)
	}))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "ordinary", NativeOLKToken: strings.Repeat("n", 43)})
	_, err := c.NativeOLKUnlock(context.Background(), "123", "synthetic-password")
	if err == nil || err.Error() != "密码不正确，请重试" {
		t.Fatalf("unsafe password error: %v", err)
	}
	if c.token != "ordinary" {
		t.Error("vault password failure reset Agent token")
	}
	c.cfg.NativeOLKToken = ""
	if _, err := c.NativeOLKStatus(context.Background()); err == nil || calls != 1 {
		t.Error("ordinary token authorized native route")
	}
}
func TestNativeOLKRefusesRedirects(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer up.Close()
	c := New(Config{BaseURL: up.URL, Token: "ordinary", NativeOLKToken: strings.Repeat("n", 43)})
	if _, err := c.NativeOLKUnlock(context.Background(), "123", "synthetic-password"); err == nil || leaked {
		t.Fatal("redirect forwarded secret")
	}
}
func TestNativeOLKConfigAndCiphertext(t *testing.T) {
	for _, cfg := range []Config{{Token: "regular", NativeOLKToken: "short"}, {Token: strings.Repeat("n", 43), NativeOLKToken: strings.Repeat("n", 43)}, {NativeOLKToken: strings.Repeat("n", 43)}} {
		if cfg.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if err := (Config{Token: "ordinary", NativeOLKToken: strings.Repeat("n", 43)}).Validate(); err != nil {
		t.Fatal(err)
	}
	if !ValidOLKCiphertext(syntheticOLK()) {
		t.Fatal("valid wire shape rejected")
	}
	for _, s := range []string{"plain", "", syntheticOLK() + "plaintext", strings.Repeat("x", 10001) + syntheticOLK(), strings.TrimPrefix(syntheticOLK(), "下周再聊")} {
		if ValidOLKCiphertext(s) {
			t.Fatal("invalid ciphertext accepted")
		}
	}
}
