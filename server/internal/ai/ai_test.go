package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestJSONCallsResponsesAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream bool `json:"stream"`
			Input  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/openai/v1/responses" || r.Header.Get("Authorization") != "Bearer k" || !req.Stream ||
			len(req.Input) != 2 || req.Input[0].Role != "developer" || !strings.HasPrefix(req.Input[0].Content, "sys") ||
			req.Input[1].Content != "user" || req.Reasoning.Effort != "low" {
			t.Errorf("bad request %s %s %+v", r.URL.Path, r.Header.Get("Authorization"), req)
		}
		w.Header().Set("content-type", "text/event-stream")
		for _, ev := range []string{
			`{"type":"response.created"}`,
			`{"type":"response.output_text.delta","delta":"{\"email\":"}`,
			`{"type":"response.output_text.delta","delta":"\"a@b.c\"}"}`,
			`{"type":"response.completed"}`,
		} {
			_, _ = w.Write([]byte("event: x\ndata: " + ev + "\n\n"))
		}
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k\n"), 0o600)
	c := New(Config{API: "responses", BaseURL: srv.URL + "/openai", APIKeyFile: keyFile, Model: "m", ReasoningEffort: "low"})
	var out struct{ Email string }
	if err := c.JSON(context.Background(), "sys", "user", &out); err != nil || out.Email != "a@b.c" {
		t.Fatalf("out = %+v, %v", out, err)
	}
}

func TestResponsesAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`data: {"type":"response.failed","response":{"error":{"message":"boom"}}}` + "\n\n"))
	}))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(keyFile, []byte("k"), 0o600)
	c := New(Config{API: "responses", BaseURL: srv.URL, APIKeyFile: keyFile, Model: "m"})
	if _, err := c.Complete(context.Background(), "sys", "user", 10); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}
