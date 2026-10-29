import ActivityKit
import Foundation

/// The one Live Activity Jarvis runs: the "Fleet" (docs/live-activity.md). Built into the app (starts it, reports its
/// tokens) and the widget extension (draws it). The hub push-starts it with `attributes-type: JarvisActivityAttributes`
/// and `attributes: {"kind": "fleet"}`, so neither name may change.
nonisolated struct JarvisActivityAttributes: ActivityAttributes {
    typealias ContentState = FleetState
    var kind = "fleet"
}

/// Deep links the activity opens: `jarvis://open?machine=<id>&session=<id>`.
nonisolated enum FleetLink {
    static func url(_ item: FleetState.Item?) -> URL? {
        guard let key = item?.key else { return URL(string: "jarvis://open") }
        var c = URLComponents()
        c.scheme = "jarvis"
        c.host = "open"
        c.queryItems = [URLQueryItem(name: "machine", value: key.machine), URLQueryItem(name: "session", value: key.session)]
        return c.url
    }

    /// The session a link opens, or nil for anything else (`jarvis://open` alone just opens the app).
    static func session(_ url: URL) -> (machine: String, session: String)? {
        guard url.scheme == "jarvis", url.host == "open",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems,
              let m = items.first(where: { $0.name == "machine" })?.value, !m.isEmpty,
              let s = items.first(where: { $0.name == "session" })?.value, !s.isEmpty else { return nil }
        return (m, s)
    }
}
