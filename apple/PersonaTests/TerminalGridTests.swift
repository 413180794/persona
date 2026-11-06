import Foundation
import Testing
@testable import Persona

private final class FixtureBundle {}

/// herdr observe frames recorded from a scratch pane (172×45) running a sample script: colors, a box with Chinese
/// in it, an emoji, an OSC 8 link and a 200-character line.
private func observedGrid() throws -> TerminalGrid {
    let url = try #require(Bundle(for: FixtureBundle.self).url(forResource: "terminal-observe", withExtension: "jsonl"))
    let g = TerminalGrid(cols: 1, rows: 1)
    for line in try String(contentsOf: url, encoding: .utf8).split(separator: "\n") {
        struct Frame: Decodable { var full: Bool; var width: Int; var height: Int; var bytes: String }
        let f = try JSONDecoder().decode(Frame.self, from: Data(line.utf8))
        if f.full { g.reset(cols: f.width, rows: f.height) }
        g.feed(try #require(Data(base64Encoded: f.bytes)))
    }
    return g
}

@MainActor struct TerminalGridTests {
    @Test func mirrorsWhatHerdrShows() throws {
        let g = try observedGrid()
        #expect(g.cols == 172 && g.rows == 45)
        let text = g.lines.map(TerminalGrid.plain)
        #expect(text[0] == "$ sh /tmp/herdr-spike/sample.sh")
        #expect(text[1] == "✓ build ok  orange truecolor reverse under")
        #expect(text[2] == "┌──────────┐" && text[3] == "│ 中文对齐 │" && text[4] == "└──────────┘")
        #expect(text[5] == "emoji 🚀 then text")
        #expect(text[7].count + text[8].count == 200 && Set(text[7] + text[8]) == ["L"]) // the shell wrapped it
        #expect(text[9] == "$")
        #expect(g.cursorVisible && g.cursorRow == 9 && g.cursorCol == 2)
    }

    @Test func keepsColorsAndStyles() throws {
        let row = try observedGrid().lines[1]
        func style(at text: String) -> TermStyle? {
            let s = row.map(\.text).joined()
            guard let r = s.range(of: text) else { return nil }
            return row[s.distance(from: s.startIndex, to: r.lowerBound)].style
        }
        #expect(style(at: "✓")?.bold == true && style(at: "✓")?.fg == .indexed(2))
        #expect(style(at: "orange")?.fg == .indexed(208))
        #expect(style(at: "truecolor")?.fg == .rgb(80, 160, 255))
        // herdr hands reverse video over as its own theme's colors swapped in
        #expect(style(at: "reverse")?.bg == .rgb(255, 255, 255))
        #expect(style(at: "under")?.underline == true)
        #expect(style(at: "  orange")?.bold == false)
    }

    @Test func wideCharactersTakeTwoCells() throws {
        let g = try observedGrid()
        // The box's right edge lines up with its corners: 「中文对齐」 is 4 characters in 8 cells.
        #expect(g.lines[3][11].text == "│" && g.lines[2][11].text == "┐")
        let zhong = try #require(g.lines[3].firstIndex { $0.text == "中" })
        #expect(g.lines[3][zhong].width == 2 && g.lines[3][zhong + 1].width == 0)
        let rocket = try #require(g.lines[5].firstIndex { $0.text == "🚀" })
        #expect(g.lines[5][rocket].width == 2 && g.lines[5][rocket + 2].text == " ")
    }

    @Test func readsLinks() throws {
        let g = try observedGrid()
        let row = g.lines[6]
        #expect(row[0].style.link == "https://example.com/docs" && row[8].style.link == "https://example.com/docs") // "link here"
        #expect(row[10].style.link == nil) // " after"
    }

    @Test func scrollbackPagesAreRowsOfColoredText() {
        let rows = TerminalGrid.rows(fromANSI: "\u{1B}[0m\u{1B}[31mred\u{1B}[0m plain\r\n\r\nnext 中\r\n")
        #expect(rows.map(TerminalGrid.plain) == ["red plain", "", "next 中"])
        #expect(rows[0][0].style.fg == .indexed(1) && rows[0][4].style.fg == .standard)
        #expect(TerminalGrid.rows(fromANSI: "plain only").map(TerminalGrid.plain) == ["plain only"])
        #expect(TerminalGrid.rows(fromANSI: "").isEmpty)
    }

    @Test func erasesAndOverwrites() {
        let g = TerminalGrid(cols: 10, rows: 3)
        g.feed("\u{1B}[2J\u{1B}[1;1Habcdef\u{1B}[1;3H\u{1B}[K")
        #expect(TerminalGrid.plain(g.lines[0]) == "ab")
        g.feed("\u{1B}[2;1H中文\u{1B}[2;2Hx") // writing into the right half of 「中」 blanks its left half
        #expect(TerminalGrid.plain(g.lines[1]) == " x文")
        g.feed("\u{1B}[3;10H中") // a wide character that would cross the edge is not drawn
        #expect(TerminalGrid.plain(g.lines[2]) == "")
        g.feed("\u{1B}[?25l")
        #expect(!g.cursorVisible)
        g.feed("\u{1B}[3;1H\u{1B}[41m\u{1B}[3X") // erase in a background color keeps the color
        #expect(g.lines[2].prefix(3).allSatisfy { $0.style.bg == .indexed(1) } && g.lines[2].count >= 3)
    }

    @Test func unknownSequencesAreSkipped() {
        let g = TerminalGrid(cols: 20, rows: 2)
        g.feed("\u{1B}[?2026h\u{1B}(B\u{1B}=\u{1B}]0;title\u{07}\u{1B}[5 qok\u{1B}[?2026l")
        #expect(TerminalGrid.plain(g.lines[0]) == "ok")
    }

    @Test func widths() {
        #expect(TerminalGrid.width("a") == 1 && TerminalGrid.width("─") == 1 && TerminalGrid.width("⏺") == 1)
        #expect(TerminalGrid.width("中") == 2 && TerminalGrid.width("，") == 2 && TerminalGrid.width("😀") == 2)
        #expect(TerminalGrid.width("👍🏽") == 2 && TerminalGrid.width("❤️") == 2)
    }
}

@MainActor struct TerminalFeedTests {
    private let key = SessionKey(machineID: "m", sessionID: "s")
    private func d(_ s: String) -> Data { Data(s.utf8) }

    @Test func framesApplyInOrder() {
        let f = TerminalFeed(key: key)
        #expect(f.apply(epoch: "e1", seq: 3, full: false, cols: 10, rows: 2, data: d("x")) == .gap) // nothing to apply it to
        #expect(f.apply(epoch: "e1", seq: 1, full: true, cols: 10, rows: 2, data: d("\u{1B}[2J\u{1B}[1;1Hab")) == .applied)
        #expect(f.apply(epoch: "e1", seq: 2, full: false, cols: 10, rows: 2, data: d("\u{1B}[1;3Hc")) == .applied)
        #expect(f.apply(epoch: "e1", seq: 2, full: false, cols: 10, rows: 2, data: d("\u{1B}[1;3HX")) == .duplicate)
        #expect(f.content.rows.map(TerminalGrid.plain) == ["abc"])
        #expect(f.apply(epoch: "e1", seq: 4, full: false, cols: 10, rows: 2, data: d("y")) == .gap) // 3 went missing
        #expect(f.apply(epoch: "e1", seq: 3, full: false, cols: 10, rows: 2, data: d("y")) == .gap) // and the stream is dropped until a whole frame
        #expect(f.apply(epoch: "e2", seq: 1, full: true, cols: 20, rows: 3, data: d("\u{1B}[2J\u{1B}[2;1Hnew")) == .applied)
        #expect(f.content.cols == 20 && f.content.rows.map(TerminalGrid.plain) == ["", "new"])
    }

    @Test func theScreenEndsAtItsLastTextOrTheCursor() {
        let f = TerminalFeed(key: key)
        _ = f.apply(epoch: "e", seq: 1, full: true, cols: 10, rows: 6, data: d("\u{1B}[2J\u{1B}[1;1H$ ls\u{1B}[2;1Ha\u{1B}[4;3H\u{1B}[?25h"))
        #expect(f.content.rows.count == 4) // rows 5 and 6 are empty and below the cursor
        #expect(f.content.cursor?.row == 3 && f.content.cursor?.col == 2)
        _ = f.apply(epoch: "e", seq: 2, full: false, cols: 10, rows: 6, data: d("\u{1B}[?25l"))
        #expect(f.content.rows.count == 2 && f.content.cursor == nil)
    }
}
