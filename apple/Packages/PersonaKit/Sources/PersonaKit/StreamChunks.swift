import CoreGraphics
import Foundation

/// A streaming reply cut where a block has certainly ended: a blank line outside a code fence, followed by an
/// unindented line (the next block has started). Everything before the last such cut stays the same as the text grows.
public nonisolated enum StreamChunks {
    public static func split(_ text: String) -> (done: [String], tail: String) {
        let lines = text.components(separatedBy: "\n")
        var done: [String] = []
        var start = 0
        var fence: MarkdownFence?
        for (i, line) in lines.enumerated() {
            let t = line.trimmingCharacters(in: .whitespaces)
            if let f = fence {
                if f.isClosed(by: t) { fence = nil }
                continue
            }
            if let f = MarkdownFence.opening(t) { fence = f; continue }
            guard t.isEmpty, i > start, i + 1 < lines.count else { continue }
            let next = lines[i + 1]
            guard !next.isEmpty, !next.hasPrefix(" "), !next.hasPrefix("\t") else { continue }
            done.append(lines[start..<i].joined(separator: "\n"))
            start = i + 1
        }
        return (done, lines[start...].joined(separator: "\n"))
    }

    /// The space MarkdownUI's theme puts between the last block of one piece and the first of the next (the larger
    /// of the two margins, `MarkdownUI.Theme.persona`).
    public static func gap(after prev: String, before next: String) -> CGFloat {
        let last = prev.split(separator: "\n").last.map { $0.trimmingCharacters(in: .whitespaces) } ?? ""
        let first = next.split(separator: "\n").first.map { $0.trimmingCharacters(in: .whitespaces) } ?? ""
        func top(_ l: String) -> CGFloat {
            if l.hasPrefix("# ") { return 16 }
            if l.hasPrefix("## ") { return 14 }
            if l.hasPrefix("### ") { return 12 }
            if l.hasPrefix("```") || l.hasPrefix("~~~") || l.hasPrefix("|") { return 4 }
            return 0
        }
        func bottom(_ l: String) -> CGFloat {
            if l.hasPrefix("```") || l.hasPrefix("~~~") || l.hasPrefix("|") { return 12 }
            if l.hasPrefix("# ") { return 8 }
            if l.hasPrefix("## ") { return 6 }
            if l.hasPrefix("### ") { return 4 }
            return 10
        }
        return max(top(first), bottom(last))
    }
}
