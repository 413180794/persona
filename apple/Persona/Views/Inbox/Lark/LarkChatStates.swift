import Foundation

/// Why a Feishu chat could not be loaded, in the page's words: what happened (`title`), what to do (`message`).
nonisolated struct LarkLoadProblem: Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        /// lark-connector on the hub's machine lost its Feishu login: only signing in there again helps.
        case notLoggedIn
        /// The phone cannot reach the hub.
        case offline
        case timeout
        case other
    }

    var kind: Kind
    var title: String
    var message: String
    var symbol: String
    /// One line over messages already on screen (from the cache), or nil to say nothing there: a timeout or no
    /// network is left to the title bar, as everywhere else (Error.shown).
    var inline: String?

    /// The hub's code for lark-connector without a Feishu login (docs/protocol.md, Feishu routes).
    static let notLoggedInCode = "lark_not_logged_in"

    static func of(_ error: Error) -> LarkLoadProblem {
        if isNotLoggedIn(error) {
            return LarkLoadProblem(kind: .notLoggedIn, title: "飞书登录已失效",
                                   message: "hub 所在机器上的 lark-connector 没有登录飞书。在那台机器上重新登录 lark-connector，再回来重试。",
                                   symbol: Theme.Icon.credentials,
                                   inline: "飞书登录已失效：在 hub 所在的机器上重新登录 lark-connector")
        }
        if error.isOffline {
            return LarkLoadProblem(kind: .offline, title: "连不上 hub", message: "检查网络连接后重试。",
                                   symbol: Theme.Icon.offline, inline: nil)
        }
        if error.isTimeout {
            return LarkLoadProblem(kind: .timeout, title: "加载超时", message: "hub 一直没有回应，稍后重试。",
                                   symbol: Theme.Icon.error, inline: nil)
        }
        return LarkLoadProblem(kind: .other, title: "没能加载这个会话", message: error.localizedDescription,
                               symbol: Theme.Icon.error, inline: error.shown)
    }

    /// The hub's code says so; a hub from before the code (502 with lark-connector's「未登录飞书」) is read by its words.
    static func isNotLoggedIn(_ error: Error) -> Bool {
        guard let e = error as? APIError else { return false }
        return e.code == notLoggedInCode || (e.status == 502 && e.message.contains("未登录飞书"))
    }
}

/// What a Feishu chat shows in place of its messages when it has none on screen.
nonisolated enum LarkChatPlaceholder: Equatable, Sendable {
    /// The first page is on its way and nothing was cached.
    case loading
    /// Loaded, and the chat has no messages.
    case empty
    /// Nothing to show and the load failed: the problem, with 重试.
    case failed(LarkLoadProblem)

    /// Nil while there is something to show (messages, or mine on their way): a problem then is one line on top.
    static func of(hasContent: Bool, loading: Bool, problem: LarkLoadProblem?) -> LarkChatPlaceholder? {
        if hasContent { return nil }
        if let problem { return .failed(problem) }
        return loading ? .loading : .empty
    }
}
