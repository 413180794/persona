import Foundation
import Testing
@testable import Jarvis

/// 定时任务's view model against a FakeHub: loading, the optimistic save and its rollback, delete, running and
/// cancelling a follow-up.
@MainActor
struct AssistantRoutinesModelTests {
    private static let routine = #"{"id":"r1","title":"早报","prompt":"写早报","time":"09:00","weekdays":[1,2,3,4,5],"enabled":true}"#

    private final class Sink {
        var errors: [String] = []
        var toasts: [String] = []
        var opened: [SessionKey] = []
    }

    private func make(_ hub: FakeHub, _ sink: Sink) -> AssistantRoutinesModel {
        AssistantRoutinesModel(deps: .init(api: { hub.client }, report: { sink.errors.append($0.localizedDescription) },
                                           toast: { sink.toasts.append($0.message) }, open: { sink.opened.append($0) }))
    }

    private func hub(routines: String = "[\(routine)]") -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/routines", json: #"{"routines":\#(routines)}"#)
        hub.on("GET", "/api/v1/assistant/followups", json: #"""
            {"followups":[{"id":"f2","at":"2026-10-31T12:00:00Z","prompt":"b","machine_id":"m"},
                          {"id":"f1","at":"2026-10-31T10:00:00Z","prompt":"a","machine_id":"m"}]}
            """#)
        hub.on("GET", "/api/v1/assistant/runs", json: #"{"runs":[]}"#)
        return hub
    }

    @Test func loadsAndSortsFollowupsByTime() async {
        let page = make(hub(), Sink())
        await page.load()
        #expect(page.loaded)
        #expect(page.routines.map(\.id) == ["r1"])
        #expect(page.followups.map(\.id) == ["f1", "f2"])
        #expect(page.error == nil)
    }

    @Test func aFailedLoadKeepsTheErrorAndStillFinishes() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/routines") { _ in throw APIError(status: 500, message: "hub down") }
        let page = make(hub, Sink())
        await page.load()
        #expect(page.loaded)
        #expect(page.error != nil)
        #expect(page.routines.isEmpty)
    }

    @Test func savingSendsTheWholeListAndKeepsWhatTheHubReturns() async throws {
        let hub = hub()
        hub.on("PUT", "/api/v1/assistant/routines") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as! [String: Any]
            #expect((body["routines"] as! [[String: Any]]).first?["enabled"] as? Bool == false)
            return Data(#"{"routines":[{"id":"r1","title":"早报","prompt":"写早报","time":"09:00","weekdays":[],"enabled":false}]}"#.utf8)
        }
        let page = make(hub, Sink())
        await page.load()
        var off = page.routines[0]
        off.enabled = false
        let saved = await page.save(off)
        #expect(saved?.enabled == false)
        #expect(page.routines.first?.enabled == false)
        #expect(hub.sent("PUT", "/api/v1/assistant/routines").count == 1)
    }

    @Test func aFailedSaveReportsAndGoesBackToTheHubsList() async {
        let hub = hub()
        hub.on("PUT", "/api/v1/assistant/routines") { _ in throw APIError(status: 500, message: "nope") }
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        var off = page.routines[0]
        off.enabled = false
        #expect(await page.save(off) == nil)
        #expect(sink.errors.count == 1)
        #expect(page.routines.first?.enabled == true, "rolled back to what the hub has")
    }

    @Test func deletingTellsWhatWasDeleted() async {
        let hub = hub()
        hub.on("PUT", "/api/v1/assistant/routines", json: #"{"routines":[]}"#)
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        await page.delete(page.routines[0])
        #expect(page.routines.isEmpty)
        #expect(sink.toasts == ["已删除「早报」"])
    }

    @Test func runningAFollowupOpensItsConversation() async {
        let hub = hub()
        hub.on("POST", "/api/v1/assistant/followups/f1/run", json: #"{"session_id":"s9","machine_id":"m"}"#)
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        await page.runFollowup(page.followups[0])
        #expect(sink.opened == [SessionKey(machineID: "m", sessionID: "s9")])
        #expect(page.followups.map(\.id) == ["f2"])
    }

    @Test func aFailedCancelReportsAndReloads() async {
        let hub = hub()
        hub.on("DELETE", "/api/v1/assistant/followups/f1") { _ in throw APIError(status: 500, message: "nope") }
        let sink = Sink()
        let page = make(hub, sink)
        await page.load()
        await page.cancel(page.followups[0])
        #expect(sink.errors.count == 1)
        #expect(page.followups.map(\.id) == ["f1", "f2"], "reloaded from the hub")
    }
}

/// 已安排的跟进 and 记忆 against a FakeHub.
@MainActor
struct AssistantPagesTests {
    private func deps(_ hub: FakeHub, errors: @escaping (Error) -> Void = { _ in }) -> AssistantDeps {
        AssistantDeps(api: { hub.client }, report: errors, toast: { _ in }, open: { _ in })
    }

    @Test func followupsLoadSoonestFirstOrSayWhy() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/followups", json: #"""
            {"followups":[{"id":"b","at":"2026-10-31T12:00:00Z","prompt":"b","machine_id":"m"},
                          {"id":"a","at":"2026-10-31T09:00:00Z","prompt":"a","machine_id":"m"}]}
            """#)
        let page = AssistantFollowupsModel(deps: deps(hub))
        await page.load()
        #expect(page.followups.list.map(\.id) == ["a", "b"])
        let down = FakeHub()
        down.on("GET", "/api/v1/assistant/followups") { _ in throw APIError(status: 502, message: "hub down") }
        let failed = AssistantFollowupsModel(deps: deps(down))
        await failed.load()
        #expect(failed.loaded && failed.error != nil)
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
