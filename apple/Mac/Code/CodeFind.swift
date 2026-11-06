#if os(macOS)
import Foundation

/// Find in the code viewer (⌘F over an open file, `CodeLinesView`): what matches and in which order. Pure, so the
/// find bar and the drawing only ask it (`PersonaMacTests/CodeFindTests`).
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

    /// The find bar's switches (Xcode's and VS Code's): Aa, Whole Word, .*.
    struct Options: Equatable, Sendable {
        var caseSensitive = false
        var wholeWord = false
        var regex = false
    }

    /// A query ready to run over many lines: the pattern compiled once. Nil for an empty query or, with `regex`, one
    /// that does not compile (`isValid` says which).
    struct Matcher: @unchecked Sendable {
        let query: String
        let options: Options
        private let pattern: NSRegularExpression?

        init?(_ query: String, options: Options = Options()) {
            guard !query.isEmpty else { return nil }
            self.query = query
            self.options = options
            if options.regex {
                guard let re = try? NSRegularExpression(pattern: query, options: options.caseSensitive ? [] : [.caseInsensitive])
                else { return nil }
                pattern = re
            } else {
                pattern = nil
            }
        }

        /// Whether `query` can be searched for with `options` (a regex that compiles).
        static func isValid(_ query: String, options: Options) -> Bool {
            query.isEmpty || !options.regex || Matcher(query, options: options) != nil
        }

        /// Where the query is in `text`, left to right, not overlapping (as Xcode and Safari count). Empty matches (a
        /// regex's `^`, `a*`) are not matches; with Whole Word a match has to start and end at a word's edge.
        func ranges(in text: String) -> [NSRange] {
            guard !text.isEmpty else { return [] }
            let s = text as NSString
            var out: [NSRange] = []
            if let pattern {
                for m in pattern.matches(in: text, range: NSRange(location: 0, length: s.length))
                where m.range.length > 0 && (!options.wholeWord || CodeFind.isWord(m.range, in: s)) {
                    out.append(m.range)
                }
                return out
            }
            let compare: NSString.CompareOptions = options.caseSensitive ? [] : [.caseInsensitive]
            var from = 0
            while from < s.length {
                let r = s.range(of: query, options: compare, range: NSRange(location: from, length: s.length - from))
                guard r.location != NSNotFound, r.length > 0 else { break }
                if !options.wholeWord || CodeFind.isWord(r, in: s) {
                    out.append(r)
                    from = NSMaxRange(r)
                } else {
                    from = r.location + 1 // "foo" inside "foofoo bar foo": look again just after where this one began
                }
            }
            return out
        }
    }

    /// A word's character, as an identifier has them: letters, digits, `_`.
    private static func isWordUnit(_ u: unichar) -> Bool {
        if u == 95 { return true } // _
        guard let scalar = Unicode.Scalar(u) else { return false } // half of a pair (an emoji): not a word character
        return CharacterSet.alphanumerics.contains(scalar)
    }

    /// Whether `r` starts and ends at a word's edge in `s` (the characters just outside it are not word characters).
    static func isWord(_ r: NSRange, in s: NSString) -> Bool {
        let before = r.location > 0 ? isWordUnit(s.character(at: r.location - 1)) : false
        let after = NSMaxRange(r) < s.length ? isWordUnit(s.character(at: NSMaxRange(r))) : false
        return !before && !after
    }

    /// Where `query` is in `text` (by default case-insensitive, the literal text).
    static func ranges(of query: String, in text: String, options: Options = Options()) -> [NSRange] {
        Matcher(query, options: options)?.ranges(in: text) ?? []
    }

    /// Every match in lines `0..<lineCount`, in reading order.
    static func matches(of query: String, options: Options = Options(), lineCount: Int, text: (Int) -> String) -> [Match] {
        guard let matcher = Matcher(query, options: options) else { return [] }
        var out: [Match] = []
        for i in 0..<max(0, lineCount) {
            for r in matcher.ranges(in: text(i)) { out.append(Match(line: i, range: r)) }
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
