import Foundation
import Observation

/// 录音不是可清理的缓存。每条独立目录，原子写入索引；共用于手机与手表。
@Observable
final class RecordingLibrary {
    static let shared = RecordingLibrary()
    nonisolated static let directory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        .appendingPathComponent("Recordings", isDirectory: true)
    private(set) var entries: [RecordingEntry] = []
    private(set) var error: String?
    @ObservationIgnored nonisolated let root: URL

    init(root: URL = RecordingLibrary.directory) {
        self.root = root
        do {
            try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
            let dirs = try FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)
            entries = dirs.compactMap { dir in
                guard UUID(uuidString: dir.lastPathComponent) != nil,
                      let data = try? Data(contentsOf: dir.appendingPathComponent("recording.json")),
                      let entry = try? JSONDecoder().decode(RecordingEntry.self, from: data),
                      entry.id.uuidString == dir.lastPathComponent else { return nil }
                return entry
            }.sorted { $0.createdAt > $1.createdAt }
        } catch { self.error = "录音目录读不出来" }
    }

    nonisolated func folder(_ id: UUID) -> URL { root.appendingPathComponent(id.uuidString, isDirectory: true) }
    nonisolated func audio(_ id: UUID, _ index: Int) -> URL { folder(id).appendingPathComponent(RecordingSegment.fileName(index)) }
    func entry(_ id: UUID) -> RecordingEntry? { entries.first { $0.id == id } }

    @discardableResult func create(source: RecordingEntry.Source, locale: String = "zh-CN") throws -> RecordingEntry {
        let entry = RecordingEntry(id: UUID(), createdAt: .now, source: source, locale: locale)
        try save(entry)
        return entry
    }

    func save(_ entry: RecordingEntry) throws {
        try FileManager.default.createDirectory(at: folder(entry.id), withIntermediateDirectories: true)
        try JSONEncoder().encode(entry).write(to: folder(entry.id).appendingPathComponent("recording.json"), options: .atomic)
        entries.removeAll { $0.id == entry.id }
        entries.append(entry)
        entries.sort { $0.createdAt > $1.createdAt }
        error = nil
    }

    func change(_ id: UUID, _ edit: (inout RecordingEntry) -> Void) {
        guard var entry = entry(id) else { return }
        edit(&entry)
        do { try save(entry) } catch { self.error = "录音信息没有保存，请保留 App 并重试" }
    }

    func addSegment(_ id: UUID, _ segment: RecordingSegment) {
        change(id) { entry in
            guard !entry.segments.contains(where: { $0.index == segment.index }) else { return }
            entry.segments.append(segment)
            entry.segments.sort { $0.index < $1.index }
        }
    }

    /// 结束消息和音频可以任意顺序到达，重复消息不会覆盖已转写的分段。
    func receive(_ transfer: RecordingTransfer) throws {
        guard transfer.valid else { throw CocoaError(.fileReadCorruptFile) }
        var entry = entry(transfer.id) ?? RecordingEntry(id: transfer.id, createdAt: transfer.createdAt, source: .watch, locale: transfer.locale)
        entry.marks = Array(Set(entry.marks + transfer.marks)).sorted()
        if var segment = transfer.segment, !entry.segments.contains(where: { $0.index == segment.index }) {
            segment.sentences = nil; segment.delivered = false
            entry.segments.append(segment)
            entry.segments.sort { $0.index < $1.index }
        }
        if let total = transfer.total { entry.active = false; entry.expectedSegments = total }
        try save(entry)
    }

    func remove(_ id: UUID) throws {
        try FileManager.default.removeItem(at: folder(id))
        entries.removeAll { $0.id == id }
    }

    /// Hourhand 的异常退出收尾：只保留定稿分段，删掉没有尾部的半成品。
    func recover(source: RecordingEntry.Source) {
        for var entry in entries where entry.active && entry.source == source {
            let known = Set(entry.segments.map { RecordingSegment.fileName($0.index) })
            let files = (try? FileManager.default.contentsOfDirectory(at: folder(entry.id), includingPropertiesForKeys: nil)) ?? []
            for file in files where file.pathExtension == "m4a" && !known.contains(file.lastPathComponent) {
                try? FileManager.default.removeItem(at: file)
            }
            entry.active = false
            entry.expectedSegments = (entry.segments.map(\.index).max() ?? -1) + 1
            do { try save(entry) } catch { self.error = "上次录音的收尾信息没有保存" }
        }
    }
}
