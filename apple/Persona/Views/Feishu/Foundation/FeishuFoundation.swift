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

/// What a Feishu view can open from another: a chat, a document, a person, a company-feed (公司圈) post. Persona is
/// not a second Feishu: there are no browsing screens, only what a link or a message leads to. The navigation stack that owns a
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

    /// Links inside Feishu views stay in the app (CLAUDE.md「链接在 App 内打开」): doc / wiki links open in the native
    /// reader and company-feed links as the post (while the hub has them, `LarkFeatures`), other web pages in the link
    /// sheet; mailto, tel and the like go to the system.
    func feishuHandlesLinks(_ open: ((FeishuRoute) -> Void)?) -> some View {
        modifier(FeishuLinkHandling(open: open))
    }
}

private struct FeishuLinkHandling: ViewModifier {
    @Environment(AppModel.self) private var model
    var open: ((FeishuRoute) -> Void)?
    @State private var link: LinkTarget?

    func body(content: Content) -> some View {
        #if os(macOS)
        content.environment(\.openURL, OpenURLAction { url in
            if let open, case .route(let route) = FeishuLinks.target(url, lark: model.larkFeatures) {
                open(route)
                return .handled
            }
            return LinkPolicy.system(url) // NSWorkspace would run any scheme it names
        })
        #else
        content
            .environment(\.openURL, OpenURLAction { url in
                switch FeishuLinks.target(url, lark: model.larkFeatures) {
                case .route(let route) where open != nil:
                    open?(route)
                    return .handled
                case .route, .sheet:
                    // The sheet's AI 修改 / 作为上下文 go to the assistant, whose config loads with its page: opened
                    // straight into the inbox it is not there yet. With no assistant at all, the system opens the link.
                    if assistant != nil {
                        link = LinkTarget(url: url)
                        return .handled
                    }
                    Task {
                        await model.refreshAssistant()
                        if assistant != nil { link = LinkTarget(url: url) } else { await UIApplication.shared.open(url) }
                    }
                    return .handled
                case .system:
                    return .systemAction
                }
            })
            .sheet(item: $link) { t in
                if let assistant { LinkSheet(target: t, key: assistant) }
            }
        #endif
    }

    private var assistant: SessionKey? { model.assistantMain ?? model.assistantCurrent }
}

nonisolated enum FeishuLinks {
    /// A Feishu / Lark cloud document (docx, wiki, docs, sheets…) the reader can open.
    static func isDoc(_ url: URL) -> Bool {
        guard let host = url.host?.lowercased(),
              host.hasSuffix("larkoffice.com") || host.hasSuffix("feishu.cn") || host.hasSuffix("larksuite.com") else { return false }
        let first = url.pathComponents.dropFirst().first ?? ""
        return ["docx", "wiki", "docs", "sheets", "base", "mindnotes", "file", "slides"].contains(first)
    }

    /// The native view a Feishu link opens in, if there is one and the hub has what it reads (`LarkFeatures`):
    /// without it the link is an ordinary web page.
    static func route(_ url: URL, lark: LarkFeatures) -> FeishuRoute? {
        if lark.chats, isDoc(url) { return .doc(url: url.absoluteString, title: nil) }
        if lark.moments, let id = LarkMoments.postID(url.absoluteString) { return .momentsPost(id: id) }
        return nil
    }

    typealias Target = FeishuLinkTarget

    static func target(_ url: URL, lark: LarkFeatures) -> Target {
        if let route = route(url, lark: lark) { return .route(route) }
        guard ["http", "https"].contains(url.scheme?.lowercased() ?? ""), !(url.host ?? "").isEmpty else { return .system }
        return .sheet
    }
}

/// Where a tapped link opens (`FeishuLinks.target`): a native Feishu view, the in-app link sheet (any other web page),
/// or the system (mailto, tel, other schemes, and what is not a web address at all: relative paths, a scheme without a
/// host). Main-actor like `FeishuRoute`, whose conformances it uses.
enum FeishuLinkTarget: Hashable {
    case route(FeishuRoute)
    case sheet
    case system
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
