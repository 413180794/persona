import Foundation
import Testing
@testable import Persona

final class VideoReadLog: @unchecked Sendable {
    private let lock = NSLock()
    private var value: [Range<Int64>] = []
    var ranges: [Range<Int64>] { lock.withLock { value } }
    func append(_ range: Range<Int64>) { lock.withLock { value.append(range) } }
}

struct VideoFileResourceTests {
    private func info(total: Int64 = 10 << 20) -> VideoFileRange {
        VideoFileRange(range: 0..<2, total: total, revision: "\"v1\"", mime: "video/mp4", data: Data([0, 0]))
    }

    @Test func middleAndTailReadsSkipPrefixAndReuseCachedChunks() async throws {
        let reads = VideoReadLog()
        let total: Int64 = (10 << 20) + 13
        let source = VideoFileSource(read: { range, revision in
            #expect(revision == "\"v1\"")
            reads.append(range)
            return VideoFileRange(range: range, total: total, revision: "\"v1\"", mime: "video/mp4",
                                  data: Data(repeating: 7, count: Int(range.count)))
        })
        let cache = try VideoFileCache(source: source, info: info(total: total), name: "preview.mp4")
        defer { cache.close() }
        let offset: Int64 = (6 << 20) + 17
        #expect(try await cache.data(at: offset, upToCount: 23) == Data(repeating: 7, count: 23))
        #expect(reads.ranges == [(6 << 20)..<((6 << 20) + VideoFileCache.chunkBytes)])
        #expect(try await cache.data(at: offset + 10, upToCount: 9) == Data(repeating: 7, count: 9))
        #expect(reads.ranges.count == 1)
        #expect(try await cache.data(at: total - 5, upToCount: 40).count == 5)
        #expect(reads.ranges.last?.upperBound == total)
        #expect(reads.ranges.allSatisfy { $0.lowerBound >= 6 << 20 })
        let url = cache.url
        cache.close()
        #expect(!FileManager.default.fileExists(atPath: url.path))
        await #expect(throws: CancellationError.self) { try await cache.data(at: offset, upToCount: 1) }
    }

    @Test func chunkBoundaryIsDeliveredInBoundedPieces() async throws {
        let metadata = info()
        let reads = VideoReadLog()
        let source = VideoFileSource(read: { range, _ in
            reads.append(range)
            return VideoFileRange(range: range, total: metadata.total, revision: metadata.revision,
                                  mime: metadata.mime, data: Data(repeating: 1, count: Int(range.count)))
        })
        let cache = try VideoFileCache(source: source, info: metadata, name: "clip.mp4")
        defer { cache.close() }
        #expect(try await cache.data(at: VideoFileCache.chunkBytes - 3, upToCount: 20).count == 3)
        #expect(try await cache.data(at: VideoFileCache.chunkBytes, upToCount: 17).count == 17)
        #expect(reads.ranges.count == 2)
        #expect(reads.ranges.allSatisfy { $0.count <= VideoFileCache.chunkBytes })
    }

    @Test(arguments: ["revision", "total", "range", "short"])
    func rejectsChangedOrIncompleteChunks(kind: String) async throws {
        let metadata = info()
        let source = VideoFileSource(read: { range, _ in
            VideoFileRange(range: kind == "range" ? 1..<(range.upperBound + 1) : range,
                           total: metadata.total + (kind == "total" ? 1 : 0),
                           revision: kind == "revision" ? "\"v2\"" : metadata.revision, mime: metadata.mime,
                           data: Data(repeating: 0, count: Int(range.count) - (kind == "short" ? 1 : 0)))
        })
        let cache = try VideoFileCache(source: source, info: metadata, name: "clip.mp4")
        defer { cache.close() }
        await #expect(throws: APIError.self) { try await cache.data(at: 0, upToCount: 1) }
    }

    @Test func cancelledReadDoesNotWriteOrRecreateClosedCache() async throws {
        let started = VideoReadLog()
        let metadata = info()
        let source = VideoFileSource(read: { range, _ in
            started.append(range)
            try await Task.sleep(for: .seconds(30))
            return VideoFileRange(range: range, total: metadata.total, revision: metadata.revision,
                                  mime: metadata.mime, data: Data(repeating: 0, count: Int(range.count)))
        })
        let cache = try VideoFileCache(source: source, info: metadata, name: "clip.mp4")
        let url = cache.url
        let task = Task { try await cache.data(at: 4 << 20, upToCount: 1) }
        let deadline = Date.now.addingTimeInterval(2)
        while started.ranges.isEmpty, Date.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(!started.ranges.isEmpty)
        task.cancel()
        cache.close()
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(!FileManager.default.fileExists(atPath: url.path))
    }

    @Test func validatesContentRangeLengthAndStrongRevision() throws {
        let url = URL(string: "https://hub.example.com/file/download")!
        let headers = ["Content-Range": "bytes 100-199/1000", "ETag": "\"v1\"", "Content-Length": "100", "Content-Type": "video/mp4"]
        let response = HTTPURLResponse(url: url, statusCode: 206, httpVersion: nil, headerFields: headers)!
        let result = try VideoFileSource.metadata(response, requested: 100..<200, revision: "\"v1\"")
        #expect(result.range == 100..<200 && result.total == 1000)
        for (key, bad) in [("Content-Range", "bytes 0-99/1000"), ("Content-Range", "bytes 100-200/1000"),
                           ("Content-Range", "bytes 100-199/*"), ("Content-Length", "99"),
                           ("ETag", "W/\"v1\""), ("ETag", "\"v2\"")] {
            var changed = headers
            changed[key] = bad
            let badResponse = HTTPURLResponse(url: url, statusCode: 206, httpVersion: nil, headerFields: changed)!
            #expect(throws: APIError.self) { try VideoFileSource.metadata(badResponse, requested: 100..<200, revision: "\"v1\"") }
        }
    }

    @MainActor @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_VIDEO_HUB"] != nil))
    func realHTTPReadsMiddleTailAndRejectsChangedRevision() async throws {
        let url = try #require(ProcessInfo.processInfo.environment["PERSONA_VIDEO_HUB"].flatMap(URL.init(string:)))
        let api = APIClient(config: ServerConfig(url: url, token: "mock"))
        let file = FileContent(path: "/Users/me/code/weather/media/preview.mp4", name: "preview.mp4", size: 0, mime: "video/mp4", truncated: true, isDir: false)
        let source = VideoFileSource(api: api, ref: FileRef(machineID: "macbook", path: file.path), file: file)
        defer { source.close() }
        let info = try await source.read(0..<2, nil)
        #expect(info.data.count == 2 && info.total > 2 << 20)
        let offset = info.total / 2
        let middle = try await source.read(offset..<(offset + 100), info.revision)
        #expect(middle.range.lowerBound == offset && middle.data.count == 100)
        let tail = try await source.read((info.total - 10)..<info.total, info.revision)
        #expect(tail.data.count == 10)
        do {
            _ = try await source.read(0..<2, "\"changed\"")
            Issue.record("A changed revision must be rejected")
        } catch let error as APIError {
            #expect(error.status == 412)
            #expect(error.message.contains("已改变"))
        }
    }
}
