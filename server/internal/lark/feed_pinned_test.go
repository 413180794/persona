package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestPinnedReadsBeyondThreePagesAndCombinesScopes(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if r.URL.Path != "/agent/v1/conversations" || q.Get("limit") != "100" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if q.Get("scope") == "done" {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
				map[string]any{"feedId": "archived", "onTopRankAt": "2099-01-01T00:00:01Z"},
				map[string]any{"feedId": "0", "onTopRankAt": "2099-01-01T00:00:01.001Z"},
			}})
			return
		}
		page, _ := strconv.Atoi(q.Get("cursor"))
		items := make([]any, 0, 100)
		for i := page * 100; i < (page+1)*100; i++ {
			items = append(items, map[string]any{
				"feedId": strconv.Itoa(i), "onTopRankAt": "2099-01-01T00:00:01.001Z",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": items, "hasMore": page < 3, "nextCursor": strconv.Itoa(page + 1),
		})
	}))
	defer srv.Close()
	rows, err := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test"}).Pinned(context.Background())
	if err != nil || len(rows) != 401 || requests != 5 {
		t.Fatalf("rows=%d requests=%d err=%v", len(rows), requests, err)
	}
	// A millisecond after the second must rank before that exact second, including across scopes.
	if rows[0].ID != "0" || rows[len(rows)-1].ID != "archived" || !rows[len(rows)-1].Done {
		t.Fatal("pinned order or scope was lost")
	}
}

func TestPinnedKeepsPagingAcrossHiddenRows(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("scope") == "done" {
			_, _ = fmt.Fprint(w, `{"items":[]}`)
		} else if q.Get("cursor") == "" {
			_, _ = fmt.Fprint(w, `{"items":[{"feedId":"hidden","isHidden":true,"onTopRankAt":"2099-01-01T00:00:00Z"}],"hasMore":true,"nextCursor":"tail"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"items":[{"feedId":"visible","onTopRankAt":"2099-01-01T00:00:00Z"},{"feedId":"ordinary"}],"hasMore":true,"nextCursor":"not-needed"}`)
		}
	}))
	defer srv.Close()
	rows, err := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test"}).Pinned(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "visible" || requests != 3 {
		t.Fatalf("rows=%v requests=%d err=%v", rows, requests, err)
	}
}

func TestPinnedDoesNotReturnPartialDataAfterPaginationFailure(t *testing.T) {
	for _, failure := range []string{"cycle", "late-http-error", "missing-cursor"} {
		t.Run(failure, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests > 3 {
					t.Error("broken pagination should stop")
					http.Error(w, "stopped", http.StatusBadGateway)
					return
				}
				if failure == "late-http-error" && requests == 2 {
					http.Error(w, "offline", http.StatusServiceUnavailable)
					return
				}
				if failure == "missing-cursor" && requests == 2 {
					_, _ = fmt.Fprint(w, `{"items":[],"hasMore":true}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"items":[{"feedId":"one","onTopRankAt":"2099-01-01T00:00:00Z"}],"hasMore":true,"nextCursor":"repeat"}`)
			}))
			defer srv.Close()
			rows, err := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test"}).Pinned(context.Background())
			if err == nil || rows != nil || requests != 2 {
				t.Fatalf("partial rows=%v requests=%d err=%v", rows, requests, err)
			}
		})
	}
}

func TestPinnedCancelledContextDoesNotCallUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("cancelled request reached upstream")
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test"}).Pinned(ctx)
	if err != context.Canceled || rows != nil {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func TestPinnedEmptySnapshotIsJSONArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"items":[{"feedId":"ordinary"}]}`)
	}))
	defer srv.Close()
	rows, err := New(Config{BaseURL: srv.URL + "/agent/v1", Token: "test"}).Pinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"conversations": rows})
	if err != nil || string(body) != `{"conversations":[]}` {
		t.Fatalf("empty pinned response must decode as an array: %s %v", body, err)
	}
}
