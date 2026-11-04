import Foundation
import Testing
@testable import Jarvis

/// 设置 › 插件's logic (PluginsModel, PluginModel): what the hub sends decodes, the form's draft is edited by path and
/// saved as a whole without the emptied fields, a field the hub refuses comes back beside that field.
@MainActor
struct PluginsModelTests {
    private static let detail = #"""
    {"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","version":"1.2.0","source":"installed",
     "enabled":true,"contributes":{"connectors":["examplenotes"]},"needs_setup":["密钥「API Key」"],"previous_version":"1.1.0",
     "form":{"fields":[
       {"name":"base_url","type":"string","title":"服务地址","format":"uri","default":"https://notes.example.com","required":true},
       {"name":"folders","type":"array","items":{"name":"items","type":"string"}},
       {"name":"every_minutes","type":"integer","minimum":5,"default":30},
       {"name":"mode","type":"string","enum":[{"value":"fast","title":"只要新的"},{"value":"safe"}]},
       {"name":"proxy","type":"object","title":"代理","fields":[{"name":"host","type":"string"},{"name":"port","type":"integer"}]}]},
     "config":{"folders":["工作"]},
     "secrets":[{"name":"api_key","title":"API Key","required":true,"set":false}],
     "permissions":[{"key":"runs_code","title":"在 hub 机器上运行程序","detail":"…"}]}
    """#

    private func make(_ hub: FakeHub) -> PluginModel { PluginModel(id: "example-notes", deps: .init(api: { hub.client })) }

    @Test func listDecodesAndSummarizes() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins", json: #"{"plugins":[{"id":"voice","name":"语音","kind":"voice","status":"running"},"# +
               #"{"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","source":"installed","enabled":true}],"can_install":true}"#)
        let page = PluginsModel(deps: .init(api: { hub.client }))
        await page.load()
        #expect(page.error == nil && page.plugins?.count == 2)
        #expect(PluginText.listSummary(page.plugins) == "2 个 · 1 个要处理")
        #expect(PluginText.summary(page.plugins![0]) == "运行中")
        #expect(PluginText.listSummary([]) == "没有")
    }

    @Test func detailDecodesTheForm() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/example-notes/-/", json: Self.detail)
        let page = make(hub)
        await page.load()
        let d = try #require(page.detail)
        #expect(d.form.map(\.name) == ["base_url", "folders", "every_minutes", "mode", "proxy"])
        #expect(d.form[0].required == true && d.form[0].default == .string("https://notes.example.com"))
        #expect(d.form[3].enum?.map(\.label) == ["只要新的", "safe"])
        #expect(d.form[4].fields?.map(\.name) == ["host", "port"])
        #expect(d.secrets.first?.set == false && d.previousVersion == "1.1.0" && !d.readOnly)
        #expect(d.plugin.needsSetup == ["密钥「API Key」"])
        #expect(page.draft == ["folders": .array([.string("工作")])] && !page.dirty)
    }

    @Test func theDraftIsSavedAsAWholeWithoutEmptiedFields() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/example-notes/-/", json: Self.detail)
        var sent: JSONValue?
        hub.on("PUT", "/api/v1/plugins/example-notes/-/config") { req in
            sent = try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data())
            return Data(Self.detail.utf8)
        }
        let page = make(hub)
        await page.load()
        page.set(.string("db.example.com"), at: ["proxy", "host"])
        page.set(.string(""), at: ["mode"])
        page.set(nil, at: ["folders"])
        page.numberText["every_minutes"] = "1o"
        #expect(page.dirty && !page.canSave && page.fieldError(["every_minutes"]) == "要是一个数")
        page.numberText["every_minutes"] = "15"
        page.set(.number(15), at: ["every_minutes"])
        #expect(page.canSave)
        #expect(await page.save())
        #expect(sent == .object(["config": .object(["proxy": .object(["host": .string("db.example.com")]), "every_minutes": .number(15)])]))
        // An emptied group goes too.
        page.set(nil, at: ["proxy", "host"])
        #expect(page.draft["proxy"] == nil)
    }

    @Test func aRefusedFieldComesBackBesideIt() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/example-notes/-/", json: Self.detail)
        hub.on("PUT", "/api/v1/plugins/example-notes/-/config") { _ in
            throw APIError(status: 400, message: "proxy.port: 要是一个整数",
                           fields: ["code": "invalid_config", "field": "proxy.port", "message": "要是一个整数"])
        }
        let page = make(hub)
        await page.load()
        page.set(.number(1.5), at: ["proxy", "port"])
        #expect(await page.save() == false)
        #expect(page.fieldError(["proxy", "port"]) == "要是一个整数" && page.error == nil)
        #expect(page.value(["proxy", "port"]) == .number(1.5), "the draft is kept")
    }

    @Test func aSecretIsSentAndOnlyItsStateComesBack() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/example-notes/-/", json: Self.detail)
        var body: String?
        hub.on("PUT", "/api/v1/plugins/example-notes/-/secrets/api_key") { req in
            body = String(decoding: req.body ?? Data(), as: UTF8.self)
            return Data(Self.detail.replacingOccurrences(of: #""set":false"#, with: #""set":true"#).utf8)
        }
        let page = make(hub)
        await page.load()
        #expect(await page.setSecret("api_key", value: "   ") == false, "nothing to save")
        #expect(await page.setSecret("api_key", value: " k3y \n"))
        #expect(body == #"{"value":"k3y"}"#)
        #expect(page.detail?.secrets.first?.set == true)
    }
}
