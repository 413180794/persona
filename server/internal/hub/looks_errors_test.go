package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

type lookReadErrorStore struct {
	settingsStore
	readErr error
}

func (s *lookReadErrorStore) Setting(ctx context.Context, key string, out any) (bool, error) {
	if key == lookKey && s.readErr != nil {
		return false, s.readErr
	}
	return s.settingsStore.Setting(ctx, key, out)
}

func TestLookChoiceDistinguishesReadFailure(t *testing.T) {
	h := newTestHub(t)
	if c, err := h.lookChoice(context.Background()); c != nil || err != nil {
		t.Fatalf("without a store: %v, %v", c, err)
	}
	s := &lookReadErrorStore{settingsStore: settingsStore{vals: map[string][]byte{}}}
	h.store = s
	for _, raw := range []string{"", `{"id":""}`} {
		if raw != "" {
			s.vals[lookKey] = []byte(raw)
		}
		if c, err := h.lookChoice(context.Background()); c != nil || err != nil {
			t.Fatalf("no choice (%q): %v, %v", raw, c, err)
		}
	}
	s.readErr = context.Canceled
	if c, err := h.lookChoice(context.Background()); c != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("read failure: %v, %v", c, err)
	}
}

func TestLookRoutesRejectReadFailure(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			t.Run(string(lang)+"/"+method, func(t *testing.T) {
				h := newTestHub(t)
				const saved = `{"id":"aurora","updated_at":"2026-01-01T00:00:00Z"}`
				s := &lookReadErrorStore{
					settingsStore: settingsStore{vals: map[string][]byte{lookKey: []byte(saved)}},
					readErr:       errors.New("temporary settings failure"),
				}
				h.store = s
				app := appClient(h)
				mux := h.routes()
				path, body := "/api/v1/looks", ""
				if method == http.MethodPut {
					path, body = "/api/v1/looks/current", `{"id":"persona","if_unset":true}`
				}
				request := func() (int, map[string]any) {
					r := httptest.NewRequest(method, path, strings.NewReader(body))
					r.RemoteAddr = "127.0.0.1:12345"
					r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
					r.Header.Set("Accept-Language", string(lang))
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					var out map[string]any
					if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
						t.Fatal(err)
					}
					return w.Code, out
				}
				status, out := request()
				if status != http.StatusInternalServerError || out["code"] != "internal" || out["error"] != i18n.T(lang, "http-status.internal") {
					t.Fatalf("read failure: %d %v", status, out)
				}
				if out["current"] != nil || s.puts != 0 || string(s.vals[lookKey]) != saved || broadcasts(app, "look") != 0 {
					t.Fatalf("failed read returned or changed the choice: %v, writes=%d", out, s.puts)
				}
				// A retry reads the original choice; migration must not overwrite it.
				s.readErr = nil
				status, out = request()
				if status != http.StatusOK {
					t.Fatalf("retry: %d %v", status, out)
				}
				if method == http.MethodGet {
					out = out["current"].(map[string]any)
				}
				if out["id"] != "aurora" || s.puts != 0 || broadcasts(app, "look") != 0 {
					t.Fatalf("retry lost the choice: %v, writes=%d", out, s.puts)
				}
			})
		}
	}
}

func TestLooksGoneLeavesChoiceOnReadFailure(t *testing.T) {
	h := newTestHub(t)
	const saved = `{"id":"aurora"}`
	s := &lookReadErrorStore{
		settingsStore: settingsStore{vals: map[string][]byte{lookKey: []byte(saved)}},
		readErr:       errors.New("temporary settings failure"),
	}
	h.store = s
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	app := appClient(h)
	h.looksGone(context.Background(), []string{"aurora"})
	if s.puts != 0 || string(s.vals[lookKey]) != saved || broadcasts(app, "look") != 0 {
		t.Fatal("failed read changed or broadcast the choice")
	}
	if !strings.Contains(logs.String(), s.readErr.Error()) {
		t.Fatalf("read failure was not logged: %q", logs.String())
	}
	s.readErr = nil
	h.looksGone(context.Background(), []string{"aurora"})
	chosen, err := h.lookChoice(context.Background())
	if err != nil || chosen == nil || chosen.ID != "persona" || s.puts != 1 || broadcasts(app, "look") != 1 {
		t.Fatalf("successful read did not restore default: %v, %v, writes=%d", chosen, err, s.puts)
	}
}
