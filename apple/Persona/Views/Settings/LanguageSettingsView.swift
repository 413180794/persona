import SwiftUI
#if os(macOS)
import AppKit
#endif

/// Settings › Appearance › Language (docs/i18n.md「选哪种语言」, docs/ios.md「界面语言」): Match System / 「简体中文」 / English / 「日本語」.
/// The choice is written where Foundation reads it when the app starts, so it shows the next time the app opens;
/// until then the page says so (the Mac can reopen itself).
struct LanguageSettingsPage: View {
    @State private var choice = LanguageStore.shared.choice

    private var store: LanguageStore { .shared }

    var body: some View {
        SettingsForm {
            Section {
                // Wrap the MainActor-isolated method in a closure instead of passing `set: pick`: Xcode 26.6
                // (Swift 6.3) crashes IRGen emitting the reabstraction thunk for the isolated method value when
                // building for macOS (#4157). Keep the wrapper until the compiler is fixed.
                Picker("语言", selection: Binding(get: { choice }, set: { pick($0) })) {
                    ForEach(AppLanguage.allCases) { language in
                        Text(language == .system ? systemTitle : language.title)
                            .lineLimit(1)
                            .tag(language)
                            .accessibilityIdentifier("language-\(language.rawValue)")
                    }
                }
                .pickerStyle(.inline)
                .labelsHidden()
            } footer: {
                Text("跟随系统时：系统的第一语言是中文（含繁体）就用简体中文，日文用日本語，其他都用 English。Apple Watch、灵动岛和小组件跟着这里，hub 发来的通知和助手的回答也用这种语言。")
            }
            if store.needsRelaunch {
                Section {
                    VStack(alignment: .leading, spacing: Theme.Space.s) {
                        Text(relaunchNote)
                            .font(Theme.Font.body)
                        #if os(macOS)
                        Button(String(localized: "立即重新打开"), action: Self.relaunch)
                            .themedButton(prominent: true)
                            .lineLimit(1)
                            .accessibilityIdentifier("language-relaunch")
                        #endif
                    }
                    .padding(.vertical, Theme.Space.xs)
                }
            }
        }
        .navigationTitle("语言")
        #if os(iOS) || os(visionOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }

    /// Match System, and what it comes to on this device.
    private var systemTitle: String {
        let name = AppLanguage(rawValue: AppLanguage.resolve(store.systemFirst))?.title ?? ""
        return String(localized: "跟随系统（\(name)）")
    }

    private var relaunchNote: String {
        let name = AppLanguage(rawValue: store.current)?.title ?? store.current
        #if os(macOS)
        return String(localized: "重新打开 Persona 后换成 \(name)。")
        #else
        return String(localized: "完全退出 Persona（在 App 切换器里上划）再打开后换成 \(name)。")
        #endif
    }

    private func pick(_ language: AppLanguage) {
        store.choice = language
        choice = language
        #if os(iOS)
        WatchSync.shared.push(language: store.current)
        #endif
    }

    #if os(macOS)
    /// Opens a new copy of the app, then quits this one.
    static func relaunch() {
        let config = NSWorkspace.OpenConfiguration()
        config.createsNewApplicationInstance = true
        NSWorkspace.shared.openApplication(at: Bundle.main.bundleURL, configuration: config) { _, _ in
            Task { @MainActor in NSApp.terminate(nil) }
        }
    }
    #endif
}

extension AppLanguage {
    /// The row's value in Settings › Appearance: what the interface will be in.
    static var settingsSummary: String {
        let store = LanguageStore.shared
        let name = AppLanguage(rawValue: store.current)?.title ?? store.current
        return store.choice == .system ? String(localized: "跟随系统（\(name)）") : name
    }
}
