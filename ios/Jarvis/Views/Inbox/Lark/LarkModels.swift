import Foundation

// MARK: - Feishu (lark-connector) wire types

nonisolated struct LarkConversation: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var avatar: String?
    var isGroup: Bool
    var isTopic: Bool?
    var unread: Int
    var atMe: Bool
    var muted: Bool
    var preview: String
    var isMine: Bool
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, avatar, unread, muted, preview
        case isGroup = "is_group", isTopic = "is_topic", atMe = "at_me", isMine = "is_mine", updatedAt = "updated_at"
    }
}

/// One element of a message as lark-connector projects it (`MessagePart`): text runs, mentions, links, images,
/// emoji, files, media, code and cards. `metadata` stays raw JSON: cards carry their whole element tree there.
nonisolated struct LarkPart: Codable, Hashable, Sendable {
    var kind: String
    var text: String?
    var url: String?
    var refId: String?
    var metadata: JSONValue?

    func meta(_ key: String) -> JSONValue? { metadata?[key] }
    func metaString(_ key: String) -> String? { meta(key)?.string }
    func metaBool(_ key: String) -> Bool { meta(key)?.bool ?? false }

    /// Ancestor chain of a structured rich-text leaf (paragraph / list / quote / heading), outermost first.
    var blocks: [LarkRichBlock] {
        guard let list = meta("blocks")?.array else { return [] }
        var out: [LarkRichBlock] = []
        for item in list {
            guard let id = item["id"]?.string, let type = item["type"]?.string else { return [] }
            out.append(LarkRichBlock(id: id, type: type, listType: item["listType"]?.int ?? 0, start: item["start"]?.int ?? 1))
        }
        return out
    }
}

nonisolated struct LarkRichBlock: Hashable, Sendable {
    var id: String
    var type: String
    var listType: Int
    var start: Int
}

nonisolated struct LarkReplyTo: Codable, Hashable, Sendable {
    var id: String
    var senderName: String
    var text: String
    var recalled: Bool?
    var parts: [LarkPart]?

    enum CodingKeys: String, CodingKey {
        case id, text, recalled, parts
        case senderName = "sender_name"
    }
}

nonisolated struct LarkReaction: Codable, Hashable, Sendable {
    var key: String
    var count: Int
    var mine: Bool?
    var users: [String]?
}

nonisolated struct LarkMessage: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var position: String
    var senderID: String?
    var senderName: String
    var senderAvatar: String?
    var senderType: String?
    var isMine: Bool
    var type: String
    var text: String
    var createdAt: Date?
    var recalled: Bool
    var edited: Bool?
    var replyCount: Int
    var threadReplyCount: Int?
    var parts: [LarkPart]?
    var replyTo: LarkReplyTo?
    var reactions: [LarkReaction]?

    enum CodingKeys: String, CodingKey {
        case id, position, type, text, recalled, edited, parts, reactions
        case senderID = "sender_id", senderName = "sender_name", senderAvatar = "sender_avatar", senderType = "sender_type"
        case isMine = "is_mine", createdAt = "created_at", replyCount = "reply_count"
        case threadReplyCount = "thread_reply_count", replyTo = "reply_to"
    }

    var isSystem: Bool { type == "SYSTEM" || (parts?.first?.metaBool("system") ?? false) }
}

/// A message inside a merge-forward card: lark-connector's own `MessageView` shape (camelCase), media already
/// pointing at the hub.
nonisolated struct LarkForwardedMessage: Decodable, Identifiable, Hashable, Sendable {
    var id: String
    var senderName: String?
    var senderAvatarUrl: String?
    var createdAt: Date?
    var parts: [LarkPart]?
    var textPreview: String?
    var isRecalled: Bool?
}

extension APIClient {
    struct LarkConversations: Decodable {
        var conversations: [LarkConversation]
        var nextCursor: String?
        enum CodingKeys: String, CodingKey { case conversations; case nextCursor = "next_cursor" }
    }
    struct LarkMessages: Decodable {
        var messages: [LarkMessage]
        var hasOlder: Bool
        enum CodingKeys: String, CodingKey { case messages; case hasOlder = "has_older" }
    }
    struct LarkEmojis: Decodable {
        struct Emoji: Decodable { var label: String; var url: String }
        var emojis: [String: Emoji]
    }
    func larkConversations(cursor: String? = nil) async throws -> LarkConversations {
        try await get("/api/v1/lark/conversations", query: ["cursor": cursor ?? "", "limit": "60"])
    }
    /// A page of a chat, or of a topic group's threads (`topic`).
    func larkMessages(_ id: String, before: String? = nil, topic: Bool = false) async throws -> LarkMessages {
        try await get("/api/v1/lark/conversations/\(id)/messages", query: ["before": before ?? "", "limit": "40", "topic": topic ? "1" : ""])
    }
    /// Replies in one message's thread.
    func larkThread(_ messageID: String, before: String? = nil) async throws -> LarkMessages {
        try await get("/api/v1/lark/messages/\(messageID)/thread", query: ["before": before ?? "", "limit": "60"])
    }
    struct LarkText: Encodable { var text: String }
    func larkSend(_ id: String, text: String) async throws { try await post("/api/v1/lark/conversations/\(id)/messages", body: LarkText(text: text)) }
    struct LarkRead: Encodable { var position: String }
    func larkRead(_ id: String, position: String) async throws { try await post("/api/v1/lark/conversations/\(id)/read", body: LarkRead(position: position)) }
    func larkEmojis() async throws -> LarkEmojis { try await get("/api/v1/lark/emojis") }
    /// Presses a callback button on a card, as the user.
    func larkCardAction(message: String, action: String) async throws {
        try await post("/api/v1/lark/messages/\(message)/card-actions/\(action)")
    }
}
