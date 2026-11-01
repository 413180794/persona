#if os(macOS)
import Foundation

/// Find in the code viewer (⌘F over an open file, `CodeLinesView`): what matches and in which order. Pure, so the
/// find bar and the drawing only ask it (`JarvisMacTests/CodeFindTests`).
nonisolated enum CodeFind {
    /// One match: its line and where in that line's text (UTF-16, as the drawn attributed string counts).
    struct Match: Equatable, Sendable {
        var line: Int
        var range: NSRange
    }

    /// What a key does in the find field.
    enum BarKey: Equatable, Sendable { case close, next, previous }

    /// Esc closes the bar, ⏎ goes to the next match, ⇧⏎ to the one before; only while the field has the keyboard.
    static func barKey(keyCode: UInt16, shift: Bool, fieldFocused: Bool, composing: Bool = false) -> BarKey? {
        guard fieldFocused, !composing else { return nil } // composing: the input method's ⏎ / Esc
        switch keyCode {
        case 53: return .close // esc
        case 36, 76: return shift ? .previous : .next // return, keypad enter
        default: return nil
        }
    }

    /// Where `query` is in `text`, case-insensitive, left to right, not overlapping (as Xcode and Safari count).
    static func ranges(of query: String, in text: String) -> [NSRange] {
        guard !query.isEmpty, !text.isEmpty else { return [] }
        let s = text as NSString
        var out: [NSRange] = []
        var from = 0
        while from < s.length {
            let r = s.range(of: query, options: .caseInsensitive, range: NSRange(location: from, length: s.length - from))
            guard r.location != NSNotFound, r.length > 0 else { break }
            out.append(r)
            from = NSMaxRange(r)
        }
        return out
    }

    /// Every match in lines `0..<lineCount`, in reading order.
    static func matches(of query: String, lineCount: Int, text: (Int) -> String) -> [Match] {
        guard !query.isEmpty else { return [] }
        var out: [Match] = []
        for i in 0..<max(0, lineCount) {
            for r in ranges(of: query, in: text(i)) { out.append(Match(line: i, range: r)) }
        }
        return out
    }

    /// The match after (or before) `current`, going round at either end; the first (or last) one when there is no
    /// current one yet. Nil without matches.
    static func step(from current: Int?, count: Int, forward: Bool) -> Int? {
        guard count > 0 else { return nil }
        guard let current, current >= 0, current < count else { return forward ? 0 : count - 1 }
        return (current + (forward ? 1 : count - 1)) % count
    }

    /// The first match on `line` or below it (the one to show when the query changes), going round to the first
    /// of all when none is below.
    static func first(in matches: [Match], atOrAfter line: Int) -> Int? {
        guard !matches.isEmpty else { return nil }
        // Matches are in reading order: the first one at or below `line` by binary search.
        var lo = 0, hi = matches.count
        while lo < hi {
            let mid = (lo + hi) / 2
            if matches[mid].line < line { lo = mid + 1 } else { hi = mid }
        }
        return lo < matches.count ? lo : 0
    }
}
#endif
