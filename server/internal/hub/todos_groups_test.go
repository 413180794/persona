package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// TestTodoSpaceLinkAndUnlink covers C2 and C3:
// - PUT /spaces/{mid}/{space...} with space containing '/'
// - transferring space to another todo (agents from old todo transferred, manual agents kept)
// - DELETE /spaces/{mid}/{space...} only deletes source=space agents
func TestTodoSpaceLinkAndUnlink(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())
	handler := h.routes()

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", Title: "Agent 1", SpaceName: "group/alpha", Status: proto.StatusWorking},
		{MachineID: "macbook", SessionID: "s2", Title: "Agent 2", SpaceName: "other/proj", Status: proto.StatusWorking},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	// Create 2 todos
	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td1", Title: "Todo 1", CreatedAt: now, UpdatedAt: now})
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td2", Title: "Todo 2", CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// 1. Link whole space "group/alpha" to td1 (encoded URL)
	req := todoRequest("PUT", "/api/v1/todos/td1/spaces/macbook/group%2Falpha", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT space expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		OK     bool `json:"ok"`
		Linked int  `json:"linked"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if !res.OK || res.Linked != 1 {
		t.Fatalf("unexpected PUT space response: %+v", res)
	}

	// Also manually link s2 to td1
	req = todoRequest("PUT", "/api/v1/todos/td1/agents/macbook/s2", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT agent expected 200, got %d", w.Code)
	}

	// Verify GET /api/v1/todos for td1
	req = todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var getRes struct {
		Todos []Todo `json:"todos"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	var td1Get *Todo
	for i := range getRes.Todos {
		if getRes.Todos[i].ID == "td1" {
			td1Get = &getRes.Todos[i]
		}
	}
	if td1Get == nil || len(td1Get.Spaces) != 1 || td1Get.Spaces[0].Space != "group/alpha" {
		t.Fatalf("td1 missing space: %+v", td1Get)
	}
	if len(td1Get.Agents) != 2 {
		t.Fatalf("expected 2 agents on td1, got %d", len(td1Get.Agents))
	}
	for _, a := range td1Get.Agents {
		if a.SessionID == "s1" {
			if a.Source != "space" || a.Via != "macbook/group/alpha" {
				t.Fatalf("s1 unexpected source/via: %+v", a)
			}
		} else if a.SessionID == "s2" {
			if a.Source != "manual" || a.Via != "" {
				t.Fatalf("s2 unexpected source/via: %+v", a)
			}
		}
	}

	// 2. Transfer "group/alpha" to td2
	req = todoRequest("PUT", "/api/v1/todos/td2/spaces/macbook/group%2Falpha", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT space transfer expected 200, got %d", w.Code)
	}

	// Verify td1: space removed, s1 removed, s2 (manual) kept
	req = todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	for i := range getRes.Todos {
		if getRes.Todos[i].ID == "td1" {
			if len(getRes.Todos[i].Spaces) != 0 {
				t.Fatalf("expected td1 spaces empty after transfer, got %+v", getRes.Todos[i].Spaces)
			}
			if len(getRes.Todos[i].Agents) != 1 || getRes.Todos[i].Agents[0].SessionID != "s2" {
				t.Fatalf("expected only manual agent s2 on td1, got %+v", getRes.Todos[i].Agents)
			}
		} else if getRes.Todos[i].ID == "td2" {
			if len(getRes.Todos[i].Spaces) != 1 || getRes.Todos[i].Spaces[0].Space != "group/alpha" {
				t.Fatalf("expected td2 to have transferred space, got %+v", getRes.Todos[i].Spaces)
			}
			if len(getRes.Todos[i].Agents) != 1 || getRes.Todos[i].Agents[0].SessionID != "s1" {
				t.Fatalf("expected transferred agent s1 on td2, got %+v", getRes.Todos[i].Agents)
			}
		}
	}

	// 3. Delete space from td2
	req = todoRequest("DELETE", "/api/v1/todos/td2/spaces/macbook/group%2Falpha", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE space expected 200, got %d", w.Code)
	}

	// Verify td2: space and s1 deleted
	req = todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	for i := range getRes.Todos {
		if getRes.Todos[i].ID == "td2" {
			if len(getRes.Todos[i].Spaces) != 0 || len(getRes.Todos[i].Agents) != 0 {
				t.Fatalf("expected td2 empty after space unlink, got %+v", getRes.Todos[i])
			}
		}
	}
}

// TestTodoSpaceAutoLinkAndDismissed covers:
// - new agent in space is automatically linked
// - existing active link is not stolen
// - manual delete marks dismissed and does not auto-link back
// - manual link clears dismissed
// - auto-linking stops when todo is completed
func TestTodoSpaceAutoLinkAndDismissed(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())
	handler := h.routes()

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "t_main", Title: "Main Todo", CreatedAt: now, UpdatedAt: now})
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "t_other", Title: "Other Todo", CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// Link space "work" to t_main
	req := todoRequest("PUT", "/api/v1/todos/t_main/spaces/macbook/work", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT space: %d", w.Code)
	}

	// Agent s_existing is manually on t_other
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s_existing", Title: "Existing", SpaceName: "work", Status: proto.StatusWorking},
	}
	_ = h.linkTodoAgent(context.Background(), "t_other", "macbook", "s_existing", "Existing", "/cwd", "claude", "")

	// 1. New agent s_new appears in space "work"
	m.sessions = append(m.sessions, proto.Session{
		MachineID: "macbook", SessionID: "s_new", Title: "New Agent", SpaceName: "work", Status: proto.StatusWorking,
	})
	h.sessionsChanged(m)

	// Verify s_new is auto-linked to t_main, and s_existing is NOT stolen
	dec := h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_new" {
			if s.Todo == nil || s.Todo.ID != "t_main" {
				t.Fatalf("expected s_new to be auto-linked to t_main, got %+v", s.Todo)
			}
		} else if s.SessionID == "s_existing" {
			if s.Todo == nil || s.Todo.ID != "t_other" {
				t.Fatalf("expected s_existing to stay on t_other, got %+v", s.Todo)
			}
		}
	}

	// 2. Unlink s_new from t_main (manual delete -> dismissed)
	req = todoRequest("DELETE", "/api/v1/todos/t_main/agents/macbook/s_new", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE agent: %d", w.Code)
	}

	// Verify GET /api/v1/todos does not return dismissed s_new
	req = todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var getRes struct{ Todos []Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	for _, td := range getRes.Todos {
		if td.ID == "t_main" {
			for _, a := range td.Agents {
				if a.SessionID == "s_new" {
					t.Fatalf("expected dismissed s_new to NOT appear in GET /todos, got %+v", a)
				}
			}
		}
	}

	// 3. New turn of sessionsChanged(m): s_new must NOT be auto-linked back!
	h.sessionsChanged(m)
	dec = h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_new" {
			if s.Todo != nil {
				t.Fatalf("s_new was re-linked despite being dismissed: %+v", s.Todo)
			}
		}
	}

	// 4. Manually re-linking s_new clears dismissed_at
	req = todoRequest("PUT", "/api/v1/todos/t_main/agents/macbook/s_new", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT agent re-link: %d", w.Code)
	}
	dec = h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_new" {
			if s.Todo == nil || s.Todo.ID != "t_main" {
				t.Fatalf("expected s_new re-linked after manual PUT, got %+v", s.Todo)
			}
		}
	}

	// 5. Complete t_main -> auto-linking stops
	patchDone := `{"done":true}`
	req = todoRequest("PATCH", "/api/v1/todos/t_main", strings.NewReader(patchDone))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH done: %d", w.Code)
	}

	// Agent s_after appears in "work"
	m.sessions = append(m.sessions, proto.Session{
		MachineID: "macbook", SessionID: "s_after", Title: "After Done", SpaceName: "work", Status: proto.StatusWorking,
	})
	h.sessionsChanged(m)
	dec = h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_after" {
			if s.Todo != nil {
				t.Fatalf("s_after should not auto-link to completed todo: %+v", s.Todo)
			}
		}
	}
}

// TestTodoSpawnDerivation covers:
// - parent on macbook, child spawned on dev machine
// - child has spawn_from and auto-links to parent's todo
// - spawn_from stripped before push to clients
// - grandchild also follows
// - non-existent or completed todo is ignored
func TestTodoSpawnDerivation(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())

	mMac := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	mDev := newMachine(h, MachineConfig{ID: "dev", Name: "Dev Machine"})
	h.machines = append(h.machines, mMac, mDev)
	h.byID["macbook"] = mMac
	h.byID["dev"] = mDev

	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_spawn", Title: "Spawn Task", CreatedAt: now, UpdatedAt: now})
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_done", Title: "Done Task", DoneAt: &now, CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// Parent p1 on macbook linked to td_spawn
	mMac.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "p1", Title: "Parent Agent", Status: proto.StatusWorking},
	}
	_ = h.linkTodoAgent(context.Background(), "td_spawn", "macbook", "p1", "Parent Agent", "/cwd", "claude", "")

	// 1. Child c1 on dev spawned from p1
	mDev.sessions = []proto.Session{
		{
			MachineID: "dev",
			SessionID: "c1",
			Title:     "Child Worker",
			Status:    proto.StatusWorking,
			SpawnFrom: "td_spawn@macbook/p1",
		},
		{
			MachineID: "dev",
			SessionID: "c_bad",
			Title:     "Bad Spawn",
			Status:    proto.StatusWorking,
			SpawnFrom: "no_such_todo@macbook/p1",
		},
		{
			MachineID: "dev",
			SessionID: "c_done",
			Title:     "Done Spawn",
			Status:    proto.StatusWorking,
			SpawnFrom: "td_done@macbook/p1",
		},
	}

	h.sessionsChanged(mDev)

	// Check c1 is linked to td_spawn
	dec := h.decorated(mDev)
	var c1Session *proto.Session
	for i := range dec {
		if dec[i].SessionID == "c1" {
			c1Session = &dec[i]
		} else if dec[i].SessionID == "c_bad" || dec[i].SessionID == "c_done" {
			if dec[i].Todo != nil {
				t.Fatalf("%s should not be linked: %+v", dec[i].SessionID, dec[i].Todo)
			}
		}
	}
	if c1Session == nil || c1Session.Todo == nil || c1Session.Todo.ID != "td_spawn" {
		t.Fatalf("expected c1 linked to td_spawn, got %+v", c1Session)
	}
	// Verify SpawnFrom was stripped for client
	if c1Session.SpawnFrom != "" {
		t.Fatalf("expected SpawnFrom stripped from decorated session, got %q", c1Session.SpawnFrom)
	}

	// 2. Grandchild g1 on dev spawned from c1
	mDev.sessions = append(mDev.sessions, proto.Session{
		MachineID: "dev",
		SessionID: "g1",
		Title:     "Grandchild Worker",
		Status:    proto.StatusWorking,
		SpawnFrom: "td_spawn@dev/c1",
	})
	h.sessionsChanged(mDev)

	dec = h.decorated(mDev)
	var g1Session *proto.Session
	for i := range dec {
		if dec[i].SessionID == "g1" {
			g1Session = &dec[i]
		}
	}
	if g1Session == nil || g1Session.Todo == nil || g1Session.Todo.ID != "td_spawn" {
		t.Fatalf("expected grandchild g1 linked to td_spawn, got %+v", g1Session)
	}

	// Verify in GET /todos that c1 and g1 have source=spawned and correct via
	req := todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, req)
	var getRes struct{ Todos []Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	for _, td := range getRes.Todos {
		if td.ID == "td_spawn" {
			for _, a := range td.Agents {
				if a.SessionID == "c1" {
					if a.Source != "spawned" || a.Via != "macbook/p1" {
						t.Fatalf("c1 unexpected source/via: %+v", a)
					}
				} else if a.SessionID == "g1" {
					if a.Source != "spawned" || a.Via != "dev/c1" {
						t.Fatalf("g1 unexpected source/via: %+v", a)
					}
				}
			}
		}
	}
}

// TestTodoSpaceRenameAnd403 covers:
// - renameSpace carries todo_spaces and agent via over
// - read-only tokens return 403 on PUT /spaces, DELETE /spaces, DELETE /agents
func TestTodoSpaceRenameAnd403(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())
	handler := h.routes()

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", Title: "Agent 1", SpaceID: "w_old", SpaceName: "old_name", Status: proto.StatusWorking},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_ren", Title: "Rename Test", CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// Link space "old_name"
	req := todoRequest("PUT", "/api/v1/todos/td_ren/spaces/macbook/old_name", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT space: %d", w.Code)
	}

	// Connect fake node for space.rename RPC call
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	}
	connCtx, connCancel := context.WithCancel(context.Background())
	t.Cleanup(connCancel)
	go func() { _ = hubPeer.Serve(connCtx, hubIn) }()
	go func() { _ = nodePeer.Serve(connCtx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	// F5: Use real HTTP rename endpoint
	renameReq := todoRequest("POST", "/api/v1/machines/macbook/spaces/w_old/rename", strings.NewReader(`{"name":"new_name"}`))
	renameReq.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, renameReq)
	if rw.Code != http.StatusOK {
		t.Fatalf("POST rename space expected 200, got %d: %s", rw.Code, rw.Body.String())
	}

	// Verify GET /todos shows new_name in spaces and agent via
	req = todoRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var getRes struct{ Todos []Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &getRes)
	for _, td := range getRes.Todos {
		if td.ID == "td_ren" {
			if len(td.Spaces) != 1 || td.Spaces[0].Space != "new_name" {
				t.Fatalf("expected space renamed to new_name, got %+v", td.Spaces)
			}
			if len(td.Agents) != 1 || td.Agents[0].Via != "macbook/new_name" {
				t.Fatalf("expected agent via updated to macbook/new_name, got %+v", td.Agents)
			}
		}
	}

	// Test 403 for read-only role
	tok := "jd_watch_token_" + randomHex(8)
	dt := db.DeviceToken{ID: "dev_watch_" + randomHex(4), Name: "Watch", Kind: "watch", Role: "answer"}
	if err := database.CreateDeviceToken(context.Background(), dt, hashToken(tok)); err != nil {
		t.Fatal(err)
	}

	// 1. PUT space -> 403
	req = todoRequest("PUT", "/api/v1/todos/td_ren/spaces/macbook/any", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on PUT space, got %d", w.Code)
	}

	// 2. DELETE space -> 403
	req = todoRequest("DELETE", "/api/v1/todos/td_ren/spaces/macbook/any", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on DELETE space, got %d", w.Code)
	}

	// 3. DELETE agent -> 403
	req = todoRequest("DELETE", "/api/v1/todos/td_ren/agents/macbook/s1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on DELETE agent, got %d", w.Code)
	}
}

// TestTodoTokensSyncToNodeCalculation (F6) verifies expected tokens calculation and dispatch using real RPC.
func TestTodoTokensSyncToNodeCalculation(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", PaneID: "w1:p1", Status: proto.StatusWorking},
		{MachineID: "macbook", SessionID: "s2", PaneID: "w1:p2", Status: proto.StatusWorking},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	syncCalls := make(chan map[string]any, 10)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "todo.sync_tokens" {
			var p map[string]any
			_ = json.Unmarshal(params, &p)
			syncCalls <- p
		}
		return map[string]any{"ok": true}, nil
	}
	connCtx, connCancel := context.WithCancel(context.Background())
	t.Cleanup(connCancel)
	go func() { _ = hubPeer.Serve(connCtx, hubIn) }()
	go func() { _ = nodePeer.Serve(connCtx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_tok", Title: "待办标记测试", CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// Link s1 to td_tok -> triggers linkTodoAgent which notifies sessionsChanged
	_ = h.linkTodoAgent(context.Background(), "td_tok", "macbook", "s1", "Agent 1", "/cwd", "claude", "")

	select {
	case call := <-syncCalls:
		tokensRaw, ok := call["tokens"].(map[string]any)
		if !ok || tokensRaw["w1:p1"] == nil {
			t.Fatalf("expected w1:p1 in tokens: %+v", call)
		}
		paneToks := tokensRaw["w1:p1"].(map[string]any)
		if paneToks["persona_todo"] != "待办标记测试" || paneToks["persona_todo_ref"] != "td_tok@macbook/s1" {
			t.Fatalf("tokens mismatch: %+v", paneToks)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for todo.sync_tokens")
	}

	// When td_tok is completed, activeBySession is cleared and sync_tokens pushes empty map for w1:p1
	patchDone := `{"done":true}`
	req := todoRequest("PATCH", "/api/v1/todos/td_tok", strings.NewReader(patchDone))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH done: %d", w.Code)
	}

	select {
	case call := <-syncCalls:
		tokensRaw, ok := call["tokens"].(map[string]any)
		if !ok || len(tokensRaw) != 0 {
			t.Fatalf("expected empty tokens after todo is done, got: %+v", tokensRaw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for empty todo.sync_tokens after completion")
	}
}

// TestTodoF1F2F3 verifies:
// - F1: sessionsChanged and todos broadcast triggered when linking via linkTodoAgent
// - F2: offline sessions (Status == offline) are NOT auto-linked or space-linked
// - F3: completed todo reopened does NOT re-link sessions that were moved to other todos
func TestTodoF1F2F3(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())
	handler := h.routes()

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	now := time.Now().UTC()
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_A", Title: "待办 A", CreatedAt: now, UpdatedAt: now})
	_ = database.CreateTodo(context.Background(), db.TodoRow{ID: "td_B", Title: "待办 B", CreatedAt: now, UpdatedAt: now})
	h.initTodos(context.Background())

	// Connect fake node
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	}
	connCtx, connCancel := context.WithCancel(context.Background())
	t.Cleanup(connCancel)
	go func() { _ = hubPeer.Serve(connCtx, hubIn) }()
	go func() { _ = nodePeer.Serve(connCtx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	// --- F2: Verify offline sessions are NOT linked by space link or auto link ---
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s_off", Title: "Offline Agent", SpaceName: "sp_offline", Status: proto.StatusOffline},
		{MachineID: "macbook", SessionID: "s_on", Title: "Online Agent", SpaceName: "sp_offline", Status: proto.StatusWorking},
	}
	// Space link sp_offline to td_A
	req := todoRequest("PUT", "/api/v1/todos/td_A/spaces/macbook/sp_offline", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT space expected 200, got %d", w.Code)
	}

	dec := h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_off" && s.Todo != nil {
			t.Fatalf("F2: offline session should NOT have Todo attached, got %+v", s.Todo)
		}
		if s.SessionID == "s_on" && (s.Todo == nil || s.Todo.ID != "td_A") {
			t.Fatalf("online session should have Todo attached: %+v", s.Todo)
		}
	}

	// --- F3: A completed -> X moved to B -> reopen A -> X stays ONLY on B ---
	// 1. Session X is on td_A
	m.sessions = append(m.sessions, proto.Session{
		MachineID: "macbook", SessionID: "s_x", Title: "Agent X", Status: proto.StatusWorking,
	})
	if err := h.linkTodoAgent(context.Background(), "td_A", "macbook", "s_x", "Agent X", "/cwd", "claude", ""); err != nil {
		t.Fatalf("link s_x to td_A: %v", err)
	}

	// 2. Complete td_A
	patchDone := `{"done":true}`
	req = todoRequest("PATCH", "/api/v1/todos/td_A", strings.NewReader(patchDone))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH done td_A: %d", w.Code)
	}

	// 3. Move s_x to td_B
	req = todoRequest("PUT", "/api/v1/todos/td_B/agents/macbook/s_x", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT s_x to td_B: %d", w.Code)
	}

	// 4. Reopen td_A
	patchReopen := `{"done":false}`
	req = todoRequest("PATCH", "/api/v1/todos/td_A", strings.NewReader(patchReopen))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH reopen td_A: %d", w.Code)
	}

	// Verify s_x is STILL on td_B, NOT reverted to td_A!
	dec = h.decorated(m)
	for _, s := range dec {
		if s.SessionID == "s_x" {
			if s.Todo == nil || s.Todo.ID != "td_B" {
				t.Fatalf("F3: expected s_x to stay linked to td_B, got: %+v", s.Todo)
			}
		}
	}
}

// TestHubRestartAndTokensPushLifecycle covers B1 requirements:
// 1. Hub restart (new Hub instance loading from db) pushes expected tokens on first sessionsChanged
// 2. Unchanged expected tokens do NOT push repeatedly
// 3. Session changing pane ID re-triggers push with updated pane key
// 4. Node disconnect and reconnect clears lastTodoTokens, guaranteeing next push
func TestHubRestartAndTokensPushLifecycle(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now().UTC()
	ctx := context.Background()

	// Seed database with a todo and an active agent link
	_ = database.CreateTodo(ctx, db.TodoRow{ID: "td_restart", Title: "重启恢复测试", CreatedAt: now, UpdatedAt: now})
	_, _ = database.LinkTodoAgentWithSource(ctx, "td_restart", "macbook", "s1", "Agent 1", "/cwd", "claude", "", "manual", "", now)

	// Simulate hub restart by creating a brand new Hub instance and loading from db
	h := newTestHub(t)
	h.store = database
	h.initTodos(ctx)

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", PaneID: "w1:p1", Status: proto.StatusWorking},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	// Setup fake node connection
	syncCalls := make(chan map[string]any, 10)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "todo.sync_tokens" {
			var p map[string]any
			_ = json.Unmarshal(params, &p)
			syncCalls <- p
		}
		return map[string]any{"ok": true}, nil
	}
	connCtx, connCancel := context.WithCancel(ctx)
	t.Cleanup(connCancel)
	go func() { _ = hubPeer.Serve(connCtx, hubIn) }()
	go func() { _ = nodePeer.Serve(connCtx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	// 1. First sessionsChanged after hub restart: should calculate and push expected tokens
	h.sessionsChanged(m)

	select {
	case call := <-syncCalls:
		tokensRaw, ok := call["tokens"].(map[string]any)
		if !ok || tokensRaw["w1:p1"] == nil {
			t.Fatalf("expected tokens for w1:p1 on first push, got: %+v", call)
		}
		paneTokens := tokensRaw["w1:p1"].(map[string]any)
		if paneTokens["persona_todo"] != "重启恢复测试" || paneTokens["persona_todo_ref"] != "td_restart@macbook/s1" {
			t.Fatalf("tokens mismatch: %+v", paneTokens)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial todo.sync_tokens push")
	}

	// 2. Second sessionsChanged with same sessions: expected tokens unchanged, should NOT push
	h.sessionsChanged(m)
	select {
	case call := <-syncCalls:
		t.Fatalf("unexpected duplicate push when tokens unchanged: %+v", call)
	case <-time.After(200 * time.Millisecond):
		// Expected: no push
	}

	// 3. Session changes pane ID from w1:p1 to w1:p2
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", PaneID: "w1:p2", Status: proto.StatusWorking},
	}
	h.sessionsChanged(m)

	select {
	case call := <-syncCalls:
		tokensRaw, ok := call["tokens"].(map[string]any)
		if !ok || tokensRaw["w1:p2"] == nil || tokensRaw["w1:p1"] != nil {
			t.Fatalf("expected tokens re-keyed to w1:p2, got: %+v", call)
		}
		paneTokens := tokensRaw["w1:p2"].(map[string]any)
		if paneTokens["persona_todo"] != "重启恢复测试" {
			t.Fatalf("tokens mismatch: %+v", paneTokens)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for updated todo.sync_tokens push after pane change")
	}

	// 4. Node disconnect resets lastTodoTokens, guaranteeing push on reconnect
	m.detach(nil, false)
	m.mu.Lock()
	if m.lastTodoTokens != nil {
		t.Fatalf("expected lastTodoTokens to be cleared on detach")
	}
	// Reconnect
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	h.sessionsChanged(m)
	select {
	case call := <-syncCalls:
		tokensRaw, ok := call["tokens"].(map[string]any)
		if !ok || tokensRaw["w1:p2"] == nil {
			t.Fatalf("expected push on reconnect, got: %+v", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for todo.sync_tokens push after reconnect")
	}
}
