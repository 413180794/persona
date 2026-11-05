// i18n-ignore-file: test fixture app (separate project) for Dynamic Island sharing, not shipped
import ActivityKit
import SwiftUI
import WidgetKit

@main
struct PeerBundle: WidgetBundle {
    var body: some Widget { PeerActivity() }
}

struct PeerActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: PeerAttributes.self) { context in
            Text("车辆 \(context.state.minutes) 分钟后到达").padding()
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.center) { Text("车辆 \(context.state.minutes) 分钟后到达") }
            } compactLeading: {
                Image(systemName: "car.fill").foregroundStyle(.green)
            } compactTrailing: {
                Text("\(context.state.minutes)分").bold()
            } minimal: {
                Text("\(context.state.minutes)分").bold()
            }
        }
    }
}
