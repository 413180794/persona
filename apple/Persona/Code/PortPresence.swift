import Foundation
import SwiftUI

nonisolated struct PortProbe: Decodable, Sendable {
    var port: Int
    var listening: Bool
    var http: Bool?
    var title: String?
}

extension APIClient {
    /// Whether anything listens on a machine's port, and its page's title (nothing is opened).
    func probePort(machine: String, port: Int) async throws -> PortProbe {
        try await get("/api/v1/machines/\(machine)/ports/\(port)")
    }

    /// The user opens a port no agent is known to listen on: allowed from now on if something answers (404 if not).
    func openPort(machine: String, port: Int) async throws -> PortProbe {
        try await post("/api/v1/machines/\(machine)/ports/\(port)/open")
    }
}

/// What the app knows of the ports links point at (docs/ports.md「在 App 里」): an agent's listed ports
/// (`Session.ports`) are live; a link to any other port is asked about (GET …/ports/{port}) as its badge appears and
/// remembered a little while, like `FilePresence`. A badge whose port is known gone is gray.
@MainActor @Observable
final class PortPresence {
    enum State: Equatable, Sendable {
        case live(title: String?)
        case gone
    }

    private struct Key: Hashable {
        var machine: MachineRef
        var port: Int
    }

    private var known: [Key: (state: State, at: Date)] = [:]
    @ObservationIgnored private var asking: Set<Key> = []

    /// A port's state: listed by an agent on the machine, else what the hub said last; nil while unknown.
    func state(_ port: Int, machine: MachineRef, listed: [ListeningPort]) -> State? {
        if let p = listed.first(where: { $0.port == port }) { return .live(title: p.title) }
        return known[Key(machine: machine, port: port)]?.state
    }

    /// Asks about ports not listed and not known (or known over a minute ago, half that when gone).
    func ask(_ ports: [Int], machine: MachineRef, listed: [ListeningPort], api: APIClient?) {
        guard let api, !api.config.isDemo else { return }
        for port in ports where !listed.contains(where: { $0.port == port }) {
            let key = Key(machine: machine, port: port)
            if let k = known[key], Date.now.timeIntervalSince(k.at) < (k.state == .gone ? 30 : 60) { continue }
            guard !asking.contains(key) else { continue }
            asking.insert(key)
            Task { [weak self] in
                let res = try? await api.probePort(machine: machine.id, port: port)
                guard let self else { return }
                self.asking.remove(key)
                if let res { self.known[key] = (res.listening ? .live(title: res.title) : .gone, .now) }
            }
        }
    }

    func noteGone(_ port: Int, machine: MachineRef) { known[Key(machine: machine, port: port)] = (.gone, .now) }

    func noteLive(_ port: Int, machine: MachineRef, title: String?) {
        known[Key(machine: machine, port: port)] = (.live(title: title), .now)
    }
}

/// Opening a port link: the hub is told (a port no agent listed), the phone listens, and the page's address there.
extension AppModel {
    /// The phone's address for the link, or why it cannot be opened (nothing listens there: the badge turns gray).
    func forwardedURL(_ link: PortLink, machine: MachineRef) async throws -> URL {
        guard let api = api(machine.group) else { throw APIError(status: 0, message: "没有连接 hub") }
        if api.config.isDemo { throw APIError(status: 403, message: "体验模式不能打开端口") }
        let listed = agentPorts(on: machine)
        if !listed.contains(where: { $0.port == link.port }) {
            do {
                let res = try await api.openPort(machine: machine.id, port: link.port)
                portPresence.noteLive(link.port, machine: machine, title: res.title)
            } catch let e as APIError where e.status == 404 {
                portPresence.noteGone(link.port, machine: machine)
                throw APIError(status: 404, message: "端口 :\(link.port) 已经没有在监听")
            }
        }
        return try await PortForwarder.shared.url(for: link, machine: machine.id, config: api.config)
    }

    /// The ports the agents on a machine listen on, one per port.
    func agentPorts(on machine: MachineRef) -> [ListeningPort] {
        var out: [ListeningPort] = []
        for s in sessionsByMachine[machine] ?? [] {
            for p in s.ports ?? [] where !out.contains(where: { $0.port == p.port }) { out.append(p) }
        }
        return out
    }

    /// The names a machine goes by in links (its host name).
    func hostnames(of machine: MachineRef) -> [String] {
        [self.machine(machine)?.hostname].compactMap { $0 }
    }
}
