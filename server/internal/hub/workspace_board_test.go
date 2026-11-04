package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/workspaceboard"
)

type memoryBoardStore struct {
	Store
	mu        sync.Mutex
	state     workspaceboard.State
	snapshots map[string]db.WorkspaceSnapshotRow
}

func (s *memoryBoardStore) TouchDeviceToken(context.Context, string) error { return nil }

func newMemoryBoardStore() *memoryBoardStore {
	return &memoryBoardStore{state: workspaceboard.Default(), snapshots: map[string]db.WorkspaceSnapshotRow{}}
}
func (s *memoryBoardStore) WorkspaceBoard(context.Context) (workspaceboard.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var out workspaceboard.State
	_ = json.Unmarshal(raw, &out)
	return out, nil
}
func (s *memoryBoardStore) SaveWorkspaceBoard(_ context.Context, expected int64, next workspaceboard.State) (workspaceboard.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != expected {
		return workspaceboard.State{}, db.ErrWorkspaceBoardConflict
	}
	next.Revision = expected + 1
	s.state = next
	return next, nil
}
func (s *memoryBoardStore) ListWorkspaceSnapshots(context.Context) ([]db.WorkspaceSnapshotRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []db.WorkspaceSnapshotRow{}
	for _, v := range s.snapshots {
		v.Data = append([]byte(nil), v.Data...)
		out = append(out, v)
	}
	return out, nil
}
func (s *memoryBoardStore) SaveWorkspaceSnapshot(_ context.Context, mid string, data json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[mid] = db.WorkspaceSnapshotRow{MachineID: mid, Data: append([]byte(nil), data...), UpdatedAt: time.Now()}
	return nil
}

func boardNode(t *testing.T, h *Hub, id string, fn rpc.Handler) *Machine {
	t.Helper()
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	h.byID[id] = m
	h.machines = append(h.machines, m)
	hi, no := io.Pipe()
	ni, ho := io.Pipe()
	hp, np := rpc.New("h", ho), rpc.New("n", no)
	np.OnRequest = fn
	ctx, cancel := context.WithCancel(context.Background())
	go hp.Serve(ctx, hi)
	go np.Serve(ctx, ni)
	t.Cleanup(func() { cancel(); hp.Close(io.EOF); np.Close(io.EOF); hi.Close(); ni.Close() })
	m.peer, m.online = hp, true
	return m
}
func boardCall(h *Hub, method, path, body string, role string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: role}))
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.workspaceBoardRoutes(mux)
	mux.ServeHTTP(w, r)
	return w
}
func boardState(t *testing.T, w *httptest.ResponseRecorder) workspaceboard.State {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	var s workspaceboard.State
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func directoryNode(_ context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "workspaces.list":
		return proto.WorkspacesResponse{Workspaces: []proto.Workspace{{ID: "empty", Source: "herdr", SourceID: "empty", Title: "Weather", Cwd: "/work/weather", Available: true}}}, nil
	case "workspace.resolve":
		var p struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Path == "/missing" {
			return nil, errors.New("directory missing")
		}
		return proto.Workspace{ID: "/work/weather", Source: "directory", SourceID: "/work/weather", Title: "Weather", Cwd: "/work/weather", Available: true}, nil
	case "fs.stat":
		return map[string]any{"stats": []map[string]string{{"path": "/work/weather", "kind": "dir"}}}, nil
	default:
		return nil, errors.New("unexpected operation: " + method)
	}
}

func TestWorkspaceBoardMetadataNeverExecutesSession(t *testing.T) {
	h := newTestHub(t)
	store := newMemoryBoardStore()
	h.store = store
	var mu sync.Mutex
	methods := []string{}
	m := boardNode(t, h, "laptop", func(ctx context.Context, method string, p json.RawMessage) (any, error) {
		mu.Lock()
		methods = append(methods, method)
		mu.Unlock()
		return directoryNode(ctx, method, p)
	})
	s := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_card","machine_id":"laptop","cwd":"/alias","title":"Fix weather","column_id":"todo"}`, roleFull))
	if len(s.Cards) != 1 || s.Cards[0].Cwd != "/work/weather" {
		t.Fatalf("noncanonical card: %+v", s)
	}
	id := s.Cards[0].ID
	mu.Lock()
	methods = nil
	mu.Unlock()
	m.mu.Lock()
	m.online = false
	m.mu.Unlock()
	s = boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":1,"action":"move_card","id":"`+id+`","column_id":"done"}`, roleFull))
	if s.Cards[0].ColumnID != "done" || s.Cards[0].Archived {
		t.Fatal("Done must not archive the workspace")
	}
	s = boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":2,"action":"update_card","id":"`+id+`","archived":true}`, roleFull))
	s = boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":3,"action":"update_card","id":"`+id+`","archived":false,"pinned":true}`, roleFull))
	if s.Cards[0].Archived || !s.Cards[0].Pinned {
		t.Fatal("restore/pin missing")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 0 {
		t.Fatalf("metadata move invoked a node: %v", methods)
	}
	// A new Hub on the same store preserves the card even without any online machine.
	restarted := newTestHub(t)
	restarted.store = store
	got := boardState(t, boardCall(restarted, "GET", "/api/v1/workspace-board", "", roleFull))
	if got.Revision != 4 || len(got.Cards) != 1 {
		t.Fatalf("restart lost board: %+v", got)
	}
}

func TestWorkspaceBoardValidationAndRoles(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemoryBoardStore()
	boardNode(t, h, "laptop", directoryNode)
	for _, body := range []string{`{"action":"add_column","title":"Other"}`, `{"revision":0,"action":"add_card","machine_id":"unknown","cwd":"/work/weather","column_id":"todo"}`, `{"revision":0,"action":"add_card","machine_id":"laptop","cwd":"/missing","column_id":"todo"}`, `{"revision":0,"action":"add_card","machine_id":"laptop","cwd":"/work/weather","column_id":"todo","source":"forged"}`} {
		w := boardCall(h, "POST", "/api/v1/workspace-board", body, roleFull)
		if w.Code != 400 {
			t.Fatalf("HTTP %d for %s: %s", w.Code, body, w.Body)
		}
	}
	w := boardCall(h, "GET", "/api/v1/workspace-board", "", roleAnswer)
	if !strings.Contains(w.Body.String(), `"can_write":false`) {
		t.Fatal(w.Body.String())
	}
	if w := boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_column","title":"Other"}`, roleAnswer); w.Code != 403 {
		t.Fatal(w.Code)
	}
	boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_column","title":"Other"}`, roleFull))
	if w := boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_column","title":"Stale"}`, roleFull); w.Code != 409 {
		t.Fatal(w.Code)
	}
	unsupported := newTestHub(t)
	if w := boardCall(unsupported, "GET", "/api/v1/workspace-board", "", roleFull); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestWorkspacesIncludeEmptyAndPersistOffline(t *testing.T) {
	h := newTestHub(t)
	store := newMemoryBoardStore()
	h.store = store
	a := boardNode(t, h, "laptop", directoryNode)
	boardNode(t, h, "server", directoryNode)
	w := boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var result struct {
		Workspaces []proto.Workspace `json:"workspaces"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Workspaces) != 2 || result.Workspaces[0].ID == result.Workspaces[1].ID {
		t.Fatalf("empty or cross-machine spaces lost: %s", w.Body)
	}
	chosen := result.Workspaces[0]
	if chosen.MachineID != "laptop" {
		t.Fatal(chosen)
	}
	raw, _ := json.Marshal(map[string]any{"revision": 0, "action": "add_card", "machine_id": "laptop", "workspace_id": chosen.ID, "column_id": "todo"})
	s := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", string(raw), roleFull))
	if s.Cards[0].Source != "herdr" {
		t.Fatal(s)
	}
	a.mu.Lock()
	a.online = false
	a.mu.Unlock()
	w = boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	for _, ws := range result.Workspaces {
		if ws.MachineID == "laptop" && (ws.Online || ws.Available || ws.LastSeen == "") {
			t.Fatalf("offline snapshot misrepresented: %+v", ws)
		}
	}
	restarted := newTestHub(t)
	restarted.store = store
	m := newMachine(restarted, MachineConfig{ID: "laptop"})
	restarted.byID["laptop"] = m
	restarted.machines = append(restarted.machines, m)
	w = boardCall(restarted, "GET", "/api/v1/workspaces", "", roleFull)
	if !strings.Contains(w.Body.String(), chosen.ID) {
		t.Fatalf("restart lost last workspace: %s", w.Body)
	}
}

func TestWorkspaceBoardConcurrentMutations(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemoryBoardStore()
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_column","title":"Review"}`, roleFull).Code
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for c := range results {
		counts[c]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
}

func TestWorkspaceBoardSessionOnlySurvivesUnavailableHost(t *testing.T) {
	h := newTestHub(t)
	store := newMemoryBoardStore()
	h.store = store
	m := boardNode(t, h, "laptop", func(ctx context.Context, method string, p json.RawMessage) (any, error) {
		if method == "workspaces.list" {
			return nil, errors.New("host not running")
		}
		return directoryNode(ctx, method, p)
	})
	m.sessions = []proto.Session{{SessionID: "external", Cwd: "/work/weather", SpaceName: "Weather"}}
	w := boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	var list proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || !list.Workspaces[0].Available {
		t.Fatalf("session-only directory lost: %s", w.Body)
	}
	raw, _ := json.Marshal(map[string]any{"revision": 0, "action": "add_card", "machine_id": "laptop", "workspace_id": list.Workspaces[0].ID, "column_id": "todo"})
	boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", string(raw), roleFull))
	// Discovery, including an unbound external session directory, was already committed before restart.
	restarted := newTestHub(t)
	restarted.store = store
	off := newMachine(restarted, MachineConfig{ID: "laptop"})
	restarted.byID["laptop"] = off
	restarted.machines = append(restarted.machines, off)
	w = boardCall(restarted, "GET", "/api/v1/workspaces", "", roleFull)
	var restored proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &restored)
	if len(restored.Workspaces) != 1 || restored.Workspaces[0].LastSeen == "" || restored.Workspaces[0].Online {
		t.Fatalf("restart lost session snapshot: %s", w.Body)
	}
}

func TestWorkspaceBoardCanonicalMachineAndMissingDirectory(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemoryBoardStore()
	var missing bool
	m := boardNode(t, h, "canonical", func(ctx context.Context, method string, p json.RawMessage) (any, error) {
		if method == "fs.stat" && missing {
			return map[string]any{"stats": []map[string]string{{"path": "/work/weather", "kind": "missing"}}}, nil
		}
		return directoryNode(ctx, method, p)
	})
	alias := newMachine(h, MachineConfig{ID: "alias"})
	h.byID["alias"] = alias
	h.fleet.absorbed = map[*Machine]*Machine{alias: m}
	s := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_card","machine_id":"alias","cwd":"/work/weather","column_id":"todo"}`, roleFull))
	if s.Cards[0].MachineID != "canonical" {
		t.Fatal("alias not normalized")
	}
	if w := boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":1,"action":"add_card","machine_id":"canonical","cwd":"/work/weather","column_id":"todo"}`, roleFull); w.Code != 400 {
		t.Fatalf("duplicate alias binding accepted: %s", w.Body)
	}
	missing = true
	w := boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	var list proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || list.Workspaces[0].Available {
		t.Fatalf("deleted directory reported available: %s", w.Body)
	}
}

func TestWorkspaceBoardLateMachineAlias(t *testing.T) {
	h := newTestHub(t)
	store := newMemoryBoardStore()
	h.store = store
	alias := boardNode(t, h, "alias", directoryNode)
	bound := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":0,"action":"add_card","machine_id":"alias","cwd":"/work/weather","column_id":"review"}`, roleFull))
	id := bound.Cards[0].ID
	extra := qualifyWorkspace("alias", proto.Workspace{Source: "herdr", SourceID: "empty", Cwd: "/work/empty", Title: "Empty"})
	raw, _ := json.Marshal([]proto.Workspace{extra})
	_ = store.SaveWorkspaceSnapshot(context.Background(), "alias", raw)
	canonical := boardNode(t, h, "canonical", directoryNode)
	h.fleet.mu.Lock()
	h.fleet.absorbed = map[*Machine]*Machine{alias: canonical}
	h.fleet.order = []*Machine{canonical}
	h.fleet.mu.Unlock()
	projected := boardState(t, boardCall(h, "GET", "/api/v1/workspace-board", "", roleFull))
	card := projected.Cards[0]
	if card.ID != id || card.ColumnID != "review" || card.MachineID != "canonical" || card.WorkspaceID == bound.Cards[0].WorkspaceID {
		t.Fatalf("late alias lost identity/routing: %+v", card)
	}
	stored, _ := store.WorkspaceBoard(context.Background())
	if stored.Cards[0].MachineID != "alias" {
		t.Fatal("presentation changed durable binding")
	}
	if w := boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":1,"action":"add_card","machine_id":"canonical","cwd":"/work/weather","column_id":"todo"}`, roleFull); w.Code != 400 {
		t.Fatalf("late alias allowed duplicate: %s", w.Body)
	}
	updated := boardState(t, boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":1,"action":"update_card","id":"`+id+`","archived":true}`, roleFull))
	if updated.Cards[0].MachineID != "canonical" || !updated.Cards[0].Archived {
		t.Fatal("mutation response lost projected identity")
	}
	if w := boardCall(h, "POST", "/api/v1/workspace-board", `{"revision":2,"action":"add_card","machine_id":"canonical","cwd":"/work/weather","column_id":"todo"}`, roleFull); w.Code != 400 {
		t.Fatal("archived alias allowed duplicate")
	}
	canonical.mu.Lock()
	canonical.online = false
	canonical.mu.Unlock()
	var list proto.WorkspacesResponse
	w := boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 2 {
		t.Fatalf("absorbed alias snapshot lost: %s", w.Body)
	}
	for _, item := range list.Workspaces {
		if item.MachineID != "canonical" || item.Online || item.Available {
			t.Fatalf("cached alias routing incorrect: %+v", item)
		}
	}
	// A newer complete catalog wins, rather than reviving deleted alias spaces.
	_ = store.SaveWorkspaceSnapshot(context.Background(), "canonical", json.RawMessage(`[]`))
	w = boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || list.Workspaces[0].Cwd != "/work/weather" {
		t.Fatalf("stale alias catalog revived: %s", w.Body)
	}
}

func TestWorkspacesUseVerifiedSessionDirectory(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemoryBoardStore()
	m := boardNode(t, h, "laptop", directoryNode)
	m.sessions = []proto.Session{{SessionID: "external", Cwd: "/work/alias", WorkspaceCwd: "/work/weather"}}
	w := boardCall(h, "GET", "/api/v1/workspaces", "", roleFull)
	var list proto.WorkspacesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Workspaces) != 1 || list.Workspaces[0].Cwd != "/work/weather" {
		t.Fatalf("symlink produced duplicate directories: %s", w.Body)
	}
	candidate := workspaceForSession("laptop", m.sessions[0])
	if candidate.Cwd != "/work/weather" || candidate.SourceID != "/work/weather" {
		t.Fatalf("session fallback ignored verified path: %+v", candidate)
	}
}

func TestWorkspaceBoardReadOnlyThroughAuthentication(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemoryBoardStore()
	now := time.Now()
	h.devtok.cache = map[string]cachedToken{hashToken("board-viewer"): {p: &principal{ID: "viewer", Role: roleAnswer}, at: now, used: now}}
	mux := http.NewServeMux()
	h.workspaceBoardRoutes(mux)
	handler := h.auth(mux)
	for _, path := range []string{"/api/v1/workspace-board", "/api/v1/workspaces"} {
		req := todoRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer board-viewer")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("read-only GET %s: %d %s", path, w.Code, w.Body)
		}
		if path == "/api/v1/workspace-board" && !strings.Contains(w.Body.String(), `"can_write":false`) {
			t.Fatal(w.Body)
		}
	}
	req := todoRequest("POST", "/api/v1/workspace-board", strings.NewReader(`{"revision":0,"action":"add_column","title":"Forbidden"}`))
	req.Header.Set("Authorization", "Bearer board-viewer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
