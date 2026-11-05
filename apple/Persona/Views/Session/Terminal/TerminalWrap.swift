import Foundation

/// One line on the phone: cells `from..<to` of a row, drawn after `indent` cells (a folded row's continuation lines
/// hang under its text; box borders in the indent are drawn again).
nonisolated struct DisplayLine: Equatable, Sendable {
    var row: Int
    var from: Int
    var to: Int
    var indent = 0
}

nonisolated struct TerminalLineLayout {
    private var previous: [TermRow] = []
    private var cursor: (row: Int, col: Int)?
    private var perLine = 0
    private var wrap = false
    private var rendered: [[DisplayLine]] = []

    mutating func lines(_ rows: [TermRow], cursor: (row: Int, col: Int)?, perLine: Int, wrap: Bool) -> [DisplayLine] {
        let reset = self.perLine != perLine || self.wrap != wrap || previous.count != rows.count
        if reset { rendered = Array(repeating: [], count: rows.count) }
        var out: [DisplayLine] = []
        out.reserveCapacity(rows.count + 16)
        for index in rows.indices {
            let oldColumn = self.cursor?.row == index ? self.cursor?.col : nil
            let newColumn = cursor?.row == index ? cursor?.col : nil
            if reset || previous[index] != rows[index] || oldColumn != newColumn {
                rendered[index] = TerminalWrap.lines([rows[index]], cursor: newColumn.map { (row: 0, col: $0) },
                                                     perLine: perLine, wrap: wrap).map {
                    var line = $0
                    line.row = index
                    return line
                }
            }
            out += rendered[index]
        }
        previous = rows
        self.cursor = cursor
        self.perLine = perLine
        self.wrap = wrap
        return out
    }
}

/// Cutting the pane's rows (laid out for its width, often 170 cells) into lines for the phone's. Unwrapped, a row is
/// one line. Wrapped, a row folds where a line may end — after a space, next to a Chinese character, never before
/// closing punctuation — and only mid-word when a word fills most of a line; continuation lines hang under the row's
/// text (after its indent and list marker: "  2. ", "⏺ ", "⎿  "). What only pads a row to the pane's width (spaces,
/// a background, a rule across the pane) ends at the phone's edge instead of folding into lines of nothing.
nonisolated enum TerminalWrap {
    static func lines(_ rows: [TermRow], cursor: (row: Int, col: Int)?, perLine: Int, wrap: Bool) -> [DisplayLine] {
        var out: [DisplayLine] = []
        out.reserveCapacity(rows.count + 16)
        for (r, row) in rows.enumerated() {
            var end = row.count
            while end > 0, row[end - 1].isBlank { end -= 1 }
            if let c = cursor, c.row == r { end = max(end, c.col + 1) }
            guard wrap, end > perLine else {
                out.append(DisplayLine(row: r, from: 0, to: end))
                continue
            }
            let cursorCol = cursor?.row == r ? cursor?.col : nil
            let hang = hangingIndent(row, end: end, perLine: perLine)
            var from = 0
            while from < end {
                let indent = from == 0 ? 0 : hang
                var to = min(from + perLine - indent, end)
                if to < end {
                    if to < row.count, row[to].width == 0, to - 1 > from { to -= 1 } // a wide character stays whole
                    if let b = breakPoint(row, from: from, to: to) { to = b }
                }
                out.append(DisplayLine(row: r, from: from, to: to, indent: indent))
                from = to
                // Padding to the pane's width ends here (but not past the cursor: typing goes there).
                if from < end, row[from..<min(end, row.count)].allSatisfy(\.isFiller), (cursorCol ?? -1) < from { break }
                // The spaces a line breaks at do not start the next.
                var skip = from
                while skip < min(end, row.count), row[skip].isBlank, (cursorCol ?? -1) != skip { skip += 1 }
                if skip < end { from = skip }
            }
        }
        return out
    }

    /// Where a line that would end mid-word at `to` ends instead: the last place in its second half a line may end.
    static func breakPoint(_ row: TermRow, from: Int, to: Int) -> Int? {
        if canBreak(row, at: to) { return nil }
        var k = to - 1
        while k > from + (to - from) / 2 {
            if canBreak(row, at: k) { return k }
            k -= 1
        }
        return nil
    }

    /// Whether a line may end before cell k.
    static func canBreak(_ row: TermRow, at k: Int) -> Bool {
        guard k > 0, k < row.count else { return true }
        let next = row[k], prev = row[k - 1]
        if next.text == " " || prev.text == " " { return true }
        if closing.contains(next.text) { return false }
        if opening.contains(prev.text) { return false }
        return next.width == 2 || prev.width == 0 // next to a wide (Chinese) character
    }

    static let closing: Set<String> = ["，", "。", "、", "：", "；", "！", "？", "）", "】", "」", "』", "》", "”", "’", ",", ".", ":", ";", "!", "?", ")", "]", "}"] // i18n-ignore
    static let opening: Set<String> = ["（", "【", "「", "『", "《", "“", "‘", "(", "[", "{"] // i18n-ignore

    /// Where a folded row's continuation lines start: after its leading spaces and borders, and a list marker with the
    /// spaces after it. At most half a line.
    static func hangingIndent(_ row: TermRow, end: Int, perLine: Int) -> Int {
        var i = 0
        func spaces() { while i < end, row[i].text == " " || borders.contains(row[i].text) { i += 1 } }
        spaces()
        let start = i
        if i < end, markers.contains(row[i].text) {
            i += Int(row[i].width)
        } else {
            while i < end, row[i].text.count == 1, row[i].text.first?.isNumber == true { i += 1 }
            if i > start, i < end, row[i].text == "." || row[i].text == ")" { i += 1 } else { i = start }
        }
        if i > start {
            if i < end, row[i].text == " " { spaces() } else { i = start }
        }
        return i >= end ? 0 : min(i, perLine / 2)
    }

    static let borders: Set<String> = ["│", "┃", "║", "▏", "▎"]
    static let markers: Set<String> = ["⏺", "⎿", "❯", "›", ">", "•", "·", "-", "*", "+", "✻", "✶", "✳", "◦", "▸", "▪", "☐", "☒", "✔", "✘"]
}

nonisolated extension TermCell {
    /// Draws a line across the cell (Claude's rules, box tops).
    var isRule: Bool {
        guard let s = text.unicodeScalars.first, text.unicodeScalars.count == 1 else { return false }
        return [0x2500, 0x2501, 0x2504, 0x2505, 0x2508, 0x2509, 0x254C, 0x254D, 0x2550].contains(s.value)
    }

    /// Only pads a row: a space (whatever its background) or a rule.
    var isFiller: Bool { text == " " || isRule }
}
