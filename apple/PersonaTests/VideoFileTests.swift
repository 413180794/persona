import AVFoundation
import Foundation
import Testing
@testable import Persona

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

    @Test(.enabled(if: ProcessInfo.processInfo.environment["PERSONA_VIDEO_FIXTURE"] != nil))
    func nativeVideoPlaysSeeksAndCleansUp() async throws {
        let fixture = try #require(ProcessInfo.processInfo.environment["PERSONA_VIDEO_FIXTURE"])
        let bytes = try Data(contentsOf: URL(fileURLWithPath: fixture))
        #expect(bytes.count > 2 << 20)
        let hub = FakeHub()
        let reads = VideoReadLog()
        let source = VideoFileSource(read: { range, revision in
            #expect(revision == nil || revision == "\"fixture\"")
            if revision != nil { try await Task.sleep(for: .milliseconds(40)) }
            reads.append(range)
            let end = min(range.upperBound, Int64(bytes.count))
            return VideoFileRange(range: range.lowerBound..<end, total: Int64(bytes.count), revision: "\"fixture\"",
                                  mime: "video/mp4", data: bytes.subdata(in: Int(range.lowerBound)..<Int(end)))
        })
        let file = FileContent(path: "/work/preview.mp4", name: "preview.mp4", size: Int64(bytes.count), mime: "video/mp4", truncated: true, isDir: false)
        let model = VideoFileModel(ref: FileRef(machineID: "box", path: "preview.mp4", cwd: "/work"), file: file, api: hub.client, source: source)
        await model.load()
        defer { model.stop() }
        #expect(model.error == nil)
        let player = try #require(model.player)
        let local = try #require(model.cacheURL)
        #expect(player.currentItem?.asset is AVURLAsset)
        let asset = try #require(player.currentItem?.asset as? AVURLAsset)
        #expect(asset.url.scheme == "persona-video")
        #expect(asset.url.query == nil && !asset.url.absoluteString.contains("fake.invalid"))
        let deadline = Date.now.addingTimeInterval(8)
        while player.currentTime().seconds < 0.3, Date.now < deadline {
            try await Task.sleep(for: .milliseconds(100))
        }
        #expect(player.currentTime().seconds >= 0.3, "视频时钟没有推进")
        #expect(model.received < Int64(bytes.count), "首播前已经下载了完整视频")
        let before = reads.ranges
        player.pause()
        let duration = try await asset.load(.duration).seconds
        let target = duration > 60 ? 90.0 : 8.0
        let sought = await player.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        #expect(sought)
        #expect(abs(player.currentTime().seconds - target) < 0.2)
        player.play()
        let resumedDeadline = Date.now.addingTimeInterval(8)
        while player.currentTime().seconds < target + 0.3, Date.now < resumedDeadline {
            try await Task.sleep(for: .milliseconds(100))
        }
        #expect(player.currentTime().seconds >= target + 0.3, "seek 后视频时钟没有继续推进")
        let after = Array(reads.ranges.dropFirst(before.count))
        if let offset = ProcessInfo.processInfo.environment["PERSONA_VIDEO_SEEK_BYTE"].flatMap(Int64.init) {
            #expect(after.contains { $0.contains(offset) }, "seek 没有读取目标关键帧的字节区间：\(after)")
        }
        if duration > 60 {
            #expect(after.contains { $0.lowerBound > Int64(bytes.count) / 2 })
            #expect(model.received < Int64(bytes.count) / 2, "拖动到后段时不应该先下载前半段")
        }
        model.stop()
        #expect(model.player == nil && model.cacheURL == nil)
        #expect(!FileManager.default.fileExists(atPath: local.path))
        let closedReads = reads.ranges.count
        try await Task.sleep(for: .milliseconds(200))
        #expect(reads.ranges.count == closedReads, "关闭后仍在读取视频")
        await model.load()
        #expect(model.player != nil && model.error == nil, "同一个 model 重开没有恢复播放器")
        let reopenedCache = try #require(model.cacheURL)
        #expect(reopenedCache != local)
        model.stop()
        #expect(!FileManager.default.fileExists(atPath: reopenedCache.path))
    }

    @Test func videoDownloadFailureIsShownAndLeavesNoPlayer() async throws {
        let hub = FakeHub()
        let source = VideoFileSource(read: { _, _ in
            throw APIError(status: 409, message: "需要重新部署")
        })
        let file = FileContent(path: "/work/preview.mp4", name: "preview.mp4", size: 9, mime: "video/mp4", truncated: false, isDir: false)
        let model = VideoFileModel(ref: FileRef(machineID: "box", path: "preview.mp4", cwd: "/work"), file: file, api: hub.client, source: source)
        await model.load()
        #expect(model.error?.contains("需要重新部署") == true)
        #expect(model.cacheURL == nil && model.player == nil)
        model.stop()
        #expect(model.cacheURL == nil && model.player == nil)
    }
}
