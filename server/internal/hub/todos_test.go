package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	url := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set (scripts/test/test-server.sh runs this on the hub machine)")
	}
	database, err := db.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.TruncateForTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestTodosCRUDAndPatch(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())
	handler := h.routes()

	// 1. POST /api/v1/todos - empty title should fail with 400
	req := httptest.NewRequest("POST", "/api/v1/todos", strings.NewReader(`{"title":""}`))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty title, got %d", w.Code)
	}

	// 2. POST /api/v1/todos - success (S9: status code 200, S4: "agents":[])
	body, _ := json.Marshal(map[string]any{"title": "天气 App 发 1.3 版", "notes": "测试说明"})
	req = httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rawJSON := w.Body.String()
	if !strings.Contains(rawJSON, `"agents":[]`) {
		t.Fatalf("expected raw JSON to contain '\"agents\":[]', got %s", rawJSON)
	}
	var created struct {
		Todo Todo `json:"todo"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Todo.ID == "" || created.Todo.Title != "天气 App 发 1.3 版" || created.Todo.Notes != "测试说明" {
		t.Fatalf("unexpected created todo: %+v", created.Todo)
	}
	todoID := created.Todo.ID

	// 3. GET /api/v1/todos - S4: "agents":[]
	req = httptest.NewRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"agents":[]`) {
		t.Fatalf("expected GET list raw JSON to contain '\"agents\":[]', got %s", w.Body.String())
	}
	var listRes struct {
		Todos []Todo `json:"todos"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	if len(listRes.Todos) == 0 || listRes.Todos[0].ID != todoID {
		t.Fatalf("expected todo in list, got %+v", listRes.Todos)
	}

	// 4. PATCH /api/v1/todos/{id} - only update title (S4: "agents":[])
	patchBody, _ := json.Marshal(map[string]any{"title": "新标题"})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todoID, bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"agents":[]`) {
		t.Fatalf("expected PATCH raw JSON to contain '\"agents\":[]', got %s", w.Body.String())
	}
	var patched struct {
		Todo Todo `json:"todo"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Todo.Title != "新标题" || patched.Todo.Notes != "测试说明" {
		t.Fatalf("unexpected patched todo: %+v", patched.Todo)
	}

	// PATCH done = true
	patchBody, _ = json.Marshal(map[string]any{"done": true})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todoID, bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Todo.DoneAt == nil {
		t.Fatalf("expected DoneAt to be set")
	}

	// PATCH done = false (reopen)
	patchBody, _ = json.Marshal(map[string]any{"done": false})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todoID, bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Todo.DoneAt != nil {
		t.Fatalf("expected DoneAt to be nil after reopen")
	}

	// 5. DELETE /api/v1/todos/{id}
	req = httptest.NewRequest("DELETE", "/api/v1/todos/"+todoID, nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Subsequent PATCH should fail with 404
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todoID, bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestTodoAgentLinkTransferAndUnlink(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", Title: "测试 Agent", Cwd: "/code/weather", Agent: "claude"},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m
	h.initTodos(context.Background())
	handler := h.routes()

	// Create 2 todos
	b1, _ := json.Marshal(map[string]any{"title": "待办 1"})
	req := httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b1))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var r1 struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &r1)
	todo1ID := r1.Todo.ID

	b2, _ := json.Marshal(map[string]any{"title": "待办 2"})
	req = httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b2))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var r2 struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &r2)
	todo2ID := r2.Todo.ID

	// 1. Link agent s1 to todo1
	req = httptest.NewRequest("PUT", "/api/v1/todos/"+todo1ID+"/agents/macbook/s1", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify session now has Todo
	dec := h.decorated(m)
	if dec[0].Todo == nil || dec[0].Todo.ID != todo1ID || dec[0].Todo.Title != "待办 1" {
		t.Fatalf("expected session todo to be todo1, got %+v", dec[0].Todo)
	}

	// 2. Transfer agent s1 to todo2: todo1's link should become released
	req = httptest.NewRequest("PUT", "/api/v1/todos/"+todo2ID+"/agents/macbook/s1", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on transfer, got %d: %s", w.Code, w.Body.String())
	}

	// Verify session now points to todo2
	dec = h.decorated(m)
	if dec[0].Todo == nil || dec[0].Todo.ID != todo2ID || dec[0].Todo.Title != "待办 2" {
		t.Fatalf("expected session todo to be todo2, got %+v", dec[0].Todo)
	}

	// Check todo1's agents in GET /api/v1/todos: s1 is now released
	req = httptest.NewRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var todosRes struct{ Todos []Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &todosRes)
	for _, td := range todosRes.Todos {
		if td.ID == todo1ID {
			if len(td.Agents) != 1 || td.Agents[0].ReleasedAt == nil {
				t.Fatalf("expected todo1 agent to be released, got %+v", td.Agents)
			}
		} else if td.ID == todo2ID {
			if len(td.Agents) != 1 || td.Agents[0].ReleasedAt != nil {
				t.Fatalf("expected todo2 agent to be active, got %+v", td.Agents)
			}
		}
	}

	// 3. Mark todo2 done: session Todo field disappears
	doneBody, _ := json.Marshal(map[string]any{"done": true})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todo2ID, bytes.NewReader(doneBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	dec = h.decorated(m)
	if dec[0].Todo != nil {
		t.Fatalf("expected session todo to be nil when todo completed, got %+v", dec[0].Todo)
	}

	// Attempting to link agent to completed todo should fail with 409
	req = httptest.NewRequest("PUT", "/api/v1/todos/"+todo2ID+"/agents/macbook/s2", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on completed todo link, got %d", w.Code)
	}

	// Reopen todo2: session Todo reappears
	reopenBody, _ := json.Marshal(map[string]any{"done": false})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+todo2ID, bytes.NewReader(reopenBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	dec = h.decorated(m)
	if dec[0].Todo == nil || dec[0].Todo.ID != todo2ID {
		t.Fatalf("expected session todo to reappear, got %+v", dec[0].Todo)
	}

	// 4. DELETE /api/v1/todos/{id}/agents/{mid}/{sid} - manual unlink
	req = httptest.NewRequest("DELETE", "/api/v1/todos/"+todo2ID+"/agents/macbook/s1", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	dec = h.decorated(m)
	if dec[0].Todo != nil {
		t.Fatalf("expected session todo to be nil after unlink, got %+v", dec[0].Todo)
	}
}

// countingStore wraps db.DB to count UpdateTodoAgentSnapshot calls for S5 test.
type countingStore struct {
	*db.DB
	snapshotCalls atomic.Int64
}

func (c *countingStore) UpdateTodoAgentSnapshot(ctx context.Context, machineID, sessionID, title, startError string) error {
	c.snapshotCalls.Add(1)
	return c.DB.UpdateTodoAgentSnapshot(ctx, machineID, sessionID, title, startError)
}

func TestTodoSnapshotAsyncUpdateAndDedupe(t *testing.T) {
	database := setupTestDB(t)
	cStore := &countingStore{DB: database}
	h := newTestHub(t)
	h.store = cStore
	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{
		{MachineID: "macbook", SessionID: "s1", Title: "初始标题", Cwd: "/code", Agent: "claude"},
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m
	h.initTodos(context.Background())
	handler := h.routes()

	// Create todo
	b, _ := json.Marshal(map[string]any{"title": "待办 A"})
	req := httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var r struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	todoID := r.Todo.ID

	// Link agent
	req = httptest.NewRequest("PUT", "/api/v1/todos/"+todoID+"/agents/macbook/s1", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("link failed: %d", w.Code)
	}

	// S5: Repeatedly trigger sessions update with same new title rapidly to verify deduplication
	for i := 0; i < 5; i++ {
		m.setSessions([]proto.Session{
			{MachineID: "macbook", SessionID: "s1", Title: "新 Agent 标题", Cwd: "/code", Agent: "claude", StartError: "start failed reason"},
		})
	}

	// Wait for async update to complete
	deadline := time.Now().Add(3 * time.Second)
	updated := false
	for time.Now().Before(deadline) {
		h.todos.mu.RLock()
		td := h.todos.todos[todoID]
		if td != nil && len(td.Agents) > 0 && td.Agents[0].Title == "新 Agent 标题" && td.Agents[0].StartError == "start failed reason" {
			updated = true
			h.todos.mu.RUnlock()
			break
		}
		h.todos.mu.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}
	if !updated {
		t.Fatalf("snapshot was not updated in memory")
	}

	// Allow brief time for any errant extra goroutines
	time.Sleep(100 * time.Millisecond)
	calls := cStore.snapshotCalls.Load()
	if calls != 1 {
		t.Fatalf("expected snapshot write to be called exactly 1 time (deduped), got %d", calls)
	}

	// Verify in database as well
	agents, err := database.ListAllTodoAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAllTodoAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].Title != "新 Agent 标题" || agents[0].StartError != "start failed reason" {
		t.Fatalf("snapshot was not updated in DB: %+v", agents)
	}
}

func TestReadOnlyRoleForbidden(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())

	// Create watch token (role: answer)
	tok := "jd_watch_token_" + randomHex(8)
	dt := db.DeviceToken{ID: "dev_watch_" + randomHex(4), Name: "Apple Watch", Kind: "watch", Role: "answer"}
	if err := database.CreateDeviceToken(context.Background(), dt, hashToken(tok)); err != nil {
		t.Fatal(err)
	}

	handler := h.routes()

	// 1. POST /api/v1/todos with watch token -> 403
	req := httptest.NewRequest("POST", "/api/v1/todos", strings.NewReader(`{"title":"手表写待办"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on POST /todos, got %d", w.Code)
	}

	// 2. PATCH /api/v1/todos/{id} -> 403
	req = httptest.NewRequest("PATCH", "/api/v1/todos/any_id", strings.NewReader(`{"title":"改"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on PATCH /todos, got %d", w.Code)
	}

	// 3. DELETE /api/v1/todos/{id} -> 403
	req = httptest.NewRequest("DELETE", "/api/v1/todos/any_id", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on DELETE /todos, got %d", w.Code)
	}

	// 4. PUT /api/v1/todos/{id}/agents/... -> 403
	req = httptest.NewRequest("PUT", "/api/v1/todos/any_id/agents/m/s", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on PUT agents, got %d", w.Code)
	}

	// 5. Send message with todo_id using watch token -> 403
	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m
	msgBody, _ := json.Marshal(map[string]any{"text": "hello", "todo_id": "any_id"})
	req = httptest.NewRequest("POST", "/api/v1/sessions/macbook/s1/messages", bytes.NewReader(msgBody))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on sendMessage with todo_id, got %d", w.Code)
	}

	// 6. Start session with todo_id using watch token -> 403
	startBody, _ := json.Marshal(map[string]any{"machine_id": "macbook", "cwd": "/code", "todo_id": "any_id"})
	req = httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(startBody))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only role on startSession with todo_id, got %d", w.Code)
	}
}

func TestStartSessionAndSendMessageWithTodoID(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())

	// Connect fake node
	_ = pipeNode(t, h, "macbook", func(ctx context.Context) error { return nil })
	m := h.byID["macbook"]
	m.peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{
				"session": proto.Session{
					MachineID: "macbook",
					SessionID: "s_new",
					Title:     "新开的 Agent",
					Cwd:       "/code/app",
					Agent:     "claude",
				},
			}, nil
		}
		if method == "session.send" {
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
	}

	handler := h.routes()

	// 1. Create an active todo and complete another todo
	b, _ := json.Marshal(map[string]any{"title": "有效待办"})
	req := httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var tr struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &tr)
	activeTodoID := tr.Todo.ID

	b, _ = json.Marshal(map[string]any{"title": "已完成待办"})
	req = httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var trDone struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &trDone)
	doneTodoID := trDone.Todo.ID

	patchDone, _ := json.Marshal(map[string]any{"done": true})
	req = httptest.NewRequest("PATCH", "/api/v1/todos/"+doneTodoID, bytes.NewReader(patchDone))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// 2. startSession validations
	// Not found todo -> 404
	startBody, _ := json.Marshal(map[string]any{"machine_id": "macbook", "cwd": "/code", "todo_id": "non_existent"})
	req = httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(startBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on startSession with unknown todo_id, got %d", w.Code)
	}

	// Completed todo -> 409
	startBody, _ = json.Marshal(map[string]any{"machine_id": "macbook", "cwd": "/code", "todo_id": doneTodoID})
	req = httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(startBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on startSession with done todo_id, got %d", w.Code)
	}

	// Active todo -> 200, session returned with Todo, and todo has agent linked
	startBody, _ = json.Marshal(map[string]any{"machine_id": "macbook", "cwd": "/code", "todo_id": activeTodoID})
	req = httptest.NewRequest("POST", "/api/v1/sessions", bytes.NewReader(startBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on startSession, got %d: %s", w.Code, w.Body.String())
	}
	var sessRes struct{ Session proto.Session }
	_ = json.Unmarshal(w.Body.Bytes(), &sessRes)
	if sessRes.Session.Todo == nil || sessRes.Session.Todo.ID != activeTodoID {
		t.Fatalf("expected session todo to be activeTodoID, got %+v", sessRes.Session.Todo)
	}

	// 3. sendMessage validations
	m.sessions = append(m.sessions, proto.Session{MachineID: "macbook", SessionID: "s2", Title: "老 Agent", Cwd: "/code", Agent: "claude"})

	// Send message with unknown todo_id -> 404
	msgBody, _ := json.Marshal(map[string]any{"text": "hi", "todo_id": "non_existent"})
	req = httptest.NewRequest("POST", "/api/v1/sessions/macbook/s2/messages", bytes.NewReader(msgBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on sendMessage with unknown todo_id, got %d", w.Code)
	}

	// Send message with done todo_id -> 409
	msgBody, _ = json.Marshal(map[string]any{"text": "hi", "todo_id": doneTodoID})
	req = httptest.NewRequest("POST", "/api/v1/sessions/macbook/s2/messages", bytes.NewReader(msgBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on sendMessage with done todo_id, got %d", w.Code)
	}

	// Send message with active todo_id -> 200, agent linked
	msgBody, _ = json.Marshal(map[string]any{"text": "hi", "todo_id": activeTodoID})
	req = httptest.NewRequest("POST", "/api/v1/sessions/macbook/s2/messages", bytes.NewReader(msgBody))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on sendMessage, got %d: %s", w.Code, w.Body.String())
	}

	dec := h.decorated(m)
	var s2 *proto.Session
	for i := range dec {
		if dec[i].SessionID == "s2" {
			s2 = &dec[i]
		}
	}
	if s2 == nil || s2.Todo == nil || s2.Todo.ID != activeTodoID {
		t.Fatalf("expected s2 to have todo, got %+v", s2)
	}
}

// S7: TestCleanPromptTitle
func TestCleanPromptTitle(t *testing.T) {
	cases := []struct {
		name     string
		prompt   string
		expected string
	}{
		{
			name: "with context and task",
			prompt: `<jarvis-context kind="context" title="待办 · 天气" url="">说明内容
多行说明</jarvis-context>
把 1.3 的发布说明整理出来
第二行要求`,
			expected: "把 1.3 的发布说明整理出来",
		},
		{
			name: "only context",
			prompt: `<jarvis-context kind="context" title="待办" url="">只有说明</jarvis-context>
`,
			expected: "",
		},
		{
			name:     "plain text",
			prompt:   "普通的第一行任务\n第二行任务",
			expected: "普通的第一行任务",
		},
		{
			name:     "blank text",
			prompt:   "   \n\n\t",
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanPromptTitle(tc.prompt)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
			if strings.Contains(got, "<jarvis-context") {
				t.Fatalf("result must never contain <jarvis-context, got %q", got)
			}
		})
	}
}

// S2: TestTodosCacheReliability
type failingStore struct {
	*db.DB
	failLoad atomic.Bool
}

func (f *failingStore) ListTodos(ctx context.Context) ([]db.TodoRow, error) {
	if f.failLoad.Load() {
		return nil, errors.New("simulated db failure")
	}
	return f.DB.ListTodos(ctx)
}

func (f *failingStore) ListAllTodoAgents(ctx context.Context) ([]db.TodoAgentRow, error) {
	if f.failLoad.Load() {
		return nil, errors.New("simulated db failure")
	}
	return f.DB.ListAllTodoAgents(ctx)
}

func TestTodosCacheReliability(t *testing.T) {
	database := setupTestDB(t)
	// Create a todo in DB first
	tRow := db.TodoRow{
		ID:        "t_exist",
		Title:     "已存在的待办",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := database.CreateTodo(context.Background(), tRow); err != nil {
		t.Fatal(err)
	}

	fStore := &failingStore{DB: database}
	fStore.failLoad.Store(true) // simulate initial load failure

	h := newTestHub(t)
	h.store = fStore
	h.initTodos(context.Background()) // fails silently, loaded = false

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	m.sessions = []proto.Session{{MachineID: "macbook", SessionID: "s1", Title: "Agent"}}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	// decorated() when not loaded must NOT block or panic or add todo
	dec := h.decorated(m)
	if len(dec) != 1 || dec[0].Todo != nil {
		t.Fatalf("decorated should not add todo when not loaded")
	}

	handler := h.routes()

	// Calling GET /api/v1/todos while failing should return 503, NOT empty list 200
	req := httptest.NewRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when store unavailable, got %d: %s", w.Code, w.Body.String())
	}

	// Calling PATCH while failing should return 503, NOT 404
	req = httptest.NewRequest("PATCH", "/api/v1/todos/t_exist", strings.NewReader(`{"title":"改"}`))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when store unavailable on PATCH, got %d: %s", w.Code, w.Body.String())
	}

	// Now store recovers
	fStore.failLoad.Store(false)

	// Next call to GET should succeed and reload cache
	req = httptest.NewRequest("GET", "/api/v1/todos", nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after recovery, got %d: %s", w.Code, w.Body.String())
	}
	var res struct{ Todos []Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Todos) != 1 || res.Todos[0].ID != "t_exist" {
		t.Fatalf("expected recovered todo list, got %+v", res.Todos)
	}
}

// S3: TestTodosConcurrencyRace
func TestTodosConcurrencyRace(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	h.initTodos(context.Background())

	m := newMachine(h, MachineConfig{ID: "macbook", Name: "MacBook"})
	for i := 0; i < 5; i++ {
		m.sessions = append(m.sessions, proto.Session{
			MachineID: "macbook",
			SessionID: fmt.Sprintf("s_%d", i),
			Title:     fmt.Sprintf("Agent %d", i),
		})
	}
	h.machines = append(h.machines, m)
	h.byID["macbook"] = m

	handler := h.routes()

	// Create test todo
	b, _ := json.Marshal(map[string]any{"title": "并发测试待办"})
	req := httptest.NewRequest("POST", "/api/v1/todos", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var cr struct{ Todo Todo }
	_ = json.Unmarshal(w.Body.Bytes(), &cr)
	todoID := cr.Todo.ID

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Goroutine 1: Rapid PATCH done toggle
	wg.Add(1)
	go func() {
		defer wg.Done()
		done := false
		for {
			select {
			case <-stop:
				return
			default:
				done = !done
				body, _ := json.Marshal(map[string]any{"done": done})
				r := httptest.NewRequest("PATCH", "/api/v1/todos/"+todoID, bytes.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, r)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Rapid PUT and DELETE agent links
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				sid := fmt.Sprintf("s_%d", i%5)
				r := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/todos/%s/agents/macbook/%s", todoID, sid), nil)
				r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, r)

				rDel := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/todos/%s/agents/macbook/%s", todoID, sid), nil)
				rDel.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				recDel := httptest.NewRecorder()
				handler.ServeHTTP(recDel, rDel)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Rapid GET list and getTodoForLink
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				r := httptest.NewRequest("GET", "/api/v1/todos", nil)
				r.Header.Set("Authorization", "Bearer "+h.cfg.Token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, r)

				_, _ = h.getTodoForLink(context.Background(), todoID)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Rapid decorated() calls on machine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = h.decorated(m)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Run concurrency for 500ms
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}
