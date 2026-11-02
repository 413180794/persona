#if DEBUG
import Foundation

/// 演示与截图只使用虚构文字，不启动麦克风、不连接 hub。
enum RecordingSamples {
    static func installIfAsked() -> UUID? {
        guard UserDefaults.standard.bool(forKey: "showRecordingSample") else { return nil }
        let id = UUID(uuidString: "00000000-0000-0000-0000-000000000001")!
        var entry = RecordingEntry(id: id, createdAt: Date(timeIntervalSince1970: 1_800_000_000), source: .watch,
                                   title: "项目讨论", active: false, expectedSegments: 1)
        entry.segments = [RecordingSegment(index: 0, start: 0, duration: 90, sentences: [
            RecordingSentence(start: 0, end: 12, text: "这次讨论确定三个重点：先验证核心流程，再补充测试，最后检查体验。"),
            RecordingSentence(start: 28, end: 40, text: "下周整理一次进展，把未解决的问题列出来。"),
            RecordingSentence(start: 65, end: 78, text: "需要把今天的结论变成待办，避免遗漏。"),
        ])]
        entry.marks = [28]
        try? RecordingLibrary.shared.save(entry)
        return id
    }
}
#endif
