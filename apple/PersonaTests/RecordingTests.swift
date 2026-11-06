import Foundation
import Testing
@testable import Persona

@MainActor struct RecordingTests {
    private func library() throws -> RecordingLibrary {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        return RecordingLibrary(root: root)
    }

    @Test func persistedAndRecovered() throws {
        let lib = try library()
        defer { try? FileManager.default.removeItem(at: lib.root) }
        let entry = try lib.create(source: .phone)
        try Data([1]).write(to: lib.audio(entry.id, 0))
        try Data([2]).write(to: lib.audio(entry.id, 1))
        lib.addSegment(entry.id, RecordingSegment(index: 0, start: 0, duration: 30))
        let restored = RecordingLibrary(root: lib.root)
        restored.recover(source: .phone)
        #expect(restored.entry(entry.id)?.active == false)
        #expect(restored.entry(entry.id)?.duration == 30)
        #expect(restored.entry(entry.id)?.receivedAll == true)
        #expect(FileManager.default.fileExists(atPath: lib.audio(entry.id, 0).path))
        #expect(!FileManager.default.fileExists(atPath: lib.audio(entry.id, 1).path))
    }

    @Test func endThenOutOfOrderSegmentsAndDuplicate() throws {
        let lib = try library()
        defer { try? FileManager.default.removeItem(at: lib.root) }
        var entry = RecordingEntry(id: UUID(), createdAt: .now, source: .watch)
        entry.expectedSegments = 2
        try lib.receive(RecordingTransfer(entry: entry))
        #expect(lib.entry(entry.id)?.receivedAll == false)
        let second = RecordingTransfer(entry: entry, segment: RecordingSegment(index: 1, start: 30, duration: 30))
        try lib.receive(second)
        try lib.receive(second)
        #expect(lib.entry(entry.id)?.segments.count == 1)
        try lib.receive(RecordingTransfer(entry: entry, segment: RecordingSegment(index: 0, start: 0, duration: 30)))
        #expect(lib.entry(entry.id)?.receivedAll == true)
        #expect(lib.entry(entry.id)?.duration == 60)
    }

    @Test func malformedTransferRejected() {
        let entry = RecordingEntry(id: UUID(), createdAt: .now, source: .watch)
        var transfer = RecordingTransfer(entry: entry, segment: RecordingSegment(index: -1, start: 0, duration: 30))
        #expect(!transfer.valid)
        transfer.segment = RecordingSegment(index: 0, start: .infinity, duration: 30)
        #expect(!transfer.valid)
        transfer.segment = nil; transfer.total = 0
        #expect(transfer.valid)
    }

    @Test func deletedRecordingCannotReturnFromLateWatchTransfer() throws {
        let lib = try library()
        defer { try? FileManager.default.removeItem(at: lib.root) }
        let entry = try lib.create(source: .watch)
        try lib.remove(entry.id)
        let restored = RecordingLibrary(root: lib.root)
        try restored.receive(RecordingTransfer(entry: entry, segment: RecordingSegment(index: 0, start: 0, duration: 30)))
        #expect(restored.entry(entry.id) == nil)
    }

    @Test func microphoneOwnerCannotReleaseAnotherSession() {
        let mic = RecordingMicrophone()
        let recording = UUID(), dictation = UUID()
        #expect(mic.claim(recording))
        #expect(!mic.claim(dictation))
        #expect(!mic.release(dictation))
        #expect(!mic.claim(dictation))
        #expect(mic.release(recording))
        #expect(mic.claim(dictation))
    }

    /// An owner that gives way (a voice call) and is gone without releasing must not keep the microphone for good.
    @Test func anOwnerThatGivesWayButDoesNotReleaseStillLetsGo() {
        final class Asked { var count = 0 }
        let mic = RecordingMicrophone()
        let call = UUID(), recording = UUID(), dictation = UUID()
        let asked = Asked()
        let held = mic.claim(call, givingWay: { asked.count += 1 })
        #expect(held)
        let second = mic.claim(dictation, givingWay: {})
        #expect(!second, "a call does not take it from a call")
        #expect(mic.claim(recording))
        #expect(asked.count == 1)
        #expect(!mic.release(call), "it is the recording's now")
        #expect(!mic.claim(dictation))
        #expect(mic.release(recording))
    }

    @Test func gapsAndEmptyRecordingsNotReady() {
        var entry = RecordingEntry(id: UUID(), createdAt: .now, source: .watch, active: false, expectedSegments: 2)
        entry.segments = [RecordingSegment(index: 0, start: 0, duration: 30), RecordingSegment(index: 2, start: 60, duration: 30)]
        #expect(!entry.receivedAll)
        entry.segments = []; entry.expectedSegments = 0
        #expect(!entry.receivedAll)
    }
}
