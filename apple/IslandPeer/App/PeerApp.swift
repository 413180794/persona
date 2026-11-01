import ActivityKit
import SwiftUI

@main
struct PeerApp: App {
    var body: some Scene {
        WindowGroup {
            Text("Peer: a ride in 8 minutes")
                .task {
                    // `-end`: take the activity away again (the list can still be empty right after launch).
                    if CommandLine.arguments.contains("-end") {
                        for _ in 0..<5 {
                            for a in Activity<PeerAttributes>.activities { await a.end(nil, dismissalPolicy: .immediate) }
                            try? await Task.sleep(for: .milliseconds(400))
                        }
                        return
                    }
                    guard Activity<PeerAttributes>.activities.isEmpty else { return }
                    _ = try? Activity.request(attributes: PeerAttributes(),
                                              content: .init(state: .init(minutes: 8), staleDate: nil))
                }
        }
    }
}
