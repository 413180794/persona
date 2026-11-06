import AppIntents
import SwiftUI
import WidgetKit

/// Control Center / Lock Screen / Action Button: open the assistant to ask something.
// Compatibility: the system addresses this type by its original module/type identity.
struct AskJarvisControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "jarvis.control.ask") {
            ControlWidgetButton(action: OpenJarvisAssistantIntent()) {
                Label("Ask Persona", systemImage: "sparkles")
            }
        }
        .displayName(String(localized: "Ask Persona"))
        .description(String(localized: "Opens the Persona assistant."))
    }
}

/// Control: open the Agents page.
// Compatibility: the system addresses this type by its original module/type identity.
struct JarvisAgentsControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "jarvis.control.agents") {
            ControlWidgetButton(action: OpenJarvisAgentsIntent()) {
                Label("Agents", systemImage: "person.crop.circle.badge.checkmark")
            }
        }
        .displayName("Persona Agents")
        .description(String(localized: "Opens the Agents tab in Persona."))
    }
}
