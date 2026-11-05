import Foundation
import Testing
@testable import Jarvis

/// 全屏查看 in the assistant's agent sheet: the sheet goes down (both of its sources cleared, so it does not rise
/// again) and the agent is handed to the assistant page to push.
@MainActor
struct TaskSheetExpandTests {
    private func store() -> TasksStore {
        TasksStore(todoStore: TodoStore(cachedTodos: [], cacheWriter: nil, hubListChanged: {}))
    }

    @Test func expandingAnAgentClosesItsSheetAndAsksForThePush() {
        let tasks = store()
        let key = SessionKey(machineID: "box", sessionID: "agent-1")
        tasks.detailAgent = key
        tasks.expand(key)
        #expect(tasks.detailAgent == nil)
        #expect(tasks.detailTask == nil)
        #expect(tasks.fullAgent == key)
    }

    @Test func expandingFromATaskClosesTheTaskSheet() {
        let tasks = store()
        let key = SessionKey(machineID: "box", sessionID: "agent-2")
        tasks.detailTask = "todo-1"
        tasks.expand(key)
        #expect(tasks.detailTask == nil)
        #expect(tasks.fullAgent == key)
    }

    @Test func forgettingDropsAPendingPush() {
        let tasks = store()
        tasks.expand(SessionKey(machineID: "box", sessionID: "agent-3"))
        tasks.forget()
        #expect(tasks.fullAgent == nil)
    }
}
