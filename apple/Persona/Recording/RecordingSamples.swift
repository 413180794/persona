#if DEBUG
// i18n-ignore-file: DEBUG-only made-up recording for screenshots
import Foundation

/// 演示与截图只使用虚构文字，不启动麦克风；配对了 hub 时照常同步进它的知识库（UI 测试用 mock hub 验这一步）。
enum RecordingSamples {
    /// Once per launch: the page appears again after its recording was deleted, and must not bring it back.
    nonisolated(unsafe) private static var installed = false

    static func installIfAsked() -> UUID? {
        guard UserDefaults.standard.bool(forKey: "showRecordingSample"), !installed else { return nil }
        installed = true
        let id = UUID(uuidString: "00000000-0000-0000-0000-000000000001")!
        RecordingLibrary.shared.restore(id)
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
