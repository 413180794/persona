import AVFoundation
import Foundation
import Testing
@testable import Jarvis

@MainActor
struct VideoFileTests {
    @Test func videoTypesAndExistingBinaryPreviews() async throws {
        #expect(VideoFileType.matches(name: "preview.MP4", mime: "application/octet-stream"))
        #expect(VideoFileType.matches(name: "clip", mime: "video/quicktime"))
        #expect(!VideoFileType.matches(name: "source.swift", mime: "text/plain"))
        for (mime, name, kind) in [("video/mp4", "preview.mp4", FileViewerModel.Kind.video),
                                  ("application/octet-stream", "preview.MOV", .video),
                                  ("image/png", "image.png", .image), ("application/pdf", "report.pdf", .pdf),
                                  ("application/octet-stream", "data.bin", .binary)] {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/machines/box/file", json: """
                {"path":"/work/\(name)","name":"\(name)","size":4194304,"mime":"\(mime)","data":"AA==","truncated":true,"is_dir":false}
                """)
            let model = FileViewerModel(ref: FileRef(machineID: "box", path: name, cwd: "/work"), api: hub.client)
            await model.load()
            #expect(model.kind == kind)
            #expect((model.video != nil) == (kind == .video))
        }
    }

    @Test func downloadUsesResolvedPathAndWritesCompleteBytes() async throws {
        let hub = FakeHub()
        let bytes = Data(repeating: 7, count: (2 << 20) + 123)
        hub.on("GET", "/api/v1/machines/box/file/download") { request in
            #expect(request.query["path"] == "/work/preview.mp4")
            #expect(request.query["cwd"] == "/work")
            #expect(request.query["token"] == nil)
            return bytes
        }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let destination = dir.appendingPathComponent("preview.mp4")
        try await hub.client.download("/api/v1/machines/box/file/download", query: ["path": "/work/preview.mp4", "cwd": "/work"], to: destination) { done, total in
            #expect(done == Int64(bytes.count) && total == done)
        }
        #expect(try Data(contentsOf: destination) == bytes)
    }

    @Test(.enabled(if: ProcessInfo.processInfo.environment["JARVIS_VIDEO_FIXTURE"] != nil))
    func nativeVideoPlaysSeeksAndCleansUp() async throws {
        let fixture = try #require(ProcessInfo.processInfo.environment["JARVIS_VIDEO_FIXTURE"])
        let bytes = try Data(contentsOf: URL(fileURLWithPath: fixture))
        #expect(bytes.count > 2 << 20)
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/box/file/download") { _ in bytes }
        let file = FileContent(path: "/work/preview.mp4", name: "preview.mp4", size: Int64(bytes.count), mime: "video/mp4", truncated: true, isDir: false)
        let model = VideoFileModel(ref: FileRef(machineID: "box", path: "preview.mp4", cwd: "/work"), file: file, api: hub.client)
        await model.load()
        defer { model.stop() }
        #expect(model.error == nil)
        let player = try #require(model.player)
        let local = try #require(model.localURL)
        #expect(try Data(contentsOf: local) == bytes)
        let deadline = Date.now.addingTimeInterval(8)
        while player.currentTime().seconds < 0.3, Date.now < deadline {
            try await Task.sleep(for: .milliseconds(100))
        }
        #expect(player.currentTime().seconds >= 0.3, "视频时钟没有推进")
        player.pause()
        let sought = await player.seek(to: CMTime(seconds: 8, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        #expect(sought)
        #expect(abs(player.currentTime().seconds - 8) < 0.2)
        model.stop()
        #expect(model.player == nil && model.localURL == nil)
        #expect(!FileManager.default.fileExists(atPath: local.path))
    }

    @Test func videoDownloadFailureIsShownAndLeavesNoPlayer() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/box/file/download") { _ in
            throw APIError(status: 409, message: "需要重新部署")
        }
        let file = FileContent(path: "/work/preview.mp4", name: "preview.mp4", size: 9, mime: "video/mp4", truncated: false, isDir: false)
        let model = VideoFileModel(ref: FileRef(machineID: "box", path: "preview.mp4", cwd: "/work"), file: file, api: hub.client)
        await model.load()
        #expect(model.error?.contains("需要重新部署") == true)
        #expect(model.localURL == nil && model.player == nil)
        model.stop()
        #expect(model.localURL == nil && model.player == nil)
    }
}
