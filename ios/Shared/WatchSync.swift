import Foundation
#if canImport(WatchConnectivity)
import WatchConnectivity
#endif

/// What the watch needs to render without its own WebSocket: live sessions and pending interactions.
nonisolated struct WatchSummary: Codable, Sendable {
    var sessions: [Session]
    var interactions: [Interaction]
    var machines: [Machine]
    /// Open inbox items that need the user (docs/inbox.md); nil from an older phone.
    var inbox: [InboxItem]? = nil
    var at: Date = .now
}

/// iPhone → Watch sync over WatchConnectivity: the server pairing and a rolling summary.
/// The watch talks to the hub directly for actions (answering, sending), so the phone only seeds it.
final class WatchSync: NSObject, WCSessionDelegate {
    static let shared = WatchSync()

    var onConfig: ((ServerConfig) -> Void)?
    var onSummary: ((WatchSummary) -> Void)?
    /// Called once the session is activated; the phone pushes its pairing then (pushes before that are dropped).
    var onActivated: (() -> Void)?
    /// iPhone side: answers the watch's "send me the pairing" request.
    var configProvider: (() -> ServerConfig?)?

    private var lastSummaryPush = Date.distantPast
    private var pendingSummary: WatchSummary?

    func activate() {
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func push(config: ServerConfig) {
        let data = (try? JSONEncoder().encode(config)) ?? Data()
        update(["config": data])
        #if os(iOS)
        // Application context only keeps the latest state; user info is queued and delivered even if the watch app
        // was never opened, so a fresh install still gets paired.
        if WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled {
            WCSession.default.transferUserInfo(["config": data])
        }
        #endif
    }

    #if os(watchOS)
    /// Watch side: ask the phone for its pairing right now (needs the phone nearby).
    func requestConfig(done: @escaping @MainActor (Bool) -> Void) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isReachable else {
            done(false)
            return
        }
        // Both handlers run on WatchConnectivity's queue: `@Sendable` keeps them off the main actor (BackgroundCallbackTests).
        WCSession.default.sendMessage(["request": "config"], replyHandler: { @Sendable reply in
            nonisolated(unsafe) let r = reply
            Task { @MainActor in
                self.apply(r)
                done(r["config"] != nil)
            }
        }, errorHandler: { @Sendable _ in Task { @MainActor in done(false) } })
    }
    #endif

    /// Throttled: application context is meant for state, not a stream.
    func push(summary: @autoclosure () -> WatchSummary) {
        #if os(iOS)
        guard WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled else { return }
        pendingSummary = summary()
        let wait = 2 - Date.now.timeIntervalSince(lastSummaryPush)
        if wait <= 0 { flushSummary() } else {
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(wait))
                self.flushSummary()
            }
        }
        #endif
    }

    private func flushSummary() {
        guard let s = pendingSummary, let data = try? JSON.encoder.encode(s) else { return }
        pendingSummary = nil
        lastSummaryPush = .now
        update(["summary": data])
    }

    private func update(_ values: [String: Any]) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated else { return }
        var ctx = WCSession.default.applicationContext
        values.forEach { ctx[$0.key] = $0.value }
        try? WCSession.default.updateApplicationContext(ctx)
    }

    private func apply(_ ctx: [String: Any]) {
        if let data = ctx["config"] as? Data, let cfg = try? JSONDecoder().decode(ServerConfig.self, from: data) {
            onConfig?(cfg)
        }
        if let data = ctx["summary"] as? Data, let s = try? JSON.decoder.decode(WatchSummary.self, from: data) {
            onSummary?(s)
        }
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        nonisolated(unsafe) let ctx = session.receivedApplicationContext
        Task { @MainActor in
            self.apply(ctx)
            self.onActivated?()
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        nonisolated(unsafe) let copy = userInfo
        Task { @MainActor in self.apply(copy) }
    }

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        nonisolated(unsafe) let reply = replyHandler
        Task { @MainActor in
            if let cfg = self.configProvider?(), let data = try? JSONEncoder().encode(cfg) {
                reply(["config": data])
            } else {
                reply([:])
            }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext ctx: [String: Any]) {
        nonisolated(unsafe) let copy = ctx
        Task { @MainActor in self.apply(copy) }
    }

    #if os(iOS)
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}
    nonisolated func sessionDidDeactivate(_ session: WCSession) { session.activate() }
    #endif
}
