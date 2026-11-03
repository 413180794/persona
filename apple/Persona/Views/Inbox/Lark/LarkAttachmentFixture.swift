#if DEBUG
import SwiftUI

/// Mock-only drafts exercise the actual preview, queue, transport and retry paths without touching an account.
enum LarkAttachmentFixture {
    private static var used = false
    static func load(config: ServerConfig?) async -> [LarkAttachment] {
        guard !used, let config, ["127.0.0.1", "localhost"].contains(config.url.host ?? ""), config.token == "mock",
              let kind = UserDefaults.standard.string(forKey: "showLarkAttachmentDraft") else { return [] }
        used = true
        do {
            if kind == "photo", let image = UIImage(named: "app-icon-default"), let data = image.pngData() {
                return [try await .stagePhoto(data)]
            }
            let dir = URL.temporaryDirectory.appending(path: UUID().uuidString)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            defer { try? FileManager.default.removeItem(at: dir) }
            let file = dir.appending(path: kind == "fail" ? "retry-once.txt" : "演示文档.txt")
            try Data("附件预览与发送测试，仅使用模拟数据。".utf8).write(to: file)
            return [try await .stageFile(file)]
        } catch { return [] }
    }
}
#endif
