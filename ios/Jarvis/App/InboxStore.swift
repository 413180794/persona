import Foundation
import Observation

/// The unified inbox as the app knows it (docs/inbox.md): the hub's items, kept on disk so the list paints at once
/// (offline too), refreshed when the hub says `inbox` and on (re)connect. Marking done applies at once and is sent
/// in the background; a failure puts the item back.
@MainActor @Observable
final class InboxStore {
    static let shared = InboxStore()

    private(set) var items: [InboxItem]
    private(set) var loading = false
    var error: String?
    @ObservationIgnored private var reloading: Task<Void, Never>?
    @ObservationIgnored private let saver = Debouncer(.seconds(1))
    private static let stateName = "inbox-items"

    private init() {
        items = StateCache.load(Self.stateName, as: [InboxItem].self) ?? []
        teachLarkSync()
    }

    var open: [InboxItem] { items.filter { !$0.isDone } }

    /// What the tab's badge counts: open items that need the user.
    var actionCount: Int { items.filter { !$0.isDone && $0.triage == .action }.count }

    /// Pulls the list (bursts of `inbox` events collapse into one request).
    func reload() {
        guard reloading == nil else { return }
        reloading = Task {
            defer { reloading = nil }
            await fetch { try await $0.inbox() }
        }
    }

    /// Pull to refresh: the hub collects Feishu and mail now.
    func refresh() async {
        await fetch { try await $0.refreshInbox() }
    }

    private func fetch(_ load: (APIClient) async throws -> [InboxItem]) async {
        guard let api = AppModel.shared.api else { return }
        loading = true
        defer { loading = false }
        do {
            apply(try await load(api))
            error = nil
        } catch {
            if let shown = error.shown { self.error = shown }
        }
    }

    private func apply(_ list: [InboxItem]) {
        guard list != items else { return }
        items = list
        teachLarkSync()
        let snapshot = list
        saver.run { StateCache.save(Self.stateName, snapshot) }
        WatchSync.shared.push(summary: AppModel.shared.watchSummary())
    }

    /// 全部看过 / 全部清除: a whole section at once.
    func setDone(_ list: [InboxItem], _ done: Bool) {
        for it in list { setDone(it, done, haptic: false) }
        Haptics.success()
    }

    func setDone(_ item: InboxItem, _ done: Bool, haptic: Bool = true) {
        guard let i = items.firstIndex(where: { $0.id == item.id }) else { return }
        let before = items[i]
        items[i].doneAt = done ? .now : nil
        items[i].doneBy = done ? "user" : nil
        if haptic { Haptics.tap() }
        Task {
            do {
                try await AppModel.shared.api?.setInboxDone(item.id, done)
            } catch {
                if let j = items.firstIndex(where: { $0.id == item.id }) { items[j] = before }
                AppModel.shared.show(error)
            }
        }
    }

    /// Items whose reply is being sent from the list (the row shows a spinner, a second tap does nothing).
    private(set) var sending: Set<String> = []

    /// 发送 on the AI 分身's draft: the hub replies in the chat / by mail and files the item as done.
    func send(_ item: InboxItem, text: String) async -> Bool {
        guard let api = AppModel.shared.api, !sending.contains(item.id) else { return false }
        sending.insert(item.id)
        defer { sending.remove(item.id) }
        do {
            try await api.inboxReply(item.id, text: text)
            if let i = items.firstIndex(where: { $0.id == item.id }) {
                items[i].doneAt = .now
                items[i].doneBy = "replied"
            }
            Haptics.success()
            AppModel.shared.toast = Toast("已发送，标为已处理", style: .success)
            return true
        } catch {
            AppModel.shared.show(error)
            return false
        }
    }

    /// Feishu chats in the inbox are the ones background sync keeps fresh (their pages open at once).
    private func teachLarkSync() {
        LarkSync.shared.know(items.compactMap(\.larkConversation))
    }
}

extension InboxItem {
    /// The Feishu chat to open, at the first new message.
    var larkConversation: LarkConversation? {
        guard let l = lark else { return nil }
        var c = LarkConversation(id: l.feedID, name: title, avatar: avatar, isGroup: l.isGroup, isTopic: l.isTopic,
                                 unread: isDone ? 0 : count, atMe: kind == "mention")
        c.updatedAt = updatedAt
        if !isDone, let p = l.startPosition { c.focus = p }
        return c
    }

    /// The newest mail of the thread, for the mail reader.
    var mailSummary: MailSummary? {
        guard let m = mail, let uid = m.uids.last else { return nil }
        return MailSummary(accountID: m.accountID, mailbox: m.mailbox, uid: uid, messageID: m.messageID, subject: title,
                           from: m.from, date: updatedAt, snippet: preview, seen: isDone || read == true)
    }
}

extension MailSummary {
    init(accountID: String, mailbox: String, uid: UInt32, messageID: String?, subject: String?, from: MailAddress?, date: Date?,
         snippet: String?, seen: Bool) {
        self.accountID = accountID
        self.mailbox = mailbox
        self.uid = uid
        self.messageID = messageID
        self.subject = subject
        self.from = from
        self.to = nil
        self.date = date
        self.snippet = snippet
        self.seen = seen
        flagged = false
        hasAttachments = false
    }
}
