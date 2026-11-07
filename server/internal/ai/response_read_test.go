package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestMessagesResponseReadFailure(t *testing.T) {
	for _, entry := range []string{"complete", "json", "answered", "rate-limit"} {
		t.Run(entry, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/" { // a node's port probe (node/ports.go), not this test's client
					http.NotFound(w, r)
					return
				}
				calls.Add(1)
				body := `{"content":[{"type":"text","text":"{\"keep\":true}"}],"stop_reason":"end_turn"}`
				status := http.StatusOK
				if entry == "rate-limit" {
					status = http.StatusTooManyRequests
					body = `{"error":{"type":"rate_limit_error","message":"too many requests"}}`
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()
			c := New(Config{BaseURL: srv.URL, APIKey: "example-key", Model: "example-model"})
			var err error
			switch entry {
			case "complete":
				var text string
				text, err = c.Complete(context.Background(), "system", "user", 32)
				if text != "" {
					t.Errorf("failed response returned text: %q", text)
				}
			case "json":
				var out struct{ Keep bool }
				err = c.JSON(context.Background(), "system", "user", &out)
				if out.Keep {
					t.Error("failed response changed structured output")
				}
			default:
				var reply Reply
				var answered bool
				reply, answered, err = c.messages(context.Background(), c.http, "example-key", Request{
					System: "system", Messages: []Message{{Role: RoleUser, Content: "user"}}, MaxTokens: 32,
				})
				if reply != (Reply{}) || answered != (entry == "answered") {
					t.Errorf("reply = %+v, answered = %v", reply, answered)
				}
			}
			if entry == "rate-limit" {
				var httpErr *httpError
				if !errors.As(err, &httpErr) || httpErr.status != http.StatusTooManyRequests {
					t.Fatalf("error = %v, want HTTP 429", err)
				}
				if reason, _, _ := classify(err); reason != ReasonBusy {
					t.Errorf("rate-limit reason = %q", reason)
				}
			} else if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("error = %v, want the response read failure", err)
			}
			if calls.Load() != 1 {
				t.Errorf("requests = %d, want no retry", calls.Load())
			}
		})
	}
}
