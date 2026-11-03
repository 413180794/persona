import AppKit
import Foundation
import Network
import ServiceManagement

/// A hub address jarvis-link may try, as it reports them (internal/link).
struct LinkEndpoint: Decodable, Hashable, Identifiable {
    var url: String
    var kind: String
    var manual: Bool?
    var id: String { url }
}

/// One status line from `jarvis-link run` (internal/link Status).
struct LinkStatus: Decodable, Equatable {
    var state: String // unpaired | connecting | connected | waiting | refused
    var machine: String?
    var address: String?
    var kind: String?
    var since: Date?
    var node: String? // online | offline | unknown
    var nodeError: String?
    var error: String?
    var retry: Date?
    var endpoints: [LinkEndpoint]?
    /// What the hub calls itself, and (while connected) how many hubs its mesh has; older hubs send neither.
    var hubName: String?
    var meshHubs: Int?
    /// active (this Mac dials the hub) | passive (the hub dials this Mac); older helpers say nothing: active.
    var mode: String?
    /// Passive mode: where it listens, the addresses the hub may use, when the hub last came in and from where, and
    /// until when a pairing link is good. Connected: `address` is the hub's address.
    var listen: String?
    var addrs: [String]?
    var lastSeen: Date?
    var from: String?
    var codeExpires: Date?

    var passive: Bool { mode == "passive" }

    enum CodingKeys: String, CodingKey {
        case state, machine, address, kind, since, node, error, retry, endpoints, mode, listen, addrs, from
        case nodeError = "node_error"
        case hubName = "hub_name"
        case meshHubs = "mesh_hubs"
        case lastSeen = "last_seen"
        case codeExpires = "code_expires"
    }
}

/// Passive mode's part of link.json, as far as the app shows it (never the credential's hash).
struct PassivePairing: Decodable, Equatable {
    var listen: String?
    var hubName: String?
    var machine: String?
    var paired: Bool

    enum CodingKeys: String, CodingKey {
        case listen, machine
        case hubName = "hub_name"
        case tokenHash = "token_sha256"
    }

    init(listen: String? = nil, hubName: String? = nil, machine: String? = nil, paired: Bool) {
        self.listen = listen
        self.hubName = hubName
        self.machine = machine
        self.paired = paired
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        listen = try c.decodeIfPresent(String.self, forKey: .listen)
        hubName = try c.decodeIfPresent(String.self, forKey: .hubName)
        machine = try c.decodeIfPresent(String.self, forKey: .machine)
        paired = !(try c.decodeIfPresent(String.self, forKey: .tokenHash) ?? "").isEmpty
    }
}

/// The resident node's service, as `jarvis-link node status --json` says.
struct NodeService: Decodable, Equatable {
    var installed: Bool
    var file: String?
    var hasBin: Bool
    var running: String?

    enum CodingKeys: String, CodingKey {
        case installed, file, running
        case hasBin = "has_bin"
    }
}

/// What the menu bar shows: the four states of the link, a link that is up while the node is not, and passive mode
/// listening for the hub.
enum LinkLook {
    case connected, nodeMissing, connecting, listening, down, unpaired

    var symbol: String {
        switch self {
        case .connected: "personalhotspot"
        case .nodeMissing: "exclamationmark.triangle"
        case .connecting: "arrow.triangle.2.circlepath"
        case .listening: "antenna.radiowaves.left.and.right"
        case .down: "personalhotspot.slash"
        case .unpaired: "link.badge.plus"
        }
    }
}

enum LinkText {
    static let kinds = ["lan": "内网", "ipv6": "公网 IPv6", "name": "域名", "ipv4": "公网 IPv4"]

    static func host(_ url: String?) -> String {
        guard let url, let c = URLComponents(string: url), let host = c.host else { return url ?? "" }
        let shown = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return c.port.map { "\(shown):\($0)" } ?? shown
    }

    static func duration(since: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(since)))
        switch s {
        case ..<60: return "不到 1 分钟"
        case ..<3600: return "\(s / 60) 分钟"
        case ..<86400: return "\(s / 3600) 小时 \(s % 3600 / 60) 分"
        default: return "\(s / 86400) 天 \(s % 86400 / 3600) 小时"
        }
    }

    /// The hub line: its name, and how many hubs its mesh has when that is known.
    static func hub(name: String, mesh: Int?) -> String {
        switch mesh {
        case let n? where n > 1: "hub「\(name)」· 互联组共 \(n) 台 hub"
        case 1: "hub「\(name)」· 没有互联别的 hub"
        default: "hub「\(name)」"
        }
    }

    static func until(_ date: Date, now: Date) -> String {
        let s = Int(date.timeIntervalSince(now).rounded(.up))
        if s <= 0 { return "马上重试" }
        if s < 60 { return "\(s) 秒后重试" }
        return "\(s / 60) 分钟后重试"
    }

    /// How long ago, in a few words.
    static func ago(_ date: Date, now: Date) -> String {
        let s = max(0, Int(now.timeIntervalSince(date)))
        switch s {
        case ..<60: return "刚刚"
        case ..<3600: return "\(s / 60) 分钟前"
        case ..<86400: return "\(s / 3600) 小时前"
        default: return "\(s / 86400) 天前"
        }
    }

    /// How long a pairing link is still good, in a few words.
    static func left(_ date: Date, now: Date) -> String {
        let s = Int(date.timeIntervalSince(now).rounded(.up))
        if s < 60 { return "不到 1 分钟" }
        return "\(s / 60) 分钟"
    }

    static func mode(_ passive: Bool) -> String { passive ? "让 hub 来连（被动）" : "我去连 hub（主动）" }
}

/// Runs the jarvis-link helper inside the app bundle, keeps it running, and turns its status lines into state.
@MainActor @Observable final class LinkModel {
    static let shared = LinkModel()

    private(set) var status: LinkStatus?
    private(set) var helperDown: String? // why the helper is not running now
    private(set) var launchAtLogin = SMAppService.mainApp.status == .enabled
    private(set) var loginNote: String?
    /// The candidates in link.json (read here, written only by the helper).
    private(set) var endpoints: [LinkEndpoint] = []
    private(set) var pairedMachine: String?
    /// The mode link.json is in, and passive mode's pairing (kept while in active mode, for switching back).
    private(set) var configMode: String?
    private(set) var passivePairing: PassivePairing?
    /// The resident node's service; nil until asked.
    private(set) var nodeService: NodeService?
    private(set) var nodeNote: String?

    @ObservationIgnored private var process: Process?
    @ObservationIgnored private var keepStdin: Pipe?
    @ObservationIgnored private var lines = Data()
    @ObservationIgnored private var restartDelay = 1.0
    @ObservationIgnored private var startedAt = Date()
    @ObservationIgnored private var quitting = false
    @ObservationIgnored private let monitor = NWPathMonitor()
    @ObservationIgnored private var sawFirstPath = false

    nonisolated static let helperURL = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/jarvis-link")
    /// The jarvisd the app carries, for the resident node when this Mac has none (`jarvis-link node install`).
    nonisolated static let jarvisdURL = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/jarvisd")
    /// Set once the app has set up (or found) the resident node by itself: later it only offers to.
    static let nodeSetUpKey = "nodeServiceSetUp"
    nonisolated static let stateEnvironment = LinkStatePaths.serviceEnvironment(
        home: FileManager.default.homeDirectoryForCurrentUser,
        environment: ProcessInfo.processInfo.environment
    )
    nonisolated static let stateURL = LinkStatePaths.root(
        home: FileManager.default.homeDirectoryForCurrentUser,
        environment: stateEnvironment
    )
    nonisolated static var helperEnvironment: [String: String] {
        var environment = stateEnvironment
        environment["PERSONA_HOME"] = stateURL.path
        environment["JARVIS_HOME"] = stateURL.path
        return environment
    }
    nonisolated static let configURL = stateURL.appendingPathComponent("link.json")
    nonisolated static let logURL = stateURL.appendingPathComponent("link.log")

    var look: LinkLook {
        if helperDown != nil { return .down }
        switch status?.state {
        case "connected": return status?.node == "offline" ? .nodeMissing : .connected
        case "listening": return status?.node == "offline" ? .nodeMissing : .listening
        case "connecting", nil: return .connecting
        case "unpaired": return .unpaired
        default: return .down
        }
    }

    /// Passive mode: what the helper says, else what link.json says.
    var passive: Bool { status?.mode.map { $0 == "passive" } ?? (configMode == "passive") }

    func start() {
        readConfig()
        startHelper()
        Task { await setUpNodeOnce() }
        monitor.pathUpdateHandler = { _ in
            Task { @MainActor in LinkModel.shared.networkChanged() }
        }
        monitor.start(queue: .main)
    }

    /// Used by the snapshots: a status without a helper.
    func show(_ status: LinkStatus?, helperDown: String? = nil, endpoints: [LinkEndpoint] = [], machine: String? = nil,
              mode: String? = nil, passive: PassivePairing? = nil, node: NodeService? = nil) {
        self.status = status
        self.helperDown = helperDown
        self.endpoints = endpoints
        self.pairedMachine = machine
        self.configMode = mode ?? status?.mode
        self.passivePairing = passive
        self.nodeService = node
    }

    private func startHelper() {
        guard !quitting else { return }
        let p = Process()
        p.executableURL = Self.helperURL
        p.environment = Self.helperEnvironment
        p.arguments = ["run", "--parent"] // exits when its stdin closes: when this app goes away, however it does
        let input = Pipe(), output = Pipe()
        p.standardInput = input
        p.standardOutput = output
        p.standardError = logHandle()
        output.fileHandleForReading.readabilityHandler = { handle in
            let chunk = handle.availableData
            guard !chunk.isEmpty else { return }
            Task { @MainActor in LinkModel.shared.consume(chunk) }
        }
        p.terminationHandler = { proc in
            let code = proc.terminationStatus
            Task { @MainActor in LinkModel.shared.helperExited(code) }
        }
        do {
            try p.run()
            process = p
            keepStdin = input
            startedAt = Date()
            helperDown = nil
        } catch {
            helperDown = "后台程序启动不了：\(error.localizedDescription)"
            scheduleRestart()
        }
    }

    private func logHandle() -> FileHandle {
        let fm = FileManager.default
        try? fm.createDirectory(at: Self.logURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        if let size = try? fm.attributesOfItem(atPath: Self.logURL.path)[.size] as? Int, size > 1 << 20 {
            try? fm.removeItem(at: Self.logURL)
        }
        if !fm.fileExists(atPath: Self.logURL.path) {
            fm.createFile(atPath: Self.logURL.path, contents: nil, attributes: [.posixPermissions: 0o600])
        }
        guard let h = try? FileHandle(forWritingTo: Self.logURL) else { return FileHandle.nullDevice }
        h.seekToEndOfFile()
        return h
    }

    private func consume(_ chunk: Data) {
        lines.append(chunk)
        while let nl = lines.firstIndex(of: 0x0A) {
            let line = lines[lines.startIndex..<nl]
            lines.removeSubrange(lines.startIndex...nl)
            if let s = try? Self.decoder.decode(LinkStatus.self, from: line) {
                if s.endpoints != status?.endpoints || s.machine != status?.machine || s.mode != status?.mode
                    || s.hubName != status?.hubName || s.state != status?.state { readConfig() }
                status = s
            }
        }
    }

    private func helperExited(_ code: Int32) {
        process = nil
        keepStdin = nil
        guard !quitting else { return }
        helperDown = "后台程序退出了（\(code)），正在重启"
        if Date().timeIntervalSince(startedAt) > 60 { restartDelay = 1 }
        scheduleRestart()
    }

    private func scheduleRestart() {
        let delay = restartDelay
        restartDelay = min(restartDelay * 2, 30)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in self?.startHelper() }
    }

    private func signal(_ sig: Int32) {
        if let p = process, p.isRunning { kill(p.processIdentifier, sig) }
    }

    /// The network changed: the helper looks at its link at once instead of waiting for the next heartbeat.
    private func networkChanged() {
        if sawFirstPath { signal(SIGUSR1) }
        sawFirstPath = true
    }

    func reconnect() { signal(SIGUSR2) }

    func setLaunchAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            loginNote = nil
        } catch {
            loginNote = "改不了：\(error.localizedDescription)"
        }
        launchAtLogin = SMAppService.mainApp.status == .enabled
        if on && SMAppService.mainApp.status == .requiresApproval {
            loginNote = "要在 系统设置 › 通用 › 登录项 里允许"
        }
    }

    func quit() {
        quitting = true
        process?.terminate()
        NSApp.terminate(nil)
    }

    func stopHelper() {
        quitting = true
        process?.terminate()
    }

    // MARK: pairing and addresses: one-off runs of the helper, which owns link.json

    func pair(link: String, machine: String) async -> (ok: Bool, message: String) {
        var args = ["pair"]
        let id = machine.trimmingCharacters(in: .whitespacesAndNewlines)
        if !id.isEmpty { args += ["--machine", id] }
        args.append("-")
        let r = await Self.runHelper(args, stdin: link)
        readConfig()
        if r.ok { signal(SIGUSR1) }
        return r
    }

    func addAddress(_ raw: String) async -> (ok: Bool, message: String) {
        let r = await Self.runHelper(["address", "add", raw.trimmingCharacters(in: .whitespacesAndNewlines)], stdin: nil)
        readConfig()
        return r
    }

    func removeAddress(_ url: String) async {
        _ = await Self.runHelper(["address", "remove", url], stdin: nil)
        readConfig()
    }

    func readConfig() {
        struct File: Decodable {
            var machine: String?
            var token: String?
            var endpoints: [LinkEndpoint]?
            var mode: String?
            var passive: PassivePairing?
        }
        guard let data = try? Data(contentsOf: Self.configURL), let f = try? JSONDecoder().decode(File.self, from: data) else {
            endpoints = []
            pairedMachine = nil
            configMode = nil
            passivePairing = nil
            return
        }
        endpoints = Self.ranked(f.endpoints ?? [])
        pairedMachine = (f.token ?? "").isEmpty ? nil : f.machine
        configMode = f.mode
        passivePairing = f.passive
    }

    // MARK: passive mode

    /// `jarvis-link pair --listen`: the link for the hub's admin page (stdout), or why not.
    func pairListen(listen: String) async -> (ok: Bool, message: String, link: String?) {
        var args = ["pair", "--listen"]
        let l = listen.trimmingCharacters(in: .whitespacesAndNewlines)
        if !l.isEmpty { args.append(l) }
        let r = await Self.runHelper(args, stdin: nil, split: true)
        readConfig()
        guard r.ok, r.out.hasPrefix("jarvis://connector?") else { return (false, r.message, nil) }
        return (true, r.err, r.out)
    }

    /// `jarvis-link mode <mode>`: switches without pairing again; the helper follows within a second.
    func setMode(_ mode: String) async -> (ok: Bool, message: String) {
        let r = await Self.runHelper(["mode", mode], stdin: nil)
        readConfig()
        return (r.ok, r.message)
    }

    // MARK: the resident node

    func refreshNodeService() async {
        let r = await Self.runHelper(["node", "status", "--json"], stdin: nil, split: true)
        nodeService = try? JSONDecoder().decode(NodeService.self, from: Data(r.out.utf8))
    }

    /// `jarvis-link node install --jarvisd <the app's>`: the resident node's launchd agent, unless there is one.
    func installNode() async {
        nodeNote = nil
        let r = await Self.runHelper(["node", "install", "--jarvisd", Self.jarvisdURL.path], stdin: nil)
        if !r.ok { nodeNote = r.message }
        await refreshNodeService()
    }

    /// The first time the app runs it sets up the resident node itself (one app is the whole node service); after
    /// that it only offers to, so a node service someone removed on purpose stays removed.
    private func setUpNodeOnce() async {
        await refreshNodeService()
        guard !UserDefaults.standard.bool(forKey: Self.nodeSetUpKey), let s = nodeService else { return }
        if !s.installed && FileManager.default.isExecutableFile(atPath: Self.jarvisdURL.path) { await installNode() }
        if nodeService?.installed == true { UserDefaults.standard.set(true, forKey: Self.nodeSetUpKey) }
    }

    /// The order jarvis-link tries them in (internal/link ranked).
    static func ranked(_ eps: [LinkEndpoint]) -> [LinkEndpoint] {
        let rank = ["lan": 0, "ipv6": 1, "name": 2]
        return eps.enumerated().sorted { a, b in
            let ra = rank[a.element.kind] ?? 3, rb = rank[b.element.kind] ?? 3
            if ra != rb { return ra < rb }
            let ma = a.element.manual == true, mb = b.element.manual == true
            if ma != mb { return ma }
            return a.offset < b.offset
        }.map(\.element)
    }

    nonisolated static func runHelper(_ args: [String], stdin: String?) async -> (ok: Bool, message: String) {
        let r = await runHelper(args, stdin: stdin, split: true)
        return (r.ok, r.message)
    }

    /// Runs the helper once: `message` is its stdout when it worked, else its stderr (or stdout); `out` and `err` apart.
    nonisolated static func runHelper(_ args: [String], stdin: String?, split: Bool) async
        -> (ok: Bool, message: String, out: String, err: String) {
        await withCheckedContinuation { done in
            let p = Process()
            p.executableURL = helperURL
            p.environment = helperEnvironment
            p.arguments = args
            let out = Pipe(), err = Pipe(), input = Pipe()
            p.standardOutput = out
            p.standardError = err
            p.standardInput = input
            p.terminationHandler = { proc in
                let o = (String(data: out.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? "")
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                let e = (String(data: err.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? "")
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                let ok = proc.terminationStatus == 0
                done.resume(returning: (ok, ok ? o : (e.isEmpty ? o : e), o, e))
            }
            do {
                try p.run()
                if let stdin { input.fileHandleForWriting.write(Data(stdin.utf8)) }
                try? input.fileHandleForWriting.close()
            } catch {
                done.resume(returning: (false, "后台程序启动不了：\(error.localizedDescription)", "", ""))
            }
        }
    }

    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        nonisolated(unsafe) let precise = ISO8601DateFormatter()
        precise.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        nonisolated(unsafe) let plain = ISO8601DateFormatter()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            // Go writes up to nanoseconds; ISO8601DateFormatter takes at most milliseconds.
            let trimmed = s.replacingOccurrences(of: #"(\.\d{3})\d+"#, with: "$1", options: .regularExpression)
            if let date = precise.date(from: trimmed) ?? plain.date(from: trimmed) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "bad date \(s)"))
        }
        return d
    }()
}
