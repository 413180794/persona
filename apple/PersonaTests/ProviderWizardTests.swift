import Foundation
import Testing
@testable import Jarvis

/// Settings › Model Providers' wizard (ProviderWizardModel), Import from ~/.zshrc (LauncherImportModel) and Main Chat's How It Runs
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

        // Clear Saved Headers: an empty string clears them.
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

    // MARK: How It Runs

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
        // Default: an explicit null takes the override away.
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

    // MARK: Built-in presets (#4197, docs/onboarding.md 4.3「三条路」, 5.4)

    private static let builtin = #"""
    {"presets":[
      {"id":"builtin/glm","plugin":"builtin","name":"GLM","display_name":"智谱 GLM","description":"国内直接开通，按量付费","kind":"launcher",
       "api":"messages","base_url":"https://open.example.com/api/anthropic","auth":"bearer","model":"glm-5","small_model":"glm-5-air",
       "models":[{"id":"glm-5","name":"GLM-5"},{"id":"glm-5-air"}],"signup_url":"https://open.example.com/keys"},
      {"id":"builtin/anthropic","plugin":"builtin","name":"Anthropic","display_name":"Anthropic 官方","kind":"launcher","api":"messages",
       "base_url":"https://api.example.com","auth":"api_key","model":"claude-x","signup_url":"javascript:alert(1)"},
      {"id":"example-gateway/team","plugin":"example-gateway","name":"示例网关","kind":"launcher","base_url":"https://gateway.example.com",
       "model":"example-pro"},
      {"id":"example-gateway/api","plugin":"example-gateway","name":"示例 API","kind":"api","api":"responses",
       "base_url":"https://api.example.com/v1","model":"m1"}]}
    """#

    private func presetWizard(_ hub: FakeHub, features: Set<String> = [ProviderWizardModel.presetsFeature],
                              readiness: AssistantReadiness? = nil,
                              landed: ((AssistantReadiness) -> Void)? = nil) -> ProviderWizardModel {
        ProviderWizardModel(deps: .init(api: { hub.client }, features: { features }, readiness: { readiness },
                                        landReadiness: landed ?? { _ in }))
    }

    private static func readiness(_ json: String) throws -> AssistantReadiness {
        try JSON.decoder.decode(AssistantReadiness.self, from: Data(json.utf8))
    }

    /// Computer, Claude Code and model all ok: the main conversation runs on Claude Code.
    private static let full = #"""
    {"version":1,"mode":"full","machine":{"id":"mbp","name":"小林的 MacBook"},"launcher":"claude",
     "steps":[{"id":"computer","state":"ok"},{"id":"claude","state":"ok","version":"2.1.320"},{"id":"model","state":"ok","launcher":"claude"},
              {"id":"first_message","state":"ok"}]}
    """#

    private func loadedPresets(_ hub: FakeHub, readiness: AssistantReadiness? = nil) async -> ProviderWizardModel {
        hub.on("GET", "/api/v1/launchers/presets", json: Self.builtin)
        let w = presetWizard(hub, readiness: readiness)
        await w.loadPresets()
        return w
    }

    @Test func theBuiltInFieldsDecode() async throws {
        let w = await loadedPresets(FakeHub())
        #expect(w.presetsFlow && w.presets.map(\.id) == ["builtin/glm", "builtin/anthropic", "example-gateway/team", "example-gateway/api"])
        let glm = w.presets[0]
        #expect(glm.isBuiltin && glm.title == "智谱 GLM" && glm.auth == "bearer" && glm.host == "open.example.com")
        #expect(glm.signup?.absoluteString == "https://open.example.com/keys" && glm.testable)
        // Only a web address opens; an extension's preset has no display name and goes by its name.
        #expect(w.presets[1].signup == nil && w.signupPresets.map(\.id) == ["builtin/glm"])
        #expect(!w.presets[2].isBuiltin && w.presets[2].title == "示例网关" && w.presets[2].auth == nil)
        #expect(!w.presets[3].testable)
        let view = try JSON.decoder.decode(LauncherView.self, from: Data(#"{"name":"glm","source":"jarvis","display_name":"智谱 GLM"}"#.utf8))
        #expect(view.displayName == "智谱 GLM" && view.title == "智谱 GLM")
        let older = try JSON.decoder.decode(LauncherView.self, from: Data(#"{"name":"glm","source":"jarvis"}"#.utf8))
        #expect(older.displayName == nil && older.title == "glm")
    }

    @Test func aPresetsKeyIsTestedWithThePresetAndNoModel() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/test", json: #"{"ok":true,"ms":412,"model":"glm-5"}"#)
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        #expect(w.presetModel == "glm-5" && !w.keyTestable)
        w.presetKey = "short"
        #expect(!w.keyTestable) // not on every keystroke
        w.presetKey = "  sk-test-0123456789\n"
        #expect(w.keyTestable)
        await w.testKey()
        #expect(w.keyTest?.ok == true && w.keyTest?.model == "glm-5")
        let sent = try body(hub.sent("POST", "/api/v1/launchers/test").first)
        #expect(sent["preset"]?.string == "builtin/glm" && sent["api_key"]?.string == "sk-test-0123456789")
        #expect(sent["auth"]?.string == "bearer")
        #expect(sent["model"] == nil && sent["base_url"] == nil && sent["name"] == nil)
        // Another of its models goes along.
        w.presetModel = "glm-5-air"
        await w.testKey()
        #expect(try body(hub.sent("POST", "/api/v1/launchers/test").last)["model"]?.string == "glm-5-air")
        // A Responses API cannot be tested: nothing is sent.
        w.presetID = "example-gateway/api"
        w.presetKey = "sk-test-0123456789"
        await w.testKey()
        #expect(hub.sent("POST", "/api/v1/launchers/test").count == 2 && w.keyTest == nil)
    }

    @Test func aPastedKeyIsTestedByItself() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/test", json: #"{"ok":true,"ms":10,"model":"glm-5"}"#)
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        w.presetKey = "sk-test-0123456789"
        w.keyChanged(after: .milliseconds(1))
        for _ in 0..<200 where w.keyTest == nil { try await Task.sleep(for: .milliseconds(10)) }
        #expect(w.keyTest?.ok == true && hub.sent("POST", "/api/v1/launchers/test").count == 1)
    }

    /// The hub's words for each failure (server/internal/i18n/locales/zh-Hans/assistant-models_probe.json) are what shows.
    @Test(arguments: [
        "认证失败（401）：API Key 不对或已失效，或者这个网关要另一种认证方式",
        "没有权限（403）：这个 Key 不能用这个模型或接口",
        "找不到接口（404）：Base URL 可能不对（要的是 Anthropic Messages 兼容的地址）",
        "被限流了（429）：稍后再试，或者额度用完了",
        "30 秒内网关没有响应",
    ])
    func aFailedTestSaysWhatTheHubSaid(_ text: String) async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/test", json: #"{"ok":false,"ms":233,"model":"glm-5","error":"\#(text)"}"#)
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        w.presetKey = "sk-test-0123456789"
        await w.testKey()
        #expect(w.keyTest?.ok == false && w.keyTest?.error == text)
        #expect(w.canSavePreset) // a failed test does not stop saving
    }

    @Test func aRefusedTestRequestSaysWhy() async {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers/test") { _ in throw APIError(status: 400, message: "没有这个供应商预设") }
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        w.presetKey = "sk-test-0123456789"
        await w.testKey()
        #expect(w.keyTest?.ok == false && w.keyTest?.error == "没有这个供应商预设")
    }

    @Test func savingSendsThePresetAndAssignWithTheNameEmpty() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers", json: #"""
        {"launcher":{"name":"glm","display_name":"智谱 GLM","source":"jarvis","has_key":true},
         "assigned":["assistant","agent.new"],"kept":[]}
        """#)
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        #expect(!w.canSavePreset)
        w.presetKey = " sk-test-0123456789 "
        #expect(w.assign && w.canSavePreset)
        #expect(await w.savePreset())
        let sent = try body(hub.sent("POST", "/api/v1/launchers").first)
        #expect(sent["preset"]?.string == "builtin/glm" && sent["api_key"]?.string == "sk-test-0123456789")
        #expect(sent["name"]?.string == "" && sent["display_name"]?.string == "" && sent["model"]?.string == "")
        #expect(sent["assign"]?.array?.compactMap(\.string) == ["assistant", "agent.new"])
        #expect(w.addedTitle == "智谱 GLM")
        #expect(w.addedSummary.assigned == "主对话和新开的 Agent 现在用它。" && w.addedSummary.kept.isEmpty)
    }

    @Test func aChineseDisplayNameIsSaved() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/launchers") { req in
            let b = try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data()).object ?? [:]
            let shown = b["display_name"]?.string ?? ""
            return Data(#"{"launcher":{"name":"glm","display_name":"\#(shown)","source":"jarvis"},"assigned":[],"kept":[]}"#.utf8)
        }
        let w = await loadedPresets(hub)
        w.presetID = "builtin/glm"
        w.presetKey = "sk-test-0123456789"
        w.presetDisplayName = " 团队的智谱 "
        w.assignChoice = false
        #expect(await w.savePreset())
        let sent = try body(hub.sent("POST", "/api/v1/launchers").first)
        #expect(sent["display_name"]?.string == "团队的智谱" && sent["assign"] == nil)
        #expect(w.added?.launcher?.title == "团队的智谱" && w.addedTitle == "团队的智谱")
        // Not asked: the screen points to Model Settings instead of saying what changed.
        #expect(w.addedSummary == AssignSummary(LauncherAdded(), asked: false) && w.addedSummary.assigned.isEmpty)
    }

    @Test func whatWasKeptSaysWhy() throws {
        let r = try JSON.decoder.decode(LauncherAdded.self, from: Data(#"""
        {"launcher":{"name":"glm","source":"jarvis"},"assigned":["agent.new"],
         "kept":[{"purpose":"assistant","reason":"customized"},{"purpose":"agent.new","reason":"incompatible"},
                 {"purpose":"assistant","reason":"failed"},{"purpose":"later.use","reason":"new_reason"}]}
        """#.utf8))
        let s = AssignSummary(r, asked: true)
        #expect(s.assigned == "新开的 Agent 现在用它。")
        #expect(s.kept == ["主对话没换：你改过，保留你的选择。", "新开的 Agent没换：它用不了这个供应商。",
                           "主对话没换成：可以在「模型配置」里手动选。", "later.use没换。"])
        // Nothing assigned: no "now uses it" line, only the kept ones.
        let none = AssignSummary(LauncherAdded(assigned: [], kept: [.init(purpose: "assistant", reason: "customized")]), asked: true)
        #expect(none.assigned.isEmpty && none.kept == ["主对话没换：你改过，保留你的选择。"])
        // An answer without assigned (an older hub) says nothing of it.
        #expect(AssignSummary(LauncherAdded(launcher: nil), asked: true) == AssignSummary(LauncherAdded(), asked: false))
    }

    @Test func theToggleFollowsWhetherClaudeCodeAlreadyWorks() async throws {
        let on = await loadedPresets(FakeHub())
        #expect(on.assign && on.assignChoice == nil) // no readiness (an older hub, or not read yet): on
        let off = await loadedPresets(FakeHub(), readiness: try Self.readiness(Self.full))
        #expect(!off.assign)
        // Not logged in, a model never tried, a launcher that runs Codex (Claude Code skipped): on.
        for steps in [#"{"id":"model","state":"problem","code":"not_logged_in"}"#, #"{"id":"model","state":"unknown","code":"model_unverified"}"#] {
            let r = try Self.readiness(Self.full.replacingOccurrences(of: #"{"id":"model","state":"ok","launcher":"claude"}"#, with: steps))
            #expect(await loadedPresets(FakeHub(), readiness: r).assign)
        }
        let codex = try Self.readiness(Self.full.replacingOccurrences(of: #"{"id":"claude","state":"ok","version":"2.1.320"}"#,
                                                                      with: #"{"id":"claude","state":"skipped"}"#))
        #expect(await loadedPresets(FakeHub(), readiness: codex).assign)
        off.assignChoice = true // touched, it stays as set
        #expect(off.assign)
        on.assignChoice = false
        #expect(!on.assign && on.presetInput?.assign == nil)
    }

    @Test func pickingAnotherPresetStartsOver() async {
        let w = await loadedPresets(FakeHub())
        w.presetID = "builtin/glm"
        w.presetKey = "sk-test-0123456789"
        w.presetName = "my_glm"
        w.presetModel = "glm-5-air"
        w.presetID = "example-gateway/team"
        #expect(w.presetKey.isEmpty && w.presetName.isEmpty && w.presetModel == "example-pro" && w.keyTest == nil)
        w.presetName = "9bad"
        w.presetKey = "k"
        #expect(w.presetNameProblem != nil && !w.canSavePreset)
    }

    @Test func anOlderHubKeepsTheWizardAsItWas() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/launchers/presets", json: Self.builtin)
        hub.on("POST", "/api/v1/launchers", json: #"{"ok":true}"#)
        let w = presetWizard(hub, features: [])
        #expect(!w.presetsFlow)
        await w.loadPresets()
        // No presets asked for: the first step is Anthropic official and the gateway, as before.
        #expect(w.presetsLoaded && w.presets.isEmpty && hub.sent("GET", "/api/v1/launchers/presets").isEmpty)
        w.kind = .anthropic
        w.name = "jv_official"
        w.apiKey = "sk-test-0123456789"
        #expect(w.address == ProviderWizardModel.anthropicURL)
        w.toggle(ProviderModel(id: "claude-x"))
        #expect(await w.save())
        let sent = try body(hub.sent("POST", "/api/v1/launchers").first)
        #expect(sent["preset"] == nil && sent["assign"] == nil && sent["display_name"] == nil)
        #expect(sent["name"]?.string == "jv_official" && sent["base_url"]?.string == ProviderWizardModel.anthropicURL)
        // Editing never takes the new flow.
        let e = presetWizard(hub, features: [ProviderWizardModel.presetsFeature])
        #expect(e.presetsFlow)
        #expect(!ProviderWizardModel(existing: try Self.existing(), deps: .init(api: { hub.client }, features: { [ProviderWizardModel.presetsFeature] })).presetsFlow)
    }

    @Test func alreadyUsingClaudeCodeTriesOnce() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/assistant/readiness", json: #"""
        {"version":1,"mode":"full","machine":{"id":"mbp","name":"小林的 MacBook"},"launcher":"claude",
         "steps":[{"id":"computer","state":"ok"}],"probe":{"ok":true,"model":"claude-x","answer":"好","millis":1830}}
        """#)
        var landed: [AssistantReadiness] = []
        let w = presetWizard(hub, landed: { landed.append($0) })
        await w.tryClaude()
        #expect(hub.sent("GET", "/api/v1/assistant/readiness").first?.query["probe"] == "1")
        guard case .answered(let r) = w.claudeTry else { Issue.record("no answer"); return }
        #expect(w.claudeTry?.ok == true && r.probe?.answer == "好" && r.machine?.name == "小林的 MacBook")
        #expect(landed.count == 1) // the app's readiness has it too

        hub.on("GET", "/api/v1/assistant/readiness", json: #"""
        {"version":1,"mode":"setup","machine":{"id":"mbp","name":"小林的 MacBook"},
         "probe":{"ok":false,"millis":2140,"error":"Not logged in · Please run /login"}}
        """#)
        await w.tryClaude()
        guard case .answered(let failed) = w.claudeTry else { Issue.record("no answer"); return }
        #expect(w.claudeTry?.ok == false && failed.probe?.error == "Not logged in · Please run /login")

        // No computer that is ready: no probe, the step in the way.
        hub.on("GET", "/api/v1/assistant/readiness", json: #"""
        {"version":1,"mode":"setup","machine":null,"steps":[{"id":"computer","state":"todo","code":"no_machine","title":"先添加一台电脑"}]}
        """#)
        await w.tryClaude()
        guard case .answered(let none) = w.claudeTry else { Issue.record("no answer"); return }
        #expect(none.probe == nil && none.steps.first?.title == "先添加一台电脑" && w.claudeTry?.ok == false)

        // An older hub without the readiness.
        let old = presetWizard(FakeHub())
        await old.tryClaude()
        #expect(old.claudeTry == .unsupported)
    }
}
