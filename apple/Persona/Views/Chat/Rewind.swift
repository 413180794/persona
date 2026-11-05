import SwiftUI

/// 回退到这里 (#2915, the web's #2436): go back in a Claude session to before one of your messages. The node drives
/// Claude Code's own /rewind in the session's terminal (server/internal/node/rewind.go), so what the app says is which
/// message: its text, which of the messages with that text counted from the end, and how many of your messages came
/// after it. Claude's list and the app's transcript are both read from the end, so older pages not loaded yet change
/// nothing. The node checks all three against its screen and stops, changing nothing, when they disagree.
nonisolated enum Rewind {
    enum Restore: String, Sendable, CaseIterable {
        case conversation
        case codeAndConversation = "code_and_conversation"

        var title: String {
            switch self {
            case .conversation: "只回退对话"
            case .codeAndConversation: "回退代码和对话"
            }
        }
    }

    struct Target: Equatable, Sendable {
        /// The message as the transcript has it (Claude's own words for it, tags and all).
        var prompt: String
        /// It is the Nth message with this text counting from the end (1 = the last one).
        var occurrence: Int
        /// How many of your messages came after it.
        var promptsAfter: Int
    }

    /// One message of yours, as Claude Code lists it in /rewind: the lines of one transcript entry are one message.
    struct Prompt: Equatable {
        var entry: String
        var raw: String
        /// One of its items was cut by the hub for the page (`Item.fullSize`): the text here is not the whole.
        var cut: Bool
    }

    /// `<transcript line uuid>#<n>` → the line's uuid; nil for items that are not a transcript line of their own
    /// (a message still being sent, the hub's notices).
    static func entry(of id: String) -> String? {
        guard let hash = id.firstIndex(of: "#") else { return nil }
        let line = String(id[..<hash])
        return UUID(uuidString: line) == nil ? nil : line
    }

    /// Your messages in order. Images alone are a message too (Claude lists them), with no text; a message queued while
    /// Claude worked is not in its list yet.
    static func prompts(_ items: [Item]) -> [Prompt] {
        var out: [Prompt] = []
        for it in items {
            guard it.role == "user", it.queued != true,
                  it.kind == .text || (it.kind == .image && it.toolUseID == nil),
                  let entry = entry(of: it.id) else { continue }
            let text = it.kind == .text ? it.cutText : ""
            if let last = out.last, last.entry == entry {
                if !text.isEmpty { out[out.count - 1].raw += (last.raw.isEmpty ? "" : "\n") + text }
                if it.fullSize != nil { out[out.count - 1].cut = true }
                continue
            }
            out.append(Prompt(entry: entry, raw: text, cut: it.fullSize != nil))
        }
        return out
    }

    private static func squash(_ s: String) -> String { s.filter { !$0.isWhitespace } }

    /// Which message the bubble with this item id stands for, in the words the node needs; nil when it cannot be told
    /// apart (no words) or is not one of your transcript messages. `full` replaces the text of a message the page cut.
    static func target(_ items: [Item], id: String, full: String? = nil) -> Target? {
        guard let entry = entry(of: id) else { return nil }
        var all = prompts(items)
        guard let at = all.firstIndex(where: { $0.entry == entry }) else { return nil }
        if let full { all[at].raw = full }
        guard !all[at].raw.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        let same = squash(all[at].raw)
        return Target(prompt: all[at].raw,
                      occurrence: 1 + all[(at + 1)...].filter { squash($0.raw) == same }.count,
                      promptsAfter: all.count - 1 - at)
    }

    /// The message was cut for the page: its whole text has to be fetched first (the first cut item's id).
    static func cutItem(_ items: [Item], id: String) -> String? {
        guard let entry = entry(of: id) else { return nil }
        let mine = items.filter { $0.role == "user" && $0.kind == .text && Self.entry(of: $0.id) == entry }
        guard mine.contains(where: { $0.fullSize != nil }) else { return nil }
        return mine.first?.id
    }

    /// Whether the menu offers it at all: a Claude conversation of an agent (not the assistant's, not a subagent or a
    /// thread another source lists). Nil when it does not; otherwise why it is off right now ("" when it is on).
    static func block(_ session: Session?) -> String? {
        guard let s = session, !s.assistant, s.sourceKind == nil, !s.sessionID.contains("@") else { return nil }
        if let agent = s.agent, agent != "claude" { return "仅 Claude" }
        if s.state == .offline || s.starting == true { return "未运行" }
        if s.state == .needsYou || s.pendingInteractions > 0 { return "待回答" }
        if s.state == .working { return "工作中" }
        return ""
    }

    struct Result: Decodable, Sendable {
        var prompt: String?
        var restore: String?
        /// The files went back too (not when nothing changed after the message).
        var codeRestored: Bool?
        /// Claude's input box was emptied: the message goes into the app's composer instead.
        var inputCleared: Bool?

        enum CodingKeys: String, CodingKey {
            case prompt, restore, codeRestored = "code_restored", inputCleared = "input_cleared"
        }
    }

    /// What the toast says after it went through.
    static func done(_ result: Result, asked: Restore) -> String {
        if asked == .codeAndConversation, result.codeRestored != true { return "已回退对话（这条之后没有代码改动）" }
        return result.codeRestored == true ? "已回退代码和对话" : "已回退对话"
    }

    /// The input box on the computer still holds the message (it could not be emptied).
    static let inputKept = "已回退，但电脑上 Claude 的输入框里还留着这条消息"

    /// The hub's refusal in words for the toast.
    static func failure(_ error: Error) -> String {
        if let e = error as? APIError {
            if (e.status == 404 || e.status == 405) && e.code != "gone" { return "这个 hub 还不支持回退，需要更新" }
            if e.status == 403 { return "回退要用完整权限的设备" }
            return e.localizedDescription
        }
        if (error as? URLError)?.code == .timedOut { return "回退超时：电脑上的 Claude 没有按预期响应" }
        return error.localizedDescription
    }
}

extension View {
    /// The questions 回退 on your own message asks (#2915): what to restore, and once more before the files go back.
    func rewindDialogs(menu: MessageMenu) -> some View { modifier(RewindDialogs(menu: menu)) }
}

private struct RewindDialogs: ViewModifier {
    @Bindable var menu: MessageMenu
    /// 回退代码和对话 was picked: asked again, it puts the files on the computer back too.
    @State private var confirmCode: RewindAsk?

    func body(content: Content) -> some View {
        let block = menu.rewindBlock() ?? ""
        content
            .confirmationDialog("回退到这里", isPresented: Binding(get: { menu.rewinding != nil }, set: { if !$0 { menu.rewinding = nil } }),
                                titleVisibility: .visible, presenting: menu.rewinding) { ask in
                if block.isEmpty {
                    Button(Rewind.Restore.conversation.title) { menu.rewind?(ask.id, ask.text, .conversation) }
                    Button(Rewind.Restore.codeAndConversation.title, role: .destructive) {
                        // The next question waits for this dialog to go: one presented while it leaves is dropped.
                        Task {
                            try? await Task.sleep(for: .milliseconds(400))
                            confirmCode = ask
                        }
                    }
                }
                Button("取消", role: .cancel) {}
            } message: { _ in
                Text(block.isEmpty ? "回到发这条消息之前：之后的对话从 Claude 里移除，这条消息放回输入框。" : "现在不能回退：\(block)")
            }
            .alert("确认回退代码", isPresented: Binding(get: { confirmCode != nil }, set: { if !$0 { confirmCode = nil } }),
                   presenting: confirmCode) { ask in
                Button("回退代码和对话", role: .destructive) { menu.rewind?(ask.id, ask.text, .codeAndConversation) }
                Button("取消", role: .cancel) {}
            } message: { _ in
                Text("电脑上的文件也会回到这条消息之前的样子，之后的改动会丢掉。")
            }
    }
}
