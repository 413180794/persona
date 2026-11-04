import Foundation
import Network
import Observation

/// What the top of the main screens says about the link to the hub, the way Telegram does it (docs/ios.md「连接状态」):
/// no banner, the title itself turns into a small spinner and 等待网络 / 连接中… / 更新中…, and quietly back.
@Observable
final class ConnectionStatus {
    enum Phase: Equatable {
        case online
        /// The phone has no network at all (NWPathMonitor).
        case waitingForNetwork
        /// There is a network but the hub does not answer: the socket is down, being opened, or stalled.
        case connecting
        /// Connected again and fetching what was missed (interactions, the open chats' backlog).
        case updating

        var title: String {
            switch self {
            case .online: ""
            case .waitingForNetwork: "等待网络"
            case .connecting: "连接中…"
            case .updating: "更新中…"
            }
        }
    }

    /// What the titles show: `phase` once it has lasted `delay`. Reconnecting after coming back to the app or
    /// switching networks usually takes well under that, and the title would just flash.
    private(set) var shown: Phase = .online {
        didSet { if shown != oldValue { onShown?(shown) } }
    }
    /// Told of each new `shown` as it is set (the iPhone shell's floating link pill: docs/shell.md「连接状态和 toast」).
    @ObservationIgnored var onShown: ((Phase) -> Void)?
    private(set) var networkUp = true
    private(set) var catchingUp = false
    /// The last time everything was fine (for the details: 「上次连上 3 分钟前」).
    private(set) var lastOnline: Date?
    static let delay: Duration = .milliseconds(1500)
    /// A catch-up that never finishes (a chat whose machine went away) stops counting after this.
    static let catchUpLimit: Duration = .seconds(8)

    /// Everything is fetched again after a reconnect: messages that waited for the network can go.
    @ObservationIgnored var onCaughtUp: (() -> Void)?

    /// Every group's socket (docs/multi-hub.md); one while there is one group.
    @ObservationIgnored var lives: () -> [LiveConnection] = { [] }
    @ObservationIgnored private let monitor = NWPathMonitor()
    @ObservationIgnored private var due: Task<Void, Never>?
    /// What each group is still fetching after its reconnect.
    @ObservationIgnored private var awaiting: [String: Set<String>] = [:]
    @ObservationIgnored private var catchUpIDs: [String: Int] = [:]
    #if DEBUG
    /// `-showConnection waiting|connecting|updating`: the titles show that state (screenshots).
    @ObservationIgnored private let forced: Phase? = switch UserDefaults.standard.string(forKey: "showConnection") {
    case "waiting": .waitingForNetwork
    case "connecting": .connecting
    case "updating": .updating
    default: nil
    }
    /// `-showConnectionCycle <waiting|connecting|updating>:<off>:<on>`: the link is fine for `off` seconds, then in that
    /// state for `on` seconds, over and over (UI tests of what shows it, coming and going, whenever they are ready).
    @ObservationIgnored private let cycle: (phase: Phase, at: Double, length: Double)? = {
        let parts = (UserDefaults.standard.string(forKey: "showConnectionCycle") ?? "").split(separator: ":")
        guard parts.count == 3, let at = Double(parts[1]), let length = Double(parts[2]) else { return nil }
        let phase: Phase? = switch parts[0] {
        case "waiting": .waitingForNetwork
        case "connecting": .connecting
        case "updating": .updating
        default: nil
        }
        return phase.map { ($0, at, length) }
    }()
    /// `-simulateOffline YES`: the network path reads as down (the simulator always has the Mac's network).
    @ObservationIgnored private let offline = UserDefaults.standard.bool(forKey: "simulateOffline")
    #endif

    init() {
        #if DEBUG
        if let forced { shown = forced }
        if offline { networkUp = false }
        if let cycle {
            Task { @MainActor [weak self] in
                while self != nil {
                    try? await Task.sleep(for: .seconds(cycle.at))
                    self?.shown = cycle.phase
                    try? await Task.sleep(for: .seconds(cycle.length))
                    self?.shown = .online
                }
            }
        }
        #endif
        monitor.pathUpdateHandler = { [weak self] path in
            let up = path.status == .satisfied
            Task { @MainActor in self?.pathChanged(up) }
        }
        monitor.start(queue: .main)
    }

    /// The state the links are in right now, before the delay.
    var phase: Phase {
        Self.phase(network: networkUp, links: lives().map { ($0.state, $0.stalled) }, catchingUp: catchingUp)
    }

    static func phase(network: Bool, link: LiveConnection.State, stalled: Bool, catchingUp: Bool) -> Phase {
        if link == .idle { return .online } // not paired, or in the background (the socket is closed on purpose)
        if !network { return .waitingForNetwork }
        if link != .connected || stalled { return .connecting }
        return catchingUp ? .updating : .online
    }

    /// Several groups (docs/multi-hub.md「连接状态」): connecting only while none of them is up — one group out of reach
    /// (a work hub behind a VPN) would otherwise keep every title on 连接中…; the hub list and the group chips say which
    /// one is down. One group: exactly the single-link rule.
    static func phase(network: Bool, links: [(state: LiveConnection.State, stalled: Bool)], catchingUp: Bool) -> Phase {
        let active = links.filter { $0.state != .idle }
        guard active.count > 1 else {
            let one = active.first ?? (.idle, false)
            return phase(network: network, link: one.state, stalled: one.stalled, catchingUp: catchingUp)
        }
        if !network { return .waitingForNetwork }
        if !active.contains(where: { $0.state == .connected && !$0.stalled }) { return .connecting }
        return catchingUp ? .updating : .online
    }

    /// Something that decides the phase changed: show it after the delay, or go back to the title right away.
    func update() {
        #if DEBUG
        if forced != nil || cycle != nil { return }
        #endif
        let now = phase
        if now == .online {
            due?.cancel()
            due = nil
            lastOnline = .now
            if shown != .online { shown = .online }
            return
        }
        if shown != .online {
            if shown != now { shown = now }
            return
        }
        guard due == nil else { return }
        due = Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.delay)
            guard let self, !Task.isCancelled else { return }
            self.due = nil
            let p = self.phase
            if p != .online { self.shown = p }
        }
    }

    private func pathChanged(_ up: Bool) {
        #if DEBUG
        if offline { return }
        #endif
        let was = networkUp
        networkUp = up
        for live in lives() {
            if up && !was {
                live.reconnectNow() // the backoff may be at 20 s: Telegram reconnects the moment the network is back
            } else if up {
                live.probe() // another interface: the old socket may be dead without knowing it
            }
        }
        update()
    }

    /// After a group's reconnect: `parts` are what is being fetched again; each reports back with `caughtUp`.
    func startCatchUp(_ group: String = "", _ parts: [String]) {
        catchUpIDs[group, default: 0] += 1
        let id = catchUpIDs[group]!
        awaiting[group] = parts.isEmpty ? nil : Set(parts)
        catchingUp = !awaiting.isEmpty
        update()
        guard awaiting[group] != nil else { onCaughtUp?(); return }
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.catchUpLimit)
            guard let self, self.catchUpIDs[group] == id, self.awaiting[group] != nil else { return }
            self.awaiting[group] = nil
            self.finishCatchUp()
        }
    }

    func caughtUp(_ part: String) {
        guard catchingUp, let group = awaiting.first(where: { $0.value.contains(part) })?.key else { return }
        awaiting[group]?.remove(part)
        guard awaiting[group]?.isEmpty == true else { return }
        awaiting[group] = nil
        finishCatchUp()
    }

    private func finishCatchUp() {
        catchingUp = !awaiting.isEmpty
        update()
        onCaughtUp?()
    }
}
