package feishubot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// retryAPIClient serves a token endpoint and h for the rest, counts hits/token fetches, and no-ops backoff. No t.Parallel in this package: callSleep is package-level.
func retryAPIClient(t *testing.T, h func(w http.ResponseWriter, r *http.Request, n *atomic.Int32)) (*Client, *httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var hits, tokens atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			tokens.Add(1)
			okJSON(w, map[string]any{"code": 0, "tenant_access_token": "tok", "expire": 7200})
			return
		}
		hits.Add(1)
		h(w, r, &hits)
	}))
	t.Cleanup(srv.Close)
	c := New("cli_x", "secret", srv.URL)
	prev := callSleep
	callSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { callSleep = prev })
	return c, srv, &hits, &tokens
}

func okJSON(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
func botInfoOK(w http.ResponseWriter) {
	okJSON(w, map[string]any{"code": 0, "bot": map[string]any{"open_id": "o1"}})
}
func callOnce(c *Client) error { _, err := c.BotInfo(context.Background()); return err }

// Transient answers are retried until the third attempt; waits/tokens give the expected backoff waits and token fetches (a token-gone refresh spends no slot, and must not refresh again for later blips).
func TestCallRetriesTransientAnswers(t *testing.T) {
	cases := []struct {
		name   string
		fail   func(w http.ResponseWriter, n *atomic.Int32)
		waits  int // backoff waits before the success
		tokens int // token endpoint fetches
	}{
		{"503 html page", func(w http.ResponseWriter, _ *atomic.Int32) {
			http.Error(w, "bad gateway", http.StatusServiceUnavailable)
		}, 2, 1},
		{"http 429", func(w http.ResponseWriter, _ *atomic.Int32) {
			w.WriteHeader(http.StatusTooManyRequests)
			okJSON(w, map[string]any{"code": 0})
		}, 2, 1},
		{"rate limit code", func(w http.ResponseWriter, _ *atomic.Int32) {
			okJSON(w, map[string]any{"code": codeRateLimited})
		}, 2, 1},
		{"5xx json body", func(w http.ResponseWriter, _ *atomic.Int32) {
			w.WriteHeader(http.StatusBadGateway)
			okJSON(w, map[string]any{"code": 12345})
		}, 2, 1},
		{"token expired then 503", func(w http.ResponseWriter, n *atomic.Int32) {
			if n.Load() == 1 {
				okJSON(w, map[string]any{"code": 99991663})
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}, 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var slept []time.Duration
			c, _, hits, tokens := retryAPIClient(t, func(w http.ResponseWriter, r *http.Request, n *atomic.Int32) {
				if n.Load() < 3 {
					tc.fail(w, n)
				} else {
					botInfoOK(w)
				}
			})
			callSleep = func(_ context.Context, d time.Duration) error {
				if i := len(slept); i < len(callBackoff) && (d < callBackoff[i]*3/4 || d > callBackoff[i]*5/4) {
					t.Errorf("wait %d = %v, want +/-25%% of %v", i, d, callBackoff[i])
				}
				slept = append(slept, d)
				return nil
			}
			info, err := c.BotInfo(context.Background())
			if err != nil || info.OpenID != "o1" || hits.Load() != 3 || len(slept) != tc.waits || tokens.Load() != int32(tc.tokens) {
				t.Fatalf("info=%+v hits=%d waits=%d tokens=%d err=%v",
					info, hits.Load(), len(slept), tokens.Load(), err)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type errBody struct{ error }

func (b errBody) Read([]byte) (int, error) { return 0, b.error }
func (b errBody) Close() error             { return nil }

// A failing connection and a body read that fails both use all three attempts and return the last error.
func TestCallTransportFailuresBounded(t *testing.T) {
	fails := map[string]func() (*http.Response, error){
		"do error": func() (*http.Response, error) { return nil, errors.New("connection reset by peer") },
		"body read error": func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errBody{errors.New("unexpected EOF")}}, nil
		},
	}
	for name, fail := range fails {
		t.Run(name, func(t *testing.T) {
			c, srv, hits, _ := retryAPIClient(t, func(http.ResponseWriter, *http.Request, *atomic.Int32) {})
			c.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
					return srv.Client().Transport.RoundTrip(req)
				}
				hits.Add(1)
				return fail()
			})}
			err := callOnce(c)
			if err == nil || hits.Load() != callMaxAttempts ||
				!strings.Contains(err.Error(), "reset") && !strings.Contains(err.Error(), "EOF") {
				t.Fatalf("err=%v hits=%d", err, hits.Load())
			}
		})
	}
}

// A cancelled context returns at once: before the request goes out, and while the backoff waits.
func TestCallCancelledContextDoesNotRetry(t *testing.T) {
	t.Run("before", func(t *testing.T) {
		c, _, hits, _ := retryAPIClient(t, func(http.ResponseWriter, *http.Request, *atomic.Int32) {})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.BotInfo(ctx); !errors.Is(err, context.Canceled) || hits.Load() != 0 {
			t.Fatalf("err=%v hits=%d", err, hits.Load())
		}
	})
	t.Run("during backoff", func(t *testing.T) {
		c, _, hits, _ := retryAPIClient(t, func(w http.ResponseWriter, _ *http.Request, _ *atomic.Int32) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		ctx, cancel := context.WithCancel(context.Background())
		callSleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
		if _, err := c.BotInfo(ctx); !errors.Is(err, context.Canceled) || hits.Load() != 1 {
			t.Fatalf("err=%v hits=%d", err, hits.Load())
		}
	})
}

// A 4xx business error is not a transport problem: one attempt, APIError with its code.
func TestCallBusinessErrorNotRetried(t *testing.T) {
	c, _, hits, _ := retryAPIClient(t, func(w http.ResponseWriter, _ *http.Request, _ *atomic.Int32) {
		w.WriteHeader(http.StatusBadRequest)
		okJSON(w, map[string]any{"code": 230002})
	})
	var apiErr *APIError
	if err := callOnce(c); !errors.As(err, &apiErr) || apiErr.Code != 230002 || hits.Load() != 1 {
		t.Fatalf("err=%v hits=%d", err, hits.Load())
	}
}

// A POST is retried only when it carries a uuid, and every attempt keeps the same uuid.
func TestSendUUIDRetryPolicy(t *testing.T) {
	cases := []struct{ name, uuid string }{{"without uuid", ""}, {"with uuid", "send:c1:answer"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, hits, _ := retryAPIClient(t, func(w http.ResponseWriter, r *http.Request, n *atomic.Int32) {
				var in map[string]any
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &in)
				if u, _ := in["uuid"].(string); u != tc.uuid {
					t.Errorf("uuid=%q, want %q", u, tc.uuid)
				}
				switch {
				case tc.uuid == "": // uuid-less POST: one rate-limit answer, no retry
					okJSON(w, map[string]any{"code": codeRateLimited})
				case n.Load() == 1:
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					okJSON(w, map[string]any{"code": 0, "data": map[string]any{"message_id": "m1"}})
				}
			})
			id, err := c.Send(context.Background(), "chat_id", "c1", TypeText, TextContent("hi"), tc.uuid)
			if tc.uuid == "" {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != codeRateLimited || hits.Load() != 1 {
					t.Fatalf("err=%v hits=%d", err, hits.Load())
				}
				return
			}
			if err != nil || id != "m1" || hits.Load() != 2 {
				t.Fatalf("id=%q err=%v hits=%d", id, err, hits.Load())
			}
		})
	}
}
