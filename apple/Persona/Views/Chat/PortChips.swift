import SwiftUI

extension EnvironmentValues {
    /// Opens a port of the conversation's machine in the in-app sheet, through the hub (set by ChatView).
    @Entry var openPort: ((PortLink) -> Void)? = nil
}

/// Badges for a message's links to ports of its machine (`PortLinks`, docs/operations/ports.md): `:5173` and the page's
/// title, the look of the file badges (`LinkBadge`); gray once nothing listens there. A tap opens the page through
/// the hub in the in-app sheet.
struct PortChips: View {
    @Environment(AppModel.self) private var model
    @Environment(\.fileScope) private var scope
    @Environment(\.openPort) private var openPort
    var text: String

    var body: some View {
        if let scope, let openPort {
            let names = model.hostnames(of: scope.machine)
            // Not the hub's own pages (#2696): those open natively, a hub on the agent's machine is no dev server.
            let links = PortLinks.find(in: text, hostnames: names).filter { !model.isHubPage($0, hostnames: names) }
            if !links.isEmpty {
                PortBadgeRow(links: links, machine: scope.machine, open: openPort)
            }
        }
    }
}

/// The ports an agent listens on (`Session.ports`), in the conversation's header: a tap opens one.
struct SessionPortChips: View {
    @Environment(\.openPort) private var openPort
    var machine: MachineRef
    var ports: [ListeningPort]

    var body: some View {
        if let openPort, !ports.isEmpty {
            PortBadgeRow(links: ports.map { PortLink(port: $0.port, path: "/", secure: false) }, machine: machine, open: openPort)
        }
    }
}

private struct PortBadgeRow: View {
    @Environment(AppModel.self) private var model
    var links: [PortLink]
    var machine: MachineRef
    var open: (PortLink) -> Void

    var body: some View {
        let listed = model.agentPorts(on: machine)
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.sm) {
                ForEach(links, id: \.self) { l in
                    let state = model.portPresence.state(l.port, machine: machine, listed: listed)
                    Button { open(l) } label: {
                        LinkBadge(symbol: Theme.Icon.servedPage, text: Self.label(l), detail: Self.detail(state),
                                  dimmed: state == .gone)
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("port-chip-\(l.port)")
                    .accessibilityHint(state == .gone ? String(localized: "This port is no longer listening") : String(localized: "Opens this page in the app"))
                    .contextMenu {
                        Button { UIPasteboard.general.string = "http://localhost:\(l.port)\(l.path)" } label: {
                            Label("Copy Link", systemImage: Theme.Icon.link)
                        }
                    }
                }
            }
        }
        .scrollClipDisabled()
        .task(id: links) { model.portPresence.ask(links.map(\.port), machine: machine, listed: listed, api: model.api(machine.group)) }
    }

    /// `:5173`, with the path when the link has one (`:8000/docs`).
    static func label(_ l: PortLink) -> String {
        let path = l.path == "/" ? "" : l.path
        return ":\(l.port)" + (path.count > 24 ? String(path.prefix(23)) + "…" : path)
    }

    static func detail(_ state: PortPresence.State?) -> String? {
        switch state {
        case .live(let title): title
        case .gone: String(localized: "Stopped [Port Chips f00645]")
        case nil: nil
        }
    }
}
