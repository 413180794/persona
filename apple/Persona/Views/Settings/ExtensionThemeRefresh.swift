import SwiftUI

/// 主题贡献只在前台刷新；远程停用贡献后，已选主题退回默认，网络失败则保留离线配色。
private struct ExtensionThemeRefresh: ViewModifier {
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var phase

    nonisolated private struct Source: Hashable, Sendable {
        var group: String
        var hub: String?
        var features: Set<String>
        var active: Bool
    }

    func body(content: Content) -> some View {
        content.tabActiveTask(id: Source(group: app.hub?.group ?? "", hub: app.api?.config.url.absoluteString,
                                         features: app.hubFeatures, active: phase == .active)) {
            guard phase == .active, !app.hubFeatures.isEmpty else { return }
            await ExtensionThemesModel.shared.refresh(api: app.api)
            guard app.hubFeatures.contains("extensions") else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                guard !Task.isCancelled else { return }
                await ExtensionThemesModel.shared.refresh(api: app.api)
            }
        }
    }
}

extension View {
    func extensionThemes() -> some View { modifier(ExtensionThemeRefresh()) }
}
