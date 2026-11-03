import AppIntents
import SwiftUI
import WidgetKit

/// Control Center / Lock Screen / Action Button: open the assistant to ask something.
// Compatibility: the system addresses this type by its original module/type identity.
struct AskJarvisControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "jarvis.control.ask") {
            ControlWidgetButton(action: OpenJarvisAssistantIntent()) {
                Label("问 Persona", systemImage: "sparkles")
            }
        }
        .displayName("问 Persona")
        .description("打开 Persona 助手。")
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
        .description("打开 Persona 的 Agents 页。")
    }
}
