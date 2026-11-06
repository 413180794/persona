package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Deleting a conversation takes an offline one out of what the apps get (the list, the pushes), kept across a
// restart; a running one, the assistant's and an unknown one can't be deleted; used again, it is listed again.
func TestDeleteSessions(t *testing.T) {
	h, n := conversationsHub(t)
	now := time.Now().UTC()
	editSessions(n.m, func(list []proto.Session) []proto.Session {
		return append(list,
			proto.Session{SessionID: "old-0007", Cwd: "/code/app", Status: proto.StatusOffline, UpdatedAt: now.Add(-time.Hour)},
			proto.Session{SessionID: "fail-0008", Cwd: "/code/app", Status: proto.StatusOffline, UpdatedAt: now.Add(-time.Minute),
				StartError: "claude did not become ready within 1m0s"},
			proto.Session{SessionID: "run-0009", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: now},
			proto.Session{SessionID: "boot-0010", Cwd: "/code/app", Status: proto.StatusOffline, Starting: true, UpdatedAt: now},
			proto.Session{SessionID: "done-0011", Cwd: "/code/app", Status: proto.StatusDone, UpdatedAt: now.Add(-time.Hour)},
			proto.Session{SessionID: "busy-0012", Cwd: "/code/app", Status: proto.StatusBlocked, UpdatedAt: now},
		)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/sessions/{mid}/{sid}", h.withMachine(h.deleteSession))
	mux.HandleFunc("GET /api/v1/sessions", h.listSessions)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	del := func(path string) int {
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	listed := func() string {
		res, err := http.Get(srv.URL + "/api/v1/sessions")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v struct {
			Sessions []proto.Session `json:"sessions"`
		}
		_ = json.NewDecoder(res.Body).Decode(&v)
		return ids(v.Sessions)
	}

	for _, id := range []string{"old-0007", "fail-0008", "done-0011"} {
		if c := del("/api/v1/sessions/server/" + id); c != http.StatusOK {
			t.Fatalf("delete %s: %d", id, c)
		}
	}
	got := listed()
	if strings.Contains(got, "old-0007") || strings.Contains(got, "fail-0008") || strings.Contains(got, "done-0011") || !strings.Contains(got, "run-0009") {
		t.Fatalf("deleted ones are left out of the list: %s", got)
	}
	if got := ids(h.shown("server", h.decorated(n.m))); strings.Contains(got, "old-0007") {
		t.Fatalf("and out of the pushes: %s", got)
	}
	if got := ids(h.decorated(n.m)); !strings.Contains(got, "old-0007") {
		t.Fatalf("the hub itself still knows it: %s", got)
	}
	for path, want := range map[string]int{
		"/api/v1/sessions/server/run-0009":  http.StatusConflict,
		"/api/v1/sessions/server/boot-0010": http.StatusConflict,
		"/api/v1/sessions/server/busy-0012": http.StatusConflict,
		"/api/v1/sessions/server/trip-0002": http.StatusBadRequest, // the assistant's: archived instead
		"/api/v1/sessions/server/nope-0000": http.StatusNotFound,
		"/api/v1/sessions/elsewhere/x":      http.StatusNotFound,
	} {
		if c := del(path); c != want {
			t.Fatalf("delete %s: %d, want %d", path, c, want)
		}
	}

	// Kept across a restart (the database's settings, not memory).
	h2 := newTestHub(t)
	h2.store = h.store
	if _, ok := h2.deletedAt("server")["old-0007"]; !ok {
		t.Fatal("not kept")
	}

	// Used again (resumed, a new message): listed again.
	editSessions(n.m, func(list []proto.Session) []proto.Session {
		for i := range list {
			if list[i].SessionID == "old-0007" {
				list[i].UpdatedAt = time.Now().UTC().Add(time.Second)
			}
		}
		return list
	})
	if got := listed(); !strings.Contains(got, "old-0007") || strings.Contains(got, "fail-0008") {
		t.Fatalf("a session used again is back: %s", got)
	}
}

// Old notes are dropped when a new one is saved: the node stopped listing those sessions long ago.
func TestDeletedNotesExpire(t *testing.T) {
	h, _ := conversationsHub(t)
	now := time.Now().UTC()
	if err := h.noteDeleted(t.Context(), "server", "a", now.Add(-deletedKeepFor-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.noteDeleted(t.Context(), "server", "b", now); err != nil {
		t.Fatal(err)
	}
	got := h.deletedAt("server")
	if _, ok := got["a"]; ok || len(got) != 1 {
		t.Fatalf("old note kept: %v", got)
	}
}
