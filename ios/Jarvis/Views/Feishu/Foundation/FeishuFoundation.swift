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

/// Everything one Feishu screen can open from another. Each screen pushes routes through `feishuOpen`; the
/// navigation stack that owns it (信息, 工作台, a sheet) resolves them with `.feishuRoutes()`.
enum FeishuRoute: Hashable {
    case chat(LarkConversation)
    case chatInfo(LarkConversation)
    case doc(url: String, title: String?)
    case profile(LarkPersonRef)
    case search(query: String?, feedID: String?)
    case event(id: String)
    case meeting(historyID: String)
    case minutes(token: String)
    case todo(guid: String)
    case approval(instanceID: String)
    case momentsPost(id: String)
    case department(id: String, name: String)
    case wikiSpace(id: String, name: String)
    case screen(FeishuScreen)
}

/// Whole screens reachable from the 工作台 grid.
enum FeishuScreen: String, Hashable, CaseIterable, Identifiable {
    case today, search, docs, wiki, docNotifications, calendar, meetings, minutes, tasks, approvals, okr, contacts, moments, saved, threads, mail
    var id: String { rawValue }

    var title: String {
        switch self {
        case .today: "今日"
        case .search: "搜索"
        case .docs: "云文档"
        case .wiki: "知识库"
        case .docNotifications: "文档通知"
        case .calendar: "日历"
        case .meetings: "会议"
        case .minutes: "妙记"
        case .tasks: "任务"
        case .approvals: "审批"
        case .okr: "OKR"
        case .contacts: "通讯录"
        case .moments: "removed"
        case .saved: "收藏"
        case .threads: "话题"
        case .mail: "飞书邮箱"
        }
    }

    var symbol: String {
        switch self {
        case .today: "sun.max.fill"
        case .search: "magnifyingglass"
        case .docs: "doc.text.fill"
        case .wiki: "books.vertical.fill"
        case .docNotifications: "bell.badge.fill"
        case .calendar: "calendar"
        case .meetings: "video.fill"
        case .minutes: "waveform"
        case .tasks: "checklist"
        case .approvals: "checkmark.seal.fill"
        case .okr: "target"
        case .contacts: "person.2.fill"
        case .moments: "circle.hexagongrid.fill"
        case .saved: "bookmark.fill"
        case .threads: "bubble.left.and.bubble.right.fill"
        case .mail: "envelope.fill"
        }
    }

    var hue: String {
        switch self {
        case .today: "orange"
        case .search, .docs, .threads: "blue"
        case .wiki: "indigo"
        case .docNotifications: "red"
        case .calendar: "orange"
        case .meetings: "turquoise"
        case .minutes: "violet"
        case .tasks: "green"
        case .approvals: "wathet"
        case .okr: "purple"
        case .contacts: "blue"
        case .moments: "carmine"
        case .saved: "yellow"
        case .mail: "blue"
        }
    }
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

    /// Feishu doc / wiki links open in the native reader instead of Safari.
    func feishuHandlesLinks(_ open: ((FeishuRoute) -> Void)?) -> some View {
        environment(\.openURL, OpenURLAction { url in
            if let open, FeishuLinks.isDoc(url) {
                open(.doc(url: url.absoluteString, title: nil))
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
}

struct FeishuRouteView: View {
    var route: FeishuRoute

    var body: some View {
        switch route {
        case .chat(let c): LarkChatView(conversation: c)
        case .chatInfo(let c): FeishuChatInfoView(conversation: c)
        case .doc(let url, let title): FeishuDocReader(url: url, title: title)
        case .profile(let p): LarkProfileView(person: p, embedded: true)
        case .search(let q, let feed): FeishuSearchView(initialQuery: q, feedID: feed)
        case .event(let id): FeishuEventDetail(eventID: id)
        case .meeting(let id): FeishuMeetingDetail(historyID: id)
        case .minutes(let token): FeishuMinutesDetail(token: token)
        case .todo(let guid): FeishuTaskDetail(guid: guid)
        case .approval(let id): FeishuApprovalDetail(instanceID: id)
        case .momentsPost(let id): LarkMomentsPostView(postID: id)
        case .department(let id, let name): FeishuDepartmentView(departmentID: id, name: name)
        case .wikiSpace(let id, let name): FeishuWikiSpaceView(spaceID: id, name: name)
        case .screen(let s): FeishuScreenView(screen: s)
        }
    }
}

struct FeishuScreenView: View {
    var screen: FeishuScreen

    var body: some View {
        switch screen {
        case .today: FeishuTodayView()
        case .search: FeishuSearchView(initialQuery: nil, feedID: nil)
        case .docs: FeishuDocsHome()
        case .wiki: FeishuWikiHome()
        case .docNotifications: FeishuDocNotificationsView()
        case .calendar: FeishuCalendarView()
        case .meetings: FeishuMeetingsView()
        case .minutes: FeishuMinutesListView()
        case .tasks: FeishuTasksView()
        case .approvals: FeishuApprovalsView()
        case .okr: FeishuOKRView()
        case .contacts: FeishuContactsView()
        case .moments: FeishuMomentsView()
        case .saved: FeishuSavedView()
        case .threads: FeishuThreadsView()
        case .mail: FeishuMailView()
        }
    }
}

// MARK: - Shared pieces

/// A screen that is not built yet (kept so every route compiles while areas land one by one).
struct FeishuPlaceholder: View {
    var title: String

    var body: some View {
        EmptyStateView(symbol: "hammer", title: title, message: "即将上线")
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
    }
}

/// Load-once / refresh state for a Feishu screen: loading spinner, error line, data.
@MainActor @Observable
final class FeishuLoader<T> {
    var value: T?
    var error: String?
    var loading = false

    func load(_ fetch: @escaping () async throws -> T) async {
        loading = true
        defer { loading = false }
        do {
            value = try await fetch()
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }
}

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
    static func short(_ d: Date?) -> String { LarkFeedRow.time(d) }
}
