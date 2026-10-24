package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstObject(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":          `{"a":1}`,
		"sure! {\"a\":{\"b\":\"}\"}} done": `{"a":{"b":"}"}}`,
		`{"s":"quote \" brace {"}`:         `{"s":"quote \" brace {"}`,
	}
	for in, want := range cases {
		got, err := FirstObject(in)
		if err != nil || got != want {
			t.Errorf("FirstObject(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := FirstObject("no json"); err == nil {
		t.Error("expected error")
	}
}

func TestJSONCallsMessagesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k" {
			t.Errorf("bad request %s %s", r.URL.Path, r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "```json\n{\"email\":\"a@b.c\"}\n```"}}})
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k\n"), 0o600)
	c := New(Config{BaseURL: srv.URL, APIKeyFile: keyFile, Model: "m"})
	var out struct{ Email string }
	if err := c.JSON(context.Background(), "sys", "user", &out); err != nil || out.Email != "a@b.c" {
		t.Fatalf("out = %+v, %v", out, err)
	}
}
