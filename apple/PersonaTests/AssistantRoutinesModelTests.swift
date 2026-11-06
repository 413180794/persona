import Foundation
import Testing
@testable import Persona

/// Scheduled Tasks' view model against a FakeHub: one list of tasks (every day, every N minutes, once), the optimistic
/// save and its rollback, delete, Run Now.
@MainActor
struct AssistantRoutinesModelTests {
    private static let tasks = #"""
        [{"id":"r1","title":"早报","prompt":"写早报","repeat":"daily","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true},
         {"id":"p1","title":"巡检","prompt":"看看 Agent","repeat":"interval","every_minutes":30,"at":"2026-10-31T12:30:00Z",
          "next_run":"2026-10-31T12:30:00Z","enabled":true,"permission_mode":"acceptEdits","launcher":"","created_by":"assistant"},
         {"id":"f2","prompt":"b","repeat":"once","at":"2026-10-31T12:00:00Z","enabled":true,"machine_id":"m"},
         {"id":"f1","title":"a","prompt":"a","repeat":"once","at":"2026-10-31T10:00:00Z","enabled":true,"machine_id":"m","session_id":"s1"}]
        """#

    private final class Sink {
        var errors: [String] = []
        var toasts: [String] = []
        var opened: [SessionKey] = []
    }

    private func make(_ hub: FakeHub, _ sink: Sink) -> AssistantRoutinesModel {
        AssistantRoutinesModel(deps: .init(api: { hub.client }, report: { sink.errors.append($0.localizedDescription) },
                                           toast: { sink.toasts.append($0.message) }, open: { sink.opened.append($0) }))
    }

    private func hub() -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/tasks", json: #"{"tasks":\#(Self.tasks)}"#)
        hub.on("GET", "/api/v1/model-config", json: #"{"purposes":[],"providers":[]}"#)
        hub.on("GET", "/api/v1/assistant/runs", json: #"{"runs":[]}"#)
        return hub
    }

    @Test func repeatingAndOneOffsApartOneOffsSoonestFirst() async {
        let page = make(hub(), Sink())
        await page.load()
        #expect(page.loaded && page.error == nil)
        #expect(page.repeating.map(\.id) == ["r1", "p1"])
        #expect(page.oneOff.map(\.id) == ["f1", "f2"])
        let p = page.repeating[1]
        #expect(p.repeats == .interval && p.everyMinutes == 30 && p.permissionMode == "acceptEdits")
        #expect(p.launcher == nil, "\"\" is the default")
        #expect(page.oneOff[0].sessionID == "s1" && page.oneOff[1].sessionID == nil)
    }

    @Test func aFailedLoadKeepsTheErrorAndStillFinishes() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/tasks") { _ in throw APIError(status: 500, message: "hub down") }
        let page = make(hub, Sink())
        await page.load()
        #expect(page.loaded)
        #expect(page.error != nil)
        #expect(page.tasks.isEmpty)
    }

    /// Only what the user edits goes up; the defaults as "", so the hub can tell "back to default" from "unchanged".
    @Test func savingSendsTheEditableFieldsAndKeepsWhatTheHubReturns() async throws {
        let hub = hub()
        hub.on("PUT", "/api/v1/assistant/tasks/p1") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as! [String: Any]
            #expect(body["permission_mode"] as? String == "bypassPermissions")
            #expect(body["launcher"] == nil && body["effort"] == nil, "model fields are managed separately")
            #expect(body["every_minutes"] as? Int == 60)
            #expect(body["last_run"] == nil && body["next_run"] == nil && body["created_by"] == nil)
            return Data(#"{"task":{"id":"p1","title":"巡检","prompt":"看看 Agent","repeat":"interval","every_minutes":60,"enabled":true,"permission_mode":"bypassPermissions","next_run":"2026-10-31T13:30:00Z"}}"#.utf8)
        }
        let page = make(hub, Sink())
        await page.load()
        var p = page.repeating[1]
        p.permissionMode = "bypassPermissions"
        p.everyMinutes = 60
        let saved = await page.save(p)
        #expect(saved?.everyMinutes == 60 && saved?.nextRun != nil)
        #expect(page.repeating[1].permissionMode == "bypassPermissions")
        #expect(hub.sent("PUT", "/api/v1/assistant/tasks/p1").count == 1)
    }

    @Test func legacyHubTimeEditsRetainItsExplicitModelFields() async {
        let hub = hub()
        hub.on("GET", "/api/v1/model-config") { _ in throw APIError(status: 404, message: "old hub") }
        hub.on("PUT", "/api/v1/assistant/tasks/p1") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as! [String: Any]
            #expect(body["launcher"] as? String == "strong")
            #expect(body["effort"] as? String == "high")
            return Data(#"{"task":{"id":"p1","title":"巡检","prompt":"看看 Agent","repeat":"interval","every_minutes":60,"enabled":true,"launcher":"strong","effort":"high"}}"#.utf8)
        }
        let page = make(hub, Sink())
        await page.load()
        var task = page.repeating[1]
        task.launcher = "strong"
        task.effort = "high"
        #expect(await page.save(task) != nil)
    }

    @Test func aNewTaskIsPostedAndAdded() async {
        let hub = hub()
        hub.on("POST", "/api/v1/assistant/tasks", json: #"{"task":{"id":"task_1","title":"喝水","prompt":"提醒我喝水","repeat":"interval","every_minutes":45,"enabled":true}}"#)
        let page = make(hub, Sink())
        await page.load()
        let saved = await page.save(APIClient.ScheduledTask(title: "喝水", prompt: "提醒我喝水", repeats: .interval, everyMinutes: 45))
        #expect(saved?.id == "task_1")
        #expect(page.repeating.map(\.id) == ["r1", "p1", "task_1"])
    }

    @Test func aFailedSaveReportsAndGoesBackToTheHubsList() async {
        let hub = hub()
        hub.on("PUT", "/api/v1/assistant/tasks/r1") { _ in throw APIError(status: 500, message: "nope") }
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        var off = page.repeating[0]
        off.enabled = false
        #expect(await page.save(off) == nil)
        #expect(sink.errors.count == 1)
        #expect(page.repeating.first?.enabled == true, "rolled back to what the hub has")
    }

    @Test func deletingTellsWhatWasDeleted() async {
        let hub = hub()
        hub.on("DELETE", "/api/v1/assistant/tasks/f2", json: #"{"ok":true}"#)
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        await page.delete(page.oneOff[1])
        #expect(page.oneOff.map(\.id) == ["f1"])
        #expect(sink.toasts == ["已删除「b」"], "a follow-up without a title goes by its prompt")
    }

    @Test func runningAOneOffOpensItsConversationAndDropsIt() async {
        let hub = hub()
        hub.on("POST", "/api/v1/assistant/tasks/f1/run", json: #"{"session_id":"s9","machine_id":"m"}"#)
        hub.on("POST", "/api/v1/assistant/tasks/p1/run", json: #"{"session_id":"s8","machine_id":"m"}"#)
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        await page.run(page.oneOff[0])
        await page.run(page.repeating[1])
        #expect(sink.opened == [SessionKey(machineID: "m", sessionID: "s9"), SessionKey(machineID: "m", sessionID: "s8")])
        #expect(page.oneOff.map(\.id) == ["f2"])
        #expect(page.repeating.map(\.id) == ["r1", "p1"], "a repeating one keeps its schedule")
    }
}

/// Memory against a FakeHub.
@MainActor
struct AssistantPagesTests {
    private func deps(_ hub: FakeHub, errors: @escaping (Error) -> Void = { _ in }) -> AssistantDeps {
        AssistantDeps(api: { hub.client }, report: errors, toast: { _ in }, open: { _ in })
    }

    @Test func deletingAMemoryReloadsTheList() async {
        let hub = FakeHub()
        var deleted = false
        hub.on("GET", "/api/v1/assistant/memory") { _ in
            Data((deleted ? #"{"files":[]}"# : #"{"files":[{"name":"a.md","content":"x"}]}"#).utf8)
        }
        hub.on("DELETE", "/api/v1/assistant/memory/a.md") { _ in deleted = true; return Data("{}".utf8) }
        let page = AssistantMemoryModel(deps: deps(hub))
        await page.load()
        #expect(page.files.map(\.name) == ["a.md"])
        await page.delete("a.md")
        #expect(page.files.isEmpty)
    }
}
