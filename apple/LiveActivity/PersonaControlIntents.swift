import AppIntents
import Foundation

// Intents behind the Control Center / Lock Screen / Action Button controls (PersonaWidgets/JarvisControls.swift).
// Compiled into the app and the widget extension; they open the app, so the bodies only run in the app.

/// Opens Persona on the assistant tab, ready to type or dictate.
// Compatibility: the system addresses this type by its original module/type identity.
struct OpenJarvisAssistantIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "Open Persona Assistant"
    nonisolated static let description = IntentDescription("Opens Persona's assistant page so you can talk or type.")
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
    nonisolated static let title: LocalizedStringResource = "Open Persona Agents"
    nonisolated static let description = IntentDescription("Opens the Agents tab in Persona.")
    nonisolated static var supportedModes: IntentModes { .foreground(.immediate) }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        await MainActor.run { AppModel.shared.selectedTab = .chats }
        #endif
        return .result()
    }
}
