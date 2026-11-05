import Foundation
import Testing
@testable import Jarvis

/// 健康 (docs/health.md) against a FakeHub: the space's conversations come from `GET /api/v1/assistant?space=health`,
/// its memory from `?space=health`, a new one is started with `"space":"health"`; pushes go over the list, and the
/// Persona page's lists never hold a health conversation.
@MainActor
struct HealthModelTests {
    private static func session(_ id: String, space: String? = "health", archived: Bool = false, updated: TimeInterval,
                                title: String? = nil) -> Session {
        let spaceField = space.map { #","space":"\#($0)""# } ?? ""
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(title ?? id)","cwd":"/home/me/.jarvis/health","status":"idle",
         "state":"idle","assistant":true,"archived":\(archived)\(spaceField),
         "updated_at":"\(ISO8601DateFormatter().string(from: Date(timeIntervalSince1970: updated)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) } catch { fatalError("bad fixture: \(error)") }
    }

    private func deps(_ hub: FakeHub) -> AssistantDeps {
        AssistantDeps(api: { hub.client }, report: { _ in }, toast: { _ in }, open: { _ in })
    }

    @Test func loadsTheSpacesConversationsAndMemory() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant") { req in
            #expect(req.query["space"] == "health")
            return Data(#"""
                {"machine_id":"m","launcher":"ccc","cwd":"/home/me/.jarvis/health","conversations":[
                 {"session_id":"h1","title":"睡眠","status":"idle","state":"idle","assistant":true,"space":"health"}]}
                """#.utf8)
        }
        hub.on("GET", "/api/v1/assistant/memory") { req in
            #expect(req.query["space"] == "health")
            return Data(#"{"files":[{"name":"MEMORY.md","content":"x"},{"name":"allergy.md","content":"y"}]}"#.utf8)
        }
        let page = HealthModel(deps: deps(hub))
        await page.load()
        #expect(page.loaded)
        #expect(page.fetched.map(\.sessionID) == ["h1"])
        #expect(page.fetched.first?.machineID == "m") // the list's machine, when an item leaves it out
        #expect(page.fetched.first?.isHealth == true)
        #expect(page.memoryCount == 1) // MEMORY.md is the index, not a memory
    }

    @Test func aFailedListShowsWhy() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant") { _ in throw APIError(status: 500, message: "hub down") }
        let page = HealthModel(deps: deps(hub))
        await page.load()
        #expect(page.loaded)
        #expect(page.fetched.isEmpty)
        #expect(page.error != nil)
    }

    @Test func pushesGoOverTheListAndArchivedOnesLeave() {
        let fetched = [Self.session("a", updated: 1, title: "old"), Self.session("b", updated: 2)]
        let live = [Self.session("a", updated: 5, title: "new"), Self.session("b", archived: true, updated: 6),
                    Self.session("c", updated: 3), Self.session("main", space: nil, updated: 9)]
        let list = HealthModel.merge(fetched, live: live)
        #expect(list.map(\.sessionID) == ["a", "c"])
        #expect(list.first?.title == "new")
    }

    @Test func theSidebarNeverListsAHealthConversation() {
        let all = [Self.session("h", updated: 5), Self.session("main", space: nil, updated: 1),
                   Self.session("old", space: nil, archived: true, updated: 2), Self.session("hOld", archived: true, updated: 3)]
        let lists = AssistantLists(all, group: "")
        #expect(lists.open.map(\.sessionID) == ["main"])
        #expect(lists.archived.map(\.sessionID) == ["old"])
        #expect(lists.health.map(\.sessionID) == ["h"])
    }

    @Test func aNewConversationOpensInTheSpace() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/assistant/conversations") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any]
            #expect(body?["space"] as? String == "health")
            #expect(body?["main"] == nil)
            return Data(#"{"session":{"machine_id":"m","session_id":"h9","status":"idle","state":"idle","assistant":true}}"#.utf8)
        }
        let s = try await hub.client.startConversation(prompt: "这周我睡得怎么样？", space: Session.healthSpace)
        #expect(s.isHealth) // marked even when the hub's answer leaves `space` out
    }

    @Test func theSpacesMemoryIsDeletedFromItself() async {
        let hub = FakeHub()
        var deleted = false
        hub.on("GET", "/api/v1/assistant/memory") { _ in
            Data((deleted ? #"{"files":[]}"# : #"{"files":[{"name":"a.md","content":"x"}]}"#).utf8)
        }
        hub.on("DELETE", "/api/v1/assistant/memory/a.md") { req in
            #expect(req.query["space"] == "health")
            deleted = true
            return Data("{}".utf8)
        }
        let page = AssistantMemoryModel(space: Session.healthSpace, deps: deps(hub))
        await page.load()
        await page.delete("a.md")
        #expect(deleted)
        #expect(page.files.isEmpty)
    }

    @Test func theAssistantsOwnMemorySendsNoSpace() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/memory") { req in
            #expect(req.query["space"] == nil)
            return Data(#"{"files":[]}"#.utf8)
        }
        let page = AssistantMemoryModel(deps: deps(hub))
        await page.load()
        #expect(page.loaded)
    }
}
