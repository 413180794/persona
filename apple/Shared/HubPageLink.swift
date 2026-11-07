import Foundation

/// A page of the hub's web app that the app has natively (#2696): the links the assistant writes into its answers
/// (#2194: `/s/<machine>/<session>`, `/persona/<conversation>`, `/todos?id=<todo>`, each optionally `?at=<item id>` for one
/// message) open here instead of the web page. The paths are web/src/lib/router.ts's; the hub writes them in
/// server/internal/assistant (`sessionLink`, `todoLink`, `transcriptText`).
nonisolated enum HubPage: Equatable, Hashable, Sendable {
    /// An agent's conversation (`/s/…`, `/t/…/<session>`); `item`: the message to land on.
    case agent(machine: String, session: String, item: String?)
    /// A Persona conversation (`/persona/<id>`).
    case conversation(id: String, item: String?)
    /// A task (`/todos?id=…`, `/todos/<id>`).
    case todo(id: String)
}

nonisolated enum HubPageLink {
    /// The page `url` is when it points at one of `hubs` (a group's addresses: paired, current, mesh endpoints, its
    /// `public_url`); nil for another site, another hub, or a path the app has no page for.
    static func page(_ url: URL, hubs: [URL]) -> HubPage? {
        guard let base = hubs.first(where: { sameOrigin(url, $0) }) else { return nil }
        guard let comps = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        var parts = segments(comps.percentEncodedPath)
        // A hub behind a reverse proxy's sub path (https://<host>/<prefix>): its pages are under that prefix.
        let prefix = segments(URLComponents(url: base, resolvingAgainstBaseURL: false)?.percentEncodedPath ?? "")
        if !prefix.isEmpty {
            guard parts.starts(with: prefix) else { return nil }
            parts.removeFirst(prefix.count)
        }
        let decoded = parts.map { $0.removingPercentEncoding }
        guard !decoded.contains(nil) else { return nil }
        return page(path: decoded.compactMap { $0 }, query: comps.queryItems ?? [])
    }

    /// The page for a path's segments (already decoded) and its query; the hub is not checked here.
    static func page(path p: [String], query: [URLQueryItem]) -> HubPage? {
        func value(_ name: String) -> String? {
            query.first { $0.name == name }?.value.flatMap { $0.isEmpty ? nil : $0 }
        }
        let at = value("at")
        switch p.first {
        case "s" where p.count == 3 && !p[1].isEmpty && !p[2].isEmpty:
            return .agent(machine: p[1], session: p[2], item: at)
        case "t" where p.count == 5 && !p[1].isEmpty && !p[4].isEmpty:
            // /t/<machine>/<space>/<slot>/<session>: the tab's agent. Without the session it names a place, not a chat.
            return .agent(machine: p[1], session: p[4], item: at)
        case "persona", LegacyNames.conversationPath:
            guard p.count == 2, !p[1].isEmpty, p[1] != "new" else { return nil }
            return .conversation(id: p[1], item: at)
        case "todos":
            if p.count == 2, !p[1].isEmpty { return .todo(id: p[1]) }
            if p.count == 1, let id = value("id") { return .todo(id: id) }
            return nil
        default:
            return nil
        }
    }

    /// Same scheme-less origin: host (case-insensitive, IPv6 without brackets) and port (the scheme's default when not
    /// written). The scheme itself may differ: the hub's TLS and plain ports are both its own.
    static func sameOrigin(_ a: URL, _ b: URL) -> Bool {
        guard let x = origin(a), let y = origin(b) else { return false }
        return x == y
    }

    private static func origin(_ url: URL) -> String? {
        guard let comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let scheme = comps.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = comps.host?.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]")), !host.isEmpty
        else { return nil }
        let port = comps.port ?? (scheme == "https" ? 443 : 80)
        return "\(host):\(port)"
    }

    private static func segments(_ path: String) -> [String] {
        path.split(separator: "/", omittingEmptySubsequences: true).map(String.init)
    }
}
