import Foundation

/// 共用声明式 UI 上下文；系统扩展只需 Shared，不依赖 App 的视图模型或钥匙串。
nonisolated extension ExtensionUIContext {
    static func here(launcher: String? = nil, machine: String? = nil, item: String? = nil, size: String? = nil) -> Self {
        Self(launcher: launcher, machine: machine, item: item, size: size, tz: TimeZone.current.identifier,
             locale: appLocale, platform: platform, uiVersion: version)
    }

    static let version = 3

    static var platform: String {
        #if os(macOS)
        ExtensionPlatform.mac
        #else
        // 协议当前只有四种平台，Watch 和 visionOS 沿用 Apple 移动端的 ios。
        ExtensionPlatform.ios
        #endif
    }

    static var appLocale: String {
        let lang = Bundle.main.preferredLocalizations.first ?? Locale.current.language.languageCode?.identifier ?? "zh-Hans"
        guard let region = Locale.current.region?.identifier, !lang.contains("-\(region)") else { return lang }
        return lang + "-" + region
    }
}
