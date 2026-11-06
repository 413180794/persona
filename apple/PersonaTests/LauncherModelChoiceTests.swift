import Foundation
import Testing
@testable import Jarvis

/// New Agent's model: the launcher's list as the hub sends it and what a start sends (docs/model-config.md「新建 Agent 选模型」).
@MainActor struct LauncherModelChoiceTests {
    private func list(_ json: String) throws -> LauncherModels {
        try JSON.decoder.decode(LauncherModels.self, from: Data(json.utf8))
    }

    private var claude: LauncherModels {
        get throws {
            try list("""
            {"agent":"claude","models":[
              {"id":"default","name":"Default (recommended)","resolved":"claude-opus-5-5","default":true,"efforts":["low","medium","high","xhigh","max"]},
              {"id":"opus","name":"Opus 5.5","resolved":"claude-opus-5-5","efforts":["low","medium","high","xhigh","max"]},
              {"id":"haiku","name":"Haiku 5.5","resolved":"claude-haiku-5-5"},
              {"id":"gateway/glm-5.3","name":"glm-5.3","description":"From gateway","efforts":["low","medium","high"]}
            ]}
            """)
        }
    }

    @Test func decodesWhatTheHubSends() throws {
        let l = try claude
        #expect(l.agent == "claude" && l.locked == nil && l.models.count == 4)
        #expect(l.models[0].isDefault && l.models[0].resolved == "claude-opus-5-5")
        #expect(!l.models[2].isDefault && l.models[2].efforts == nil)
        // Only id is required: a newer CLI's entry without a name still lists.
        let bare = try list(#"{"agent":"codex","models":[{"id":"model-a","default_effort":"medium"}],"locked":"fixed"}"#)
        #expect(bare.models[0].name == "model-a" && bare.models[0].defaultEffort == "medium" && bare.locked == "fixed")
    }

    @Test func theDefaultSendsNoModel() throws {
        var c = LauncherModelChoice(list: try claude)
        #expect(c.canPick && c.sent == nil && c.title == "默认" && c.detail == "claude-opus-5-5")
        #expect(c.choices.map(\.id) == ["opus", "haiku", "gateway/glm-5.3"])
        c.picked = "gateway/glm-5.3"
        #expect(c.sent == "gateway/glm-5.3" && c.title == "glm-5.3" && c.detail == "gateway/glm-5.3")
        c.picked = "opus"
        #expect(c.detail == "opus → claude-opus-5-5")
        c.picked = "default" // the default entry picked by id is still no model
        #expect(c.sent == nil)
        c.picked = "gone" // a pick that is no longer listed
        #expect(c.sent == nil && c.title == "默认")
    }

    @Test func aLockedLauncherSendsNothing() throws {
        var l = try claude
        l.locked = "启动器固定了模型参数，不能覆盖具体模型"
        let c = LauncherModelChoice(list: l, picked: "opus")
        #expect(!c.canPick && c.sent == nil)
        #expect(!LauncherModelChoice(list: LauncherModels(agent: "traex", models: [])).canPick)
    }

    @Test func effortsFollowTheModelThatRuns() throws {
        var c = LauncherModelChoice(list: try claude)
        #expect(c.efforts == ["low", "medium", "high", "xhigh", "max"]) // the default's
        c.picked = "haiku"
        #expect(c.efforts == [])
        c.picked = "gateway/glm-5.3"
        #expect(c.efforts == ["low", "medium", "high"])
        // Model Settings' model runs when nothing is picked.
        c.picked = ""
        c.configured = "gateway/glm-5.3"
        #expect(c.efforts == ["low", "medium", "high"] && c.detail == "模型配置：gateway/glm-5.3")
        c.configured = "not-listed"
        #expect(c.efforts == nil)
    }

    @Test func searchMatchesNameIdResolvedAndDescription() throws {
        let c = LauncherModelChoice(list: try claude)
        #expect(c.matching("").count == 3)
        #expect(c.matching("GLM").map(\.id) == ["gateway/glm-5.3"])
        #expect(c.matching("haiku-5").map(\.id) == ["haiku"])
        #expect(c.matching("from gateway").map(\.id) == ["gateway/glm-5.3"])
        #expect(c.matching("nothing").isEmpty)
    }

    /// Model Settings (#943): an id the list lacks can be used, the current unlisted one stays shown, and Default names what is
    /// inherited.
    @Test func modelConfigurationTakesIdsTheListLacks() throws {
        var c = LauncherModelChoice(list: try claude, configured: "model-chat", configuredLabel: "继承")
        #expect(c.defaultDetail == "继承：model-chat")
        #expect(c.customModelID("  my-gateway/model-x ") == "my-gateway/model-x")
        #expect(c.customModelID("opus") == nil) // listed: pick its row
        #expect(c.customModelID("two words") == nil && c.customModelID("") == nil)
        c.picked = "my-gateway/model-x"
        #expect(c.unlisted == "my-gateway/model-x" && c.title == "默认")
        c.picked = "opus"
        #expect(c.unlisted == nil)
        let decoded = try list(#"{"agent":"claude","machine":"desk","models":[]}"#)
        #expect(decoded.machine == "desk")
    }

    @Test func gptNamesDoNotChangeIDsOrCustomAliases() throws {
        let raw = "gpt-6.1-sol"
        let l = try list("""
        {"agent":"codex","models":[
          {"id":"gpt-6.1","name":"GPT-6.1","default":true},
          {"id":"gpt-6.1-sol","name":"GPT-6.1-Sol"},
          {"id":"gpt-6.1-mini","name":"我的日常模型"},
          {"id":"custom-alias","name":"gpt-6.1-sol","resolved":"gpt-6.1-sol"},
          {"id":"gpt-6.1-codex"},
          {"id":"glm-5.3","name":"GLM 5.3"}
        ]}
        """)
        var c = LauncherModelChoice(list: l, picked: raw)
        #expect(c.title == "6.1 Sol")
        #expect(c.selected?.displayName == SessionModes.modelName(raw))
        #expect(c.selected?.id == raw && c.selected?.name == "GPT-6.1-Sol")
        #expect(c.sent == raw && c.detail == raw)
        #expect(l.models[2].displayName == "我的日常模型")
        #expect(l.models[3].displayName == "gpt-6.1-sol") // explicit alias, not a raw id
        #expect(l.models[4].displayName == "6.1 Codex")
        #expect(l.models[5].displayName == "GLM 5.3")
        let customDefault = AgentModel(id: "custom-model", name: "gpt-6.1-sol", isDefault: true)
        #expect(LauncherModelChoice(list: LauncherModels(agent: "codex", models: [customDefault])).defaultDetail == "gpt-6.1-sol")
        #expect(c.matching("6.1 Sol").contains { $0.id == raw })
        c.picked = ""
        #expect(c.title == "默认" && c.sent == nil && c.defaultDetail == "6.1")
        c.configured = raw
        #expect(c.defaultDetail == "模型配置：6.1 Sol" && c.configured == raw)
        c.picked = "not-listed"
        #expect(c.title == "默认" && c.unlisted == "not-listed" && c.sent == nil)
        let request = APIClient.StartRequest(machineID: "example-box", cwd: "/code", launcher: "codex", model: raw)
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
        #expect(body?["model"] as? String == raw)
        let encoded = try JSONSerialization.jsonObject(with: JSONEncoder().encode(l)) as? [String: Any]
        let entries = encoded?["models"] as? [[String: Any]]
        #expect(entries?[1]["id"] as? String == raw)
        #expect(entries?[1]["name"] as? String == "GPT-6.1-Sol")
    }

    @Test func aStartCarriesThePick() throws {
        let req = APIClient.StartRequest(machineID: "box", cwd: "/code", launcher: "claude", model: "opus")
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(req)) as? [String: Any]
        #expect(body?["model"] as? String == "opus")
        let none = try JSONSerialization.jsonObject(with: JSONEncoder().encode(APIClient.StartRequest(machineID: "box", cwd: "/code", launcher: "claude"))) as? [String: Any]
        #expect(none?["model"] == nil)
    }
}
