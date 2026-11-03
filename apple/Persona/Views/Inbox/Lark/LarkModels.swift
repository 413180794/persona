import Foundation

// MARK: - Feishu (lark-connector) wire types

nonisolated struct LarkConversation: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var avatar: String?
    var isGroup: Bool
    var isTopic: Bool?
    var unread: Int
    var markedUnread: Bool?
    var atMe: Bool
    var atAll: Bool?
    var muted: Bool
    var pinned: Bool?
    var flagged: Bool?
    var done: Bool?
    var tags: [String]?
    var preview: String
    var previewSender: String?
    var isMine: Bool
    var updatedAt: Date?
    var readPosition: String?
    var mentionPosition: String?
    /// Client only: open the chat at this message (a flagged message) instead of the first unread.
    var focus: String?

    enum CodingKeys: String, CodingKey {
        case id, name, avatar, unread, muted, pinned, flagged, done, tags, preview
        case isGroup = "is_group", isTopic = "is_topic", markedUnread = "marked_unread", atMe = "at_me", atAll = "at_all"
        case previewSender = "preview_sender", isMine = "is_mine", updatedAt = "updated_at"
        case readPosition = "read_position", mentionPosition = "mention_position"
    }

    init(id: String, name: String, avatar: String? = nil, isGroup: Bool, isTopic: Bool? = nil, unread: Int = 0, atMe: Bool = false,
         muted: Bool = false, preview: String = "", isMine: Bool = false, focus: String? = nil) {
        self.id = id
        self.name = name
        self.avatar = avatar
        self.isGroup = isGroup
        self.isTopic = isTopic
        self.unread = unread
        self.atMe = atMe
        self.muted = muted
        self.preview = preview
        self.isMine = isMine
        self.focus = focus
    }

    /// The number on the avatar: unread messages, or 1 for a chat marked unread.
    var badge: Int { unread > 0 ? unread : (markedUnread == true ? 1 : 0) }
    var isPinned: Bool { pinned == true }
    var isFlagged: Bool { flagged == true }
    var isDone: Bool { done == true }
}

/// A flagged message in 标记, with the chat it belongs to.
nonisolated struct LarkFlaggedMessage: Codable, Identifiable, Hashable, Sendable {
    var feedID: String
    var chatName: String
    var chatAvatar: String?
    var flaggedAt: Date?
    var message: LarkMessage
    var id: String { message.id }

    enum CodingKeys: String, CodingKey {
        case message
        case feedID = "feed_id", chatName = "chat_name", chatAvatar = "chat_avatar", flaggedAt = "flagged_at"
    }
}

nonisolated struct LarkLabel: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var count: Int?
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
    var badgeCount: Int?
    var atMe: Bool?
    var isRead: Bool?
    var parts: [LarkPart]?
    var replyTo: LarkReplyTo?
    var reactions: [LarkReaction]?

    enum CodingKeys: String, CodingKey {
        case id, position, type, text, recalled, edited, parts, reactions
        case senderID = "sender_id", senderName = "sender_name", senderAvatar = "sender_avatar", senderType = "sender_type"
        case isMine = "is_mine", createdAt = "created_at", replyCount = "reply_count"
        case threadReplyCount = "thread_reply_count", replyTo = "reply_to"
        case badgeCount = "badge_count", atMe = "at_me", isRead = "is_read"
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
    nonisolated struct LarkMessages: Decodable, Sendable {
        var messages: [LarkMessage]
        var hasOlder: Bool
        var hasNewer: Bool?
        var olderCursor: String?
        var newerCursor: String?
        /// The chat is a topic group and these are its topics (the hub found out even when the request did not say so).
        var topic: Bool?
        enum CodingKeys: String, CodingKey {
            case messages, topic
            case hasOlder = "has_older", hasNewer = "has_newer", olderCursor = "older_cursor", newerCursor = "newer_cursor"
        }
    }
    struct LarkEmojis: Decodable {
        struct Emoji: Decodable { var label: String; var url: String }
        struct Group: Decodable, Hashable { var title: String; var keys: [String] }
        var emojis: [String: Emoji]
        var groups: [Group]?
    }
    struct LarkFlags: Decodable {
        var conversations: [LarkConversation]
        var messages: [LarkFlaggedMessage]
    }
    struct LarkChatLabels: Decodable {
        var labelIDs: [String]
        var labels: [LarkLabel]
        enum CodingKeys: String, CodingKey { case labels; case labelIDs = "label_ids" }
    }
    /// What to change on a chat: 已完成, 置顶, 标记, 免打扰, labels. Nil fields stay as they are.
    struct LarkSettings: Encodable {
        var done: Bool?
        var pinned: Bool?
        var flagged: Bool?
        var muted: Bool?
        var labelIDs: [String]?
        enum CodingKeys: String, CodingKey { case done, pinned, flagged, muted; case labelIDs = "label_ids" }
    }
    struct LarkReadReport: Encodable {
        var position: String
        var badgeCount: Int?
        var messageIDs: [String]?
        enum CodingKeys: String, CodingKey { case position; case badgeCount = "badge_count", messageIDs = "message_ids" }
    }

    /// One page of a feed scope: `chats` (消息), `unread`, `done` (已完成), `label:<id>`.
    func larkConversations(scope: String = "chats", cursor: String? = nil) async throws -> LarkConversations {
        try await get("/api/v1/lark/conversations", query: ["scope": scope, "cursor": cursor ?? "", "limit": "60"])
    }
    func larkPinned() async throws -> [LarkConversation] {
        try await get("/api/v1/lark/pinned", as: LarkConversations.self).conversations
    }
    func larkLabels() async throws -> [LarkLabel] {
        struct R: Decodable { var labels: [LarkLabel] }
        return try await get("/api/v1/lark/labels", as: R.self).labels
    }
    func larkUnreadCount() async throws -> Int {
        struct R: Decodable { var count: Int }
        return try await get("/api/v1/lark/unread-count", as: R.self).count
    }
    func larkFlags() async throws -> LarkFlags { try await get("/api/v1/lark/flags") }
    func larkChatLabels(_ id: String) async throws -> LarkChatLabels { try await get("/api/v1/lark/conversations/\(id)/labels") }
    func larkUpdate(_ id: String, _ settings: LarkSettings) async throws {
        try await post("/api/v1/lark/conversations/\(id)/settings", body: settings)
    }
    struct LarkUnread: Encodable { var unread: Bool }
    func larkSetUnread(_ id: String, unread: Bool) async throws {
        try await post("/api/v1/lark/conversations/\(id)/unread", body: LarkUnread(unread: unread))
    }
    /// A page of a chat: the latest (no cursor), older than `before`, newer than `after`, or around `around`;
    /// a topic group's threads with `topic`.
    /// Decoded off the main thread: an open chat asks again on every change in it, and a page of cards takes a few ms.
    func larkMessages(_ id: String, before: String? = nil, after: String? = nil, around: String? = nil, topic: Bool = false) async throws -> LarkMessages {
        try await getDecodedOffMain("/api/v1/lark/conversations/\(id)/messages",
                                    query: ["before": before ?? "", "after": after ?? "", "around": around ?? "", "limit": "40", "topic": topic ? "1" : ""])
    }
    /// Replies in one message's thread (decoded off the main thread, like `larkMessages`).
    func larkThread(_ messageID: String, before: String? = nil) async throws -> LarkMessages {
        try await getDecodedOffMain("/api/v1/lark/messages/\(messageID)/thread", query: ["before": before ?? "", "limit": "60"])
    }
    struct LarkText: Encodable {
        var text: String
        /// The same for every try of one message, so the hub sends it once however often it is retried.
        var clientID: String?
        /// Who Feishu notifies; each one's `@name` is in `text`. Left out when nobody is mentioned (an older hub
        /// ignores it anyway).
        var mentions: [LarkMention]?
        enum CodingKeys: String, CodingKey { case text, mentions, clientID = "client_id" }
    }
    /// lark-connector can take a while to confirm a send; a retry after an early timeout is what sent twice.
    static let larkSendTimeout: TimeInterval = 45
    func larkSend(_ id: String, text: String, clientID: String? = nil, mentions: [LarkMention] = []) async throws {
        try await post("/api/v1/lark/conversations/\(id)/messages",
                       body: LarkText(text: text, clientID: clientID, mentions: mentions.isEmpty ? nil : mentions),
                       timeout: Self.larkSendTimeout)
    }
    struct LarkReplyBody: Encodable {
        var text: String
        /// True: a reply in the message's thread; false: a quoted reply in its chat.
        var thread: Bool
        var clientID: String?
        var mentions: [LarkMention]?
        enum CodingKeys: String, CodingKey { case text, thread, mentions, clientID = "client_id" }
    }
    /// A reply to one message: quoted in its chat, or in its thread. Same idempotency and timeout as `larkSend`.
    func larkReply(messageID: String, text: String, thread: Bool, clientID: String? = nil, mentions: [LarkMention] = []) async throws {
        try await post("/api/v1/lark/messages/\(messageID)/replies",
                       body: LarkReplyBody(text: text, thread: thread, clientID: clientID, mentions: mentions.isEmpty ? nil : mentions),
                       timeout: Self.larkSendTimeout)
    }
    struct LarkMembers: Decodable { var members: [LarkMember] }
    /// A chat's members for the @ picker: all of them for an empty `query`, else the hub's search (name, group
    /// nickname, pinyin).
    func larkMembers(_ id: String, query: String, limit: Int = 30) async throws -> LarkMembers {
        try await get("/api/v1/lark/conversations/\(id)/members", query: ["q": query, "limit": String(limit)])
    }
    /// Reports what the reader has seen (never sent just for opening a chat).
    func larkRead(_ id: String, _ report: LarkReadReport) async throws { try await post("/api/v1/lark/conversations/\(id)/read", body: report) }
    func larkEmojis() async throws -> LarkEmojis { try await get("/api/v1/lark/emojis") }
    /// Presses a callback button on a card, as the user.
    func larkCardAction(message: String, action: String) async throws {
        try await post("/api/v1/lark/messages/\(message)/card-actions/\(action)")
    }

    // MARK: People, reactions, link previews, removed

    func larkProfile(_ userID: String, feedID: String? = nil) async throws -> LarkProfile {
        try await get("/api/v1/lark/users/\(userID)", query: ["feed_id": feedID ?? ""])
    }
    /// The 1:1 chat with a user (created when there is none yet).
    func larkDirectChat(_ userID: String) async throws -> String {
        struct R: Decodable { var feed_id: String }
        return try await post("/api/v1/lark/users/\(userID)/chat", as: R.self).feed_id
    }
    struct LarkReact: Encodable { var key: String; var remove: Bool }
    func larkReact(_ messageID: String, key: String, remove: Bool) async throws {
        try await post("/api/v1/lark/messages/\(messageID)/reactions", body: LarkReact(key: key, remove: remove))
    }
    func larkQuickReactions() async throws -> [String] {
        struct R: Decodable { var keys: [String] }
        return try await get("/api/v1/lark/reactions/quick", as: R.self).keys
    }
    func larkURLPreview(message: String, url: String) async throws -> LarkURLPreview {
        try await get("/api/v1/lark/messages/\(message)/url-preview", query: ["url": url])
    }
    func larkMomentsPost(_ id: String) async throws -> LarkMomentsPost { try await get("/api/v1/lark/moments/posts/\(id)") }
    func larkMomentsComments(_ id: String) async throws -> [LarkMomentsComment] {
        struct R: Decodable { var comments: [LarkMomentsComment] }
        return try await get("/api/v1/lark/moments/posts/\(id)/comments", as: R.self).comments
    }
}

/// A person's profile card (lark-connector `UserProfileDetail`, the fields the card shows).
nonisolated struct LarkProfile: Decodable, Sendable {
    struct Field: Decodable, Hashable, Sendable {
        var key: String
        var label: String?
        var type: Int?
        var value: JSONValue?
        var url: String?
    }
    struct Action: Decodable, Hashable, Sendable {
        var key: String
        var label: String?
        var enabled: Bool?
    }
    struct Status: Decodable, Sendable { var title: String? }
    struct WorkStatus: Decodable, Sendable { var status: Int? }
    struct Relation: Decodable, Sendable {
        var external: Bool?
        var tenantName: String?
        var memo: String?
    }
    var displayName: String?
    var enName: String?
    var alias: String?
    var anotherName: String?
    var groupNickname: String?
    var avatarUrl: String?
    var coverUrl: String?
    var description: String?
    var medalName: String?
    var timezone: String?
    var userType: String?
    var customStatus: Status?
    var workStatus: WorkStatus?
    var relation: Relation?
    var fields: [Field]?
    var actions: [Action]?
}

/// The card Feishu shows under a link (a removed post: author, excerpt, counts).
nonisolated struct LarkURLPreview: Decodable, Sendable {
    struct Card: Decodable, Sendable {
        var author: String?
        var avatarUrl: String?
        var body: String?
        var footer: String?
        var subtitle: String?
    }
    var title: String?
    var host: String?
    var url: String?
    var card: Card?
}

/// One removed post.
nonisolated struct LarkMomentsPost: Decodable, Sendable {
    struct Segment: Decodable, Sendable { var kind: String?; var text: String?; var key: String?; var emojiKey: String? }
    struct Content: Decodable, Sendable { var text: String?; var segments: [Segment]? }
    struct Image: Decodable, Hashable, Sendable { var url: String?; var width: Double?; var height: Double? }
    struct ReactionGroup: Decodable, Hashable, Sendable { var type: String; var count: Int; var selfInvolved: Bool? }
    var id: String
    var userName: String?
    var userId: String?
    var userAvatarUrl: String?
    var content: Content?
    var images: [Image]?
    var reactionGroups: [ReactionGroup]?
    var reactionCount: Int?
    var commentCount: Int?
    var createTime: Double?
    var categoryNames: [String]?
}

nonisolated struct LarkMomentsComment: Decodable, Identifiable, Sendable {
    var id: String
    var userName: String?
    var userId: String?
    var userAvatarUrl: String?
    var content: LarkMomentsPost.Content?
    var createTime: Double?
    var reactionCount: Int?
    var isHot: Bool?
}
