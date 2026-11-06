import Foundation
import Testing
@testable import Persona

@MainActor
struct AudioFileTests {
    @Test func audioTypes() {
        for name in ["memo.m4a", "a.MP3", "b.wav", "c.flac", "d.aiff", "e.caf"] { #expect(AudioFileType.matches(name: name, mime: nil), "\(name)") }
        #expect(AudioFileType.matches(name: "clip", mime: "audio/mpeg"))
        #expect(!AudioFileType.matches(name: "video.mp4", mime: "video/mp4"))
        #expect(!AudioFileType.matches(name: "notes.txt", mime: "text/plain"))
    }

    @Test func clockAndSpeed() {
        #expect(AudioFileType.clock(0) == "0:00")
        #expect(AudioFileType.clock(65.9) == "1:05")
        #expect(AudioFileType.clock(3723) == "1:02:03")
        #expect(AudioFileType.clock(.nan) == "0:00")
        #expect(AudioFileView.speedText(1) == "1×")
        #expect(AudioFileView.speedText(1.25) == "1.25×")
        #expect(AudioFileView.speedText(0.75) == "0.75×")
    }

    @Test func viewerPicksAudioBeforeVideo() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/box/file", json: """
            {"path":"/w/memo.m4a","name":"memo.m4a","size":9,"mime":"audio/mp4","stream":true,"revision":"r","truncated":false,"is_dir":false}
            """)
        let model = FileViewerModel(ref: FileRef(machineID: "box", path: "memo.m4a", cwd: "/w"), api: hub.client, stream: true)
        await model.load()
        #expect(model.kind == .audio)
        #expect(model.audio != nil && model.video == nil)
    }

    @Test func downloadFailureIsShown() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/box/file/download") { _ in throw APIError(status: 409, message: "需要重新部署") }
        let file = FileContent(path: "/w/a.mp3", name: "a.mp3", size: 9, truncated: false, isDir: false)
        let model = AudioFileModel(ref: FileRef(machineID: "box", path: "a.mp3", cwd: "/w"), file: file, api: hub.client)
        await model.load()
        #expect(model.error?.contains("需要重新部署") == true)
        #expect(model.player == nil && model.localURL == nil)
    }
}
