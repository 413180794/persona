import Foundation

/// One "thing" in the unified inbox (docs/inbox.md): the new messages in a Feishu chat or a mail thread, with the
/// hub's AI triage and whether it has been dealt with. Mirrors hub/inbox.go `InboxItem`.
nonisolated struct InboxItem: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var source: String // lark | mail
    var kind: String   // dm | mention | group | bot | mail
    var account: String
    var accountName: String
    var title: String
    var sender: String?
    var avatar: String?
    var preview: String
    var count: Int
    var startedAt: Date?
    var updatedAt: Date?
    var tier: String?
    var summary: String?
    var reason: String?
    var read: Bool?
    var doneAt: Date?
    var doneBy: String?
    var lark: Lark?
    var mail: Mail?
    /// A Feishu document's @me / comment: the rows about the user with their status columns (docs/inbox.md「云文档」).
    var doc: Doc?
    /// New foreign mail translated by the hub as it came in, by target language (subject and preview here; the body
    /// comes with the mail).
    var translations: [String: Translated]?
    /// A removed post where someone commented on, replied to or @ed the user (docs/inbox.md「removed」).
    var moment: Moment?
    /// Only in GET /api/v1/inbox/{id}: the conversation as text.
    var context: String?
    /// The AI 分身's work on it: judged, handled by an agent, a reply drafted (docs/inbox.md).
    var agent: Agent?

    nonisolated struct Moment: Codable, Hashable, Sendable {
        var postID: String
        var notice: String?  // comment | reply | atInPost | atInComment (the newest)
        var replyTo: String? // the comment 发送 answers; nil: on the post
        var myPost: Bool?
        var postText: String?

        enum CodingKeys: String, CodingKey {
            case notice
            case postID = "post_id", replyTo = "reply_to", myPost = "my_post", postText = "post_text"
        }
    }

    nonisolated struct Agent: Codable, Hashable, Sendable {
        var state: String // skipped | queued | running | drafted | failed
        var task: String?
        var reason: String?
        var machineID: String?
        var sessionID: String?
        var draft: String?
        var note: String?
        var error: String?
        /// Status moves the agent proposes with its draft (document items); 发送 makes the ones kept.
        var changes: [DocChange]?

        enum CodingKeys: String, CodingKey {
            case state, task, reason, draft, note, error, changes
            case machineID = "machine_id", sessionID = "session_id"
        }

        /// The agent's conversation (in 对话), to see how it got to the draft.
        var sessionKey: SessionKey? {
            guard let m = machineID, let s = sessionID, !m.isEmpty, !s.isEmpty else { return nil }
            return SessionKey(machineID: m, sessionID: s)
        }
    }

    /// The agent's reply, ready to send.
    var agentDraft: String? {
        guard agent?.state == "drafted", let d = agent?.draft, !d.isEmpty else { return nil }
        return d
    }

    /// An agent is on it (queued or working).
    var agentWorking: Bool { agent?.state == "queued" || agent?.state == "running" }

    nonisolated struct Lark: Codable, Hashable, Sendable {
        var feedID: String
        var isGroup: Bool
        var isTopic: Bool?
        var startPosition: String?
        /// The chat's read position in Feishu when collected; older hubs leave it out.
        var readPosition: String?
        var mentionPosition: String?
        var atAll: Bool?

        enum CodingKeys: String, CodingKey {
            case feedID = "feed_id", isGroup = "is_group", isTopic = "is_topic", startPosition = "start_position"
            case readPosition = "read_position", mentionPosition = "mention_position", atAll = "at_all"
        }
    }

    nonisolated struct Translated: Codable, Hashable, Sendable {
        var subject: String
        var preview: String
    }

    /// The hub's translation into `target` (mail only); whether to show it is the app's setting.
    func translated(_ target: String) -> Translated? { mail != nil ? translations?[target] : nil }

    nonisolated struct Doc: Codable, Hashable, Sendable {
        var url: String
        var link: String?
        var type: String   // docx | doc | sheet | base | other
        var notice: String // mention | comment | reply
        var rows: [DocRow]?
        var reply: Reply?
        var error: String?

        nonisolated struct Reply: Codable, Hashable, Sendable {
            var kind: String // thread | record | cell | doc
            var label: String
        }

        /// 表格 / 多维表格 / 文档.
        var typeName: String {
            switch type {
            case "sheet": "表格"
            case "base": "多维表格"
            case "docx", "doc": "文档"
            default: "云文档"
            }
        }

        var symbol: String {
            switch type {
            case "sheet": Theme.Icon.sheet
            case "base": Theme.Icon.base
            default: Theme.Icon.document
            }
        }
    }

    nonisolated struct DocRow: Codable, Hashable, Sendable, Identifiable {
        var ref: String
        var table: String?
        var title: String
        var fields: [DocField]?
        var status: [DocStatus]?
        var id: String { ref }
    }

    nonisolated struct DocField: Codable, Hashable, Sendable {
        var name: String
        var value: String
    }

    nonisolated struct DocStatus: Codable, Hashable, Sendable, Identifiable {
        var field: String
        var name: String
        var value: String
        var options: [DocOption]
        var id: String { field }
    }

    nonisolated struct DocOption: Codable, Hashable, Sendable {
        var name: String
        var color: String?
    }

    nonisolated struct DocChange: Codable, Hashable, Sendable {
        var ref: String
        var field: String
        var value: String
    }

    nonisolated struct Mail: Codable, Hashable, Sendable {
        var accountID: String
        var mailbox: String
        var uids: [UInt32]
        var messageID: String?
        var from: MailAddress?
        var direct: Bool?
        var thread: String?

        enum CodingKeys: String, CodingKey {
            case mailbox, uids, from, direct, thread
            case accountID = "account_id", messageID = "message_id"
        }
    }

    enum CodingKeys: String, CodingKey {
        case id, source, kind, account, title, sender, avatar, preview, count, tier, summary, reason, read, lark, mail, doc, moment, context, agent, translations
        case accountName = "account_name", startedAt = "started_at", updatedAt = "updated_at", doneAt = "done_at", doneBy = "done_by"
    }

    var isDone: Bool { doneAt != nil }
    var isLark: Bool { source == "lark" }

    /// Triage tiers, most urgent first. Items not triaged yet count as 需要你 until the AI says otherwise.
    enum Tier: String, CaseIterable, Sendable {
        case action, fyi, noise
        var title: String {
            switch self {
            case .action: "需要你"
            case .fyi: "知悉即可"
            case .noise: "已折叠"
            }
        }
    }

    var triage: Tier { tier.flatMap(Tier.init(rawValue:)) ?? .action }
    var isTriaged: Bool { tier?.isEmpty == false }

    /// The short tag next to the title.
    var kindLabel: String? {
        switch kind {
        case "dm": "私聊"
        // Read elsewhere: the @ is no longer unread, so the row stops flagging it (it stays for its tier).
        case "mention": read == true ? nil : lark?.atAll == true ? "@所有人" : "@我"
        case "bot": "机器人"
        case "doc": doc?.notice == "comment" || doc?.notice == "reply" ? "评论" : "@我"
        case "moment": moment?.notice?.hasPrefix("atIn") == true ? "@我" : "removed"
        default: nil
        }
    }
}

extension APIClient {
    private nonisolated struct InboxList: Decodable, Sendable { var items: [InboxItem] }

    private static func decodeInbox(_ data: Data) async throws -> [InboxItem] {
        try await Task.detached(priority: .userInitiated) {
            try JSON.decoder.decode(InboxList.self, from: data).items
        }.value
    }

    /// Open items and the ones done in the last few days, newest first.
    func inbox() async throws -> [InboxItem] {
        try await Self.decodeInbox(request("GET", "/api/v1/inbox"))
    }

    /// Collects Feishu and mail now, then lists.
    func refreshInbox() async throws -> [InboxItem] {
        try await Self.decodeInbox(post("/api/v1/inbox/refresh", timeout: 60))
    }

    // Item ids are `lark:<feed>:<position>` / `mail:<account>:<thread>:<uid>`: path-safe as they are
    // (`URL.appending(path:)` would encode an escaped id a second time).

    /// One item with its conversation text.
    func inboxItem(_ id: String) async throws -> InboxItem { try await get("/api/v1/inbox/" + id) }

    func setInboxDone(_ id: String, _ done: Bool) async throws {
        try await post("/api/v1/inbox/" + id + "/done", body: ["done": done])
    }

    private struct InboxText: Codable { var text: String }
    private struct InboxReplyBody: Encodable { var text: String; var changes: [InboxItem.DocChange]? }
    private struct InboxDraftBody: Encodable { var instruction: String }

    /// A reply drafted by the hub's model; `instruction` steers it ("同意，但下周再说").
    func inboxDraft(_ id: String, instruction: String = "") async throws -> String {
        try await post("/api/v1/inbox/" + id + "/draft", body: InboxDraftBody(instruction: instruction), timeout: 90, as: InboxText.self).text
    }

    /// Sends the user's reply (in the Feishu chat, as a mail reply, or as a comment in the document, moving the rows'
    /// status in `changes` first) and files the item as done.
    func inboxReply(_ id: String, text: String, changes: [InboxItem.DocChange] = []) async throws {
        try await post("/api/v1/inbox/" + id + "/reply", body: InboxReplyBody(text: text, changes: changes.isEmpty ? nil : changes), timeout: 60)
    }

    /// Moves one row's status in the document; the item comes back read again.
    func inboxDocStatus(_ id: String, _ change: InboxItem.DocChange) async throws -> InboxItem {
        try await post("/api/v1/inbox/" + id + "/doc/status", body: change, timeout: 60, as: InboxItem.self)
    }
}
