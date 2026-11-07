import Foundation

// The terminal tab's screen model (docs/integrations/terminal.md). What arrives is not a program's raw output but herdr redrawing a
// pane's grid it has already emulated — cursor moves, SGR, text, erases, OSC 8 links — so a small VT subset is enough;
// anything else is skipped without harm. The same parser reads the colored scrollback pages (`/screen?format=ansi`),
// which are just SGR and text, one row per line.

/// A cell's color: the theme's default, one of the 256 indexed colors, or true color.
nonisolated enum TermColor: Hashable, Sendable {
    case standard
    case indexed(UInt8)
    case rgb(UInt8, UInt8, UInt8)
}

nonisolated struct TermStyle: Hashable, Sendable {
    var fg = TermColor.standard
    var bg = TermColor.standard
    var bold = false
    var dim = false
    var italic = false
    var underline = false
    var inverse = false
    var strike = false
    var hidden = false
    /// Where an OSC 8 link goes.
    var link: String?

    static let plain = TermStyle()
}

nonisolated struct TermCell: Equatable, Sendable {
    /// One grapheme; "" for the right half of a wide character.
    var text: String
    /// 1, or 2 for a wide character (its right half is a 0-width cell after it).
    var width: UInt8
    var style: TermStyle

    static let blank = TermCell(text: " ", width: 1, style: .plain)
    var isBlank: Bool { text == " " && style.bg == .standard && !style.inverse && !style.underline && !style.strike }
}

typealias TermRow = [TermCell]

/// A grid of cols × rows fed VT bytes, or — `growing` — rows that grow as text comes (the scrollback pages).
nonisolated final class TerminalGrid {
    private(set) var cols: Int
    private(set) var rows: Int
    private(set) var lines: [TermRow]
    private(set) var cursorRow = 0
    private(set) var cursorCol = 0
    private(set) var cursorVisible = true
    let growing: Bool

    private var style = TermStyle()
    private var parser = Parser()

    init(cols: Int, rows: Int) {
        self.cols = max(cols, 1)
        self.rows = max(rows, 1)
        growing = false
        lines = Array(repeating: [], count: self.rows)
    }

    private init(growing: Void) {
        cols = .max
        rows = 1
        self.growing = true
        lines = [[]]
    }

    /// A fresh screen of this size (a whole frame follows).
    func reset(cols: Int, rows: Int) {
        self.cols = max(cols, 1)
        self.rows = max(rows, 1)
        lines = Array(repeating: [], count: self.rows)
        cursorRow = 0; cursorCol = 0; cursorVisible = true
        style = TermStyle(); parser = Parser()
    }

    /// Rows of colored text, one per line (a scrollback page from `/screen?format=ansi`; plain text works too).
    static func rows(fromANSI text: String) -> [TermRow] {
        guard !text.isEmpty else { return [] }
        let g = TerminalGrid(growing: ())
        let unix = text.replacingOccurrences(of: "\r\n", with: "\n")
        g.feed(unix)
        var out = g.lines
        if out.last?.isEmpty == true, unix.hasSuffix("\n") { out.removeLast() }
        return out
    }

    func feed(_ text: String) {
        for ch in text { parser.step(ch, into: self) }
    }

    func feed(_ data: Data) { feed(String(decoding: data, as: UTF8.self)) }

    /// The row as plain text, trailing blanks trimmed.
    static func plain(_ row: TermRow) -> String {
        var s = ""
        for c in row where c.width > 0 { s += c.text }
        while s.hasSuffix(" ") { s.removeLast() }
        return s
    }

    // MARK: Actions the parser takes

    fileprivate func printChar(_ ch: Character) {
        let w = TerminalGrid.width(ch)
        if w == 0 { // a lone combining mark: onto the cell before
            if cursorCol > 0, cursorCol - 1 < lines[cursorRow].count { lines[cursorRow][cursorCol - 1].text.append(ch) }
            return
        }
        if !growing && cursorCol + w > cols {
            return // herdr places every row itself; nothing it sends runs past the edge
        }
        put(TermCell(text: String(ch), width: UInt8(w), style: style), at: cursorCol)
        if w == 2 { putRightHalf(at: cursorCol + 1) }
        cursorCol += w
    }

    /// In place: a whole screen is thousands of these.
    private func put(_ cell: TermCell, at col: Int) {
        let r = cursorRow
        if lines[r].count <= col { lines[r].append(contentsOf: repeatElement(TermCell.blank, count: col - lines[r].count + 1)) }
        // Overwriting half of a wide character blanks its other half.
        if lines[r][col].width == 0, col > 0, lines[r][col - 1].width == 2 { lines[r][col - 1] = .blank }
        if lines[r][col].width == 2, col + 1 < lines[r].count, cell.width != 2 { lines[r][col + 1] = .blank }
        lines[r][col] = cell
    }

    /// The right half of the wide character just put at col - 1. Not `put`: the cell there may be the right half of
    /// the wide character it replaced (herdr redraws streaming text that way), and that rule would blank the new one.
    private func putRightHalf(at col: Int) {
        let r = cursorRow
        if lines[r].count <= col { lines[r].append(contentsOf: repeatElement(TermCell.blank, count: col - lines[r].count + 1)) }
        if lines[r][col].width == 2, col + 1 < lines[r].count { lines[r][col + 1] = .blank }
        lines[r][col] = TermCell(text: "", width: 0, style: style)
    }

    fileprivate func control(_ ch: Character) {
        switch ch {
        case "\r": cursorCol = 0
        case "\n": lineFeed()
        case "\r\n": cursorCol = 0; lineFeed() // one Character in Swift
        case "\u{08}": cursorCol = max(0, cursorCol - 1)
        case "\t": cursorCol = min(growing ? cursorCol + 8 : cols - 1, (cursorCol / 8 + 1) * 8)
        default: break
        }
    }

    private func lineFeed() {
        if growing {
            cursorRow += 1
            if cursorRow >= lines.count { lines.append([]) }
            cursorCol = 0
            return
        }
        if cursorRow + 1 < rows {
            cursorRow += 1
        } else {
            lines.removeFirst()
            lines.append([])
        }
    }

    fileprivate func csi(_ final: Character, _ params: [Int], privateMarker: Character?) {
        func p(_ i: Int, _ d: Int = 1) -> Int { i < params.count && params[i] > 0 ? params[i] : d }
        let lastCol = growing ? Int.max / 2 : cols - 1
        if let privateMarker {
            if privateMarker == "?", final == "h" || final == "l", params.contains(25) { cursorVisible = final == "h" }
            return // 2026 (synchronized output), mouse modes, …: nothing to keep
        }
        switch final {
        case "H", "f": moveTo(row: p(0) - 1, col: p(1) - 1)
        case "A": moveTo(row: cursorRow - p(0), col: cursorCol)
        case "B", "e": moveTo(row: cursorRow + p(0), col: cursorCol)
        case "C", "a": moveTo(row: cursorRow, col: cursorCol + p(0))
        case "D": moveTo(row: cursorRow, col: cursorCol - p(0))
        case "E": moveTo(row: cursorRow + p(0), col: 0)
        case "F": moveTo(row: cursorRow - p(0), col: 0)
        case "G", "`": moveTo(row: cursorRow, col: p(0) - 1)
        case "d": moveTo(row: p(0) - 1, col: cursorCol)
        case "J": eraseDisplay(params.first ?? 0)
        case "K": eraseLine(params.first ?? 0)
        case "X": blank(row: cursorRow, from: cursorCol, to: min(cursorCol + p(0), lastCol + 1))
        case "P": deleteChars(p(0))
        case "@": insertChars(p(0))
        case "L": for _ in 0..<min(p(0), rows) { lines.insert([], at: cursorRow); lines.removeLast() }
        case "M": for _ in 0..<min(p(0), rows) { lines.remove(at: cursorRow); lines.append([]) }
        case "S": for _ in 0..<min(p(0), rows) { lines.removeFirst(); lines.append([]) }
        case "T": for _ in 0..<min(p(0), rows) { lines.removeLast(); lines.insert([], at: 0) }
        case "m": sgr(params)
        default: break
        }
    }

    private func moveTo(row: Int, col: Int) {
        if growing {
            cursorRow = max(0, row)
            while cursorRow >= lines.count { lines.append([]) }
            cursorCol = max(0, col)
        } else {
            cursorRow = min(max(0, row), rows - 1)
            cursorCol = min(max(0, col), cols - 1)
        }
    }

    private func eraseDisplay(_ mode: Int) {
        switch mode {
        case 0:
            eraseLine(0)
            for r in (cursorRow + 1)..<lines.count { lines[r] = [] }
        case 1:
            for r in 0..<cursorRow { lines[r] = [] }
            eraseLine(1)
        default:
            for r in lines.indices { lines[r] = [] }
        }
    }

    private func eraseLine(_ mode: Int) {
        let row = lines[cursorRow]
        switch mode {
        case 0: blank(row: cursorRow, from: cursorCol, to: row.count)
        case 1: blank(row: cursorRow, from: 0, to: min(cursorCol + 1, row.count))
        default: lines[cursorRow] = []
        }
    }

    /// Blanks cols [from, to) of a row in the current background (an erase keeps the background color).
    private func blank(row r: Int, from: Int, to: Int) {
        var row = lines[r]
        let bg = TermCell(text: " ", width: 1, style: TermStyle(bg: style.bg))
        if style.bg != .standard, row.count < to, !growing {
            row.append(contentsOf: repeatElement(TermCell.blank, count: min(to, cols) - row.count))
        }
        guard from < row.count else { return }
        for c in from..<min(to, row.count) { row[c] = bg }
        if style.bg == .standard { while row.last?.isBlank == true { row.removeLast() } }
        lines[r] = row
    }

    private func deleteChars(_ n: Int) {
        var row = lines[cursorRow]
        guard cursorCol < row.count else { return }
        row.removeSubrange(cursorCol..<min(row.count, cursorCol + n))
        lines[cursorRow] = row
    }

    private func insertChars(_ n: Int) {
        var row = lines[cursorRow]
        guard cursorCol < row.count else { return }
        row.insert(contentsOf: repeatElement(TermCell.blank, count: n), at: cursorCol)
        if !growing, row.count > cols { row.removeLast(row.count - cols) }
        lines[cursorRow] = row
    }

    private func sgr(_ params: [Int]) {
        var i = 0
        let ps = params.isEmpty ? [0] : params
        func color(_ at: inout Int) -> TermColor? {
            guard at + 1 < ps.count else { return nil }
            if ps[at + 1] == 5, at + 2 < ps.count { at += 2; return .indexed(UInt8(clamping: ps[at])) }
            if ps[at + 1] == 2, at + 4 < ps.count {
                at += 4
                return .rgb(UInt8(clamping: ps[at - 2]), UInt8(clamping: ps[at - 1]), UInt8(clamping: ps[at]))
            }
            return nil
        }
        while i < ps.count {
            let p = ps[i]
            switch p {
            case 0: let link = style.link; style = TermStyle(); style.link = link
            case 1: style.bold = true
            case 2: style.dim = true
            case 3: style.italic = true
            case 4: style.underline = true
            case 7: style.inverse = true
            case 8: style.hidden = true
            case 9: style.strike = true
            case 21, 22: style.bold = false; style.dim = false
            case 23: style.italic = false
            case 24: style.underline = false
            case 27: style.inverse = false
            case 28: style.hidden = false
            case 29: style.strike = false
            case 30...37: style.fg = .indexed(UInt8(p - 30))
            case 38: if let c = color(&i) { style.fg = c }
            case 39: style.fg = .standard
            case 40...47: style.bg = .indexed(UInt8(p - 40))
            case 48: if let c = color(&i) { style.bg = c }
            case 49: style.bg = .standard
            case 90...97: style.fg = .indexed(UInt8(p - 90 + 8))
            case 100...107: style.bg = .indexed(UInt8(p - 100 + 8))
            default: break
            }
            i += 1
        }
    }

    fileprivate func osc(_ body: String) {
        // OSC 8 ; params ; uri — a link from here on, an empty uri ends it.
        guard body.hasPrefix("8;") else { return }
        let parts = body.split(separator: ";", maxSplits: 2, omittingEmptySubsequences: false)
        let uri = parts.count > 2 ? String(parts[2]) : ""
        style.link = uri.isEmpty ? nil : uri
    }

    // MARK: Width

    /// Cells a character takes: 2 for East Asian wide / fullwidth and emoji shown as emoji, 0 for a lone combining mark.
    static func width(_ ch: Character) -> Int {
        let scalars = ch.unicodeScalars
        guard let s = scalars.first else { return 1 }
        let v = s.value
        if v < 0x300 { return 1 }
        if scalars.count == 1, s.properties.generalCategory == .nonspacingMark || s.properties.generalCategory == .enclosingMark {
            return 0
        }
        if scalars.contains(where: { $0.value == 0xFE0F || $0.value == 0x200D }) { return 2 }
        if s.properties.isEmojiPresentation { return 2 }
        switch v {
        case 0x1100...0x115F, 0x231A...0x231B, 0x2329...0x232A, 0x2E80...0x303E, 0x3041...0x33FF, 0x3400...0x4DBF,
             0x4E00...0x9FFF, 0xA000...0xA4CF, 0xA960...0xA97F, 0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE10...0xFE19,
             0xFE30...0xFE6F, 0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x1F300...0x1F64F, 0x1F900...0x1F9FF,
             0x20000...0x2FFFD, 0x30000...0x3FFFD:
            return 2
        default:
            return 1
        }
    }
}

/// ESC / CSI / OSC recognizer, one character at a time (a sequence may span feeds).
nonisolated private struct Parser {
    enum State { case ground, escape, csi, osc, oscEscape, skipOne }
    var state = State.ground
    var params: [Int] = []
    var current: Int?
    var privateMarker: Character?
    var oscBody = ""

    mutating func step(_ ch: Character, into g: TerminalGrid) {
        switch state {
        case .ground:
            if ch == "\u{1B}" {
                state = .escape
            } else if let a = ch.asciiValue, a < 0x20 || a == 0x7F {
                g.control(ch)
            } else {
                g.printChar(ch)
            }
        case .escape:
            switch ch {
            case "[": state = .csi; params = []; current = nil; privateMarker = nil
            case "]": state = .osc; oscBody = ""
            case "(", ")", "*", "+", "#", " ", "%": state = .skipOne // charset designations: one more character
            default: state = .ground // ESC 7 / 8 / = / > / M …: nothing kept
            }
        case .skipOne:
            state = .ground
        case .csi:
            guard let a = ch.asciiValue else { state = .ground; return }
            switch a {
            case 0x30...0x39: current = (current ?? 0) * 10 + Int(a - 0x30)
            case 0x3B, 0x3A: params.append(current ?? 0); current = nil // ';' and ':' (sub-parameters, flattened)
            case 0x3C...0x3F: privateMarker = ch
            case 0x20...0x2F: break // intermediates
            case 0x40...0x7E:
                if let current { params.append(current) }
                g.csi(ch, params, privateMarker: privateMarker)
                state = .ground
            default: state = .ground
            }
        case .osc:
            if ch == "\u{07}" {
                g.osc(oscBody); state = .ground
            } else if ch == "\u{1B}" {
                state = .oscEscape
            } else if oscBody.count < 4096 {
                oscBody.append(ch)
            }
        case .oscEscape: // ESC \ ends it
            g.osc(oscBody)
            state = .ground
            if ch != "\\" { step(ch, into: g) }
        }
    }
}

// MARK: Scrollback

extension TerminalGrid {
    /// How many rows the screen moved up from `old` to `new` (0: it did not): herdr redraws a grid in place, so rows
    /// that scroll off the top are in no frame — the phone keeps them itself when it drives the terminal (no
    /// scrollback in herdr then, docs/integrations/terminal.md). Moved by k: new's top rows are old's from row k, for at least a
    /// third of what is left (the bottom — Claude's spinner, its prompt box — changes as it goes), and those rows say
    /// something (rules and blank rows match anywhere). Moved further than the screen in one frame: nothing to match.
    static func scrolled(from old: [TermRow], to new: [TermRow]) -> Int {
        guard old.count >= 4, new.count >= 4, old[0] != new[0] else { return 0 }
        let keyOld = old.map(Self.rowKey), keyNew = new.map(Self.rowKey)
        for k in 1..<(old.count - 2) {
            var run = 0, telling = 0
            while run < new.count, k + run < old.count, keyNew[run] == keyOld[k + run], new[run] == old[k + run] {
                if !new[run].allSatisfy(\.isFiller) { telling += 1 }
                run += 1
            }
            if run >= max(3, (old.count - k) / 3), telling >= 2 { return k }
        }
        return 0
    }

    /// A cheap stand-in for comparing rows (most do not match).
    private static func rowKey(_ row: TermRow) -> Int {
        var h = Hasher()
        for c in row { h.combine(c.text); h.combine(c.style) }
        return h.finalize()
    }
}
