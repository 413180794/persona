#if DEBUG
import SwiftUI

/// Mock-only drafts exercise the actual preview, queue, transport and retry paths without touching an account.
enum LarkAttachmentFixture {
    private static var used = false
    private static var brokeDraft = false

    static func breakDraft(config: ServerConfig, identity: LarkOutboxIdentity, feed: String) async {
        guard !brokeDraft, ["127.0.0.1", "localhost"].contains(config.url.host ?? ""), config.token == "mock",
              let kind = UserDefaults.standard.string(forKey: "showLarkAttachmentDraft"), ["corrupt", "legacy"].contains(kind) else { return }
        brokeDraft = true
        let store = LarkOutboxStore(identity: identity, feed: feed)
        do {
            _ = try await store.save(.init())
            if kind == "legacy" {
                let folder = URL.temporaryDirectory.appending(path: UUID().uuidString)
                try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
                defer { try? FileManager.default.removeItem(at: folder) }
                let file = folder.appending(path: "legacy-draft.txt")
                try Data("Legacy attachment fixture".utf8).write(to: file)
                let attachment = try await LarkAttachment.stageFile(file)
                let legacy = LarkOutboxStore(identity: identity, feed: feed, legacyConfig: config)
                _ = try await legacy.save(.init(draft: .init(text: "Legacy draft"), attachments: [attachment]))
            } else {
                try Data("unreadable mock draft".utf8).write(to: await store.directory.appending(path: "state.json"))
            }
        } catch { /* The ordinary UI still reports a storage error. */ }
    }

    static func load(config: ServerConfig?) async -> [LarkAttachment] {
        guard !used, let config, ["127.0.0.1", "localhost"].contains(config.url.host ?? ""), config.token == "mock",
              let kind = UserDefaults.standard.string(forKey: "showLarkAttachmentDraft") else { return [] }
        used = true
        guard !["corrupt", "legacy"].contains(kind) else { return [] }
        do {
            if kind == "photo", let image = UIImage(named: "app-icon-default"), let data = image.pngData() {
                return [try await .stagePhoto(data)]
            }
            let dir = URL.temporaryDirectory.appending(path: UUID().uuidString)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            defer { try? FileManager.default.removeItem(at: dir) }
            let name = kind == "fail" ? "retry-once.txt" : kind == "receipt" ? "lost-receipt.txt" : kind == "idless" ? "idless-receipt.txt" : "演示文档.txt"
            let file = dir.appending(path: name)
            try Data("附件预览与发送测试，仅使用模拟数据。".utf8).write(to: file)
            return [try await .stageFile(file)]
        } catch { return [] }
    }
}
#endif
