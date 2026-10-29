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
    private(set) var shown: Phase = .online
    private(set) var networkUp = true
    private(set) var catchingUp = false
    /// The last time everything was fine (for the details: 「上次连上 3 分钟前」).
    private(set) var lastOnline: Date?
    static let delay: Duration = .milliseconds(1500)
    /// A catch-up that never finishes (a chat whose machine went away) stops counting after this.
    static let catchUpLimit: Duration = .seconds(8)

    /// Everything is fetched again after a reconnect: messages that waited for the network can go.
    @ObservationIgnored var onCaughtUp: (() -> Void)?

    @ObservationIgnored private let live: LiveConnection
    @ObservationIgnored private let monitor = NWPathMonitor()
    @ObservationIgnored private var due: Task<Void, Never>?
    @ObservationIgnored private var awaiting = Set<String>()
    @ObservationIgnored private var catchUpID = 0
    #if DEBUG
    /// `-showConnection waiting|connecting|updating`: the titles show that state (screenshots).
    @ObservationIgnored private let forced: Phase? = switch UserDefaults.standard.string(forKey: "showConnection") {
    case "waiting": .waitingForNetwork
    case "connecting": .connecting
    case "updating": .updating
    default: nil
    }
    /// `-simulateOffline YES`: the network path reads as down (the simulator always has the Mac's network).
    @ObservationIgnored private let offline = UserDefaults.standard.bool(forKey: "simulateOffline")
    #endif

    init(live: LiveConnection) {
        self.live = live
        #if DEBUG
        if let forced { shown = forced }
        if offline { networkUp = false }
        #endif
        monitor.pathUpdateHandler = { [weak self] path in
            let up = path.status == .satisfied
            Task { @MainActor in self?.pathChanged(up) }
        }
        monitor.start(queue: .main)
    }

    /// The state the link is in right now, before the delay.
    var phase: Phase {
        Self.phase(network: networkUp, link: live.state, stalled: live.stalled, catchingUp: catchingUp)
    }

    static func phase(network: Bool, link: LiveConnection.State, stalled: Bool, catchingUp: Bool) -> Phase {
        if link == .idle { return .online } // not paired, or in the background (the socket is closed on purpose)
        if !network { return .waitingForNetwork }
        if link != .connected || stalled { return .connecting }
        return catchingUp ? .updating : .online
    }

    /// Something that decides the phase changed: show it after the delay, or go back to the title right away.
    func update() {
        #if DEBUG
        if forced != nil { return }
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
        if up && !was {
            live.reconnectNow() // the backoff may be at 20 s: Telegram reconnects the moment the network is back
        } else if up {
            live.probe() // another interface: the old socket may be dead without knowing it
        }
        update()
    }

    /// After a reconnect: `parts` are what is being fetched again; each reports back with `caughtUp`.
    func startCatchUp(_ parts: [String]) {
        catchUpID += 1
        let id = catchUpID
        awaiting = Set(parts)
        catchingUp = !awaiting.isEmpty
        update()
        guard catchingUp else { onCaughtUp?(); return }
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.catchUpLimit)
            guard let self, self.catchUpID == id, self.catchingUp else { return }
            self.awaiting = []
            self.finishCatchUp()
        }
    }

    func caughtUp(_ part: String) {
        guard catchingUp, awaiting.remove(part) != nil, awaiting.isEmpty else { return }
        finishCatchUp()
    }

    private func finishCatchUp() {
        catchingUp = false
        update()
        onCaughtUp?()
    }
}
