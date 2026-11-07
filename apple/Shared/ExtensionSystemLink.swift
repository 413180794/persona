import Foundation

/// A system entry carries only the extension, the page and its group; no address or credential goes into the deep link.
nonisolated struct ExtensionSystemLink: Hashable, Sendable {
    var owner: String
    var ui: String?
    var group: String
    var state: String?

    init(owner: String, ui: String?, group: String, state: String? = nil) {
        self.owner = owner
        self.ui = ui
        self.group = group
        self.state = state
    }

    init?(url: URL) {
        guard HubNaming.accepts(scheme: url.scheme), url.host == "extension",
              url.user == nil, url.password == nil, url.port == nil, url.fragment == nil,
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        let path = url.pathComponents.filter { $0 != "/" }
        guard (1...2).contains(path.count), Self.segment(path[0]), path.count == 1 || Self.segment(path[1]) else { return nil }
        let query = components.queryItems ?? []
        guard Set(query.map(\.name)).count == query.count,
              let source = query.first(where: { $0.name == "group" })?.value else { return nil }
        owner = path[0]
        ui = path.count == 2 ? path[1] : nil
        group = source
        state = query.first { $0.name == "state" }?.value
        guard (state?.utf8.count ?? 0) <= 16_384 else { return nil }
    }

    var url: URL? {
        guard Self.segment(owner), ui.map(Self.segment) ?? true, (state?.utf8.count ?? 0) <= 16_384 else { return nil }
        var components = URLComponents()
        components.scheme = "persona"
        components.host = "extension"
        components.path = "/" + owner + (ui.map { "/" + $0 } ?? "")
        components.queryItems = [URLQueryItem(name: "group", value: group)]
        if let state { components.queryItems?.append(URLQueryItem(name: "state", value: state)) }
        return components.url
    }

    private static func segment(_ value: String) -> Bool {
        let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
        return !value.isEmpty && value != "." && value != ".." && value.count <= 128
            && value.unicodeScalars.allSatisfy(allowed.contains)
    }
}
