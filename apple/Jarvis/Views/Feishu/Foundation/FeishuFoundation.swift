import SwiftUI

// MARK: - API

/// Feishu data beyond chats comes through the hub's read-only window onto lark-connector's agent API:
/// `/api/v1/lark/raw/<path>` = lark-connector `/agent/v1/<path>` (see docs/protocol.md 飞书 API). Media / API paths inside
/// the JSON already point back at the hub, so they load with `LarkRemoteImage` / `LarkMedia`.
extension APIClient {
    /// GET one lark-connector agent route, decoded. `path` has no leading slash, e.g. "documents/recent".
    func larkRaw<T: Decodable>(_ path: String, query: [String: String] = [:], as: T.Type = T.self) async throws -> T {
        try await get("/api/v1/lark/raw/" + path, query: query)
    }

    /// GET one route as untyped JSON (for shapes that vary).
    func larkRawJSON(_ path: String, query: [String: String] = [:]) async throws -> JSONValue {
        try await get("/api/v1/lark/raw/" + path, query: query)
    }

    /// lark-connector's paged, filtered search (`POST search/page`, read-only): tab, query, cursor, limit and filters
    /// (senderIds, chatIds, feedId, startTime, endTime, docTypes, ownerIds, sortType…).
    func larkSearchPage<T: Decodable>(_ body: [String: JSONValue], as: T.Type = T.self) async throws -> T {
        try JSON.decoder.decode(T.self, from: try await request("POST", "/api/v1/lark/raw/search/page", body: body))
    }
}

// MARK: - Routes

/// What a Feishu view can open from another: a chat, a document, a person, a removed post. Jarvis is not a second
/// Feishu: there are no browsing screens, only what a link or a message leads to. The navigation stack that owns a
/// Feishu view resolves these with `.feishuRoutes()`.
enum FeishuRoute: Hashable {
    case chat(LarkConversation)
    case doc(url: String, title: String?)
    case profile(LarkPersonRef)
    case momentsPost(id: String)
}

extension EnvironmentValues {
    /// Opens a Feishu route in the surrounding navigation stack.
    @Entry var feishuOpen: ((FeishuRoute) -> Void)? = nil
}

extension View {
    /// Resolves `FeishuRoute` values pushed onto this navigation stack.
    func feishuRoutes() -> some View {
        navigationDestination(for: FeishuRoute.self) { FeishuRouteView(route: $0) }
    }

    /// Feishu doc / wiki links open in the native reader and removed links as the post, instead of Safari.
    func feishuHandlesLinks(_ open: ((FeishuRoute) -> Void)?) -> some View {
        environment(\.openURL, OpenURLAction { url in
            if let open, let route = FeishuLinks.route(url) {
                open(route)
                return .handled
            }
            return .systemAction
        })
    }
}

nonisolated enum FeishuLinks {
    /// A Feishu / Lark cloud document (docx, wiki, docs, sheets…) the reader can open.
    static func isDoc(_ url: URL) -> Bool {
        guard let host = url.host?.lowercased(),
              host.hasSuffix("larkoffice.com") || host.hasSuffix("feishu.cn") || host.hasSuffix("larksuite.com") else { return false }
        let first = url.pathComponents.dropFirst().first ?? ""
        return ["docx", "wiki", "docs", "sheets", "base", "mindnotes", "file", "slides"].contains(first)
    }

    /// The native view a Feishu link opens in, if there is one.
    static func route(_ url: URL) -> FeishuRoute? {
        if isDoc(url) { return .doc(url: url.absoluteString, title: nil) }
        if let id = LarkMoments.postID(url.absoluteString) { return .momentsPost(id: id) }
        return nil
    }
}

struct FeishuRouteView: View {
    var route: FeishuRoute

    var body: some View {
        switch route {
        case .chat(let c): LarkChatView(conversation: c)
        case .doc(let url, let title): FeishuDocReader(url: url, title: title)
        case .profile(let p): LarkProfileView(person: p, embedded: true)
        case .momentsPost(let id): LarkMomentsPostView(postID: id)
        }
    }
}

// MARK: - Shared pieces

/// The square, colored icon Feishu uses for apps and document types.
struct FeishuAppIcon: View {
    var symbol: String
    var hue: String
    var size: CGFloat = 44

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.45, weight: .semibold))
            .foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(LarkColor.hue(hue).gradient, in: .rect(cornerRadius: size * 0.26, style: .continuous))
    }
}

nonisolated enum FeishuTime {
    /// Milliseconds (number or numeric string) or ISO text → Date.
    static func date(_ v: JSONValue?) -> Date? {
        guard let v else { return nil }
        if let s = v.string, s.contains("-") || s.contains("T") { return JSON.parseDate(s) }
        guard let n = v.double, n > 0 else { return nil }
        return Date(timeIntervalSince1970: n > 1e11 ? n / 1000 : n)
    }

    /// Compact relative time used in Feishu lists: 刚刚 / 5 分钟前 / 14:05 / 昨天 / 10月21日.
    static func short(_ d: Date?) -> String { LarkTime.short(d) }
}
