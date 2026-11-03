import AppIntents
import SwiftUI
import WidgetKit

/// Control Center / Lock Screen / Action Button: open the assistant to ask something.
// Compatibility: the system addresses this type by its original module/type identity.
struct AskJarvisControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "jarvis.control.ask") {
            ControlWidgetButton(action: OpenJarvisAssistantIntent()) {
                Label("问 Jarvis", systemImage: "sparkles")
            }
        }
        .displayName("问 Jarvis")
        .description("打开 Jarvis 助手。")
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
        .displayName("Jarvis Agents")
        .description("打开 Jarvis 的 Agents 页。")
    }
}
