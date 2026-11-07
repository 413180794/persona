import Foundation
import Testing
@testable import Persona

/// Files into a folder of the file browser (docs/protocol/protocol.md "Changing files", #4858): what is sent, and the "already exists" question.
@MainActor
struct FolderTransfersTests {
    private func file(_ name: String = "a.txt") throws -> URL {
        let dir = FileManager.default.temporaryDirectory.appending(path: "folder-tests/" + UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appending(path: name)
        try Data("hello".utf8).write(to: url)
        return url
    }

    private static let uploaded = #"{"path": "/home/me/proj/a.txt", "name": "a.txt", "size": 5}"#

    @Test func sendsIntoTheFolderUnderItsName() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files", json: Self.uploaded)
        var asked = 0
        let out = try await FolderTransfers.send(try file(), name: "a.txt", dir: "~/proj", cwd: "/w", machine: "m1", api: hub.client,
                                                 ask: { _ in asked += 1; return .cancel }) { _ in }
        #expect(out?.path == "/home/me/proj/a.txt")
        #expect(asked == 0)
        let req = try #require(hub.sent("POST", "/api/v1/machines/m1/files").first)
        #expect(req.query == ["name": "a.txt", "dest": "dir", "dir": "~/proj", "cwd": "/w"])
        #expect(req.body == Data("hello".utf8))
    }

    /// A taken name asks once; the answer goes with the second try; Cancel sends nothing more.
    @Test func aTakenNameAsks() async throws {
        for (choice, policy) in [(FolderTransfers.Choice.keep, "keep"), (.replace, "replace")] {
            let hub = FakeHub()
            hub.on("POST", "/api/v1/machines/m1/files") { req in
                if req.query["conflict"] == nil { throw APIError(status: 409, message: "a.txt already exists", fields: ["code": "exists", "name": "a.txt"]) }
                return Data(Self.uploaded.utf8)
            }
            var asked: [String] = []
            var restarted = 0
            let out = try await FolderTransfers.send(try file(), name: "a.txt", dir: "~/proj", cwd: "", machine: "m1", api: hub.client,
                                                     ask: { asked.append($0); return choice }, restart: { restarted += 1 }) { _ in }
            #expect(out != nil)
            #expect(asked == ["a.txt"])
            #expect(restarted == 1)
            #expect(hub.sent("POST", "/api/v1/machines/m1/files").map { $0.query["conflict"] } == [nil, policy])
        }
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files") { _ in throw APIError(status: 409, message: "exists", fields: ["code": "exists"]) }
        let out = try await FolderTransfers.send(try file(), name: "a.txt", dir: "~", cwd: "", machine: "m1", api: hub.client,
                                                 ask: { _ in .cancel }) { _ in }
        #expect(out == nil)
        #expect(hub.sent("POST", "/api/v1/machines/m1/files").count == 1)
    }

    /// Exists again after the answer (a folder of that name) is an error, not another question.
    @Test func asksOnlyOnce() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files") { _ in throw APIError(status: 409, message: "d already exists", fields: ["code": "exists"]) }
        var asked = 0
        await #expect(throws: APIError.self) {
            _ = try await FolderTransfers.send(try file("d"), name: "d", dir: "~", cwd: "", machine: "m1", api: hub.client,
                                               ask: { _ in asked += 1; return .replace }) { _ in }
        }
        #expect(asked == 1)
    }

    @Test func failuresInWords() {
        let e = { (code: String) in APIError(status: 409, message: "raw", fields: ["code": code]) }
        #expect(FolderTransfers.message(e("read_only"), name: "a.txt")?.contains("home") == true)
        #expect(FolderTransfers.message(e("too_large"), name: "a.txt")?.contains("2 GB") == true)
        #expect(FolderTransfers.message(APIError(status: 409, message: "MacBook 上的 personad 太旧", fields: ["code": "node_outdated"]), name: "a") == "MacBook 上的 personad 太旧")
        #expect(FolderTransfers.message(APIError(status: 400, message: "unknown dest"), name: "a")?.contains("hub 版本太旧") == true)
        #expect(FolderTransfers.message(CancellationError(), name: "a") == nil)
    }

    /// One size format everywhere: an empty file reads in the same unit as a 30-byte one, never "Zero KB" / "0 KB".
    @Test func sizesShareOneFormat() {
        let unit = { (n: Int64) in n.storageText.filter { !$0.isNumber && !$0.isWhitespace && $0 != "," } }
        #expect(unit(0) == unit(30))
        #expect(!Int64(0).storageText.lowercased().contains("zero"))
        #expect(Int64(0).storageText.contains("0"))
        #expect(unit(2_048) != unit(30))
    }
}
