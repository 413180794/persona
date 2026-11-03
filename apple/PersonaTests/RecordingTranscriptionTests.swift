import Foundation
import Testing
@testable import Jarvis

@MainActor struct RecordingTranscriptionTests {
    @Test func timestampsAndContextAreLocalOnly() {
        var entry = RecordingEntry(id: UUID(), createdAt: .now, source: .watch, title: "现场记录", active: false, expectedSegments: 1)
        entry.segments = [RecordingSegment(index: 0, start: 0, duration: 30,
            sentences: [RecordingSentence(start: 12, end: 16, text: "下周检查进度。")])]
        let ref = entry.context
        #expect(ref != nil)
        #expect(ref?.text.contains("[0:12] 下周检查进度。") == true)
        #expect(ref?.text.contains(entry.id.uuidString) == false)
        #expect(ref?.url == "")
        let parsed = ContextRef.parse(ref!.xml + "\n请整理待办")
        #expect(parsed.refs.count == 1)
        #expect(parsed.rest == "请整理待办")
    }

    @Test func incompleteOrSilentRecordingCannotAttach() {
        var entry = RecordingEntry(id: UUID(), createdAt: .now, source: .phone, active: false, expectedSegments: 2)
        entry.segments = [RecordingSegment(index: 0, start: 0, duration: 30, sentences: [])]
        #expect(entry.context == nil)
        entry.expectedSegments = 1
        #expect(entry.transcribedAll)
        #expect(entry.context == nil)
    }

    @Test func sentencesSplitOnPunctuationAndPause() {
        let raw = RecordingSentenceBuilder.build([
            (text: "第一句。", start: 0, end: 2), (text: "第二", start: 3, end: 4),
            (text: "句话", start: 4, end: 5), (text: "接下来", start: 8, end: 10),
        ])
        #expect(raw.map(\.text) == ["第一句。", "第二句话", "接下来"])
        #expect(raw.map(\.start) == [0, 3, 8])
    }

    @Test func resumesOnlyUnfinishedSegments() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: root) }
        let lib = RecordingLibrary(root: root)
        var entry = try lib.create(source: .phone)
        entry.active = false; entry.expectedSegments = 2
        entry.segments = [
            RecordingSegment(index: 0, start: 0, duration: 30, sentences: [RecordingSentence(start: 0, end: 3, text: "已保存")]),
            RecordingSegment(index: 1, start: 30, duration: 30),
        ]
        try lib.save(entry)
        let runner = RecordingTranscription(library: lib) { url, _ in
            #expect(url.lastPathComponent == "seg-001.m4a")
            return [RecordingSentence(start: 2, end: 4, text: "继续")]
        }
        runner.enqueue(entry.id)
        for _ in 0..<100 where lib.entry(entry.id)?.transcribedAll != true { try await Task.sleep(for: .milliseconds(10)) }
        #expect(lib.entry(entry.id)?.sentences.map(\.text) == ["已保存", "继续"])
        #expect(lib.entry(entry.id)?.sentences.last?.start == 32)
        #expect(RecordingLibrary(root: root).entry(entry.id)?.transcribedAll == true)
    }
}
