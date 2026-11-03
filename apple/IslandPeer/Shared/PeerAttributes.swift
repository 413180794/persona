import ActivityKit

/// Another app's Live Activity (a ride arriving in N minutes), to share the Dynamic Island with Persona' Fleet.
struct PeerAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable { var minutes: Int }
}
