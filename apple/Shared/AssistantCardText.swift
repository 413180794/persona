import Foundation

/// The assistant's cards in words (#2024, docs/assistant-views.md): where a reply is read as plain text (the watch,
/// what Siri says), a ```persona-view block is its `fallback`, the words the assistant wrote for exactly this, and an
/// old ```persona-card block its title. A block with neither, one that does not parse, or one still streaming is
/// 〔卡片〕: never its JSON. Compiled into the watch (watchOS 10 APIs only).
nonisolated enum AssistantCardText {
    static let view = "persona-view"
    static let card = "persona-card"
    static let mark = "〔卡片〕"

    /// `text` with every card block replaced by its words; anything else is left as it is.
    static func replacingCards(_ text: String) -> String {
        guard text.contains(view) || text.contains(card) else { return text }
        var out: [String] = []
        var lines = text.components(separatedBy: "\n")[...]
        while let line = lines.popFirst() {
            guard let open = Fence(opening: line), open.language == view || open.language == card else {
                out.append(line)
                continue
            }
            var body: [String] = []
            var closed = false
            while let next = lines.popFirst() {
                if open.closes(next) { closed = true; break }
                body.append(next)
            }
            out.append(closed ? words(open.language, json: body.joined(separator: "\n")) : mark)
        }
        return out.joined(separator: "\n")
    }

    /// The words for one block's content.
    static func words(_ language: String, json: String) -> String {
        guard let data = json.data(using: .utf8),
              let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return mark }
        if language == view, let f = nonEmpty(object["fallback"]) { return f }
        if let t = nonEmpty(object["title"]) { return "〔\(t)〕" }
        return mark
    }

    private static func nonEmpty(_ v: Any?) -> String? {
        guard let s = (v as? String)?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty else { return nil }
        return s
    }

    /// A fenced code block's opening line (CommonMark: up to three spaces, three or more ` or ~, an info string whose
    /// first word is the language).
    private struct Fence {
        var char: Character
        var count: Int
        var language: String

        init?(opening line: String) {
            let indent = line.prefix { $0 == " " }
            guard indent.count <= 3 else { return nil }
            let rest = line.dropFirst(indent.count)
            guard let c = rest.first, c == "`" || c == "~" else { return nil }
            let run = rest.prefix { $0 == c }
            guard run.count >= 3 else { return nil }
            let info = rest.dropFirst(run.count).trimmingCharacters(in: .whitespaces)
            if c == "`", info.contains("`") { return nil }
            char = c
            count = run.count
            language = String(info.split(whereSeparator: { $0 == " " || $0 == "\t" }).first ?? "").lowercased()
        }

        /// A closing fence: the same character, at least as long, nothing after it.
        func closes(_ line: String) -> Bool {
            let indent = line.prefix { $0 == " " }
            guard indent.count <= 3 else { return false }
            let rest = line.dropFirst(indent.count).trimmingCharacters(in: .whitespaces)
            return rest.count >= count && rest.allSatisfy { $0 == char }
        }
    }
}
