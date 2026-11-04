import Foundation
import Testing
@testable import Jarvis

/// A market plugin's auto-upgrade (docs/plugins.md「自动升级」): the hub's fields decode, the 升级 section says what
/// happens to a newer version (and when it waits for the user), the switches go to the hub; a hub without the feature
/// shows no switch.
@MainActor
struct PluginUpgradeTests {
    private static let gateway = #"""
    {"id":"example-gateway","name":"Example 网关面板","kind":"ui","status":"running","version":"1.0.0","source":"installed",
     "auto_upgrade":true,
     "upgrade":{"id":"example-gateway","name":"Example 网关面板","market":"m1","from":"1.0.0","to":"1.1.0","state":"needs_permission",
                "added_permissions":[{"key":"network","title":"访问更多网络","detail":"status.example.com"}]},
     "upgrade_note":{"kind":"needs_permission","from":"1.0.0","to":"1.1.0","at":"2026-11-04T14:34:21.439310005Z"},
     "permissions":[]}
    """#

    @Test func aMarketPluginsUpgradeDecodes() throws {
        let d = try JSON.decoder.decode(HubPluginDetail.self, from: Data(Self.gateway.utf8))
        let p = d.plugin
        #expect(p.autoUpgrade == true)
        let u = try #require(p.upgrade)
        #expect(u.market == "m1" && u.from == "1.0.0" && u.to == "1.1.0" && u.state == "needs_permission")
        #expect(PluginText.upgradeDetail(u) == "要多一项权限：访问更多网络（status.example.com）")
        #expect(PluginText.upgradeAction(u) == "去确认")
        #expect(PluginText.summary(p) == PluginText.state(p) + " · 新版本待确认")
        let note = try #require(p.upgradeNote)
        #expect(note.at.timeIntervalSince1970 > 1_700_000_000)
        #expect(PluginText.upgradeNote(note) == nil, "asking is the newer version's line, not news of its own")
        // A plugin from elsewhere (or an older hub) has none of it.
        let plain = try JSON.decoder.decode(HubPlugin.self, from: Data(#"{"id":"x","name":"X","kind":"ui","status":"running"}"#.utf8))
        #expect(plain.autoUpgrade == nil && plain.upgrade == nil && plain.upgradeNote == nil)
    }

    @Test func eachStateSaysWhatHappens() {
        func up(_ state: String, detail: String? = nil) -> PluginUpgrade {
            PluginUpgrade(id: "p", name: "P", market: "m1", from: "1.0.0", to: "1.1.0", state: state, addedPermissions: nil, detail: detail)
        }
        #expect(PluginText.upgradeDetail(up("waiting_idle")) == "插件空闲时自动升级")
        #expect(PluginText.upgradeDetail(up("off")) == "自动升级关着" && PluginText.upgradeAction(up("off")) == "去升级")
        #expect(PluginText.upgradeDetail(up("skipped")) == "上次升级后起不来，已回到 1.0.0，不再自动升级到这个版本")
        #expect(PluginText.upgradeDetail(up("failed", detail: "下载失败")) == "没装上：下载失败")
        #expect(PluginText.upgradeAction(up("waiting_idle")) == nil && PluginText.upgradeAction(up("incompatible")) == nil)
        #expect(PluginText.morePermissions([]) == "要的权限有变化")
        let two = [PluginPermission(key: "network", title: "访问更多网络", detail: "a.example.com"), PluginPermission(key: "hub_api", title: "使用 hub 的接口", detail: nil)]
        #expect(PluginText.morePermissions(two) == "要多两项权限：访问更多网络（a.example.com）、使用 hub 的接口")
        let at = Date()
        #expect(PluginText.upgradeNote(PluginUpgradeNote(kind: "upgraded", from: "0.9.0", to: "1.0.0", at: at)) == "已自动升级到 1.0.0（原来是 0.9.0）")
        #expect(PluginText.upgradeNote(PluginUpgradeNote(kind: "rolled_back", from: "1.0.0", to: "1.1.0", at: at)) == "1.1.0 起不来，已回到 1.0.0")
    }

    @Test func thePluginsSwitchGoesToTheHub() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/plugins/example-gateway/-/", json: Self.gateway)
        var sent: JSONValue?
        hub.on("PUT", "/api/v1/plugins/example-gateway/-/auto-upgrade") { req in
            sent = try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data())
            return Data(Self.gateway.replacingOccurrences(of: #""auto_upgrade":true"#, with: #""auto_upgrade":false"#).utf8)
        }
        let page = PluginModel(id: "example-gateway", deps: .init(api: { hub.client }))
        await page.load()
        await page.setAutoUpgrade(false)
        #expect(sent == .object(["auto": .bool(false)]))
        #expect(page.detail?.plugin.autoUpgrade == false && page.error == nil)
    }

    @Test func theGlobalSwitchIsThereOnlyWhenTheHubHasIt() async {
        let hub = FakeHub()
        let page = PluginMarketModel(deps: .init(api: { hub.client }))
        await page.loadAutoUpgrade()
        #expect(page.autoUpgrade == nil, "a hub from before auto-upgrade: no switch")
        hub.on("GET", "/api/v1/plugins/-/upgrades", json: #"{"auto":true,"upgrades":[],"off":[],"last":{}}"#)
        var sent: JSONValue?
        hub.on("PUT", "/api/v1/plugins/-/upgrades") { req in
            sent = try JSON.decoder.decode(JSONValue.self, from: req.body ?? Data())
            return Data(#"{"auto":false,"upgrades":[],"off":[],"last":{}}"#.utf8)
        }
        await page.loadAutoUpgrade()
        #expect(page.autoUpgrade == true)
        await page.setAutoUpgrade(false)
        #expect(sent == .object(["auto": .bool(false)]) && page.autoUpgrade == false && page.error == nil)
    }
}
