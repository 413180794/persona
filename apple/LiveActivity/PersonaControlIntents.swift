import AppIntents
import Foundation

// Intents behind the Control Center / Lock Screen / Action Button controls (PersonaWidgets/JarvisControls.swift).
// Compiled into the app and the widget extension; they open the app, so the bodies only run in the app.

/// Opens Persona on the assistant tab, ready to type or dictate.
// Compatibility: the system addresses this type by its original module/type identity.
struct OpenJarvisAssistantIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "打开 Persona 助手"
    nonisolated static let description = IntentDescription("打开 Persona 的助手页，接着说或打字。")
    nonisolated static var supportedModes: IntentModes { .foreground(.immediate) }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        await MainActor.run { AppModel.shared.selectedTab = .assistant }
        #endif
        return .result()
    }
}

/// Opens Persona on the Agents tab (personal agents are one tap from there).
// Compatibility: the system addresses this type by its original module/type identity.
struct OpenJarvisAgentsIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "打开 Persona Agents"
    nonisolated static let description = IntentDescription("打开 Persona 的 Agents 页。")
    nonisolated static var supportedModes: IntentModes { .foreground(.immediate) }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        await MainActor.run { AppModel.shared.selectedTab = .chats }
        #endif
        return .result()
    }
}
