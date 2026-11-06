import Foundation
import SwiftUI
import Testing
@testable import Persona

struct FileAttachTests {
    @Test func insertsAtTheEnd() {
        var r = PathInsert.insert(["/tmp/report-20261029-153012.pdf"], into: "", at: nil)
        #expect(r.text == "/tmp/report-20261029-153012.pdf ")
        #expect(r.cursor == r.text.utf16.count)
        r = PathInsert.insert(["/tmp/a.txt"], into: "看看这个", at: nil)
        #expect(r.text == "看看这个 /tmp/a.txt ")
        r = PathInsert.insert(["/tmp/a.txt"], into: "看看这个 ", at: nil)
        #expect(r.text == "看看这个 /tmp/a.txt ")
    }

    @Test func insertsAtTheCursorAndReplacesASelection() {
        let text = "比较和的区别" // cursor after 「比较」
        var r = PathInsert.insert(["/tmp/a.txt"], into: text, at: 2..<2)
        #expect(r.text == "比较 /tmp/a.txt 和的区别")
        #expect(r.cursor == "比较 /tmp/a.txt ".utf16.count)
        r = PathInsert.insert(["/tmp/b.txt"], into: "open THIS now", at: 5..<9)
        #expect(r.text == "open /tmp/b.txt now")
        #expect(r.cursor == "open /tmp/b.txt".utf16.count)
        // Emoji are two UTF-16 units: offsets stay on the right side of them.
        r = PathInsert.insert(["/tmp/c"], into: "👍👍", at: 2..<2)
        #expect(r.text == "👍 /tmp/c 👍")
        // A stale selection past the end is clamped.
        r = PathInsert.insert(["/tmp/c"], into: "hi", at: 9..<12)
        #expect(r.text == "hi /tmp/c ")
    }

    @Test func severalPathsAndQuoting() {
        let r = PathInsert.insert(["/tmp/a.txt", "/tmp/会议纪要 v2-20261029-153012.docx"], into: "", at: nil)
        #expect(r.text == "/tmp/a.txt '/tmp/会议纪要 v2-20261029-153012.docx' ")
        #expect(PathInsert.quoted("/tmp/it's.txt") == "'/tmp/it'\\''s.txt'")
        #expect(PathInsert.quoted("/var/folders/x/T/plain.pdf") == "/var/folders/x/T/plain.pdf")
    }

    @Test func selectionFromAnEarlierText() {
        let long = "a much longer draft than the one now"
        let sel = TextSelection(insertionPoint: long.endIndex)
        #expect(PathInsert.range(sel, in: "short") == 5..<5)
        #expect(PathInsert.range(sel, in: "") == 0..<0)
        #expect(PathInsert.range(nil, in: "x") == nil)
        let mid = "比较和的区别"
        #expect(PathInsert.range(TextSelection(insertionPoint: mid.index(mid.startIndex, offsetBy: 2)), in: mid) == 2..<2)
    }

    @Test func messages() {
        let big = APIError(status: 413, message: "文件超过 200 MB", fields: ["code": "too_large"])
        #expect(FileUploads.message(big, name: "a.zip") == "「a.zip」超过 200 MB，发不了")
        let old = APIError(status: 409, message: "MacBook 上的 personad 太旧，不能接收文件，需要重新部署", fields: ["code": "node_outdated"])
        #expect(FileUploads.message(old, name: "a.zip") == old.message)
        #expect(FileUploads.message(APIError(status: 404, message: "404 page not found"), name: "a.zip")?.contains("hub 版本太旧") == true)
        #expect(FileUploads.message(URLError(.cancelled), name: "a.zip") == nil)
        #expect(FileUploads.message(CancellationError(), name: "a.zip") == nil)
    }

    @Test func stagesACopy() async throws {
        let src = FileManager.default.temporaryDirectory.appending(path: "stage-src-\(UUID().uuidString).txt")
        try Data("hello".utf8).write(to: src)
        defer { try? FileManager.default.removeItem(at: src) }
        let copy = try await FileUploads.stage(src)
        defer { try? FileManager.default.removeItem(at: copy.deletingLastPathComponent()) }
        #expect(copy != src)
        #expect(copy.lastPathComponent == src.lastPathComponent)
        #expect(try Data(contentsOf: copy) == Data("hello".utf8))
    }
}
