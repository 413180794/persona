import Foundation

/// A message as the watch shows it: plain text without the markup the phone and Claude Code put into messages
/// (`<persona-context>` / `<jarvis-context>` blocks fold into one mark, 〔title〕 or 〔上下文 N 项〕, ahead of what the user wrote, the note listing attached photos and Claude's `[Image #n]`
/// placeholders become one 「图片」 mark, sent videos 「视频」, pasted-content tags go) and without Markdown's marks (`##`, `**`, backticks,
/// fences, quotes, link targets; list dashes become •): the watch draws plain text, and the marks were shown as typed.
nonisolated enum WatchText {
    private static let context = try! NSRegularExpression(
        pattern: #"<(?:jarvis|persona)-context\b(?:[^>]*?title="([^"]*)")?[^>]*>[\s\S]*?</(?:jarvis|persona)-context>\s*"#)
    private static let rules: [(String, String)] = [
        (#"\s*<attached_images>[\s\S]*?</attached_images>\s*"#, "\n〔图片〕\n"),
        // Videos sent as files (<jarvis-file kind="video" …/>, VideoAttach.swift).
        (#"\s*<jarvis-file\b[^>]*?kind="video"[^>]*>(?:\s*</jarvis-file>)?\s*"#, "\n〔视频〕\n"),
        (#"\s*<jarvis-file\b[^>]*>(?:\s*</jarvis-file>)?\s*"#, "\n〔文件〕\n"),
        (#"(〔视频〕\s*)+"#, "〔视频〕 "),
        (#"\[Image #\d+\]\s*"#, "〔图片〕"),
        (#"</?pasted_content[^>]*>"#, ""),
        (#"(〔图片〕\s*)+"#, "〔图片〕 "),
        // Markdown, line by line (?m): fences, headings, quotes, rules, then list dashes; inline marks after.
        (#"(?m)^[ \t]*```.*\n?"#, ""),
        (#"(?m)^#{1,6}[ \t]+"#, ""),
        (#"(?m)^[ \t]*>[ \t]?"#, ""),
        (#"(?m)^[ \t]*([-*_][ \t]*){3,}$"#, ""),
        (#"(?m)^([ \t]*)[-*+][ \t]+"#, "$1• "),
        (#"\*\*([^*\n]+)\*\*"#, "$1"),
        (#"__([^_\n]+)__"#, "$1"),
        (#"(?<![*A-Za-z0-9_])\*([^*\s][^*\n]*?)\*(?![*A-Za-z0-9_])"#, "$1"), // *italic*, also between CJK; not 2 * 3 or a*b*c
        (#"`([^`\n]+)`"#, "$1"),
        (#"\[([^\]\n]+)\]\([^)\s]+\)"#, "$1"),
        (#"\n{3,}"#, "\n\n"),
    ]

    static func shown(_ raw: String) -> String {
        // The assistant's cards first: their words, not their JSON (the fence rule below would leave the JSON).
        let text = AssistantCardText.replacingCards(raw)
        let ns = text as NSString
        let blocks = context.matches(in: text, range: NSRange(location: 0, length: ns.length))
        var t = context.stringByReplacingMatches(in: text, range: NSRange(location: 0, length: ns.length), withTemplate: "")
        if let first = blocks.first {
            let title = first.range(at: 1).location == NSNotFound ? "" : ns.substring(with: first.range(at: 1))
            t = (blocks.count == 1 && !title.isEmpty ? "〔\(title)〕\n" : "〔上下文 \(blocks.count) 项〕\n") + t
        }
        for (pattern, with) in rules {
            t = t.replacingOccurrences(of: pattern, with: with, options: .regularExpression)
        }
        return t.replacingOccurrences(of: "&quot;", with: "\"").replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">").replacingOccurrences(of: "&amp;", with: "&")
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
