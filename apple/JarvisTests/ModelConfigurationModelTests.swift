import Foundation
import Testing
@testable import Jarvis

@MainActor
struct ModelConfigurationModelTests {
    private static let purpose = #"{"id":"lark.agent","name":"飞书 /agent","group":"对话与 Agent","kind":"launcher","override":{},"effective":{"provider":"launcher.strong","model":"model-a"},"revision":0,"fields":["provider","model"],"provider_ids":["launcher.strong"],"inherited_from":"助手"}"#
    private static let providers = #"[{"id":"launcher.strong","name":"strong","kind":"launcher","model":"model-a"},{"id":"legacy.ai","name":"hub API","kind":"api","model":"model-b"}]"#
    private static let detail = #"{"purpose":\#(purpose),"providers":\#(providers)}"#

    private func make(_ hub: FakeHub) -> ModelConfigurationModel { .init(deps: .init(api: { hub.client })) }

    @Test func loadsTheServerCatalogAndFiltersByEffectiveModel() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config", json: #"{"purposes":[\#(Self.purpose)],"providers":\#(Self.providers)}"#)
        let page = make(hub)
        await page.load()
        #expect(page.loaded && page.error == nil)
        #expect(page.filtered("model-a").map(\.id) == ["lark.agent"])
        #expect(page.filtered("分类器").isEmpty)
        #expect(page.summary(page.purposes[0]) == "strong · model-a")
    }

    /// A use whose connection the hub holds (docs/model-config.md「上游限流」): the time as Go writes it, counted down.
    @Test func decodesAHeldConnectionAndCountsItDown() async throws {
        let held = #"{"id":"inbox.triage","name":"分拣与摘要","group":"收件箱","kind":"api","override":{},"effective":{"provider":"legacy.ai"},"revision":0,"status":"额度用完，23 分钟后重试","limit":{"reason":"quota","until":"2026-11-02T20:13:00.123456789+08:00"}}"#
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config", json: #"{"purposes":[\#(held)],"providers":\#(Self.providers)}"#)
        let page = make(hub)
        await page.load()
        let limit = try #require(page.purposes.first?.limit)
        #expect(limit.reason == "quota")
        let until = limit.until
        #expect(abs(until.timeIntervalSince1970 - 1_793_621_580.123) < 0.01)
        #expect(limit.text(now: until.addingTimeInterval(-23 * 60 + 1)) == "额度用完，23 分钟后重试")
        #expect(limit.text(now: until.addingTimeInterval(-45)) == "额度用完，45 秒后重试")
        #expect(limit.text(now: until.addingTimeInterval(1)) == "额度用完，稍后重试")
        #expect(ModelLimit(reason: "busy", until: until).text(now: until.addingTimeInterval(-5)) == "并发已满，5 秒后重试")
        #expect(page.purposes.first?.status == "额度用完，23 分钟后重试")
    }

    @Test func legacyHubCannotBecomeAnEmptyEditableConfiguration() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config") { _ in throw APIError(status: 404, message: "not found") }
        let page = make(hub)
        await page.load()
        #expect(page.loaded && page.error?.contains("更新服务器") == true)
        #expect(!page.loading && page.purpose == nil)
        #expect(await page.save() == false)
        #expect(hub.sent("PATCH", "/api/v1/model-config/purposes/lark.agent").isEmpty)
    }

    @Test func candidateProvidersRespectThePurposeCapabilities() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config/purposes/lark.agent", json: Self.detail)
        let page = make(hub)
        await page.load(purposeID: "lark.agent")
        #expect(page.candidates.map(\.id) == ["launcher.strong"])
        #expect(page.purpose?.supports("effort") == false)
        #expect(!page.dirty)
    }

    @Test func changingProvidersDoesNotCarryTheOldModelOrEffort() async {
        let page = make(FakeHub())
        page.draft = .init(provider: "old", model: "model-a", effort: "high")
        page.chooseProvider("new")
        #expect(page.draft == .init(provider: "new"))
        page.chooseProvider("")
        #expect(page.draft.isEmpty)
    }

    @Test func saveSendsTheRevisionNullInheritanceAndOnlySupportedFields() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config/purposes/lark.agent", json: Self.detail)
        hub.on("PATCH", "/api/v1/model-config/purposes/lark.agent") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as! [String: Any]
            #expect(body["revision"] as? Int == 0)
            #expect(body["provider"] is NSNull)
            #expect(body["model"] as? String == "model-c")
            #expect(body["effort"] == nil)
            return Data(Self.detail.utf8)
        }
        let page = make(hub)
        await page.load(purposeID: "lark.agent")
        page.draft.model = " model-c "
        page.draft.effort = "high"
        #expect(await page.save())
        #expect(page.error == nil && !page.dirty && !page.saving)
    }

    @Test func failedSaveKeepsTheDraftAndConflictRequiresReload() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config/purposes/lark.agent", json: Self.detail)
        hub.on("PATCH", "/api/v1/model-config/purposes/lark.agent") { _ in throw APIError(status: 409, message: "changed") }
        let page = make(hub)
        await page.load(purposeID: "lark.agent")
        page.draft.model = "my-model"
        #expect(await page.save() == false)
        #expect(page.draft.model == "my-model" && page.conflict && page.dirty)
        #expect(!page.saving)
        await page.load(purposeID: "lark.agent")
        #expect(!page.conflict && page.error == nil && !page.dirty)
    }

    @Test func restoringInheritanceEncodesExplicitNulls() throws {
        let patch = ModelConfigurationPatch(revision: 9, binding: .init(), fields: ["provider", "model", "effort"])
        let body = try JSONSerialization.jsonObject(with: JSON.encoder.encode(patch)) as! [String: Any]
        #expect(body["revision"] as? Int == 9)
        #expect(body["provider"] is NSNull && body["model"] is NSNull && body["effort"] is NSNull)
    }

    @Test func aTaskTimeEditDoesNotOverwriteCentralModelChoices() throws {
        var task = APIClient.ScheduledTask(id: "task_1", title: "早报", prompt: "写早报")
        task.launcher = "old"
        task.effort = "low"
        task.includesModelSettings = false
        let body = try JSONSerialization.jsonObject(with: JSON.encoder.encode(task)) as! [String: Any]
        #expect(body["launcher"] == nil && body["effort"] == nil)
        #expect(body["title"] as? String == "早报")
    }
}
