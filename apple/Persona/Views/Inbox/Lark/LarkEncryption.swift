import Foundation
import SwiftUI

nonisolated struct LarkEncryption: Codable, Hashable, Sendable {
    var version: Int
    var state: String
    var publicParts: [LarkPart]
    var bodyParts: [LarkPart]?
    var keyHandout: Bool?

    var isOpen: Bool { version == 1 && state == "decrypted" }
    var visibleParts: [LarkPart] { isOpen ? (bodyParts ?? []) : publicParts }
    var sharesKey: Bool { isOpen && keyHandout == true }
    var hasBody: Bool { !visibleParts.isEmpty }
    var coverLabel: String { state == "unavailable" ? "加密消息 · 仅显示公开内容" : "加密消息 · 对外展示" }
    var textSummary: String { visibleParts.compactMap(\.text).joined().trimmingCharacters(in: .whitespacesAndNewlines) }
    var summary: String {
        let text = textSummary
        if !text.isEmpty { return text }
        return sharesKey ? "分发了本群的加密钥匙" : "加密消息"
    }
    var cached: Self { Self(version: version, state: "unavailable", publicParts: publicParts) }
}

nonisolated extension LarkEncryption {
    /// A partial/unknown optional projection must not discard an otherwise readable message page.
    /// Missing version or state stays closed; never fall back to legacy flattened plaintext.
    init(from decoder: Decoder) throws {
        guard let c = try? decoder.container(keyedBy: CodingKeys.self) else {
            self.init(version: 0, state: "unavailable", publicParts: [])
            return
        }
        self.init(version: (try? c.decodeIfPresent(Int.self, forKey: .version)) ?? 0,
                  state: (try? c.decodeIfPresent(String.self, forKey: .state)) ?? "unavailable",
                  publicParts: (try? c.decodeIfPresent([LarkPart].self, forKey: .publicParts)) ?? [])
        if version == 1 && state == "decrypted" {
            bodyParts = try? c.decodeIfPresent([LarkPart].self, forKey: .bodyParts)
            keyHandout = try? c.decodeIfPresent(Bool.self, forKey: .keyHandout)
        }
    }
}

struct LarkEncryptedContent: View {
    let encryption: LarkEncryption

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if !encryption.isOpen && encryption.hasBody {
                Label(encryption.coverLabel, systemImage: Theme.Icon.locked)
                    .font(Theme.Font.caption).foregroundStyle(.secondary)
            }
            if encryption.hasBody {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.xs) {
                    if encryption.isOpen {
                        Image(systemName: Theme.Icon.locked).font(Theme.Font.caption).foregroundStyle(.secondary)
                            .accessibilityLabel("加密消息")
                    }
                    LarkPartsView(parts: encryption.visibleParts)
                }
            }
            if encryption.isOpen && encryption.hasBody && !encryption.publicParts.isEmpty {
                HStack(alignment: .firstTextBaseline, spacing: Theme.Space.xs) {
                    Text("对外展示：").font(Theme.Font.caption).foregroundStyle(.secondary)
                    LarkPartsView(parts: encryption.publicParts, base: LarkTextBase(size: Theme.Level.caption.size, color: .secondary))
                }
                .accessibilityElement(children: .combine)
            }
            if encryption.sharesKey {
                Label("分发了本群的加密钥匙", systemImage: Theme.Icon.locked)
                    .font(encryption.hasBody ? Theme.Font.label : Theme.Font.body)
                    .foregroundStyle(encryption.hasBody ? .secondary : .primary)
            }
            if !encryption.hasBody && !encryption.sharesKey {
                Label("加密消息", systemImage: Theme.Icon.locked).font(Theme.Font.body).foregroundStyle(.secondary)
            }
        }
    }
}

extension LarkMessage {
    nonisolated var displayText: String { olk?.summary ?? text }

    /// Disk snapshots contain only the public cover, even when a read returned plaintext.
    nonisolated func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(position, forKey: .position)
        try c.encodeIfPresent(senderID, forKey: .senderID)
        try c.encode(senderName, forKey: .senderName)
        try c.encodeIfPresent(senderAvatar, forKey: .senderAvatar)
        try c.encodeIfPresent(senderType, forKey: .senderType)
        try c.encode(isMine, forKey: .isMine)
        try c.encode(type, forKey: .type)
        try c.encode(olk?.cached.summary ?? text, forKey: .text)
        try c.encodeIfPresent(createdAt, forKey: .createdAt)
        try c.encode(recalled, forKey: .recalled)
        try c.encodeIfPresent(edited, forKey: .edited)
        try c.encode(replyCount, forKey: .replyCount)
        try c.encodeIfPresent(threadReplyCount, forKey: .threadReplyCount)
        try c.encodeIfPresent(badgeCount, forKey: .badgeCount)
        try c.encodeIfPresent(atMe, forKey: .atMe)
        try c.encodeIfPresent(isRead, forKey: .isRead)
        try c.encodeIfPresent(olk?.publicParts ?? parts, forKey: .parts)
        try c.encodeIfPresent(replyTo, forKey: .replyTo)
        try c.encodeIfPresent(reactions, forKey: .reactions)
        try c.encodeIfPresent(clientID, forKey: .clientID)
        try c.encodeIfPresent(localState, forKey: .localState)
        try c.encodeIfPresent(olk?.cached, forKey: .olk)
    }
}

extension LarkReplyTo {
    /// A cover is not the quoted body. Keep the distinction in both chat rows and the restored composer.
    nonisolated var displayText: String {
        guard let olk else { return text }
        guard !olk.isOpen else { return olk.summary }
        guard olk.hasBody else { return "加密消息" }
        return olk.textSummary.isEmpty ? olk.coverLabel : "\(olk.coverLabel)：\(olk.textSummary)"
    }

    nonisolated func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(senderName, forKey: .senderName)
        try c.encode(olk?.cached.summary ?? text, forKey: .text)
        try c.encodeIfPresent(recalled, forKey: .recalled)
        try c.encodeIfPresent(olk?.publicParts ?? parts, forKey: .parts)
        try c.encodeIfPresent(olk?.cached, forKey: .olk)
    }
}
