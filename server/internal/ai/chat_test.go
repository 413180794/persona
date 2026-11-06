package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestChatSendsTheTurnsAndReadsTheUsage(t *testing.T) {
	var got struct {
		System   string    `json:"system"`
		Messages []Message `json:"messages"`
		Max      int       `json:"max_tokens"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"m-2025","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":5}}`))
	}))
	defer srv.Close()
	turns := []Message{{RoleUser, "a"}, {RoleAssistant, "b"}, {RoleUser, "c"}}
	r, err := New(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"}).Chat(context.Background(), Request{System: "sys", Messages: turns, MaxTokens: 99})
	if err != nil {
		t.Fatal(err)
	}
	if got.System != "sys" || got.Max != 99 || !reflect.DeepEqual(got.Messages, turns) {
		t.Fatalf("request %+v", got)
	}
	want := Reply{Text: "ok", Model: "m-2025", Usage: Usage{InputTokens: 12, OutputTokens: 3, CacheReadInputTokens: 4, CacheCreationInputTokens: 5}}
	if r != want {
		t.Fatalf("reply %+v", r)
	}
}

func TestChatOnTheResponsesAPIReadsTheUsage(t *testing.T) {
	var got struct {
		Input []Message `json:"input"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		for _, ev := range []string{
			`{"type":"response.output_text.delta","delta":"hi"}`,
			`{"type":"response.completed","response":{"model":"gpt-x","usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":6}}}}`,
		} {
			_, _ = w.Write([]byte("data: " + ev + "\n\n"))
		}
	}))
	defer srv.Close()
	r, err := New(Config{API: "responses", BaseURL: srv.URL, APIKey: "k", Model: "m"}).Chat(context.Background(),
		Request{System: "sys", Messages: []Message{{RoleUser, "a"}, {RoleAssistant, "b"}, {RoleUser, "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Input) != 4 || got.Input[0].Role != "developer" || got.Input[2] != (Message{RoleAssistant, "b"}) {
		t.Fatalf("input %+v", got.Input)
	}
	if want := (Reply{Text: "hi", Model: "gpt-x", Usage: Usage{InputTokens: 4, OutputTokens: 2, CacheReadInputTokens: 6}}); r != want {
		t.Fatalf("reply %+v", r)
	}
}

// A deadline on the context is the bound, not the client's 60 seconds; the model asked for stands when the upstream
// names none.
func TestChatGoesByTheContextsDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"late"}]}`))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL + "/slow", APIKey: "k", Model: "m"})
	c.http.Timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.Chat(ctx, Request{Messages: []Message{{RoleUser, "a"}}, MaxTokens: 5})
	if err != nil || r.Text != "late" || r.Model != "m" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := c.Complete(context.Background(), "", "a", 5); err == nil {
		t.Fatal("without a deadline the client's timeout holds")
	}
}
