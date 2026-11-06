import AppIntents
import SwiftUI
import WidgetKit

/// Control Center / Lock Screen / Action Button: open the assistant to ask something.
struct AskPersonaControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "persona.control.ask") {
            ControlWidgetButton(action: OpenPersonaAssistantIntent()) {
                Label("Ask Persona", systemImage: "sparkles")
            }
        }
        .displayName("Ask Persona")
        .description("Opens the Persona assistant.")
    }
}

/// Control: open the Agents page.
struct PersonaAgentsControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "persona.control.agents") {
            ControlWidgetButton(action: OpenPersonaAgentsIntent()) {
                Label("Agents", systemImage: "person.crop.circle.badge.checkmark")
            }
        }
        .displayName("Persona Agents")
        .description("Opens the Agents tab in Persona.")
    }
}
