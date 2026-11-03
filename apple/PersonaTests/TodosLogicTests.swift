import Foundation
import Testing
@testable import Jarvis

@Suite(.serialized)
@MainActor
struct TodosLogicTests {
    private func session(_ id: String, state: AgentState, updated: TimeInterval = 0) -> Session {
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(id)","cwd":"/tmp","status":"idle","state":"\(state.rawValue)",
         "updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    @Test func todoStatusPriorityOrder() {
        // 1. Done (已完成): done_at != nil
        var tDone = TodoItem(id: "1", title: "Done", doneAt: Date(), createdAt: Date(), updatedAt: Date())
        #expect(TodosLogic.todoStatus(tDone) == .done)

        // 6. Not started (未开始): no agent is attached
        let tNotStarted = TodoItem(id: "2", title: "NotStarted", doneAt: nil, createdAt: Date(), updatedAt: Date(), agents: [])
        #expect(TodosLogic.todoStatus(tNotStarted) == .notStarted)

        // 2. Needs you (等你): an agent needs the user
        let sNeedsYou = session("s1", state: .needsYou)
        let sWorking = session("s2", state: .working)
        let sUnread = session("s3", state: .unread)
        let sIdle = session("s4", state: .idle)
        let lookup = TodosLogic.makeSessionLookup([sNeedsYou, sWorking, sUnread, sIdle])

        let a1 = TodoItem.Agent(machineID: "m", sessionID: "s1")
        let a2 = TodoItem.Agent(machineID: "m", sessionID: "s2")
        let a3 = TodoItem.Agent(machineID: "m", sessionID: "s3")
        let a4 = TodoItem.Agent(machineID: "m", sessionID: "s4")

        let tNeedsYou = TodoItem(id: "3", title: "NeedsYou", agents: [a1, a2, a3, a4])
        #expect(TodosLogic.todoStatus(tNeedsYou, lookup: lookup) == .needsYou)

        // 3. Unread (待查看): an agent finished and nobody has looked at it
        let tUnread = TodoItem(id: "4", title: "Unread", agents: [a2, a3, a4])
        #expect(TodosLogic.todoStatus(tUnread, lookup: lookup) == .unread)

        // 4. Working (进行中): an agent is working
        let tWorking = TodoItem(id: "5", title: "Working", agents: [a2, a4])
        #expect(TodosLogic.todoStatus(tWorking, lookup: lookup) == .working)

        // 5. Follow-up (待跟进): every agent is idle
        let tFollowUp = TodoItem(id: "6", title: "FollowUp", agents: [a4])
        #expect(TodosLogic.todoStatus(tFollowUp, lookup: lookup) == .followup)

        // A failed start counts as idle
        let aFailed = TodoItem.Agent(machineID: "m", sessionID: "s_failed", startError: "failed to start")
        let tFailed = TodoItem(id: "7", title: "Failed", agents: [aFailed])
        #expect(TodosLogic.todoStatus(tFailed, lookup: lookup) == .followup)
    }

    @Test func sortTodosOrder() {
        let now = Date()
        let tNeedsYou = TodoItem(id: "needs", title: "Needs", doneAt: nil, createdAt: now.addingTimeInterval(-100), agents: [TodoItem.Agent(machineID: "m", sessionID: "s_needs")])
        let tWorkingNew = TodoItem(id: "work_new", title: "WorkNew", doneAt: nil, createdAt: now.addingTimeInterval(-10), agents: [TodoItem.Agent(machineID: "m", sessionID: "s_work")])
        let tWorkingOld = TodoItem(id: "work_old", title: "WorkOld", doneAt: nil, createdAt: now.addingTimeInterval(-50), agents: [TodoItem.Agent(machineID: "m", sessionID: "s_work")])
        let tDone = TodoItem(id: "done", title: "Done", doneAt: now, createdAt: now.addingTimeInterval(-5), agents: [])

        let sNeeds = session("s_needs", state: .needsYou)
        let sWork = session("s_work", state: .working)
        let lookup = TodosLogic.makeSessionLookup([sNeeds, sWork])

        let sorted = TodosLogic.sortTodos([tDone, tWorkingOld, tNeedsYou, tWorkingNew], lookup: lookup)
        #expect(sorted.map(\.id) == ["needs", "work_new", "work_old", "done"])
    }

    @Test func todoAgentGroupsAndUnassigned() {
        let s1 = session("s1", state: .working)
        let s2 = session("s2", state: .idle)
        let sUnassigned = session("s_unassigned", state: .working)

        let t1 = TodoItem(id: "t1", title: "Todo 1", doneAt: nil, createdAt: Date(), agents: [TodoItem.Agent(machineID: "m", sessionID: "s1")])
        var s2WithTodo = s2
        s2WithTodo.todo = SessionTodoRef(id: "t1", title: "Todo 1")

        let groups = TodosLogic.buildTodoAgentGroups(
            sessions: [s1, s2WithTodo, sUnassigned],
            todos: [t1],
            isMuted: { _ in false }
        )

        #expect(groups.count == 2)
        #expect(groups[0].key == "todo:t1")
        #expect(groups[0].name == "Todo 1")
        #expect(groups[0].sessions.map(\.sessionID) == ["s1", "s2"])

        #expect(groups[1].key == "todo:none")
        #expect(groups[1].name == "没有待办")
        #expect(groups[1].sessions.map(\.sessionID) == ["s_unassigned"])
    }

    @Test func formatTodoContextOutput() {
        let xml = TodosLogic.formatTodoContext(title: "测试 <&> \"待办\"", notes: "说明内容")
        #expect(xml == "<jarvis-context kind=\"context\" title=\"待办 · 测试 &lt;&amp;&gt; &quot;待办&quot;\" url=\"\">说明内容</jarvis-context>")
    }

    /// An older hub answers GET /api/v1/todos with 404: the store's refresh hides the entrance and shows no error.
    @Test func refreshHidesTheEntranceOnAnOldHub() async {
        let hub = FakeHub()  // no todos route, so the fake hub answers 404
        let store = TodoStore.shared
        store.forget()
        await store.refresh(api: hub.client)
        #expect(store.isSupported == false)
        #expect(store.error == nil)
    }

    /// A write that fails (the fake hub has no PATCH route, so it answers 404) puts the list back as it was: the optimistic
    /// "done" is undone, not left showing.
    @Test func toggleDoneRollsBackWhenTheWriteFails() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/todos", json: #"{"todos":[{"id":"t1","title":"Rollback"}]}"#)
        let store = TodoStore.shared
        await store.refresh(api: hub.client)
        let before = store.todos
        let todo = try #require(store.todo(id: "t1"))

        store.toggleDone(todo, api: hub.client)
        #expect(store.todo(id: "t1")?.isDone == true)  // the optimistic change shows at once
        var waited = 0
        while store.todo(id: "t1")?.isDone == true, waited < 200 {
            try await Task.sleep(for: .milliseconds(10))
            waited += 1
        }
        #expect(store.todos == before)
    }

    /// Only a 404 from GET /api/v1/todos means an old hub (the sidebar entrance hides); other failures keep it shown.
    @Test func only404MarksAnOldHub() {
        #expect(TodosLogic.isOldHub(APIError(status: 404, message: "no route")))
        #expect(!TodosLogic.isOldHub(APIError(status: 500, message: "boom")))
        #expect(!TodosLogic.isOldHub(URLError(.notConnectedToInternet)))
    }

    /// The detail page reads a todo's text from the store when it shows that todo: another todo's text never lands in
    /// the fields (the Mac list picks a different todo on the same page).
    @Test func editorShowsTheTextOfTheTodoItIsGiven() {
        let a = TodoItem(id: "a", title: "Todo A", notes: "Notes A")
        let b = TodoItem(id: "b", title: "Todo B", notes: nil)
        var text = TodoEditorText(a)
        text.reseed(from: b, editingTitle: false, editingNotes: false)
        #expect(text == TodoEditorText(title: "Todo B", notes: ""))
    }

    /// A field the user is typing in keeps their text; the stored text fills the other field.
    @Test func editorKeepsTheFieldBeingEdited() {
        let a = TodoItem(id: "a", title: "Todo A", notes: "Notes A")
        let stored = TodoItem(id: "a", title: "Renamed elsewhere", notes: "Notes elsewhere")
        var text = TodoEditorText(a)
        text.title = "Still typing"
        text.reseed(from: stored, editingTitle: true, editingNotes: false)
        #expect(text == TodoEditorText(title: "Still typing", notes: "Notes elsewhere"))
    }

    /// 「按待办」 needs the hub's todos: an old hub does not offer it, and a stored choice of it falls back to 「按项目」.
    @Test func todoGroupingNeedsTheHubsTodos() {
        #expect(!DevGrouping.offered(supportsTodos: false).contains(.todo))
        #expect(DevGrouping.offered(supportsTodos: true).contains(.todo))
        #expect(DevGrouping.resolved(.todo, supportsTodos: false) == .space)
        #expect(DevGrouping.resolved(.todo, supportsTodos: true) == .todo)
        #expect(DevGrouping.resolved(.state, supportsTodos: false) == .state)
    }

    /// A session takes a todo's actions only on the selected hub: on another hub group the request would go to the
    /// wrong hub and find no todo.
    /// The Agents of the todos are read one by one on the hub's group; an Agent that is not known is skipped.
    @Test func linkedSessionsAreReadPerAgentOnTheHubGroup() {
        let known = session("s1", state: .working)
        let todo = TodoItem(id: "t", title: "T", agents: [TodoItem.Agent(machineID: "m", sessionID: "s1"),
                                                      TodoItem.Agent(machineID: "m", sessionID: "gone")])
        var asked: [SessionKey] = []
        let found = TodosLogic.linkedSessions(of: [todo], hubGroup: "work") { key in
            asked.append(key)
            return key.sessionID == "s1" ? known : nil
        }
        #expect(found.map(\.sessionID) == ["s1"])
        #expect(asked.count == 2)
        #expect(asked.allSatisfy { $0.group == "work" })
    }

    @Test func todoActionsStayOnTheSelectedHub() {
        #expect(TodosLogic.isOnTodosHub(sessionGroup: "", hubGroup: nil))
        #expect(TodosLogic.isOnTodosHub(sessionGroup: "work", hubGroup: "work"))
        #expect(!TodosLogic.isOnTodosHub(sessionGroup: "home", hubGroup: "work"))
        #expect(!TodosLogic.isOnTodosHub(sessionGroup: "home", hubGroup: nil))
    }

    @Test func todoPresentationPreservesLabelsOrderAndColors() {
        let states: [TaskState] = [.needsYou, .unread, .working, .followup, .notStarted, .done]
        #expect(states.map(\.todoLabel) == ["等你", "待查看", "进行中", "待跟进", "未开始", "已完成"])
        #expect(states.map(\.todoRank) == [0, 1, 2, 3, 4, 5])
        #expect(TaskState.allCases.sorted { $0.todoRank < $1.todoRank } == states)
        let tones = [StatusPalette.needsYou, StatusPalette.unread, StatusPalette.working,
                     StatusPalette.idle, StatusPalette.idle, StatusPalette.idle]
        for (state, tone) in zip(states, tones) {
            #expect(state.todoTone.dot == tone.dot)
            #expect(state.todoTone.text == tone.text)
            #expect(state.todoTone.tint == tone.tint)
        }
    }

    @Test func missingActiveAgentNeedsFollowup() {
        let todo = TodoItem(id: "missing", title: "Missing", agents: [.init(machineID: "m", sessionID: "gone")])
        #expect(TodosLogic.todoStatus(todo, lookup: TodosLogic.makeSessionLookup([])) == .followup)
    }

    @Test func failedAgentCountsAsIdleEvenWithAWorkingSession() {
        let todo = TodoItem(id: "failed", title: "Failed", agents: [
            .init(machineID: "m", sessionID: "s", startError: "failed to start")
        ])
        let lookup = TodosLogic.makeSessionLookup([session("s", state: .working)])
        #expect(TodosLogic.todoStatus(todo, lookup: lookup) == .followup)
    }

    @Test func releasedAgentsDoNotAffectTheState() {
        let todo = TodoItem(id: "released", title: "Released", agents: [
            .init(machineID: "m", sessionID: "s", releasedAt: Date())
        ])
        let lookup = TodosLogic.makeSessionLookup([session("s", state: .needsYou)])
        #expect(TodosLogic.todoStatus(todo, lookup: lookup) == .notStarted)
    }

    @Test func startingAgentStillCountsAsWorking() {
        var starting = session("s", state: .idle)
        starting.starting = true
        let todo = TodoItem(id: "starting", title: "Starting", agents: [.init(machineID: "m", sessionID: "s")])
        #expect(TodosLogic.todoStatus(todo, lookup: TodosLogic.makeSessionLookup([starting])) == .working)
    }

    @Test func legacyTodoItemsCacheDecodes() throws {
        // The old Codable model omitted nil agents and optional fields, and had no origin or plan_id.
        let data = Data(#"""
        [{"id":"empty","title":"Empty"},
         {"id":"null","title":"Null","agents":null},
         {"id":"full","title":"Full","notes":"Notes","done_at":"2026-01-02T03:04:05Z",
          "created_at":"2026-01-01T03:04:05Z","updated_at":"2026-01-02T03:04:05Z",
          "agents":[{"machine_id":"m","session_id":"s","title":"Agent","cwd":"/tmp",
                     "agent":"codex","linked_at":"2026-01-01T03:04:05Z",
                     "released_at":"2026-01-02T03:04:05Z","start_error":"failed"}]}]
        """#.utf8)
        let todos = try JSON.decoder.decode([TodoItem].self, from: data)
        #expect(todos.count == 3)
        #expect(todos[0].agents.isEmpty)
        #expect(todos[1].agents.isEmpty)
        let todo = todos[2]
        #expect(todo.id == "full" && todo.title == "Full" && todo.notes == "Notes")
        #expect(todo.isDone && todo.createdAt != nil && todo.updatedAt != nil)
        #expect(todo.origin == nil && todo.planID == nil)
        let agent = try #require(todo.agents.first)
        #expect(agent.machineID == "m" && agent.sessionID == "s" && agent.title == "Agent")
        #expect(agent.cwd == "/tmp" && agent.agent == "codex" && agent.startError == "failed")
        #expect(agent.linkedAt != nil && agent.releasedAt != nil)
    }

}
