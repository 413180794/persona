import Foundation
import Testing
@testable import Jarvis

/// 设置 › 模型供应商's wizard (ProviderWizardModel), 从 ~/.zshrc 导入 (LauncherImportModel) and 主对话's 运行方式
/// (#2455), against FakeHub: what each sends, and what the hub's answers come to.
@MainActor
struct ProviderWizardTests {
    private static let discovered = #"""
    {"models":[{"id":"glm-5","name":"GLM-5","context_window":200000,"efforts":["low","high"],"default_effort":"high"},
               {"id":"kimi-k2","context_window":256000}]}
    """#

    private func body(_ req: LocalRequest?) throws -> [String: JSONValue] {
        try #require(try JSON.decoder.decode(JSONValue.self, from: req?.body ?? Data()).object)
    }

    private func wizard(_ hub: FakeHub, existing: LauncherView? = nil) -> ProviderWizardModel {
        ProviderWizardModel(existing: existing, deps: .init(api: { hub.client }))
    }

    private static func existing(headers: Bool = true) throws -> LauncherView {
        let json = #"""
        {"name":"jv_glm","source":"jarvis","kind":"launcher","base_url":"https://gateway.example.com","model":"glm-5","has_key":true,
         "auth":"bearer","has_headers":\#(headers),"models":[{"id":"glm-5","context_window":200000}],"opus_model":"glm-5-plus",
         "timeout_ms":600000,"origin":"zshrc:cc_glm@macbook"}
        """#
        return try JSON.decoder.decode(LauncherView.self, from: Data(json.utf8))
    }

    @Test func aNewGatewayGoesThroughTheThreeSteps() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/discover", json: Self.discovered)
        hub.on("POST", "/api/v1/launchers/test", json: #"{"ok":true,"ms":640,"model":"glm-5"}"#)
        hub.on("POST", "/api/v1/launchers", json: #"{"ok":true}"#)
        let w = wizard(hub)
        w.kind = .custom
        #expect(!w.connectionReady)
        w.name = "9bad"
        w.baseURL = "https://gateway.example.com"
        w.apiKey = " sk-test-wizard "
        #expect(w.nameProblem != nil && !w.connectionReady)
        w.name = "jv_glm"
        w.auth = "bearer"
        #expect(w.connectionReady && !w.canTest) // no model to test with yet

        await w.discover()
        #expect(w.choices.map(\.id) == ["glm-5", "kimi-k2"] && w.models.isEmpty && w.discoverError == nil)
        let probe = try body(hub.sent("POST", "/api/v1/launchers/discover").first)
        #expect(probe["base_url"]?.string == "https://gateway.example.com" && probe["auth"]?.string == "bearer")
        #expect(probe["api_key"]?.string == "sk-test-wizard" && probe["name"] == nil && probe["headers"] == nil)

        await w.runTest()
        #expect(w.test?.ok == true && w.test?.ms == 640)
        #expect(try body(hub.sent("POST", "/api/v1/launchers/test").first)["model"]?.string == "glm-5")

        // The first ticked is the default; unticking the default moves it on.
        #expect(!w.canSave)
        w.toggle(w.choices[0])
        w.toggle(w.choices[1])
        #expect(w.defaultModel == "glm-5" && w.canSave)
        w.toggle(w.choices[0])
        #expect(w.defaultModel == "kimi-k2" && w.models.map(\.id) == ["kimi-k2"])
        #expect(w.addModel(" custom-x ") && !w.addModel("custom-x") && !w.addModel(" "))
        w.update("custom-x") { $0.contextWindow = 1_000_000 }
        w.headers = "X-Team: demo"
        w.sonnetModel = "kimi-k2"
        w.compactWindow = "150000"

        #expect(await w.save())
        let saved = try body(hub.sent("POST", "/api/v1/launchers").first)
        #expect(saved["name"]?.string == "jv_glm" && saved["model"]?.string == "kimi-k2" && saved["auth"]?.string == "bearer")
        #expect(saved["kind"]?.string == "launcher" && saved["api_key"]?.string == "sk-test-wizard")
        #expect(saved["headers"]?.string == "X-Team: demo" && saved["sonnet_model"]?.string == "kimi-k2")
        #expect(saved["compact_window"]?.int == 150000 && saved["timeout_ms"]?.int == 0)
        let models = saved["models"]?.array ?? []
        #expect(models.compactMap { $0["id"]?.string } == ["kimi-k2", "custom-x"])
        #expect(models.last?["context_window"]?.int == 1_000_000)
    }

    @Test func reservedNamesAreRefusedInline() {
        for name in ["claude", "presets", "test", "discover", "import"] {
            #expect(LauncherName.problem(name) != nil, "\(name) accepted")
        }
        #expect(LauncherName.problem("glm") == nil)
    }

    @Test func anthropicOfficialHasItsOwnAddressAndHeader() {
        let w = wizard(FakeHub())
        w.kind = .anthropic
        w.baseURL = "https://ignored.example.com"
        w.auth = "bearer"
        #expect(w.address == ProviderWizardModel.anthropicURL && w.authMode == "api_key")
    }

    @Test func editingKeepsTheHeadersUnlessClearedOrRetyped() async throws {
        let hub = FakeHub()
        var bodies: [[String: JSONValue]] = []
        hub.on("PUT", "/api/v1/launchers/jv_glm") { req in
            bodies.append(try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data()).object ?? [:])
            return Data(#"{"ok":true}"#.utf8)
        }
        let e = try Self.existing()
        #expect(e.origin == "zshrc:cc_glm@macbook" && e.hasHeaders == true && e.timeoutMS == 600000)
        let w = wizard(hub, existing: e)
        #expect(!w.isNew && w.kind == .custom && w.auth == "bearer" && w.defaultModel == "glm-5" && w.opusModel == "glm-5-plus")
        #expect(w.connectionReady && w.canSave) // the stored key will do
        #expect(w.hasStoredHeaders)

        // Untouched: no headers key at all (the hub keeps them), and no key.
        #expect(await w.save())
        #expect(bodies[0].keys.contains("headers") == false && bodies[0]["api_key"] == nil)
        #expect(bodies[0]["timeout_ms"]?.int == 600000 && bodies[0]["opus_model"]?.string == "glm-5-plus")

        // 清除已设置的请求头: an empty string clears them.
        w.clearHeaders = true
        #expect(!w.hasStoredHeaders)
        #expect(await w.save())
        #expect(bodies[1]["headers"] == .string(""))

        // Typed over: the new ones.
        w.headers = "X-Other: 1\n"
        #expect(await w.save())
        #expect(bodies[2]["headers"]?.string == "X-Other: 1")
    }

    @Test func aStoredProviderTestsWithItsStoredKey() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/test", json: #"{"ok":false,"ms":30000,"model":"glm-5","error":"网关 401"}"#)
        hub.on("POST", "/api/v1/launchers/discover", json: #"{"models":[],"error":"网关没有 /v1/models"}"#)
        let w = wizard(hub, existing: try Self.existing(headers: false))
        await w.runTest()
        let probe = try body(hub.sent("POST", "/api/v1/launchers/test").first)
        #expect(probe["name"]?.string == "jv_glm" && probe["api_key"] == nil && probe["headers"] == nil)
        #expect(w.test?.ok == false && w.test?.error == "网关 401")
        await w.discover()
        #expect(w.discoveredOnce && w.discoverError == "网关没有 /v1/models" && w.choices.map(\.id) == ["glm-5"])
    }

    @Test func importPreviewsTicksAndImports() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/launchers/import", json: #"""
        {"machine":"macbook","providers":[
          {"name":"cc_glm","base_url":"https://gateway.example.com","has_key":true,"models":[{"id":"glm-5"},{"id":"glm-5-air"}],
           "aliases":["cc_glm","cc_glm_air"],"existing":"jv_glm"},
          {"name":"cc_kimi","base_url":"https://kimi.example.com","has_key":true,"model":"kimi-k2","aliases":["cc_kimi"]},
          {"name":"cc_plain","has_key":false}]}
        """#)
        hub.on("POST", "/api/v1/launchers/import", json: #"{"created":["cc_kimi"],"updated":["jv_glm"]}"#)
        let m = LauncherImportModel(deps: .init(api: { hub.client }))
        m.machine = "macbook"
        await m.load()
        #expect(m.phase == .preview && m.picked == ["cc_glm", "cc_kimi", "cc_plain"])
        #expect(hub.sent("GET", "/api/v1/launchers/import").first?.query["machine"] == "macbook")
        #expect(ImportText.detail(m.providers[0]) == "gateway.example.com · 2 个模型")
        #expect(ImportText.detail(m.providers[1]) == "kimi.example.com · kimi-k2")
        #expect(ImportText.detail(m.providers[2]) == "Anthropic 官方 · 没有 Key")
        m.toggle("cc_plain")
        #expect(m.canImport)
        await m.runImport()
        let sent = try body(hub.sent("POST", "/api/v1/launchers/import").first)
        #expect(sent["machine"]?.string == "macbook")
        #expect(sent["providers"]?.array?.compactMap(\.string) == ["cc_glm", "cc_kimi"])
        #expect(m.phase == .done && m.result?.created == ["cc_kimi"] && m.result?.updated == ["jv_glm"])
        m.restart()
        #expect(m.phase == .machine && m.preview == nil && m.picked.isEmpty)
    }

    @Test func anOutdatedNodeSaysSo() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/launchers/import") { _ in
            throw APIError(status: 409, message: "这台机器上的 personad 太旧", fields: ["code": "node_outdated"])
        }
        let m = LauncherImportModel(deps: .init(api: { hub.client }))
        m.machine = "macbook"
        await m.load()
        #expect(m.phase == .machine && m.outdated && m.error == "这台机器上的 personad 太旧")
        hub.on("GET", "/api/v1/launchers/import") { _ in throw APIError(status: 503, message: "离线", fields: ["code": "offline"]) }
        await m.load()
        #expect(!m.outdated && m.error == "离线")
    }

    // MARK: 运行方式

    private static let assistant = #"""
    {"id":"assistant","category_id":"assistant","uses":[{"name":"与当前 persona 的直接对话"}],"name":"主对话","group":"默认模型",
     "kind":"launcher","override":{"provider":"launcher.jv_glm"},"effective":{"provider":"launcher.jv_glm","model":"glm-5","runtime":"agentsdk"},
     "revision":3,"fields":["provider","model","effort","runtime"],"runtimes":["cli","agentsdk"],"provider_ids":["launcher.jv_glm","launcher.claude"]}
    """#
    private static let providers = #"[{"id":"launcher.jv_glm","name":"jv_glm","kind":"launcher"},{"id":"launcher.claude","name":"claude","kind":"launcher"}]"#

    @Test func theMainConversationPicksItsRuntime() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/model-config", json: #"{"purposes":[\#(Self.assistant)],"providers":\#(Self.providers)}"#)
        hub.on("GET", "/api/v1/model-config/purposes/assistant", json: #"{"purpose":\#(Self.assistant),"providers":\#(Self.providers)}"#)
        var patches: [[String: JSONValue]] = []
        hub.on("PATCH", "/api/v1/model-config/purposes/assistant") { req in
            patches.append(try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data()).object ?? [:])
            return Data(#"{"purpose":\#(Self.assistant),"providers":\#(Self.providers)}"#.utf8)
        }
        let page = ModelConfigurationModel(deps: .init(api: { hub.client }))
        await page.load()
        #expect(page.modelSummary(page.purposes[0]) == "glm-5 · Agent SDK")
        await page.load(purposeID: "assistant")
        #expect(page.runtimes == ["cli", "agentsdk"])
        page.draft.runtime = AssistantRuntime.agentSDK
        // Another provider clears the model, not the runtime.
        page.chooseProvider("launcher.claude")
        #expect(page.draft.runtime == "agentsdk" && page.draft.model == nil)
        #expect(await page.save())
        #expect(patches[0]["runtime"]?.string == "agentsdk" && patches[0]["provider"]?.string == "launcher.claude")
        // 跟随默认: an explicit null takes the override away.
        page.restoreInheritance()
        #expect(await page.save())
        #expect(patches[1].keys.contains("runtime") && patches[1]["runtime"] == .null)
    }

    @Test func otherUsesSendNoRuntime() throws {
        let patch = ModelConfigurationPatch(revision: 1, binding: ModelBinding(provider: "launcher.x", runtime: "agentsdk"),
                                            fields: ["provider", "model"])
        let sent = try #require(try JSON.decoder.decode(JSONValue.self, from: JSON.encoder.encode(patch)).object)
        #expect(sent["runtime"] == nil && sent["provider"]?.string == "launcher.x")
        let purpose = try JSON.decoder.decode(ModelPurpose.self, from: Data(Self.assistant.replacingOccurrences(of: #","runtime"]"#, with: "]").utf8))
        #expect(purpose.supports("runtime") == false && purpose.runtimes == ["cli", "agentsdk"])
    }

    @Test func theRuntimeStatusSaysWhenItFellBack() throws {
        #expect(AssistantRuntime.status(runtime: nil, fallback: nil) == nil)
        #expect(AssistantRuntime.status(runtime: "cli", fallback: " ") == nil)
        #expect(AssistantRuntime.status(runtime: "agentsdk", fallback: nil) == "Agent SDK")
        #expect(AssistantRuntime.status(runtime: nil, fallback: "找不到 Node.js 22+") == "已退回 CLI：找不到 Node.js 22+")
        let s = try JSON.decoder.decode(Session.self, from: Data(#"{"session_id":"s1","runtime_fallback":"找不到 Node.js 22+","assistant":true,"started_by_jarvis":true,"pending_interactions":0,"state":"idle"}"#.utf8))
        #expect(s.runtimeFallback == "找不到 Node.js 22+" && s.runtime == nil)
    }

    @Test func thePickersDefaultReadsAsDefault() throws {
        let r = try JSON.decoder.decode(APIClient.LaunchersResponse.self, from: Data(#"""
        {"launchers":[{"name":"claude"}],"default":{"provider":"launcher.jv_glm","model":"vendor/glm-5","effort":"high"}}
        """#.utf8))
        #expect(r.default?.launcher == "jv_glm" && r.default?.label == "默认：jv_glm · glm-5")
        #expect(LauncherDefault(provider: "launcher.claude").label == "默认：claude")
        #expect(LauncherDefault(provider: nil, model: "x").label == nil)
        let old = try JSON.decoder.decode(APIClient.LaunchersResponse.self, from: Data(#"{"launchers":[]}"#.utf8))
        #expect(old.default == nil)
    }
}
