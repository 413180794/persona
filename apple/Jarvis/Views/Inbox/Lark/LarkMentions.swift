import SwiftUI

/// Someone @-mentioned in a message I send: Feishu notifies them. The text must hold the literal `@name`
/// (docs/protocol.md, the send and reply routes); 所有人 is `user_id` "all".
nonisolated struct LarkMention: Codable, Hashable, Sendable {
    var userID: String
    var name: String

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case name
    }

    static let everyone = LarkMention(userID: "all", name: "所有人")
}

/// A chat member for the @ picker: `name` is the group nickname when there is one.
nonisolated struct LarkMember: Decodable, Hashable, Sendable, Identifiable {
    var userID: String
    var name: String
    var avatar: String?
    var id: String { userID }

    enum CodingKeys: String, CodingKey {
        case userID = "user_id"
        case name, avatar
    }
}

/// Typing @ in a group's box: what is being looked up, what picking someone writes, and who the sent text mentions.
nonisolated enum LarkMentions {
    /// Longest lookup after @ that still keeps the picker up.
    static let maxQuery = 20

    /// The name being typed after the last @ at the end of the text ("" right after the @), or nil when no @ is being
    /// typed: none, one inside a word like an email address, or a space / new line typed since (the picked name and
    /// its space end it too).
    static func query(in text: String) -> String? {
        guard let at = text.lastIndex(of: "@") else { return nil }
        if at > text.startIndex {
            let before = text[text.index(before: at)]
            if before.isASCII && (before.isLetter || before.isNumber) { return nil }
        }
        let typed = text[text.index(after: at)...]
        guard typed.count <= maxQuery, !typed.contains(where: { $0.isWhitespace }) else { return nil }
        return String(typed)
    }

    /// The text with the @ being typed replaced by `@name ` (Feishu's mention, a space after it to go on writing).
    static func inserting(_ m: LarkMention, into text: String) -> String {
        guard query(in: text) != nil, let at = text.lastIndex(of: "@") else { return text + "@" + m.name + " " }
        return String(text[..<at]) + "@" + m.name + " "
    }

    /// Who the text mentions out of those picked: one whose `@name` was deleted is left out, someone picked twice
    /// counts once, and of several picked with the same name only as many as the text still holds stay (the first
    /// picked). Longer names are matched first, so `@Tommy` does not keep a picked Tom.
    static func mentions(in text: String, picked: [LarkMention]) -> [LarkMention] {
        var seen = Set<String>()
        let unique = picked.filter { seen.insert($0.userID).inserted }
        var rest = text
        var left: [String: Int] = [:]
        for name in Set(unique.map(\.name)).sorted(by: { $0.count > $1.count }) {
            let token = "@" + name
            left[name] = rest.components(separatedBy: token).count - 1
            rest = rest.replacingOccurrences(of: token, with: "\u{0}")
        }
        return unique.filter { m in
            guard let n = left[m.name], n > 0 else { return false }
            left[m.name] = n - 1
            return true
        }
    }

    /// 所有人 is offered while the lookup is empty or could still become 所有人 / all.
    static func offersEveryone(_ query: String) -> Bool {
        query.isEmpty || LarkMention.everyone.name.hasPrefix(query) || "all".hasPrefix(query.lowercased())
    }
}

/// The list over a group's box while an @ is typed: 所有人, then the chat's members matching what follows the @ (the
/// box asks the hub, LarkComposer).
struct LarkMentionPicker: View {
    var query: String
    var members: [LarkMember]
    var pick: (LarkMention) -> Void

    var body: some View {
        ViewThatFits(in: .vertical) {
            list
            ScrollView { list }
        }
        .frame(maxHeight: Theme.Size.box240)
        .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("lark.mentionPicker")
    }

    private var list: some View {
        VStack(alignment: .leading, spacing: 0) {
            if LarkMentions.offersEveryone(query) {
                row(name: LarkMention.everyone.name, avatar: nil, isGroup: true) { pick(.everyone) }
            }
            ForEach(members) { m in
                row(name: m.name, avatar: m.avatar, isGroup: false) { pick(LarkMention(userID: m.userID, name: m.name)) }
            }
        }
        .padding(.vertical, Theme.Space.xs)
    }

    private func row(name: String, avatar: String?, isGroup: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.s) {
                LarkAvatarImage(name: name, url: avatar, isGroup: isGroup, size: Theme.Size.well)
                Text(name).font(Theme.Font.body).lineLimit(1)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(minHeight: Theme.Size.tapTarget)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("@\(name)")
    }
}
