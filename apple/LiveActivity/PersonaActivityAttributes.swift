import ActivityKit
import Foundation

/// The one Live Activity Persona runs: the "Fleet" (docs/apple/live-activity.md). Built into the app (starts it, reports its
/// tokens) and the widget extension (draws it). The hub push-starts it with `attributes-type: PersonaActivityAttributes`
/// and `attributes: {"kind": "fleet"}`, so neither name may change.
nonisolated struct PersonaActivityAttributes: ActivityAttributes {
    typealias ContentState = FleetState
    var kind = "fleet"
}

/// Deep links the activity opens: `persona://open?machine=<id>&session=<id>`.
nonisolated enum FleetLink {
    static func url(_ item: FleetState.Item?) -> URL? {
        guard let key = item?.key else { return URL(string: "persona://open") }
        var c = URLComponents()
        c.scheme = "persona"
        c.host = "open"
        c.queryItems = [URLQueryItem(name: "machine", value: key.machine), URLQueryItem(name: "session", value: key.session)]
        return c.url
    }

    /// A tap on the activity itself (not on one of its rows): the one agent when it shows only one, else just the
    /// app, where it was (list or chat, same scroll position) — with several agents, landing in whichever came first
    /// was a guess.
    static func whole(_ state: FleetState) -> URL? {
        state.items.count == 1 ? url(state.items.first) : URL(string: "persona://open")
    }

    /// A tap on the lock screen, by what `FleetLockScreen` draws: one request alone (the first a button settles, or
    /// the one nameless line when names are hidden) opens its agent, as its View button does; rows or only counts, `whole`.
    /// Before #4318 the lock screen tapped as `whole`, which with several agents only opened the app.
    static func lockScreen(_ state: FleetState, stale: Bool) -> URL? {
        if !stale, let act = state.firstActionable { return url(act) }
        if !state.details, let first = state.items.first, first.needsYou { return url(first) }
        return whole(state)
    }

    /// The session a link opens, or nil for anything else (`persona://open` alone just opens the app).
    static func session(_ url: URL) -> (machine: String, session: String)? {
        guard ["persona", LegacyNames.urlScheme].contains(url.scheme?.lowercased() ?? ""), url.host == "open",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              let m = items.first(where: { $0.name == "machine" })?.value, !m.isEmpty,
              let s = items.first(where: { $0.name == "session" })?.value, !s.isEmpty else { return nil }
        return (m, s)
    }
}
