package hub

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

type listTestPage struct {
	Sessions []proto.Session `json:"sessions"`
	Remove   []string        `json:"remove"`
	HasMore  bool            `json:"has_more"`
	Cursor   string          `json:"next_cursor"`
	Revision string          `json:"revision"`
	Total    int             `json:"total"`
}

func runtimePage(t *testing.T, h *Hub, query string) listTestPage {
	t.Helper()
	w := httptest.NewRecorder()
	h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+query, nil))
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
	}
	var page listTestPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestRuntimeListPagesAndSummary(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{
		{MachineID: "demo", SessionID: "b", Title: "Second", State: "working", UpdatedAt: time.Unix(100, 0), Cwd: "/demo/app", SpaceID: "work", LastText: strings.Repeat("你", 500), Background: []proto.BackgroundTask{{ID: "task"}}},
		{MachineID: "demo", SessionID: "a", Title: "First", State: "needs_you", UpdatedAt: time.Unix(100, 0), Cwd: "/demo/app", SpaceID: "work"},
		{MachineID: "demo", SessionID: "c", Title: "Old", State: "idle", UpdatedAt: time.Unix(90, 0), Cwd: "/demo/other"},
	}
	first := runtimePage(t, h, "summary=1&limit=1")
	if first.Total != 3 || !first.HasMore || first.Sessions[0].SessionID != "a" {
		t.Fatalf("first: %+v", first)
	}
	// Move a row while paging: the old frozen view still contains each row once.
	m.mu.Lock()
	m.sessions[1].UpdatedAt = time.Unix(200, 0)
	m.mu.Unlock()
	h.sessionsChanged(m)
	second := runtimePage(t, h, "summary=1&limit=1&cursor="+url.QueryEscape(first.Cursor))
	if second.Sessions[0].SessionID != "b" || second.Revision != first.Revision || len(second.Sessions[0].Background) != 0 || len([]rune(second.Sessions[0].LastText)) != 180 {
		t.Fatalf("second: %+v", second)
	}
	third := runtimePage(t, h, "summary=1&limit=1&cursor="+url.QueryEscape(second.Cursor))
	if third.HasMore || third.Sessions[0].SessionID != "c" {
		t.Fatalf("third: %+v", third)
	}
	filtered := runtimePage(t, h, "summary=true&state=working&machine_id=demo&project=%2Fdemo%2Fapp&group=work&q=second")
	if filtered.Total != 1 || filtered.Sessions[0].SessionID != "b" {
		t.Fatalf("filtered: %+v", filtered)
	}
	legacy := runtimePage(t, h, "machine_id=demo")
	if len(legacy.Sessions) != 3 || len(legacy.Sessions[0].Background) != 1 || len([]rune(legacy.Sessions[0].LastText)) != 500 {
		t.Fatalf("legacy changed: %+v", legacy)
	}
}

func TestRuntimeListDeltaTransitionsAndMachineRemoval(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{{MachineID: "demo", SessionID: "s/with/slashes", Title: "Waiting", State: "needs_you", UpdatedAt: time.Unix(100, 0)}}
	first := runtimePage(t, h, "summary=1&state=needs_you")
	m.mu.Lock()
	m.sessions[0].State = "working"
	m.sessions[0].Title = "Now working"
	m.mu.Unlock()
	h.sessionsChanged(m)
	delta := runtimePage(t, h, "summary=1&state=needs_you&since="+url.QueryEscape(first.Revision))
	if len(delta.Sessions) != 0 || len(delta.Remove) != 1 || delta.Remove[0] != runtimeListID("demo", "s/with/slashes") || delta.Total != 0 {
		t.Fatalf("exit: %+v", delta)
	}
	all := runtimePage(t, h, "summary=1")
	h.mu.Lock()
	h.machines = nil
	delete(h.byID, "demo")
	h.mu.Unlock()
	gone := runtimePage(t, h, "summary=1&since="+url.QueryEscape(all.Revision))
	if len(gone.Remove) != 1 || gone.Total != 0 {
		t.Fatalf("gone: %+v", gone)
	}
}

func TestRuntimeListCountsAndErrors(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{{MachineID: "demo", SessionID: "s", State: "unread", Cwd: "/demo/app"}}
	for _, flag := range []string{"1", "true"} {
		w := httptest.NewRecorder()
		h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?counts="+flag, nil))
		var counts struct {
			Total int
			State map[string]int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &counts); err != nil || counts.Total != 1 || counts.State["unread"] != 1 {
			t.Fatalf("counts %s: %s %v", flag, w.Body, err)
		}
	}
	for _, query := range []string{"limit=0", "limit=201", "summary=maybe", "cursor=", "since=nonsense"} {
		w := httptest.NewRecorder()
		h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+query, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_query") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
	}
	p := runtimePage(t, h, "limit=1")
	other := newTestHub(t)
	w := httptest.NewRecorder()
	other.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?since="+url.QueryEscape(p.Revision), nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "revision_expired") {
		t.Fatalf("restart: %d %s", w.Code, w.Body)
	}
}

func TestRuntimeListSubscriberOnlyReceivesRevision(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{{MachineID: "demo", SessionID: "s", State: "idle", Title: "Old"}}
	h.sessionListIndex()
	c := &client{hub: h, listMode: true, delta: true, out: make(chan []byte, 4), role: roleFull}
	h.clients[c] = struct{}{}
	h.sessionListNotice(c)
	assertNotice := func() {
		t.Helper()
		select {
		case raw := <-c.out:
			var v map[string]any
			_ = json.Unmarshal(raw, &v)
			if v["type"] != "sessions_list" || v["revision"] == "" || len(raw) > 160 {
				t.Fatalf("not lightweight: %s", raw)
			}
		default:
			t.Fatal("missing notice")
		}
	}
	assertNotice()
	m.mu.Lock()
	m.sessions[0].Title = "Changed"
	m.mu.Unlock()
	h.sessionsChanged(m)
	assertNotice()
	h.sessionsChanged(m)
	if len(c.out) != 0 {
		t.Fatal("unchanged list sent a notice")
	}
}
