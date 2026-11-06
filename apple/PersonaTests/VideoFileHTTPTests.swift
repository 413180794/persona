import Foundation
import Testing
@testable import Persona

private final class VideoRangeProtocol: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var pending = (started: false, stopped: false)
    static func resetPending() { lock.withLock { pending = (false, false) } }
    static var pendingState: (started: Bool, stopped: Bool) { lock.withLock { pending } }
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "video.invalid" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token")
        #expect(request.value(forHTTPHeaderField: "Range") == "bytes=0-1")
        #expect(request.url?.query?.contains("token") == false)
        let mode = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "path" }?.value ?? "ok"
        var status = 206
        var headers = ["Content-Range": "bytes 0-1/10000000", "ETag": "\"v1\"", "Content-Length": "2", "Content-Type": "video/mp4"]
        var body = Data([1, 2])
        switch mode {
        case "ignored": status = 200; headers["Content-Length"] = "10000000"
        case "range": headers["Content-Range"] = "bytes 2-3/10000000"
        case "revision": headers["ETag"] = "\"v2\""
        case "short": body = Data([1])
        case "oversized": body = Data([1, 2, 3])
        case "old":
            status = 409
            body = Data(#"{"error":"需要重新部署","code":"node_outdated"}"#.utf8)
            headers = ["Content-Type": "application/json", "Content-Length": String(body.count)]
        default: break
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        if mode == "pending" {
            Self.lock.withLock { Self.pending.started = true }
            return
        }
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {
        if request.url?.query?.contains("path=pending") == true { Self.lock.withLock { Self.pending.stopped = true } }
    }
}

@MainActor
struct VideoFileHTTPTests {
    private func source(mode: String) -> VideoFileSource {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [VideoRangeProtocol.self]
        let api = APIClient(config: ServerConfig(url: URL(string: "https://video.invalid")!, token: "fixture-token"),
                            session: URLSession(configuration: configuration))
        let file = FileContent(path: mode, name: "clip.mp4", size: 10_000_000, mime: "video/mp4", truncated: true, isDir: false)
        return VideoFileSource(api: api, ref: FileRef(machineID: "example-node", path: mode, cwd: "/example"), file: file)
    }

    @Test func requestCarriesAuthenticationRangeAndVersion() async throws {
        let source = source(mode: "ok")
        defer { source.close() }
        let result = try await source.read(0..<2, "\"v1\"")
        #expect(result.data == Data([1, 2]) && result.total == 10_000_000)
    }

    @Test(arguments: ["ignored", "range", "revision", "short", "oversized"])
    func rejectsWholeResponsesAndInvalidPieces(mode: String) async throws {
        let source = source(mode: mode)
        defer { source.close() }
        await #expect(throws: (any Error).self) { try await source.read(0..<2, "\"v1\"") }
    }

    @Test func cancellationStopsAnIncompleteHTTPBody() async throws {
        VideoRangeProtocol.resetPending()
        let source = source(mode: "pending")
        defer { source.close() }
        let task = Task { try await source.read(0..<2, nil) }
        let deadline = Date.now.addingTimeInterval(2)
        while !VideoRangeProtocol.pendingState.started, Date.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(VideoRangeProtocol.pendingState.started)
        task.cancel()
        await #expect(throws: (any Error).self) { try await task.value }
        #expect(VideoRangeProtocol.pendingState.stopped)
    }

    @Test func preservesOldNodeErrorAndCode() async throws {
        let source = source(mode: "old")
        defer { source.close() }
        do {
            _ = try await source.read(0..<2, nil)
            Issue.record("An old node must be rejected")
        } catch let error as APIError {
            #expect(error.status == 409 && error.code == "node_outdated")
            #expect(error.message == "需要重新部署")
        }
    }
}
