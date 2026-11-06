package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// The todos and the machines' session lists (todo_sessions.go): whole workspaces, agents following their workspace
// or the agent that spawned them, the herdr marks.

// seedTodos puts todos in the store before the assistant reads it.
func seedTodos(t *testing.T, database *db.DB, rows ...db.TodoRow) {
	t.Helper()
	now := time.Now().UTC()
	for _, r := range rows {
		if r.CreatedAt.IsZero() {
			r.CreatedAt, r.UpdatedAt = now, now
		}
		if err := database.CreateTodo(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
}

// A whole workspace (its name may have a slash) goes to a todo with its online agents; moved to another todo it takes
// them along (an agent linked by hand stays); unlinked, the agents it brought go.
func TestTodoSpaceLinkAndUnlink(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "td1", Title: "Todo 1"}, db.TodoRow{ID: "td2", Title: "Todo 2"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook",
		proto.Session{SessionID: "s1", Title: "Agent 1", SpaceName: "group/alpha", Status: proto.StatusWorking},
		proto.Session{SessionID: "s2", Title: "Agent 2", SpaceName: "other/proj", Status: proto.StatusWorking})

	if w := e.must("PUT", "/api/v1/todos/td1/spaces/macbook/group%2Falpha", ""); w.Body.String() != "{\"linked\":1,\"ok\":true}\n" {
		t.Fatalf("PUT space = %s", w.Body)
	}
	e.must("PUT", "/api/v1/todos/td1/agents/macbook/s2", "")
	td1 := e.get("td1")
	if len(td1.Spaces) != 1 || td1.Spaces[0].Space != "group/alpha" || len(td1.Agents) != 2 {
		t.Fatalf("td1 = %+v", td1)
	}
	if a := agentOf(*td1, "s1"); a.Source != "space" || a.Via != "macbook/group/alpha" {
		t.Fatalf("s1 = %+v", a)
	}
	if a := agentOf(*td1, "s2"); a.Source != "manual" || a.Via != "" {
		t.Fatalf("s2 = %+v", a)
	}

	e.must("PUT", "/api/v1/todos/td2/spaces/macbook/group%2Falpha", "")
	if td1 := e.get("td1"); len(td1.Spaces) != 0 || len(td1.Agents) != 1 || td1.Agents[0].SessionID != "s2" {
		t.Fatalf("td1 after the move = %+v", td1)
	}
	if td2 := e.get("td2"); len(td2.Spaces) != 1 || len(td2.Agents) != 1 || td2.Agents[0].SessionID != "s1" {
		t.Fatalf("td2 after the move = %+v", td2)
	}
	if e.todoOf("macbook", "s1") != "td2" || e.todoOf("macbook", "s2") != "td1" {
		t.Fatal("the report does not follow the move")
	}

	e.must("DELETE", "/api/v1/todos/td2/spaces/macbook/group%2Falpha", "")
	if td2 := e.get("td2"); len(td2.Spaces) != 0 || len(td2.Agents) != 0 {
		t.Fatalf("td2 after the unlink = %+v", td2)
	}
	if w := e.do("DELETE", "/api/v1/todos/td2/spaces/macbook/group%2Falpha", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unlinking it again: %d", w.Code)
	}
}

// A new agent in a linked workspace joins its todo; one on another todo is not taken; one removed by hand is not taken
// back, unless linked by hand again; a done todo takes no more.
func TestTodoSpaceAutoLinkAndDismissed(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "t_main", Title: "Main Todo"}, db.TodoRow{ID: "t_other", Title: "Other Todo"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook")
	e.must("PUT", "/api/v1/todos/t_main/spaces/macbook/work", "")
	existing := proto.Session{SessionID: "s_existing", Title: "Existing", SpaceName: "work", Status: proto.StatusWorking}
	e.f.setSessions("macbook", existing)
	e.must("PUT", "/api/v1/todos/t_other/agents/macbook/s_existing", "")

	list := []proto.Session{existing, {SessionID: "s_new", Title: "New Agent", SpaceName: "work", Status: proto.StatusWorking}}
	e.sessions("macbook", list...)
	if e.todoOf("macbook", "s_new") != "t_main" || e.todoOf("macbook", "s_existing") != "t_other" {
		t.Fatalf("s_new on %q, s_existing on %q", e.todoOf("macbook", "s_new"), e.todoOf("macbook", "s_existing"))
	}
	if snap := e.f.lastSnapshot(); snap.Todos == nil || len(snap.Todos.Links) != 2 {
		t.Fatalf("the hub was not told: %+v", snap.Todos)
	}
	waitFor(t, "the stored link", func() bool {
		agents, _ := database.ListAllTodoAgents(context.Background())
		return len(agents) == 2
	})

	e.must("DELETE", "/api/v1/todos/t_main/agents/macbook/s_new", "")
	if agentOf(*e.get("t_main"), "s_new") != nil {
		t.Fatal("a removed agent is listed")
	}
	e.sessions("macbook", list...)
	if got := e.todoOf("macbook", "s_new"); got != "" {
		t.Fatalf("a removed agent was linked back to %q", got)
	}
	e.must("PUT", "/api/v1/todos/t_main/agents/macbook/s_new", "")
	if got := e.todoOf("macbook", "s_new"); got != "t_main" {
		t.Fatalf("linked by hand again: %q", got)
	}

	e.must("PATCH", "/api/v1/todos/t_main", `{"done":true}`)
	e.sessions("macbook", append(list, proto.Session{SessionID: "s_after", SpaceName: "work", Status: proto.StatusWorking})...)
	if got := e.todoOf("macbook", "s_after"); got != "" {
		t.Fatalf("a done todo took s_after: %q", got)
	}
}

// An agent spawned by one working for an open todo (persona_from: <todo>@<machine>/<session>) joins it, on any
// machine, and so does its own child; a todo that is gone or done takes none.
func TestTodoSpawnDerivation(t *testing.T) {
	database := todoTestDB(t)
	now := time.Now().UTC()
	seedTodos(t, database, db.TodoRow{ID: "td_spawn", Title: "Spawn Task"}, db.TodoRow{ID: "td_done", Title: "Done Task", DoneAt: &now})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook", proto.Session{SessionID: "p1", Title: "Parent Agent", Status: proto.StatusWorking})
	e.must("PUT", "/api/v1/todos/td_spawn/agents/macbook/p1", "")

	dev := []proto.Session{
		{SessionID: "c1", Title: "Child Worker", Status: proto.StatusWorking, SpawnFrom: "td_spawn@macbook/p1"},
		{SessionID: "c_bad", Status: proto.StatusWorking, SpawnFrom: "no_such_todo@macbook/p1"},
		{SessionID: "c_done", Status: proto.StatusWorking, SpawnFrom: "td_done@macbook/p1"},
		{SessionID: "c_off", Status: proto.StatusOffline, SpawnFrom: "td_spawn@macbook/p1"},
	}
	e.sessions("dev", dev...)
	if e.todoOf("dev", "c1") != "td_spawn" || e.todoOf("dev", "c_bad") != "" || e.todoOf("dev", "c_done") != "" || e.todoOf("dev", "c_off") != "" {
		t.Fatalf("report = %+v", e.s.todoReport(context.Background()).Links)
	}
	e.sessions("dev", append(dev, proto.Session{SessionID: "g1", Status: proto.StatusWorking, SpawnFrom: "td_spawn@dev/c1"})...)
	if e.todoOf("dev", "g1") != "td_spawn" {
		t.Fatal("the grandchild did not follow")
	}
	td := e.get("td_spawn")
	if a := agentOf(*td, "c1"); a.Source != "spawned" || a.Via != "macbook/p1" || a.Title != "Child Worker" {
		t.Fatalf("c1 = %+v", a)
	}
	if a := agentOf(*td, "g1"); a.Source != "spawned" || a.Via != "dev/c1" {
		t.Fatalf("g1 = %+v", a)
	}
	// Read again from the store (a restart): the same.
	waitFor(t, "the stored links", func() bool {
		agents, _ := database.ListAllTodoAgents(context.Background())
		return len(agents) == 3
	})
	reloadTodos(t, e.s)
	if e.todoOf("dev", "g1") != "td_spawn" {
		t.Fatal("not stored")
	}
}

// A workspace the hub renamed (todos.space_renamed): the link and its agents' via follow; when the new name is
// another todo's, the old link is void.
func TestTodoSpaceRename(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "td_ren", Title: "Rename Test"}, db.TodoRow{ID: "t2", Title: "Todo 2"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook", proto.Session{SessionID: "s1", SpaceID: "w_old", SpaceName: "old_name", Status: proto.StatusWorking})
	e.must("PUT", "/api/v1/todos/td_ren/spaces/macbook/old_name", "")
	e.must("PUT", "/api/v1/todos/t2/spaces/macbook/beta", "")
	e.s.TodoSpaceRenamed(context.Background(), TodoSpaceRenamed{MachineID: "macbook", Old: "old_name", New: "new_name"})
	td := e.get("td_ren")
	if len(td.Spaces) != 1 || td.Spaces[0].Space != "new_name" || len(td.Agents) != 1 || td.Agents[0].Via != "macbook/new_name" {
		t.Fatalf("td_ren = %+v", td)
	}
	reloadTodos(t, e.s)
	if td := e.get("td_ren"); len(td.Spaces) != 1 || td.Spaces[0].Space != "new_name" {
		t.Fatalf("not stored: %+v", td)
	}

	e.s.TodoSpaceRenamed(context.Background(), TodoSpaceRenamed{MachineID: "macbook", Old: "new_name", New: "beta"})
	if td := e.get("td_ren"); len(td.Spaces) != 0 || len(td.Agents) != 0 || e.todoOf("macbook", "s1") != "" {
		t.Fatalf("td_ren after a rename onto t2's = %+v", td)
	}
	if t2 := e.get("t2"); len(t2.Spaces) != 1 || t2.Spaces[0].Space != "beta" {
		t.Fatalf("t2 = %+v", t2)
	}
}

// Offline agents join nothing; a done todo reopened does not take back an agent that moved on meanwhile.
func TestTodoOfflineAndReopen(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "td_A", Title: "待办 A"}, db.TodoRow{ID: "td_B", Title: "待办 B"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook",
		proto.Session{SessionID: "s_off", SpaceName: "sp", Status: proto.StatusOffline},
		proto.Session{SessionID: "s_on", SpaceName: "sp", Status: proto.StatusWorking},
		proto.Session{SessionID: "s_x", Status: proto.StatusWorking})
	e.must("PUT", "/api/v1/todos/td_A/spaces/macbook/sp", "")
	if e.todoOf("macbook", "s_off") != "" || e.todoOf("macbook", "s_on") != "td_A" {
		t.Fatal("offline agent linked, or online one not")
	}
	e.must("PUT", "/api/v1/todos/td_A/agents/macbook/s_x", "")
	e.must("PATCH", "/api/v1/todos/td_A", `{"done":true}`)
	e.must("PUT", "/api/v1/todos/td_B/agents/macbook/s_x", "")
	e.must("PATCH", "/api/v1/todos/td_A", `{"done":false}`)
	if got := e.todoOf("macbook", "s_x"); got != "td_B" {
		t.Fatalf("s_x on %q after td_A's reopen", got)
	}
}

// An agent of a todo now done that joins another on its own (its workspace) took part in the done one and works for
// the new one: in the list, the report and the store, after the next list and a reload, and when the done todo is
// renamed or reopened (#2904).
func TestTodoAutoLinkReleasesTheDoneTodo(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		a, b := e.create("A"), e.create("B")
		e.f.setSessions("macbook", proto.Session{SessionID: "s1", Title: "Agent 1", Status: proto.StatusWorking})
		e.must("PUT", "/api/v1/todos/"+a.ID+"/spaces/macbook/work", "")
		e.must("PUT", "/api/v1/todos/"+b.ID+"/agents/macbook/s1", "")
		e.must("PATCH", "/api/v1/todos/"+b.ID, `{"done":true}`)
		s1 := proto.Session{SessionID: "s1", Title: "Agent 1", SpaceName: "work", Status: proto.StatusWorking}
		e.sessions("macbook", s1)
		stored := func() (active []string, all int) {
			rows, err := database.ListAllTodoAgents(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.SessionID == "s1" {
					all++
					if r.ReleasedAt == nil {
						active = append(active, r.TodoID)
					}
				}
			}
			return active, all
		}
		if database != nil {
			waitFor(t, "s1 stored on A only", func() bool {
				active, all := stored()
				return len(active) == 1 && active[0] == a.ID && all == 2
			})
		}
		check := func(when string) {
			t.Helper()
			if got := e.todoOf("macbook", "s1"); got != a.ID {
				t.Fatalf("%s: the report says s1 works for %q, want A (%q)", when, got, a.ID)
			}
			if ag := agentOf(*e.get(a.ID), "s1"); ag == nil || ag.ReleasedAt != nil || ag.Source != "space" {
				t.Fatalf("%s: s1 on A = %+v", when, ag)
			}
			if ag := agentOf(*e.get(b.ID), "s1"); ag == nil || ag.ReleasedAt == nil {
				t.Fatalf("%s: the done todo lists s1 as a current agent: %+v", when, ag)
			}
			if database != nil {
				if active, all := stored(); len(active) != 1 || active[0] != a.ID || all != 2 {
					t.Fatalf("%s: stored active %v of %d rows", when, active, all)
				}
			}
		}
		check("linked")
		e.sessions("macbook", s1)
		check("the next list")
		reloadTodos(t, e.s)
		check("after a reload")
		e.must("PATCH", "/api/v1/todos/"+b.ID, `{"title":"B renamed"}`)
		check("the done todo renamed")
		e.must("PATCH", "/api/v1/todos/"+b.ID, `{"done":false}`)
		check("the done todo reopened")
	})
}

// An agent removed right after it joined on its own is removed (no 404), and the link being written does not bring it
// back.
func TestTodoImmediateUnlinkRaceDoesNotReviveLink(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "t_race", Title: "Race Todo"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook")
	e.must("PUT", "/api/v1/todos/t_race/spaces/macbook/workspace_race", "")
	fast := proto.Session{SessionID: "s_fast", SpaceName: "workspace_race", Status: proto.StatusWorking}
	e.sessions("macbook", fast)
	e.must("DELETE", "/api/v1/todos/t_race/agents/macbook/s_fast", "")
	time.Sleep(200 * time.Millisecond)
	agents, err := database.ListAllTodoAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) == 1 && (agents[0].ReleasedAt == nil || agents[0].DismissedAt == nil) {
		t.Fatalf("revived by the write: %+v", agents[0])
	}
	if agentOf(*e.get("t_race"), "s_fast") != nil {
		t.Fatal("listed after its removal")
	}
	e.sessions("macbook", fast)
	if got := e.todoOf("macbook", "s_fast"); got != "" {
		t.Fatalf("linked back to %q", got)
	}
}

// Linking a whole workspace keeps an agent linked by hand as it was, and unlinking it keeps that agent.
func TestTodoSpaceGroupLinkKeepsManualLink(t *testing.T) {
	database := todoTestDB(t)
	seedTodos(t, database, db.TodoRow{ID: "td_grp", Title: "Group Todo"})
	e := newTodoEnv(t, database)
	e.f.setSessions("macbook", proto.Session{SessionID: "s_manual", SpaceName: "proj_x", Status: proto.StatusWorking})
	e.must("PUT", "/api/v1/todos/td_grp/agents/macbook/s_manual", "")
	e.must("PUT", "/api/v1/todos/td_grp/spaces/macbook/proj_x", "")
	if a := agentOf(*e.get("td_grp"), "s_manual"); a == nil || a.Source != "manual" {
		t.Fatalf("after the group link: %+v", a)
	}
	e.must("DELETE", "/api/v1/todos/td_grp/spaces/macbook/proj_x", "")
	if a := agentOf(*e.get("td_grp"), "s_manual"); a == nil || a.ReleasedAt != nil {
		t.Fatalf("after the unlink: %+v", a)
	}
}

// ---- the herdr marks ----

// lastTokens is what machine's node was last told; nil (and false) when it was told nothing.
func (f *fakeHub) lastTokens(machine string) (map[string]map[string]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := f.tokens[machine]
	if len(calls) == 0 {
		return nil, false
	}
	return calls[len(calls)-1], true
}

func (f *fakeHub) tokenCalls(machine string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tokens[machine])
}

// The node is told the marks of the herdr panes whose agents work for open todos (by session id); a done todo's go.
func TestTodoTokensSyncToNode(t *testing.T) {
	e := newTodoEnv(t, nil)
	e.f.setSessions("macbook",
		proto.Session{SessionID: "s1", PaneID: "w1:p1", Status: proto.StatusWorking},
		proto.Session{SessionID: "s2", PaneID: "w1:p2", Status: proto.StatusWorking},
		proto.Session{SessionID: "s3", Status: proto.StatusWorking}) // no pane
	todo := e.create("待办标记测试")
	e.must("PUT", "/api/v1/todos/"+todo.ID+"/agents/macbook/s1", "")
	e.must("PUT", "/api/v1/todos/"+todo.ID+"/agents/macbook/s3", "")
	waitFor(t, "the marks", func() bool {
		got, _ := e.f.lastTokens("macbook")
		return got["s1"]["persona_todo"] == "待办标记测试" && got["s1"]["persona_todo_ref"] == todo.ID+"@macbook/s1" && len(got) == 1
	})
	e.must("PATCH", "/api/v1/todos/"+todo.ID, `{"done":true}`)
	waitFor(t, "the marks gone", func() bool { got, ok := e.f.lastTokens("macbook"); return ok && len(got) == 0 })
}

// After a restart the node is told the marks with the first list; the same marks again are not sent, a pane that
// changed under a session neither (they go by session); a new connection of the node sends them again (it may have
// restarted and have none).
func TestTodoTokensAfterARestart(t *testing.T) {
	database := todoTestDB(t)
	now := time.Now().UTC()
	seedTodos(t, database, db.TodoRow{ID: "td_restart", Title: "重启恢复测试"})
	if _, err := database.LinkTodoAgentWithSource(context.Background(), "td_restart", "macbook", "s1", "Agent 1", "/cwd", "claude", "", "manual", "", now); err != nil {
		t.Fatal(err)
	}
	e := newTodoEnv(t, database)
	if err := e.s.ensureTodosLoaded(context.Background()); err != nil {
		t.Fatal(err)
	}
	list := []proto.Session{{SessionID: "s1", PaneID: "w1:p1", Status: proto.StatusWorking}}
	e.sessions("macbook", list...)
	waitFor(t, "the marks", func() bool {
		got, _ := e.f.lastTokens("macbook")
		return got["s1"]["persona_todo_ref"] == "td_restart@macbook/s1"
	})
	e.sessions("macbook", list...)
	e.sessions("macbook", proto.Session{SessionID: "s1", PaneID: "w1:p2", Status: proto.StatusWorking})
	time.Sleep(100 * time.Millisecond)
	if n := e.f.tokenCalls("macbook"); n != 1 {
		t.Fatalf("%d sends, want 1", n)
	}
	// The node reconnects (it may have restarted): its first list on the new connection sends the marks again.
	e.f.setSessions("macbook", list...)
	e.s.SessionsChanged(context.Background(), DeviceSessions{MachineID: "macbook", Sessions: list, Connected: now.Add(time.Minute)})
	waitFor(t, "the marks again", func() bool { return e.f.tokenCalls("macbook") == 2 })
}

// Links changing fast: the node ends up told the latest marks.
func TestTodoSyncTokensLatestWins(t *testing.T) {
	e := newTodoEnv(t, nil)
	e.f.setSessions("macbook",
		proto.Session{SessionID: "s1", PaneID: "w1:p1", Status: proto.StatusWorking},
		proto.Session{SessionID: "s2", PaneID: "w1:p2", Status: proto.StatusWorking})
	t1, t2 := e.create("First"), e.create("Second")
	e.must("PUT", "/api/v1/todos/"+t1.ID+"/agents/macbook/s1", "")
	e.must("PUT", "/api/v1/todos/"+t2.ID+"/agents/macbook/s2", "")
	e.must("DELETE", "/api/v1/todos/"+t1.ID+"/agents/macbook/s1", "")
	waitFor(t, "the latest marks", func() bool {
		got, _ := e.f.lastTokens("macbook")
		_, s1 := got["s1"]
		return !s1 && got["s2"]["persona_todo"] == "Second"
	})
}

// newTodo makes a todo through the API, its id.
func (e *todoEnv) newTodo(title string) string {
	var created struct{ Todo Todo }
	_ = json.Unmarshal(e.must("POST", "/api/v1/todos", `{"title":"`+title+`"}`).Body.Bytes(), &created)
	return created.Todo.ID
}

// A botmux topic dispatched (`botmux dispatch`) by an agent of an open todo joins it, on another machine too and with
// its parent parked; a dispatcher on no todo brings nothing.
func TestTodoBotmuxDispatchFollows(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		td := e.newTodo("派活")
		e.sessions("mac", proto.Session{SessionID: "p1", Status: proto.StatusWorking, Chat: "oc_a", Topic: "om_p1",
			Dispatched: []string{"om_c1", "om_c2"}},
			proto.Session{SessionID: "p2", Status: proto.StatusWorking, Dispatched: []string{"om_c3"}})
		e.must("PUT", "/api/v1/todos/"+td+"/agents/mac/p1", "")
		// The parent parks; its children start, one on another machine.
		e.sessions("mac", proto.Session{SessionID: "p1", Status: proto.StatusOffline, Chat: "oc_a", Topic: "om_p1",
			Dispatched: []string{"om_c1", "om_c2"}},
			proto.Session{SessionID: "p2", Status: proto.StatusWorking, Dispatched: []string{"om_c3"}},
			proto.Session{SessionID: "c1", Status: proto.StatusWorking, Topic: "om_c1"})
		e.sessions("n37", proto.Session{SessionID: "c2", Status: proto.StatusWorking, Topic: "om_c2"},
			proto.Session{SessionID: "c3", Status: proto.StatusWorking, Topic: "om_c3"})
		if e.todoOf("mac", "c1") != td || e.todoOf("n37", "c2") != td || e.todoOf("n37", "c3") != "" {
			t.Fatalf("report = %+v", e.s.todoReport(context.Background()).Links)
		}
		if a := agentOf(*e.get(td), "c2"); a.Source != "spawned" || a.Via != "mac/p1" {
			t.Fatalf("c2 = %+v", a)
		}
	})
}

// A botmux agent with no todo joins the one most of its chat's agents work for, when that one has twice the next at
// least; a tie or a close second, another chat bring nothing.
func TestTodoBotmuxChatMajority(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, database *db.DB) {
		big, small, other := e.newTodo("三循环"), e.newTodo("工具链"), e.newTodo("别的群")
		s := func(id, chat string) proto.Session {
			return proto.Session{SessionID: id, Status: proto.StatusWorking, Chat: chat, Topic: "om_" + id}
		}
		list := []proto.Session{s("a1", "oc_x"), s("a2", "oc_x"), s("b1", "oc_x"), s("o1", "oc_y"), s("o2", "oc_y")}
		e.sessions("mac", list...)
		for id, td := range map[string]string{"a1": big, "a2": big, "b1": small, "o1": other, "o2": small} {
			e.must("PUT", "/api/v1/todos/"+td+"/agents/mac/"+id, "")
		}
		// oc_x: 2 against 1, twice; oc_y: 1 against 1, a tie.
		e.sessions("mac", append(list, s("new_x", "oc_x"), s("new_y", "oc_y"), s("new_z", "oc_z"))...)
		if e.todoOf("mac", "new_x") != big || e.todoOf("mac", "new_y") != "" || e.todoOf("mac", "new_z") != "" {
			t.Fatalf("report = %+v", e.s.todoReport(context.Background()).Links)
		}
		if a := agentOf(*e.get(big), "new_x"); a.Source != "chat" || a.Via != "oc_x" {
			t.Fatalf("new_x = %+v", a)
		}
		// 3 against 2: not twice.
		// b2 joins big on its own (3 against 1); the user puts it on small while that link may still be on its way to the
		// store (#2704): the user's link is kept, and memory ends up as the store has it.
		e.sessions("n37", s("b2", "oc_x"))
		e.must("PUT", "/api/v1/todos/"+small+"/agents/n37/b2", "")
		if database != nil {
			onBig := func() (memory, stored int) {
				if a := agentOf(*e.get(big), "b2"); a != nil {
					memory = 1
				}
				rows, err := database.ListAllTodoAgents(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range rows {
					if r.TodoID == big && r.MachineID == "n37" && r.SessionID == "b2" {
						stored++
					}
				}
				return
			}
			waitUntil(t, func() bool { m, st := onBig(); return m == st })
			rows, _ := database.ListAllTodoAgents(context.Background())
			for _, r := range rows {
				if r.MachineID == "n37" && r.SessionID == "b2" && r.ReleasedAt == nil && r.TodoID != small {
					t.Fatalf("stored active link %+v", r)
				}
			}
		}
		if e.todoOf("n37", "b2") != small {
			t.Fatalf("b2 works for %q, want the user's %q", e.todoOf("n37", "b2"), small)
		}
		e.sessions("n37", s("b2", "oc_x"), s("late", "oc_x"))
		if e.todoOf("n37", "late") != "" {
			t.Fatal("joined a todo with a close second")
		}
	})
}

func TestChatMajority(t *testing.T) {
	k := func(s string) [2]string { return [2]string{"m", s} }
	for _, c := range []struct {
		agents map[[2]string]string
		want   string
	}{
		{nil, ""},
		{map[[2]string]string{k("a"): "t1"}, "t1"},
		{map[[2]string]string{k("a"): "t1", k("b"): "t2"}, ""},
		{map[[2]string]string{k("a"): "t1", k("b"): "t1", k("c"): "t2"}, "t1"},
		{map[[2]string]string{k("a"): "t1", k("b"): "t1", k("c"): "t1", k("d"): "t2", k("e"): "t2"}, ""},
		{map[[2]string]string{k("self"): "t2", k("a"): "t1"}, "t1"}, // self does not count
	} {
		if got := chatMajority(c.agents, k("self")); got != c.want {
			t.Errorf("chatMajority(%v) = %q, want %q", c.agents, got, c.want)
		}
	}
}

// The chat rule does not feed itself: an agent it linked does not vote, so one agent on a todo brings the chat's
// others once, not their followers' followers. A machine not heard from lately votes no more; an agent removed by hand
// is not taken back; a done todo takes nobody.
func TestTodoBotmuxChatLimits(t *testing.T) {
	todoStores(t, func(t *testing.T, e *todoEnv, _ *db.DB) {
		td, done := e.newTodo("一个"), e.newTodo("做完的")
		s := func(id, chat string) proto.Session {
			return proto.Session{SessionID: id, Status: proto.StatusWorking, Chat: chat, Topic: "om_" + id}
		}
		e.sessions("mac", s("a1", "oc_x"), s("d1", "oc_d"))
		e.must("PUT", "/api/v1/todos/"+td+"/agents/mac/a1", "")
		e.must("PUT", "/api/v1/todos/"+done+"/agents/mac/d1", "")
		e.must("PATCH", "/api/v1/todos/"+done, `{"done":true}`)
		e.sessions("mac", s("a1", "oc_x"), s("d1", "oc_d"), s("x1", "oc_x"), s("d2", "oc_d"))
		if e.todoOf("mac", "x1") != td || e.todoOf("mac", "d2") != "" {
			t.Fatalf("report = %+v", e.s.todoReport(context.Background()).Links)
		}
		// a1 leaves the todo: x1, linked by the chat, votes for nobody.
		e.must("DELETE", "/api/v1/todos/"+td+"/agents/mac/a1", "")
		e.sessions("mac", s("a1", "oc_x"), s("x1", "oc_x"), s("x2", "oc_x"))
		if e.todoOf("mac", "x2") != "" {
			t.Fatal("an agent the chat linked brought another")
		}
		// Removed by hand, x1 is not taken back by its chat.
		e.must("PUT", "/api/v1/todos/"+td+"/agents/mac/a1", "")
		e.must("DELETE", "/api/v1/todos/"+td+"/agents/mac/x1", "")
		e.sessions("mac", s("a1", "oc_x"), s("x1", "oc_x"))
		if e.todoOf("mac", "x1") != "" {
			t.Fatal("removed by hand, taken back")
		}
		// A machine whose last list is old does not vote.
		e.s.todos.mu.Lock()
		seen := e.s.todos.botmux["mac"]
		seen.at = seen.at.Add(-2 * botmuxChatStale)
		e.s.todos.botmux["mac"] = seen
		e.s.todos.mu.Unlock()
		e.sessions("n37", s("far", "oc_x"))
		if e.todoOf("n37", "far") != "" {
			t.Fatal("a stale machine voted")
		}
	})
}
