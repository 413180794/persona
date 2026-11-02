import Foundation
#if canImport(WatchConnectivity)
import WatchConnectivity
#endif

/// Each group's credential the watch uses, and the groups' names (docs/multi-hub.md「手表」).
nonisolated struct WatchConfigs: Codable, Sendable, Equatable {
    var configs: [String: ServerConfig]
    var names: [String: String] = [:]
}

/// What the watch needs to render without its own WebSocket: live sessions and pending interactions.

nonisolated struct WatchSummary: Codable, Sendable {
    var sessions: [Session]
    var interactions: [Interaction]
    var machines: [Machine]
    /// Open inbox items that need the user (docs/inbox.md); nil from an older phone.
    var inbox: [InboxItem]? = nil
    var at: Date = .now
}

#if canImport(WatchConnectivity)
/// iPhone → Watch sync over WatchConnectivity: the server pairing and a rolling summary.
/// The watch talks to the hub directly for actions (answering, sending), so the phone only seeds it.
final class WatchSync: NSObject, WCSessionDelegate {
    static let shared = WatchSync()

    var onConfig: ((ServerConfig) -> Void)?
    /// Every group's credential (a phone that knows several hubs).
    var onConfigs: ((WatchConfigs) -> Void)?
    var onSummary: ((WatchSummary) -> Void)?
    /// Called once the session is activated; the phone pushes its pairing then (pushes before that are dropped).
    var onActivated: (() -> Void)?
    /// iPhone side: answers the watch's "send me the pairing" request.
    var configProvider: (() -> ServerConfig?)?
    var configsProvider: (() -> WatchConfigs?)?
    var onRecording: ((RecordingTransfer) -> Void)?
    var onRecordingReceipt: ((RecordingReceipt) -> Void)?
    var onRecordingEvent: (() -> Void)?

    private var lastSummaryPush = Date.distantPast
    private var pendingSummary: WatchSummary?

    func activate() {
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    /// Every group's credential for the watch (docs/multi-hub.md「手表」), and what the groups are called; `config`
    /// (the first group's) stays for a watch app from before several hubs.
    func push(configs: [String: ServerConfig], names: [String: String]) {
        guard let first = configs[""] ?? configs.values.first else { return }
        let all = (try? JSONEncoder().encode(WatchConfigs(configs: configs, names: names))) ?? Data()
        let data = (try? JSONEncoder().encode(first)) ?? Data()
        update(["config": data, "configs": all])
        #if os(iOS)
        if WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled {
            WCSession.default.transferUserInfo(["config": data, "configs": all])
        }
        #endif
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
        if let data = ctx["configs"] as? Data, let all = try? JSONDecoder().decode(WatchConfigs.self, from: data) {
            onConfigs?(all)
        } else if let data = ctx["config"] as? Data, let cfg = try? JSONDecoder().decode(ServerConfig.self, from: data) {
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
            self.onRecordingEvent?()
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        let recording = RecordingTransfer(dictionary: userInfo)
        let receipt = RecordingReceipt(dictionary: userInfo)
        nonisolated(unsafe) let copy = userInfo
        Task { @MainActor in
            if let recording { self.onRecording?(recording) }
            else if let receipt { self.onRecordingReceipt?(receipt) }
            else { self.apply(copy) }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        nonisolated(unsafe) let reply = replyHandler
        Task { @MainActor in
            var answer: [String: Any] = [:]
            if let cfg = self.configProvider?(), let data = try? JSONEncoder().encode(cfg) { answer["config"] = data }
            if let all = self.configsProvider?(), let data = try? JSONEncoder().encode(all) { answer["configs"] = data }
            reply(answer)
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext ctx: [String: Any]) {
        nonisolated(unsafe) let copy = ctx
        Task { @MainActor in self.apply(copy) }
    }

    nonisolated func sessionReachabilityDidChange(_ session: WCSession) {
        Task { @MainActor in self.onRecordingEvent?() }
    }

    nonisolated func session(_ session: WCSession, didReceive file: WCSessionFile) {
        #if os(iOS)
        guard let transfer = RecordingTransfer(dictionary: file.metadata), let segment = transfer.segment else { return }
        // 回调返回时系统删除临时文件，必须先同步移动，再交给主线程更新索引。
        let dir = RecordingLibrary.directory.appendingPathComponent(transfer.id.uuidString, isDirectory: true)
        let destination = dir.appendingPathComponent(RecordingSegment.fileName(segment.index))
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            if !FileManager.default.fileExists(atPath: destination.path) {
                try FileManager.default.moveItem(at: file.fileURL, to: destination)
            }
            Task { @MainActor in self.onRecording?(transfer) }
        } catch { /* 不确认；手表保留音频并重试。 */ }
        #endif
    }

    nonisolated func session(_ session: WCSession, didFinish fileTransfer: WCSessionFileTransfer, error: Error?) {
        #if os(watchOS)
        guard let transfer = RecordingTransfer(dictionary: fileTransfer.file.metadata) else { return }
        let failed = error != nil
        Task { @MainActor in WatchRecordingTransfer.shared.finished(transfer, failed: failed) }
        #endif
    }

    nonisolated func session(_ session: WCSession, didFinish userInfoTransfer: WCSessionUserInfoTransfer, error: Error?) {
        #if os(watchOS)
        guard let transfer = RecordingTransfer(dictionary: userInfoTransfer.userInfo) else { return }
        let failed = error != nil
        Task { @MainActor in WatchRecordingTransfer.shared.finished(transfer, failed: failed) }
        #endif
    }

    func confirmRecording(_ receipt: RecordingReceipt) {
        guard WCSession.default.activationState == .activated else { return }
        WCSession.default.transferUserInfo(receipt.dictionary)
    }

    #if os(iOS)
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}
    nonisolated func sessionDidDeactivate(_ session: WCSession) { session.activate() }
    #endif
}
#endif
