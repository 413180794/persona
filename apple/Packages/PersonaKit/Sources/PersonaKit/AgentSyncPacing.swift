import Foundation

/// How often a working agent's chat is pulled in the background (`AgentSync`, docs/apple/ios.md「后台同步」): at most once
/// every `workingGap`. Its transcript grows every second or two, and each pull is a request, a decode of the chat's
/// whole disk cache and a write of it again. The moment its turn ends or it needs the user (when it is likely to be
/// opened) it is pulled at once. Opening it brings the rest over its own subscription.
public enum AgentSyncPacing {
    public static let workingGap: TimeInterval = 60

    /// When a chat with news is pulled: now (nil), or once `workingGap` has passed since its last pull while its
    /// agent is still working.
    public static func heldUntil(working: Bool, lastPull: Date?, now: Date) -> Date? {
        guard working, let lastPull, now.timeIntervalSince(lastPull) < workingGap else { return nil }
        return lastPull.addingTimeInterval(workingGap)
    }
}
