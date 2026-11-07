import Foundation
import Testing
import UIKit
@testable import Persona

private final class FixtureBundle {}

/// herdr observe frames recorded from a 172×45 pane running Claude Code (2.1.312): `mixed` is a long answer in
/// Chinese and English streaming in, `permission` a Bash permission dialog. The .txt next to each is what
/// `herdr pane read` showed when the recording stopped.
private func recorded(_ name: String) throws -> (grid: TerminalGrid, herdr: [String]) {
    let bundle = Bundle(for: FixtureBundle.self)
    let url = try #require(bundle.url(forResource: name, withExtension: "jsonl"))
    let g = TerminalGrid(cols: 1, rows: 1)
    for line in try String(contentsOf: url, encoding: .utf8).split(separator: "\n") {
        struct Frame: Decodable { var full: Bool; var width: Int; var height: Int; var bytes: String }
        let f = try JSONDecoder().decode(Frame.self, from: Data(line.utf8))
        if f.full { g.reset(cols: f.width, rows: f.height) }
        g.feed(try #require(Data(base64Encoded: f.bytes)))
    }
    let txt = try #require(bundle.url(forResource: name, withExtension: "txt"))
    let herdr = try String(contentsOf: txt, encoding: .utf8).split(separator: "\n", omittingEmptySubsequences: false)
        .map { trimmed(String($0)) }
    return (g, Array(herdr.prefix(45)))
}

/// Claude puts a no-break space after ❯; trailing spaces of either kind are not what a row shows.
private func trimmed(_ s: String) -> String {
    s.replacingOccurrences(of: "[ \u{00A0}]+$", with: "", options: .regularExpression)
}

private func text(_ row: TermRow, _ line: DisplayLine) -> String {
    row[line.from..<min(line.to, row.count)].filter { $0.width > 0 }.map(\.text).joined()
}

@MainActor struct TerminalWrapTests {
    /// herdr redraws streaming text by putting each Chinese character at its column, often over the one that was
    /// there: the right half written after it used to blank the character itself (whole words went missing).
    @Test(arguments: ["terminal-observe-mixed", "terminal-observe-permission"])
    func streamedFramesEndAsHerdrShows(_ name: String) throws {
        let (g, herdr) = try recorded(name)
        let ours = g.lines.map(TerminalGrid.plain)
        for (i, line) in herdr.enumerated() where i < ours.count {
            #expect(trimmed(ours[i]) == line, "row \(i)")
        }
        #expect(ours.contains { $0.contains("\\n，无论窗口多宽都会在原处断行") })
    }

    @Test func overwritingAWideCharacterInPlaceKeepsIt() {
        let g = TerminalGrid(cols: 10, rows: 1)
        g.feed("\u{1B}[1;1H旧字\u{1B}[1;1H新\u{1B}[1;3H字")
        #expect(TerminalGrid.plain(g.lines[0]) == "新字")
        g.feed("\u{1B}[1;2H中") // over the right half of 「新」 and the left of 「字」: both go
        #expect(TerminalGrid.plain(g.lines[0]) == " 中")
    }

    /// Folding at the phone's width ends lines between words and next to Chinese characters, never before closing
    /// punctuation; continuation lines hang under the text.
    @Test func foldsBetweenWords() throws {
        let (g, _) = try recorded("terminal-observe-mixed")
        let rows = g.lines
        for perLine in [38, 44, 52] {
            let lines = TerminalWrap.lines(rows, cursor: nil, perLine: perLine, wrap: true)
            for (i, line) in lines.enumerated() where i + 1 < lines.count && lines[i + 1].row == line.row {
                let row = rows[line.row], next = lines[i + 1]
                let last = row[line.to - 1].width == 0 ? row[line.to - 2].text : row[line.to - 1].text
                let first = row[next.from].text
                let midWord = next.from == line.to && last.first?.isLetter == true && last.first?.isASCII == true
                    && first.first?.isLetter == true && first.first?.isASCII == true
                #expect(!midWord, "\(perLine): \(text(row, line)) | \(text(row, next))")
                #expect(!TerminalWrap.closing.contains(first), "\(perLine): line starts with \(first)")
                #expect(line.to - line.from + line.indent <= perLine)
            }
        }
        // 「⏺ 终端以 cell 为单位…」 hangs under its text, after the marker.
        let answer = try #require(rows.firstIndex { TerminalGrid.plain($0).hasPrefix("⏺ 终端以 cell") })
        let folded = TerminalWrap.lines(rows, cursor: nil, perLine: 40, wrap: true).filter { $0.row == answer }
        #expect(folded.count > 1 && folded.dropFirst().allSatisfy { $0.indent == 2 })
    }

    @Test func dialogOptionsHangUnderTheirText() throws {
        let (g, _) = try recorded("terminal-observe-permission")
        let rows = g.lines
        let option = try #require(rows.firstIndex { TerminalGrid.plain($0).contains("2. Yes, allow reading from") })
        let folded = TerminalWrap.lines(rows, cursor: nil, perLine: 40, wrap: true).filter { $0.row == option }
        #expect(folded.count >= 2)
        #expect(folded.dropFirst().allSatisfy { $0.indent == 6 }) // "   2. " — under "Yes"
        #expect(folded.map { text(rows[option], $0) }.joined(separator: " ")
            .replacingOccurrences(of: " +", with: " ", options: .regularExpression).contains("/private/tmp/term-lab/files from this project"))
    }

    /// A row padded to the pane's width — Claude's rules (─, ╌) and the grey band behind a message — is one line
    /// on the phone, not three of dashes or of an empty colored block.
    @Test func paddingEndsAtThePhonesEdge() throws {
        let (g, _) = try recorded("terminal-observe-permission")
        let rows = g.lines
        let lines = TerminalWrap.lines(rows, cursor: nil, perLine: 44, wrap: true)
        for (r, row) in rows.enumerated() where !row.isEmpty && row.allSatisfy(\.isFiller) {
            #expect(lines.filter { $0.row == r }.count <= 1, "row \(r)")
        }
        let prompt = try #require(rows.firstIndex { TerminalGrid.plain($0).hasPrefix("❯ Run ls -la") })
        #expect(rows[prompt].last?.style.bg != .standard) // the band runs to the pane's edge
        #expect(lines.filter { $0.row == prompt }.count == 2) // its text folds once, the band does not add lines
    }

    @Test func unwrappedIsOneLinePerRow() throws {
        let (g, _) = try recorded("terminal-observe-mixed")
        let lines = TerminalWrap.lines(g.lines, cursor: nil, perLine: 40, wrap: false)
        #expect(lines.count == g.lines.count && lines.allSatisfy { $0.from == 0 && $0.indent == 0 })
    }
}

struct KeyBatcherTests {
    @Test func textThenKeysGoInOneRequest() {
        var b = KeyBatcher()
        for e: TerminalInputEvent in [.text("l"), .text("s"), .key("enter"), .key("up")] { b.push(e) }
        #expect(b.take()! == (keys: ["enter", "up"], text: "ls"))
        #expect(b.take() == nil)
    }

    @Test func textAfterAKeyWaitsForTheNextRequest() {
        var b = KeyBatcher()
        for e: TerminalInputEvent in [.key("esc"), .text("a"), .key("tab"), .text("b")] { b.push(e) }
        #expect(b.take()! == (keys: ["esc"], text: ""))
        #expect(b.take()! == (keys: ["tab"], text: "a"))
        #expect(b.take()! == (keys: [], text: "b"))
        #expect(b.isEmpty)
    }

    @Test func backspaceTakesBackWhatHasNotGoneOut() {
        var b = KeyBatcher()
        for e: TerminalInputEvent in [.text("gti"), .key("backspace"), .key("backspace"), .text("it")] { b.push(e) }
        #expect(b.take()! == (keys: [], text: "git"))
        for e: TerminalInputEvent in [.text("x"), .key("backspace"), .key("backspace")] { b.push(e) }
        #expect(b.take()! == (keys: ["backspace"], text: "")) // the x never went out; the one before it did
    }
}

/// What one streamed frame costs the phone: parse it, cut the rows into lines, draw what shows. Printed for the
/// record (docs/integrations/terminal.md); the bound only catches a large regression (debug build, simulator).
@MainActor struct TerminalFrameCostTests {
    @Test func aStreamedFrameIsCheap() throws {
        let url = try #require(Bundle(for: FixtureBundle.self).url(forResource: "terminal-observe-mixed", withExtension: "jsonl"))
        struct Frame: Decodable { var full: Bool; var width: Int; var height: Int; var bytes: String }
        let frames = try String(contentsOf: url, encoding: .utf8).split(separator: "\n")
            .map { try JSONDecoder().decode(Frame.self, from: Data($0.utf8)) }
        let view = TerminalScrollView(frame: CGRect(x: 0, y: 0, width: 370, height: 600))
        view.layoutIfNeeded()
        let g = TerminalGrid(cols: 1, rows: 1)
        let ctx = try #require(CGContext(data: nil, width: 370 * 3, height: 600 * 3, bitsPerComponent: 8, bytesPerRow: 0,
                                         space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.scaleBy(x: 3, y: 3)
        var costs: [Double] = []
        for f in frames {
            let start = ContinuousClock.now
            if f.full { g.reset(cols: f.width, rows: f.height) }
            g.feed(try #require(Data(base64Encoded: f.bytes)))
            let content = TerminalContent(rows: g.lines, cols: g.cols, cursor: g.cursorVisible ? (g.cursorRow, g.cursorCol) : nil)
            view.update(content: content, wrap: true, fontSize: 13, font: .jetbrains, theme: .ink, dark: true)
            view.layoutIfNeeded()
            view.canvas.layer.render(in: ctx)
            let d = ContinuousClock.now - start
            costs.append(Double(d.components.attoseconds) / 1e15 + Double(d.components.seconds) * 1000)
        }
        let sorted = costs.sorted()
        print("terminal frame cost: \(frames.count) frames, median \(sorted[sorted.count / 2]) ms, p95 \(sorted[sorted.count * 95 / 100]) ms, max \(sorted.last!) ms")
        #expect(sorted[sorted.count / 2] < 50)
    }
}

/// The phone driving the terminal: herdr's frames of a 44 × 30 grid (`terminal session control`, recorded from Claude
/// Code 2.1.312 writing sixty numbered lines). herdr redraws the grid in place and keeps no scrollback at that size,
/// so what scrolled off is only what the phone noticed moving up.
@MainActor struct TerminalControlTests {
    private struct Rec: Decodable { var t: Double; var full: Bool; var width: Int; var height: Int; var bytes: String }

    /// The recording as the app gets it: frame by frame, or through a gate like the node's (frames within `gate`
    /// seconds of the one sent before go out as one: the diffs run on, a whole frame drops what came before it).
    private func frames(gate: Double = 0) throws -> [Rec] {
        let url = try #require(Bundle(for: FixtureBundle.self).url(forResource: "terminal-control-scroll", withExtension: "jsonl"))
        let all = try String(contentsOf: url, encoding: .utf8).split(separator: "\n")
            .map { try JSONDecoder().decode(Rec.self, from: Data($0.utf8)) }
        guard gate > 0 else { return all }
        var out: [Rec] = [], pending: Rec?, due = 0.0, last = -1.0
        func join(_ a: String, _ b: String) -> String { (Data(base64Encoded: a)! + Data(base64Encoded: b)!).base64EncodedString() }
        for f in all {
            if let p = pending, f.t >= due { out.append(p); last = due; pending = nil }
            if var p = pending {
                if f.full { p = f } else { p.bytes = join(p.bytes, f.bytes); p.width = f.width; p.height = f.height }
                pending = p
            } else if f.t - last >= gate {
                out.append(f); last = f.t
            } else {
                pending = f; due = last + gate
            }
        }
        if let pending { out.append(pending) }
        return out
    }

    private func driving() -> TerminalFeed {
        let f = TerminalFeed(key: SessionKey(machineID: "m", sessionID: "s"))
        f.setGrid(TerminalGridSize(cols: 44, rows: 30), nil)
        f.controlChanged("active", why: nil)
        return f
    }

    /// 0: as herdr sent them; 0.03: the node's gate (Claude redraws every ~110 ms, nothing merges); 0.25: much
    /// coarser, the screen moves several lines a frame.
    @Test(arguments: [0, 0.03, 0.25]) func whatScrolledOffIsKept(gate: Double) throws {
        let f = driving()
        for (i, r) in try frames(gate: gate).enumerated() {
            _ = f.apply(epoch: "e", seq: i + 1, full: r.full, cols: r.width, rows: r.height, data: try #require(Data(base64Encoded: r.bytes)))
        }
        #expect(!f.scrollback.isEmpty)
        let numbered = f.content.rows.map(TerminalGrid.plain).compactMap { line -> Int? in
            guard let m = line.firstMatch(of: /^\D{0,4}?(\d+)\. \S/) else { return nil }
            return Int(m.1)
        }
        #expect(numbered == Array(1...60)) // every line once, in order: none lost, none twice
    }

    /// Driving it, rows are the view's width: one line each, nothing folds.
    @Test func rowsOfTheScreensWidthDoNotFold() throws {
        let f = driving()
        for (i, r) in try frames().enumerated() {
            _ = f.apply(epoch: "e", seq: i + 1, full: r.full, cols: r.width, rows: r.height, data: try #require(Data(base64Encoded: r.bytes)))
        }
        let rows = f.content.rows
        #expect(TerminalWrap.lines(rows, cursor: nil, perLine: 44, wrap: true).count == rows.count)
    }

    @Test func onlyAMoveUpIsAScroll() {
        func rows(_ lines: [String]) -> [TermRow] {
            let g = TerminalGrid(cols: 12, rows: lines.count)
            g.feed(lines.enumerated().map { "\u{1B}[\($0.offset + 1);1H\($0.element)" }.joined())
            return g.lines
        }
        let a = rows(["one", "two", "three", "four", "five", "────", "> ", "────"])
        #expect(TerminalGrid.scrolled(from: a, to: rows(["three", "four", "five", "six", "seven", "────", "> x", "────"])) == 2)
        #expect(TerminalGrid.scrolled(from: a, to: rows(["one", "two", "three", "four", "five", "six", "────", "> "])) == 0) // grew in place
        #expect(TerminalGrid.scrolled(from: a, to: rows(["new", "screen", "", "", "", "────", "> ", "────"])) == 0) // cleared
        // Rules and blank rows line up anywhere: they do not make a scroll.
        let b = rows(["", "────", "", "────", "a", "b", "c", "d"])
        #expect(TerminalGrid.scrolled(from: b, to: rows(["────", "", "────", "x", "y", "z", "w", "v"])) == 0)
    }

    @Test func goingBackToWatchingDropsWhatWasKept() throws {
        let f = driving()
        for (i, r) in try frames().enumerated() {
            _ = f.apply(epoch: "e", seq: i + 1, full: r.full, cols: r.width, rows: r.height, data: try #require(Data(base64Encoded: r.bytes)))
        }
        f.controlChanged("taken", why: "电脑上另一个程序接管了这个终端")
        #expect(f.control == .declined && f.controlNote == "电脑上另一个程序接管了这个终端" && f.scrollback.isEmpty && !f.drives)
    }
}

struct TerminalKeyBytesTests {
    @Test func keysAreWhatATerminalSends() {
        let cases: [(String, String)] = [
            ("enter", "\r"), ("shift+enter", "\u{1B}\r"), ("tab", "\t"), ("shift+tab", "\u{1B}[Z"), ("esc", "\u{1B}"),
            ("backspace", "\u{7F}"), ("ctrl+c", "\u{03}"), ("ctrl+a", "\u{01}"), ("ctrl+[", "\u{1B}"), ("ctrl+\\", "\u{1C}"),
            ("up", "\u{1B}[A"), ("left", "\u{1B}[D"), ("ctrl+up", "\u{1B}[1;5A"), ("alt+left", "\u{1B}[1;3D"),
            ("shift+right", "\u{1B}[1;2C"), ("alt+x", "\u{1B}x"), ("ctrl+alt+c", "\u{1B}\u{03}"), ("shift+a", "A"),
            ("space", " "), ("pagedown", "\u{1B}[6~"), ("f1", "\u{1B}OP"), ("home", "\u{1B}[H"), ("delete", "\u{1B}[3~"),
        ]
        for (name, bytes) in cases { #expect(TerminalKeyBytes.encode(name) == bytes, "\(name)") }
        #expect(TerminalKeyBytes.encode("nonsense") == nil)
    }

    @Test func aPasteWithLinesIsOnePiece() {
        #expect(TerminalKeyBytes.text("ls -la") == "ls -la")
        #expect(TerminalKeyBytes.text("a\nb\r\nc") == "\u{1B}[200~a\rb\rc\u{1B}[201~")
    }
}

extension TerminalWrapTests {
    @Test(arguments: ["terminal-observe-mixed", "terminal-observe-permission"])
    func incrementalLayoutMatchesFullLayoutForEveryFrame(_ name: String) throws {
        let bundle = Bundle(for: FixtureBundle.self)
        let file = try #require(bundle.url(forResource: name, withExtension: "jsonl"))
        let frames = try String(contentsOf: file, encoding: .utf8).split(separator: "\n")
        for width in [23, 40, 80] {
            for wrap in [false, true] {
                var layout = TerminalLineLayout()
                let grid = TerminalGrid(cols: 1, rows: 1)
                for raw in frames {
                    struct Frame: Decodable { var full: Bool; var width: Int; var height: Int; var bytes: String }
                    let frame = try JSONDecoder().decode(Frame.self, from: Data(raw.utf8))
                    if frame.full { grid.reset(cols: frame.width, rows: frame.height) }
                    grid.feed(try #require(Data(base64Encoded: frame.bytes)))
                    let cursor: (row: Int, col: Int)? = grid.cursorVisible ? (grid.cursorRow, grid.cursorCol) : nil
                    let expected = TerminalWrap.lines(grid.lines, cursor: cursor, perLine: width, wrap: wrap)
                    #expect(layout.lines(grid.lines, cursor: cursor, perLine: width, wrap: wrap) == expected)
                    #expect(layout.lines(grid.lines, cursor: cursor, perLine: width, wrap: wrap) == expected)
                }
                let history = [grid.lines[0]] + grid.lines
                #expect(layout.lines(history, cursor: nil, perLine: width, wrap: wrap)
                        == TerminalWrap.lines(history, cursor: nil, perLine: width, wrap: wrap))
                #expect(layout.lines(history, cursor: (0, 90), perLine: width + 1, wrap: !wrap)
                        == TerminalWrap.lines(history, cursor: (0, 90), perLine: width + 1, wrap: !wrap))
            }
        }
    }
}
