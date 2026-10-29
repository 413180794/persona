import Foundation

/// A message as the watch shows it: plain text without the markup the phone and Claude Code put into messages
/// (`<jarvis-context>` blocks become their title, the note listing attached photos and Claude's `[Image #n]`
/// placeholders become one 「图片」 mark, pasted-content tags go).
nonisolated enum WatchText {
    private static let context = try! NSRegularExpression(
        pattern: #"<jarvis-context\b[^>]*?title="([^"]*)"[^>]*>[\s\S]*?</jarvis-context>\s*"#)
    private static let rules: [(String, String)] = [
        (#"\s*<attached_images>[\s\S]*?</attached_images>\s*"#, "\n〔图片〕\n"),
        (#"\[Image #\d+\]\s*"#, "〔图片〕"),
        (#"</?pasted_content[^>]*>"#, ""),
        (#"(〔图片〕\s*)+"#, "〔图片〕 "),
    ]

    static func shown(_ text: String) -> String {
        let ns = text as NSString
        var t = context.stringByReplacingMatches(in: text, range: NSRange(location: 0, length: ns.length), withTemplate: "〔$1〕\n")
        for (pattern, with) in rules {
            t = t.replacingOccurrences(of: pattern, with: with, options: .regularExpression)
        }
        return t.replacingOccurrences(of: "&quot;", with: "\"").replacingOccurrences(of: "&lt;", with: "<")
            .replacingOccurrences(of: "&gt;", with: ">").replacingOccurrences(of: "&amp;", with: "&")
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
