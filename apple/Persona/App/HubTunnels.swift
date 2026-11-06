import Foundation

/// A way through to a paired hub when its own addresses do not answer: its SSH tunnel
/// (docs/network.md「经 SSH 隧道连 hub」) or, in builds that have the in-app tunnel core for a hub whose extension
/// contributes one, its in-app tunnel (「经 App 内隧道连 hub」). A hub may have both; each is tried in turn, the SSH
/// one first.
struct HubTunnel: Hashable {
    enum Kind: Hashable {
        case ssh
        case inApp
    }

    let hub: PairedHub
    let kind: Kind

    /// Through it every time, not only when the hub's own addresses do not answer.
    var always: Bool {
        switch kind {
        case .ssh: hub.ssh?.always == true
        case .inApp: hub.inAppTunnel?.always == true
        }
    }

    /// The group went through it last time (kept while the app was in the background).
    var recent: Bool {
        switch kind {
        case .ssh: SSHTunnels.shared.recent.contains(hub.id)
        case .inApp: InAppTunnels.shared.recent.contains(hub.id)
        }
    }

    /// Up and routing the hub's requests: true; it could not be made: false.
    func open() async -> Bool {
        switch kind {
        case .ssh:
            guard let route = hub.ssh else { return false }
            return (try? await SSHTunnels.shared.open(hub.id, route: route, hub: hub.config.url)) != nil
        case .inApp:
            guard let route = hub.inAppTunnel else { return false }
            return (try? await InAppTunnels.shared.open(hub.id, route: route, hub: hub.config.url)) != nil
        }
    }

    func close() {
        switch kind {
        case .ssh: SSHTunnels.shared.close(hub.id)
        case .inApp: InAppTunnels.shared.close(hub.id)
        }
    }

    func watchDirect(answers: @escaping @MainActor () async -> Bool, moveBack: @escaping @MainActor () -> Void) {
        switch kind {
        case .ssh: SSHTunnels.shared.watchDirect(hub.id, answers: answers, moveBack: moveBack)
        case .inApp: InAppTunnels.shared.watchDirect(hub.id, answers: answers, moveBack: moveBack)
        }
    }
}

extension AppModel {
    // MARK: Tunnels to the hub (docs/network.md「经 SSH 隧道连 hub」「经 App 内隧道连 hub」)

    /// The group's tunnels ready to use, each paired hub's SSH one before its in-app one.
    func hubTunnels(_ group: String) -> [HubTunnel] {
        registry.members(group).flatMap { h -> [HubTunnel] in
            var out: [HubTunnel] = []
            if h.ssh?.isUsable == true { out.append(HubTunnel(hub: h, kind: .ssh)) }
            if InAppTunnel.available, h.inAppTunnel?.isUsable == true { out.append(HubTunnel(hub: h, kind: .inApp)) }
            return out
        }
    }

    /// Where a group's socket goes next when it has a tunnel: straight to the hub when one of its addresses answers
    /// (the tunnels close), else through a tunnel; tunnels set to be used every time first. Nothing answering at all:
    /// the first address, for the socket's own retries.
    func tunnelRoute(_ group: String) async -> ServerConfig? {
        let candidates = HubRoute.candidates(registry, group: group)
        let tunnels = hubTunnels(group)
        let always = tunnels.filter { $0.always }
        for t in always {
            if await t.open() { return only(t, of: tunnels).hub.config }
        }
        let fallback = tunnels.filter { !$0.always }
        // Through a tunnel last time (back from the background, a dropped socket): it starts while the direct way is
        // asked, so an address that does not answer costs no wait (up to the probe's 3 s) on top.
        let warm = fallback.filter { $0.recent }
        let warming: Task<HubTunnel?, Never>? = warm.isEmpty ? nil : Task { @MainActor in
            for t in warm { if await t.open() { return t } }
            return nil
        }
        if let direct = await HubRoute.answering(candidates) {
            _ = await warming?.value
            for t in tunnels { t.close() }
            return direct.config
        }
        if let t = await warming?.value { return through(only(t, of: tunnels), group) }
        for t in fallback where !warm.contains(t) {
            if await t.open() { return through(only(t, of: tunnels), group) }
        }
        return candidates.first?.config
    }

    /// `t` carries its hub's traffic: the hub's other tunnel closes (both route the same hub address in HubTransport, so
    /// the one left open would later clear the route of the one in use).
    private func only(_ t: HubTunnel, of tunnels: [HubTunnel]) -> HubTunnel {
        for o in tunnels where o.hub.id == t.hub.id && o.kind != t.kind { o.close() }
        return t
    }

    /// The group goes through `t`, asking the direct way again every so often.
    private func through(_ t: HubTunnel, _ group: String) -> ServerConfig {
        t.watchDirect(answers: { [weak self] in
            guard let self else { return false }
            return await HubRoute.answering(HubRoute.candidates(self.registry, group: group)) != nil
        }, moveBack: { [weak self] in self?.link(group)?.live.reroute() })
        return t.hub.config
    }

    /// Every tunnel of every hub closes (the app went to the background, or there is no hub left).
    func closeAllTunnels() {
        SSHTunnels.shared.closeAll()
        InAppTunnels.shared.closeAll()
    }

    /// One paired hub's tunnels close (it is removed, or its group is).
    func closeTunnels(_ id: String) {
        SSHTunnels.shared.close(id)
        InAppTunnels.shared.close(id)
    }
}
