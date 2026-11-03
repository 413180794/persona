import Foundation

/// What a `jarvis://` link handed to the app does (`onOpenURL`).
nonisolated enum MacOpenURL {
    enum Route: Equatable {
        case session(SessionKey)
        case app
        case pair
        case ignore
    }

    static func route(_ url: URL) -> Route {
        guard ["persona", "jarvis"].contains(url.scheme?.lowercased() ?? "") else { return .ignore }
        switch url.host()?.lowercased() {
        case "open":
            // The same query as the iPhone's Live Activity links (FleetLink).
            let q = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            func value(_ name: String) -> String? { q.first { $0.name == name }?.value.flatMap { $0.isEmpty ? nil : $0 } }
            guard let m = value("machine"), let s = value("session") else { return .app }
            // "group|machine" for another group's row (FleetMerge); resolved against the groups where it is opened.
            return SessionKey(path: m + "/" + s).map { .session($0) } ?? .app
        case "pair": return .pair
        default: return .ignore
        }
    }
}
