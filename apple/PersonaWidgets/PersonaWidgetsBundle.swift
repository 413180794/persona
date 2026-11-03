import SwiftUI
import WidgetKit

@main
struct PersonaWidgetsBundle: WidgetBundle {
    var body: some Widget {
        FleetLiveActivity()
        AskJarvisControl()
        JarvisAgentsControl()
    }
}
