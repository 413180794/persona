package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
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

func TestRuntimeExactLookupFullRow(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	var full proto.Session
	// Include chat-only/task fields summaries omit; compare the entire DTO below,
	// not merely the subset the active chat currently happens to consume.
	if err := json.Unmarshal([]byte(`{
		"machine_id":"demo", "session_id":"old/session@child?exact&+", "title":"Old detail",
		"cwd":"/demo/app", "status":"idle", "state":"working", "agent":"claude",
		"launcher":"example", "model":"example-model", "permission_mode":"acceptEdits", "effort":"high",
		"ultracode":true, "ultracode_pending":true, "ultracode_note":"Keep this note", "efforts":["low","high"],
		"background":[{"id":"bg","description":"Background task"}],
		"subagents":[{"id":"sub","running":true}], "workflows":[{"id":"flow","running":true}],
		"queued":["First queued message","Second queued message"], "ports":[{"port":3000}],
		"loop":{"prompt":"Full loop prompt"}, "goal":{"objective":"Complete objective"},
		"plan":{"current":"Complete step"}, "work":{"phase":"blocked","on":"external","line":"Keep full work"},
		"channels":["example"], "context_tokens":100, "context_window":1000, "last_text":"Complete preview",
		"parent_session_id":"parent", "root_session_id":"root", "is_subagent":true, "codex_route_note":"Full route note"
	}`), &full); err != nil {
		t.Fatal(err)
	}
	m.sessions = []proto.Session{full}
	for n := range 100 {
		m.sessions = append(m.sessions, proto.Session{MachineID: "demo", SessionID: fmt.Sprintf("new%03d", n), UpdatedAt: time.Unix(int64(n+1), 0)})
	}
	other := newMachine(h, MachineConfig{ID: "other"})
	other.sessions = []proto.Session{{MachineID: "other", SessionID: full.SessionID, Title: "Other machine"}}
	h.machines, h.byID["other"] = append(h.machines, other), other
	q := "summary=0&limit=50&machine_id=demo&session_id=" + url.QueryEscape(full.SessionID)
	exact := runtimePage(t, h, q)
	legacy := runtimePage(t, h, "machine_id=demo")
	if exact.Total != 1 || len(exact.Sessions) != 1 || exact.HasMore || exact.Cursor != "" || len(exact.Remove) != 0 || exact.Revision == "" ||
		!reflect.DeepEqual(exact.Sessions[0], legacy.Sessions[0]) {
		t.Fatalf("exact row differs from legacy collection: %+v", exact)
	}
	if full := exact.Sessions[0]; len(full.Background) != 1 || len(full.Subagents) != 1 || len(full.Workflows) != 1 || len(full.Queued) != 2 ||
		full.PermissionMode != "acceptEdits" || full.Launcher != "example" || full.Ultracode == nil || full.Loop.Prompt == "" {
		t.Fatalf("full row lost detail: %+v", full)
	}
	if page := runtimePage(t, h, "summary=1&limit=50&machine_id=demo"); slices.ContainsFunc(page.Sessions, func(s proto.Session) bool { return s.SessionID == full.SessionID }) {
		t.Fatal("test target should be beyond the first ordinary page")
	}
	for _, q := range []string{"machine_id=demo&session_id=missing", "machine_id=missing&session_id=" + url.QueryEscape(full.SessionID),
		"machine_id=demo&session_id=" + url.QueryEscape(full.SessionID) + "&state=offline"} {
		page := runtimePage(t, h, q)
		if page.Total != 0 || len(page.Sessions) != 0 || page.Sessions == nil || page.Remove == nil || page.HasMore || page.Cursor != "" {
			t.Fatalf("missing: %+v", page)
		}
	}
	m.mu.Lock()
	m.sessions[0].Title = "Updated detail"
	m.sessions[0].Queued = []string{"New queue"}
	m.mu.Unlock()
	h.sessionsChanged(m)
	updated := runtimePage(t, h, q)
	if updated.Revision == exact.Revision || updated.Sessions[0].Title != "Updated detail" || updated.Sessions[0].Queued[0] != "New queue" {
		t.Fatalf("stale exact row: %+v", updated)
	}
	// Use the same deleted-session projection as every other collection reader.
	m.mu.Lock()
	m.sessions[0].Status = proto.StatusOffline
	m.mu.Unlock()
	h.deleted.mu.Lock()
	h.deleted.loaded, h.deleted.m = true, map[string]time.Time{"demo/" + full.SessionID: time.Now().UTC()}
	h.deleted.mu.Unlock()
	h.sessionsChanged(m)
	if page := runtimePage(t, h, q); page.Total != 0 || len(page.Sessions) != 0 {
		t.Fatalf("deleted row exposed: %+v", page)
	}
}

func TestRuntimeExactLookupValidationAndCompatibility(t *testing.T) {
	h := newTestHub(t)
	for _, query := range []string{"session_id=s", "machine_id=&session_id=s", "machine_id=demo&session_id=", "machine_id=demo&session_id=s&session_id=s",
		"machine_id=demo&machine_id=other&session_id=s", "machine_id=demo&session_id=s&since=v1.AAAAAAAAAAAAAAAAAAAAAA.0", "machine_id=demo&session_id=s&cursor=bad"} {
		w := httptest.NewRecorder()
		h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+query, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_query") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
	}
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	for n := range 60 {
		m.sessions = append(m.sessions, proto.Session{MachineID: "demo", SessionID: fmt.Sprintf("s%02d", n)})
	}
	// Unknown queries remain ignored, including on a legacy complete-list call.
	if page := runtimePage(t, h, "machine_id=demo&future_session_id=missing"); len(page.Sessions) != 60 || page.Revision != "" {
		t.Fatalf("legacy unknown query changed: %+v", page)
	}
	if page := runtimePage(t, h, "summary=0&limit=50&machine_id=demo&future_session_id=missing"); len(page.Sessions) != 50 || page.Total != 60 {
		t.Fatalf("unknown bounded query changed: %+v", page)
	}
	// A pre-lookup hub would ignore session_id and return the first 50 rows.
	// The separate advertised feature, not sessions_list, gates client use.
	if !slices.Contains(h.features(), featureSessionsList) || !slices.Contains(h.features(), featureSessionsLookup) ||
		slices.Contains(proto.Features, featureSessionsLookup) {
		t.Fatal("lookup must be advertised by the hub, not older node feature sets")
	}
}

func TestRuntimeExactLookupAuthorization(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{{MachineID: "demo", SessionID: "s"}}
	mux := h.routes()
	f := extTokensOf(t, h)
	read := f.issue("example-read", "Read", []string{extension.ScopeAgentsRead})
	noRead := f.issue("example-write", "Write", []string{extension.ScopeAgentsControl})
	path := "/api/v1/sessions?summary=0&limit=50&machine_id=demo&session_id=s"
	for _, token := range []string{"", "invalid", noRead} {
		code, body := call(t, mux, "GET", path, token, "")
		if code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("unauthorized lookup: %d %v", code, body)
		}
	}
	if code, body := call(t, mux, "GET", path, read, ""); code != 200 || body["total"] != float64(1) {
		t.Fatalf("agents.read lookup: %d %v", code, body)
	}
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
