import Foundation
import Testing
@testable import PersonaKit

struct TableParsingTests {
    private func parse(_ text: String, separator: Character = ",", pages: Int = 1) -> [[String]] {
        var p = DelimitedParser(separator: separator.asciiValue!)
        guard pages > 1 else { return p.feed(text, final: true) }
        // Cut anywhere (inside quotes, between the two quotes of ""), as a page boundary can.
        let bytes = Array(text.utf8)
        var out: [[String]] = []
        var start = 0
        for i in 1...pages {
            let end = i == pages ? bytes.count : bytes.count * i / pages
            out += p.feed(String(decoding: bytes[start..<end], as: UTF8.self), final: i == pages)
            start = end
        }
        return out
    }

    @Test func csvQuotesSeparatorsAndLineBreaks() {
        let text = "a,b,c\r\n1,\"x, y\",\"say \"\"hi\"\"\"\n2,\"two\nlines\",\n\n3,,\"\"\n"
        let rows = parse(text)
        #expect(rows == [["a", "b", "c"], ["1", "x, y", "say \"hi\""], ["2", "two\nlines", ""], ["3", "", ""]])
        for pages in [2, 3, 7, 19, text.utf8.count] {
            #expect(parse(text, pages: pages) == rows, "cut into \(pages)")
        }
    }

    @Test func lastRowWithoutNewlineAndTabs() {
        #expect(parse("a\tb\n1\t2", separator: "\t") == [["a", "b"], ["1", "2"]])
        #expect(parse("\"unterminated, still", separator: ",") == [["unterminated, still"]])
        #expect(parse("") == [])
    }

    @Test func separatorByNameAndFirstLine() {
        #expect(DelimitedParser.separator(name: "a.tsv", firstLine: "a,b") == UInt8(ascii: "\t"))
        #expect(DelimitedParser.separator(name: "a.csv", firstLine: "a;b;c") == UInt8(ascii: ";"))
        #expect(DelimitedParser.separator(name: "a.csv", firstLine: "a,b;c") == UInt8(ascii: ","))
    }

    @Test func columnNames() {
        #expect(TableSheet.columnName(0) == "A")
        #expect(TableSheet.columnName(25) == "Z")
        #expect(TableSheet.columnName(26) == "AA")
        #expect(TableSheet.columnName(701) == "ZZ")
        #expect(TableSheet.columnName(702) == "AAA")
    }

    @Test func sheetCap() {
        var s = TableSheet(name: "t")
        s.append(Array(repeating: ["x"], count: TableSheet.maxRows - 1))
        s.append([["a", "b"], ["c"]])
        #expect(s.rows.count == TableSheet.maxRows && s.capped && s.columns == 2)
    }

    /// scripts/mock/mock_office.py's 「逐小时预报.xlsx」 (hourly forecast): two sheets, shared strings, numbers, a date style, booleans.
    static let workbook = Data(base64Encoded: "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")!

    @Test func xlsxSheetsAndCells() throws {
        let sheets = try XLSXReader.read(Self.workbook)
        #expect(sheets.map(\.name) == ["逐小时预报", "城市"])
        let hours = sheets[0]
        #expect(hours.rows.count == 25 && hours.columns == 5)
        #expect(hours.rows[0] == ["时间", "城市", "温度 °C", "降雨概率 %", "风速 km/h"])
        #expect(hours.rows[1] == ["00:00", "上海", "12", "0", "5"])
        #expect(hours.rows[2][2] == "17.6")
        let cities = sheets[1]
        #expect(cities.rows[1] == ["上海", "UTC+8", "31.2", "121.5", "2026-10-31", "TRUE"])
        #expect(cities.rows[3][5] == "FALSE")
        #expect(cities.rows[3][3] == "-9.1")
    }

    @Test func notAWorkbook() {
        #expect(throws: (any Error).self) { try XLSXReader.read(Data("hello".utf8)) }
        #expect(throws: (any Error).self) { try XLSXReader.read(Data(Self.workbook.prefix(400))) }
    }

    @Test func numbersAndDates() {
        #expect(XLSXReader.number("12") == "12")
        #expect(XLSXReader.number("12.0") == "12")
        #expect(XLSXReader.number("0.30000000000000004") == "0.3")
        #expect(XLSXReader.number("-9.1") == "-9.1")
        #expect(XLSXReader.number("1E+20") == "1e+20")
        #expect(XLSXReader.date("46326") == "2026-10-31")
        #expect(XLSXReader.date("46326.5") == "2026-10-31 12:00")
        #expect(XLSXReader.date("0.25") == "06:00")
    }

}
