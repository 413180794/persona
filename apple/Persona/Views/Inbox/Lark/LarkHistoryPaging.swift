import Foundation
import SwiftUI

/// One older page at a time. A failed or non-progressing page waits for an explicit
/// retry; changing the visible window invalidates even a cancellation-ignoring reply.
struct LarkHistoryPaging {
    struct Request: Equatable {
        let id = UUID()
        let cursor: String
    }
    /// What the header says when paging stopped: the sentence, and a shorter one for when it does not fit on a line
    /// (large text sizes).
    struct Problem: Equatable {
        let text: String
        let short: String
        static let stopped = Problem(text: String(localized: "暂时无法加载更早的消息"), short: String(localized: "暂时无法加载"))
        static let failed = Problem(text: String(localized: "更早的消息未加载，点击重试"), short: String(localized: "加载失败，点击重试"))
        static let accountChanged = Problem(text: String(localized: "飞书账号已切换，请重新打开会话"), short: String(localized: "飞书账号已切换"))
        static let upgrade = Problem(text: String(localized: "更新 Hub 后可加载更早的消息"), short: String(localized: "请更新 Hub"))
        static let signedOut = Problem(text: String(localized: "飞书已退出登录，请重新连接"), short: String(localized: "飞书已退出登录"))
    }
    private(set) var pending: Request?
    private(set) var problem: Problem?
    private var visited: Set<String> = []

    mutating func begin(cursor: String?, retry: Bool = false) -> Request? {
        guard pending == nil, let cursor, !cursor.isEmpty, problem == nil || retry else { return nil }
        let request = Request(cursor: cursor)
        pending = request
        problem = nil
        return request
    }

    mutating func cancel() { pending = nil }

    mutating func fail(_ request: Request, problem: Problem) {
        guard pending == request else { return }
        pending = nil
        self.problem = problem
    }

    /// Returns false for a response from an older visit or a replaced window.
    @discardableResult
    mutating func finish(_ request: Request, page: APIClient.LarkMessages, pager: inout LarkPager) -> Bool {
        guard pending == request, pager.olderRequest == request.cursor else { return false }
        pending = nil
        visited.insert(request.cursor)
        var page = page
        var seen = Set(pager.messages.map(\.id))
        page.messages = page.messages.filter { seen.insert($0.id).inserted }
        let next = page.olderCursor ?? page.messages.first?.position
        // A valid final empty page ends pagination. An empty/duplicate page that
        // claims more history, or a cursor cycle, must never start a request loop.
        if page.hasOlder && (page.messages.isEmpty || next == nil || visited.contains(next!)) {
            problem = .stopped
            // Keep useful rows, but retry the same boundary rather than skip a gap.
            page.olderCursor = request.cursor
        }
        pager.prepend(page)
        return true
    }

    static func problem(for error: Error) -> Problem {
        switch (error as? APIError)?.code {
        case "lark_account_changed": .accountChanged
        case "lark_history_upgrade_required": .upgrade
        case "lark_not_logged_in": .signedOut
        default: .failed
        }
    }
}

extension APIClient {
    /// The response must belong to the account that owns the open chat. Older
    /// Hubs ignore this query; the missing owner is an upgrade prompt, never data.
    func larkOlderMessages(_ feed: String, before: String, account: String, topic: Bool) async throws -> LarkMessages {
        let page: LarkMessages = try await getDecodedOffMain("/api/v1/lark/conversations/\(feed)/messages",
            query: ["before": before, "limit": "40", "topic": topic ? "1" : "", "account_id": account])
        guard let owner = page.accountID else {
            throw APIError(status: 426, message: String(localized: "更新 Hub 后可加载更早的消息"), fields: ["code": "lark_history_upgrade_required"])
        }
        guard owner == account else {
            throw APIError(status: 409, message: String(localized: "飞书账号已切换"), fields: ["code": "lark_account_changed"])
        }
        return page
    }
}

/// Whether a chat's list is within half a screen of its top, with the content's height (see LarkChatView).
nonisolated struct LarkHistoryReach: Equatable {
    var near: Bool
    var content: CGFloat
}

/// Keeps the message being read where it is on screen while an older page goes in above it. Measured on the
/// simulator: the list's scroll position (by id) held the history header instead, and a size-change anchor did
/// nothing while it did, so the message went down by the whole page and the list asked for the next one at once.
/// When a page lands, one message on screen becomes the anchor; only that row reports where it is in the content,
/// and every time layout above it moves it, the list moves by as much, directly on its scroll view (as `ChatPin`
/// does). Content coordinates do not change when the list scrolls, so a finger or a fling keeps going, and rows
/// changing below (a message arriving) leave it alone.
@MainActor final class LarkHistoryHold {
    nonisolated static let space = "larkChatContent"

    #if os(iOS) || os(visionOS)
    weak var list: UIScrollView?
    #endif
    /// Forgets the position the chat last asked its list for (stored positions are applied again when content changes).
    var letGo: () -> Void = {}
    /// The messages on screen (the list's scroll targets), from the list's own visibility callback.
    var visible: [String] = []
    /// A page just landed and its rows are laying out: where the list stands against its top is not known yet.
    var settling = false
    private var anchor: String?
    private var last: CGFloat?
    private var until: Date?

    /// The message to hold: the middle one on screen, so it stays laid out while the page goes in.
    func pick(in order: [String]) -> String? {
        let index = Dictionary(order.enumerated().map { ($1, $0) }, uniquingKeysWith: { a, _ in a })
        let shown = visible.filter { index[$0] != nil }.sorted { index[$0]! < index[$1]! }
        return shown.isEmpty ? nil : shown[shown.count / 2]
    }

    /// Hold `id` from its next report on; `ready` once it has reported where it is.
    func begin(_ id: String) {
        letGo()
        anchor = id
        last = nil
        until = .now.addingTimeInterval(1.5)
    }

    var ready: Bool { last != nil }

    /// Where the list stands, for the log.
    var describe: String {
        #if os(iOS) || os(visionOS)
        guard let list else { return "no list" }
        return "offset \(Int(list.contentOffset.y)) inset \(Int(list.adjustedContentInset.top)) height \(Int(list.bounds.height)) content \(Int(list.contentSize.height))"
        #else
        return ""
        #endif
    }

    /// Whether the list is within half a screen of its top, read from the scroll view itself (nil: not found). The
    /// geometry callback can lag the hold's own moves; a page landing then looked like it was still at the top and
    /// asked for the next one at once (measured).
    var nearTop: Bool? {
        #if os(iOS) || os(visionOS)
        guard let list else { return nil }
        let inset = list.adjustedContentInset
        return list.contentOffset.y + inset.top <= (list.bounds.height - inset.top - inset.bottom) / 2
        #else
        return nil
        #endif
    }

    func end() { anchor = nil; last = nil; until = nil }

    /// The anchor row's top in the list's content.
    func moved(_ id: String, to y: CGFloat) {
        guard id == anchor else { return }
        guard let until, Date.now <= until else { end(); return }
        defer { last = y }
        #if os(iOS) || os(visionOS)
        guard let last, let list, abs(y - last) >= 0.5 else { return }
        // No upper bound from the content size: it follows the rows a pass late (ChatPin).
        list.contentOffset.y = max(-list.adjustedContentInset.top, list.contentOffset.y + y - last)
        #endif
    }

    #if os(iOS) || os(visionOS)
    /// Sits in the list's content and hands the scroll view around it to the hold.
    struct Finder: UIViewRepresentable {
        let hold: LarkHistoryHold

        func makeUIView(context: Context) -> Probe {
            let v = Probe()
            v.hold = hold
            v.isUserInteractionEnabled = false
            return v
        }

        func updateUIView(_ v: Probe, context: Context) { v.hold = hold }

        final class Probe: UIView {
            weak var hold: LarkHistoryHold?

            override func didMoveToWindow() {
                super.didMoveToWindow()
                var v = superview
                while let s = v, !(s is UIScrollView) { v = s.superview }
                if let list = v as? UIScrollView { hold?.list = list }
            }
        }
    }
    #else
    /// The Mac's list keeps its own place (nothing to hand over).
    struct Finder: View {
        let hold: LarkHistoryHold
        var body: some View { Color.clear }
    }
    #endif
}
