import Foundation

/// 系统级位置只存在于新路径；复用 schema 类型，不把 App 的视图模型带进系统扩展。
nonisolated extension APIClient {
    func extensionWidgets() async throws -> [ExtensionWidgetPlace] {
        let answer: ExtensionWidgetPlaces = try await get("/api/v1/extensions/-/widgets")
        return answer.widgets
    }

    func extensionWidget(_ place: ExtensionWidgetPlace, family: String) async throws -> ExtensionWidgetAnswer {
        guard place.families.contains(family), ExtensionWidgetFamily.all.contains(family) else {
            throw APIError(status: 400, message: String(localized: "这个扩展不支持该小组件尺寸"))
        }
        let request = ExtensionWidgetRequest(context: .here(size: family))
        return try JSON.decoder.decode(ExtensionWidgetAnswer.self, from: try await self.request(
            "POST", "/api/v1/extensions/\(place.extension)/-/widget/\(place.id)", body: request))
    }

    func reportExtensionWidgets(device: String, widgets: [ExtensionDeviceWidget]) async throws {
        _ = try await request("PUT", "/api/v1/devices/\(device)/widgets", body: ExtensionDeviceWidgets(widgets: widgets))
    }

    func extensionShortcuts() async throws -> [ExtensionShortcutPlace] {
        let answer: ExtensionShortcutPlaces = try await get("/api/v1/extensions/-/shortcuts")
        return answer.shortcuts
    }

    func runExtensionShortcut(_ place: ExtensionShortcutPlace, values: [String: String]) async throws -> ExtensionShortcutResult {
        try JSON.decoder.decode(ExtensionShortcutResult.self, from: try await request(
            "POST", "/api/v1/extensions/\(place.extension)/-/shortcut/\(place.id)",
            body: ExtensionShortcutRequest(values: values, context: .here()), timeout: 30))
    }
}

nonisolated extension ExtensionWidgetAnswer {
    var expiration: Date? {
        guard let data = try? JSON.encoder.encode(expiresAt) else { return nil }
        return try? JSON.decoder.decode(Date.self, from: data)
    }
}
