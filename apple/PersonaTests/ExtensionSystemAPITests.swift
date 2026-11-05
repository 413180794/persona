import Foundation
import Testing
@testable import Jarvis

@MainActor struct ExtensionSystemAPITests {
    @Test func widgetRequestsUseTheFamilyAndNewPath() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/extensions/-/widgets", json: #"{"widgets":[{"extension":"sample","id":"today","title":"今天","families":["small"],"refresh_minutes":30}]}"#)
        hub.on("POST", "/api/v1/extensions/sample/-/widget/today", json: #"{"view":{"sections":[{"items":[{"type":"metric","title":"用量","value":"12"}]}]},"expires_at":"2040-01-01T00:00:00Z"}"#)
        let place = try #require(try await hub.client.extensionWidgets().first)
        let answer = try await hub.client.extensionWidget(place, family: "small")
        #expect(answer.expiration != nil && answer.view.sections.count == 1)
        let data = try #require(hub.sent("POST", "/api/v1/extensions/sample/-/widget/today").first?.body)
        let body = try JSON.decoder.decode(JSONValue.self, from: data)
        #expect(body["context"]?["size"] == .string("small"))
        #expect(body["context"]?["ui_version"] == .number(3))
        #expect(body["context"]?["platform"] == .string("ios"))
        do {
            _ = try await hub.client.extensionWidget(place, family: "large")
            Issue.record("未支持的尺寸不应发请求")
        } catch { #expect((error as? APIError)?.status == 400) }
        #expect(hub.sent("POST", "/api/v1/extensions/sample/-/widget/today").count == 1)
    }

    @Test func shortcutsAndReportsReuseGeneratedWireTypes() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/extensions/-/shortcuts", json: #"{"shortcuts":[{"extension":"sample","id":"budget","title":"设预算"}]}"#)
        hub.on("POST", "/api/v1/extensions/sample/-/shortcut/budget", json: #"{"text":"已设预算"}"#)
        hub.on("PUT", "/api/v1/devices/device/widgets", json: #"{"widgets":[]}"#)
        let shortcut = try #require(try await hub.client.extensionShortcuts().first)
        let answer = try await hub.client.runExtensionShortcut(shortcut, values: ["amount": "100", "enabled": "true"])
        #expect(answer.text == "已设预算")
        let request = try #require(hub.sent("POST", "/api/v1/extensions/sample/-/shortcut/budget").first)
        let body = try JSON.decoder.decode(JSONValue.self, from: try #require(request.body))
        #expect(body["values"] == .object(["amount": .string("100"), "enabled": .string("true")]))
        try await hub.client.reportExtensionWidgets(device: "device", widgets: [])
        let report = try #require(hub.sent("PUT", "/api/v1/devices/device/widgets").first?.body)
        #expect(try JSON.decoder.decode(ExtensionDeviceWidgets.self, from: report).widgets.isEmpty)
        #expect(!hub.requests.contains { $0.path.contains("/plugins/") })
    }
}
