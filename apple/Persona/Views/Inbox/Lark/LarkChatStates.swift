import Foundation

/// Why a Feishu chat could not be loaded, in the page's words: what happened (`title`), what to do (`message`).
nonisolated struct LarkLoadProblem: Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        /// The Lark gateway on the hub's machine lost its Feishu login: only signing in there again helps.
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

    /// The hub's code for the Lark gateway without a Feishu login (docs/protocol/protocol.md, Feishu routes).
    static let notLoggedInCode = "lark_not_logged_in"

    /// `subject` names what did not load in the catch-all title (this chat, this topic).
    static func of(_ error: Error, subject: String = String(localized: "this chat")) -> LarkLoadProblem {
        if isNotLoggedIn(error) {
            return LarkLoadProblem(kind: .notLoggedIn, title: String(localized: "Lark Sign-in Expired"),
                                   message: String(localized: "The Lark gateway on the hub machine isn't signed in to Lark. Sign in to the Lark gateway again on that machine, then come back and retry."),
                                   symbol: Theme.Icon.credentials,
                                   inline: String(localized: "Lark sign-in expired: sign in to the Lark gateway again on the hub machine"))
        }
        if error.isOffline {
            return LarkLoadProblem(kind: .offline, title: String(localized: "Can't reach the hub"), message: String(localized: "Check your network connection and try again."),
                                   symbol: Theme.Icon.offline, inline: nil)
        }
        if error.isTimeout {
            return LarkLoadProblem(kind: .timeout, title: String(localized: "Loading timed out"), message: String(localized: "The hub didn't respond. Try again later."),
                                   symbol: Theme.Icon.error, inline: nil)
        }
        return LarkLoadProblem(kind: .other, title: String(localized: "Couldn't load \(subject)"), message: error.localizedDescription,
                               symbol: Theme.Icon.error, inline: error.shown)
    }

    /// The hub's code says so; a hub from before the code (502 with the Lark gateway's「未登录飞书」) is read by its words.
    static func isNotLoggedIn(_ error: Error) -> Bool {
        guard let e = error as? APIError else { return false }
        return e.code == notLoggedInCode || (e.status == 502 && e.message.contains("未登录飞书")) // i18n-ignore
    }
}

/// What a Feishu chat shows in place of its messages when it has none on screen.
nonisolated enum LarkChatPlaceholder: Equatable, Sendable {
    /// The first page is on its way and nothing was cached.
    case loading
    /// Loaded, and the chat has no messages.
    case empty
    /// Nothing to show and the load failed: the problem, with Retry.
    case failed(LarkLoadProblem)

    /// Nil while there is something to show (messages, or mine on their way): a problem then is one line on top.
    static func of(hasContent: Bool, loading: Bool, problem: LarkLoadProblem?) -> LarkChatPlaceholder? {
        if hasContent { return nil }
        if let problem { return .failed(problem) }
        return loading ? .loading : .empty
    }
}
