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
