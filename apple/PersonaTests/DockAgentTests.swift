import Foundation
import Testing
@testable import Persona

/// The main conversation's dock shows the agents opened by hand after its tasks (#1113): only the main conversation,
/// only the ones waiting or working, never one a task already stands for.
@MainActor
struct DockAgentTests {
    private func session(_ id: String, state: String, assistant: Bool = false, todo: String? = nil, since: String? = nil,
                         title: String? = nil, group: String? = nil) throws -> Session {
        var o: [String: Any] = ["machine_id": "box", "session_id": id, "title": title ?? "agent \(id)", "cwd": "/code/\(id)",
                                "status": "idle", "state": state, "started_by_persona": false, "assistant": assistant,
                                "pending_interactions": 0]
        if let todo { o["todo"] = ["id": todo, "title": "t"] }
        if let since { o["state_since"] = since }
        if let group { o["app_hub_group"] = group }
        return try JSON.decoder.decode(Session.self, from: JSONSerialization.data(withJSONObject: o))
    }

    private func key(_ id: String, group: String = "") -> SessionKey { SessionKey(group: group, machineID: "box", sessionID: id) }

    @Test func goingIsWaitingThenWorkingNewestFirst() throws {
        let going = DockAgent.going([
            try session("idle", state: "idle"),
            try session("old", state: "working", since: "2026-11-04T08:00:00Z"),
            try session("unread", state: "unread"),
            try session("asks", state: "needs_you", since: "2026-11-04T07:00:00Z"),
            try session("new", state: "working", since: "2026-11-04T09:00:00Z"),
            try session("offline", state: "offline"),
            try session("persona", state: "working", assistant: true),
        ])
        #expect(going.map(\.key.sessionID) == ["asks", "new", "old"])
        #expect(going.first?.isWaiting == true && going.first?.title == "agent asks")
    }

    @Test func onlyTheMainConversationShowsThem() throws {
        let going = DockAgent.going([try session("a", state: "working"), try session("b", state: "needs_you")])
        let main = DockAgent.shown(going, conversation: "main", main: "main", group: "", taskAgents: [], manualTodos: [])
        #expect(main.map(\.key.sessionID) == ["b", "a"])
        #expect(DockAgent.shown(going, conversation: "side", main: "main", group: "", taskAgents: [], manualTodos: []).isEmpty)
        #expect(DockAgent.shown(going, conversation: "main", main: nil, group: "", taskAgents: [], manualTodos: []).isEmpty)
        #expect(DockAgent.shown(going, conversation: "", main: "", group: "", taskAgents: [], manualTodos: []).isEmpty)
    }

    @Test func aTasksAgentIsNotShownTwice() throws {
        let going = DockAgent.going([
            try session("task", state: "working"),                      // a task's agent, by the todo list
            try session("tagged", state: "working", todo: "todo_task"), // tagged with a task the list has not got yet
            try session("hand", state: "working", todo: "todo_hand"),   // works for a todo made by hand: still manual
            try session("free", state: "needs_you"),                    // no todo at all
            try session("other", state: "working", todo: "todo_hand", group: "g2"), // another hub's todo of the same id
        ])
        let shown = DockAgent.shown(going, conversation: "main", main: "main", group: "",
                                    taskAgents: [key("task")], manualTodos: ["todo_hand"])
        #expect(shown.map(\.key.sessionID) == ["free", "hand"])
        #expect(Set(shown.map(\.key)).count == shown.count)
    }
}
