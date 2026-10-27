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
    /// Only in GET /api/v1/inbox/{id}: the conversation as text.
    var context: String?

    nonisolated struct Lark: Codable, Hashable, Sendable {
        var feedID: String
        var isGroup: Bool
        var isTopic: Bool?
        var startPosition: String?
        var mentionPosition: String?
        var atAll: Bool?

        enum CodingKeys: String, CodingKey {
            case feedID = "feed_id", isGroup = "is_group", isTopic = "is_topic", startPosition = "start_position"
            case mentionPosition = "mention_position", atAll = "at_all"
        }
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
        case id, source, kind, account, title, sender, avatar, preview, count, tier, summary, reason, read, lark, mail, context
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
        case "mention": lark?.atAll == true ? "@所有人" : "@我"
        case "bot": "机器人"
        default: nil
        }
    }
}

extension APIClient {
    private struct InboxList: Decodable { var items: [InboxItem] }

    /// Open items and the ones done in the last few days, newest first.
    func inbox() async throws -> [InboxItem] { try await get("/api/v1/inbox", as: InboxList.self).items }

    /// Collects Feishu and mail now, then lists.
    func refreshInbox() async throws -> [InboxItem] { try await post("/api/v1/inbox/refresh", timeout: 60, as: InboxList.self).items }

    // Item ids are `lark:<feed>:<position>` / `mail:<account>:<thread>:<uid>`: path-safe as they are
    // (`URL.appending(path:)` would encode an escaped id a second time).

    /// One item with its conversation text.
    func inboxItem(_ id: String) async throws -> InboxItem { try await get("/api/v1/inbox/" + id) }

    func setInboxDone(_ id: String, _ done: Bool) async throws {
        try await post("/api/v1/inbox/" + id + "/done", body: ["done": done])
    }

    private struct InboxText: Codable { var text: String }
    private struct InboxDraftBody: Encodable { var instruction: String }

    /// A reply drafted by the hub's model; `instruction` steers it ("同意，但下周再说").
    func inboxDraft(_ id: String, instruction: String = "") async throws -> String {
        try await post("/api/v1/inbox/" + id + "/draft", body: InboxDraftBody(instruction: instruction), timeout: 90, as: InboxText.self).text
    }

    /// Sends the user's reply (in the Feishu chat, or as a mail reply) and files the item as done.
    func inboxReply(_ id: String, text: String) async throws {
        try await post("/api/v1/inbox/" + id + "/reply", body: InboxText(text: text), timeout: 60)
    }
}
