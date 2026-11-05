package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
)

func TestKBTagUpdatesAcrossHTTPAndRPC(t *testing.T) {
	for _, transport := range []string{"HTTP", "RPC"} {
		for _, tc := range []struct {
			name   string
			fields string
			want   []string
			locked bool
		}{
			{"omitted_preserves", ``, []string{"old-tag"}, false},
			{"null_preserves", `"tags":null`, []string{"old-tag"}, false},
			{"empty_clears", `"tags":[]`, nil, true},
			{"nonempty_replaces", `"tags":["new-tag"]`, []string{"new-tag"}, true},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				s := kbServiceOn(t, newFakeHub(), newKBStore(), t.TempDir(), nil)
				e := kbEng(t, s)
				if err := e.Store.PutProject(&kb.Project{Slug: "example", Name: "Example", Tagging: kb.Tagging{Tags: []string{"old-tag"}}}); err != nil {
					t.Fatal(err)
				}
				stale, ok := e.Store.Project("example")
				if !ok {
					t.Fatal("seeded project not found")
				}
				if transport == "HTTP" {
					rec := httptest.NewRecorder()
					s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/kb/projects/example", strings.NewReader("{"+tc.fields+"}")))
					if rec.Code != http.StatusOK {
						t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
					}
					var out assistantwire.KBUpdateResult
					if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID != "example" {
						t.Fatalf("PUT result: %s, err=%v", rec.Body, err)
					}
				} else {
					raw := `{"kind":"projects","id":"example"`
					if tc.fields != "" {
						raw += "," + tc.fields
					}
					out, handled, err := s.kbRequest(context.Background(), assistantwire.MethodKBUpdate, json.RawMessage(raw+"}"))
					if !handled || err != nil {
						t.Fatalf("kb.update: handled=%v err=%v", handled, err)
					}
					if got := out.(assistantwire.KBUpdateResult).ID; got != "example" {
						t.Fatalf("kb.update id = %q", got)
					}
				}

				readBack := func() {
					t.Helper()
					rec := httptest.NewRecorder()
					s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/kb/projects/example", nil))
					var httpOut assistantwire.KBGetResult
					if err := json.Unmarshal(rec.Body.Bytes(), &httpOut); err != nil || rec.Code != http.StatusOK || httpOut.Detail == nil {
						t.Fatalf("GET: %d %s, err=%v", rec.Code, rec.Body, err)
					}
					out, handled, err := s.kbRequest(context.Background(), assistantwire.MethodKBGet, json.RawMessage(`{"kind":"projects","id":"example"}`))
					if !handled || err != nil {
						t.Fatalf("kb.get: handled=%v err=%v", handled, err)
					}
					for _, detail := range []*kb.Detail{httpOut.Detail, out.(assistantwire.KBGetResult).Detail} {
						var tagging kb.Tagging
						if err := json.Unmarshal(detail.Meta, &tagging); err != nil {
							t.Fatal(err)
						}
						if !slices.Equal(tagging.Tags, tc.want) {
							t.Errorf("readback tags = %v, want %v", tagging.Tags, tc.want)
						}
						if tc.name == "empty_clears" {
							var meta map[string]json.RawMessage
							if err := json.Unmarshal(detail.Meta, &meta); err != nil {
								t.Fatal(err)
							}
							// The app replaces its cached tags only when metadata contains an array.
							if got := strings.TrimSpace(string(meta["tags"])); got != "[]" {
								t.Errorf("readback tags JSON = %q, want []", got)
							}
						}
						if locked := slices.Contains(tagging.LockedFields, "tags"); locked != tc.locked {
							t.Errorf("tags field locked = %v, want %v", locked, tc.locked)
						}
					}
				}
				readBack()
				if tc.locked {
					// A background write based on the old metadata must retain the manual correction.
					if err := e.Store.PutProject(stale); err != nil {
						t.Fatal(err)
					}
					readBack()
				}
			})
		}
	}
}
