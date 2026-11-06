import Foundation

/// A fenced code block's opener, and which line ends it (CommonMark): only a line of the same character, at least as
/// many of them, with nothing after but spaces or tabs. So a ```` block that shows a ``` one, or a ```js line inside a ```
/// block, does not end it (as on the web, #2268). Lines are passed trimmed of surrounding spaces.
public nonisolated struct MarkdownFence: Sendable, Equatable {
    public let character: Character
    public let count: Int

    /// The fence `line` opens, or nil when it is not a fence (a backtick line with another backtick after the run,
    /// "```code```", is inline code).
    public static func opening(_ line: String) -> MarkdownFence? {
        guard line.hasPrefix("```") || line.hasPrefix("~~~"), let c = line.first else { return nil }
        let count = line.prefix(while: { $0 == c }).count
        if c == "`", line.dropFirst(count).contains("`") { return nil }
        return MarkdownFence(character: c, count: count)
    }

    /// Whether `line` ends the block this fence opened.
    public func isClosed(by line: String) -> Bool {
        let run = line.prefix(while: { $0 == character }).count
        return run >= count && line.dropFirst(run).allSatisfy { $0 == " " || $0 == "\t" }
    }
}
