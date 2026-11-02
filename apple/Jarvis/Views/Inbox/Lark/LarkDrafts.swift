import Foundation

/// What was typed into a Feishu box and not sent: the words, and the message the chat's quote bar answers.
nonisolated struct LarkDraft: Codable, Hashable, Sendable {
    var text = ""
    var replyTo: LarkReplyTo?

    /// Nothing worth keeping: no words (blank lines do not count) and no quote.
    var isEmpty: Bool { replyTo == nil && text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}

/// Unsent Feishu drafts, one per chat (keyed by the chat's feed id) and one per thread sheet (keyed by the thread's
/// root message id). Leaving a chat keeps what was typed and the quote it answers; opening it again puts them back;
/// sending clears them. Kept in memory for the run and on disk across launches (StateCache: drafts change on every
/// keystroke, which UserDefaults must not see), written a moment after the last change and when the box goes away.
@MainActor
final class LarkDrafts {
    enum Key: Hashable, Sendable {
        case chat(String)
        case thread(String)

        /// The key on disk.
        var name: String {
            switch self {
            case .chat(let id): "chat:\(id)"
            case .thread(let id): "thread:\(id)"
            }
        }
    }

    static let shared = LarkDrafts()

    private var drafts: [String: LarkDraft]
    private let store: @MainActor ([String: LarkDraft]) -> Void
    private let saver: Debouncer

    /// `load` / `store` stand in for StateCache in tests.
    init(load: () -> [String: LarkDraft]? = { StateCache.load(LarkDrafts.stateName) },
         store: @escaping @MainActor ([String: LarkDraft]) -> Void = { StateCache.save(LarkDrafts.stateName, $0) },
         delay: Duration = .seconds(1)) {
        drafts = (load() ?? [:]).filter { !$0.value.isEmpty }
        self.store = store
        saver = Debouncer(delay)
    }

    nonisolated static let stateName = "lark-drafts"

    /// The draft of that box; empty when there is none.
    subscript(key: Key) -> LarkDraft {
        drafts[key.name] ?? LarkDraft()
    }

    /// Keeps what the box holds now (an empty draft removes it); written to disk a moment after the last change.
    func set(_ draft: LarkDraft, for key: Key) {
        let old = drafts[key.name]
        if draft.isEmpty { drafts[key.name] = nil } else { drafts[key.name] = draft }
        guard drafts[key.name] != old else { return }
        saver.run { [weak self] in self?.flush() }
    }

    /// Writes the drafts now (the box went away; the app may be on its way to the background).
    func flush() {
        store(drafts)
    }
}
