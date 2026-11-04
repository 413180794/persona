import Foundation
import SwiftUI

nonisolated struct LarkMessageDisplay: Codable, Equatable, Sendable {
    var accountID: String
    var antiRecall: Bool
    var showEditHistory: Bool

    enum CodingKeys: String, CodingKey {
        case accountID = "account_id", antiRecall = "anti_recall", showEditHistory = "show_edit_history"
    }
    static let hidden = Self(accountID: "", antiRecall: false, showEditHistory: false)

    func showsBody(of message: LarkMessage) -> Bool {
        !message.recalled || (antiRecall && message.retained == true && !message.isSystem && !(message.parts ?? []).isEmpty)
    }
    func showsHistory(of message: LarkMessage) -> Bool {
        showEditHistory && showsBody(of: message) && !(message.editHistory ?? []).isEmpty
    }

    func forwardedPreview(_ message: JSONValue) -> String {
        if message["isRecalled"]?.bool == true && !(antiRecall && message["hasRetainedRecallContent"]?.bool == true) {
            return "消息已撤回"
        }
        let text: String
        if let raw = message["olk"], raw != .null {
            let encryption = (try? JSON.encoder.encode(raw)).flatMap { try? JSON.decoder.decode(LarkEncryption.self, from: $0) }
            text = encryption?.summary ?? "加密消息"
        } else { text = message["textPreview"]?.string ?? message["text"]?.string ?? "" }
        return text + (message["isRecalled"]?.bool == true ? " · 已撤回" : "")
    }
}

nonisolated struct LarkEditVersion: Codable, Hashable, Sendable {
    var editedAt: String
    var text: String
    var parts: [LarkPart]?
    var olk: LarkEncryption?

    enum CodingKeys: String, CodingKey { case editedAt = "edited_at", text, parts, olk }
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(editedAt, forKey: .editedAt)
        try c.encode(olk?.cached.summary ?? text, forKey: .text)
        try c.encodeIfPresent(olk?.publicParts ?? parts, forKey: .parts)
        try c.encodeIfPresent(olk?.cached, forKey: .olk)
    }
}

extension EnvironmentValues {
    @Entry var larkDisplay: LarkMessageDisplay = .hidden
}

extension APIClient {
    func larkMessageDisplay() async throws -> LarkMessageDisplay { try await get("/api/v1/lark/message-display") }
    func larkMessageDisplayChange(_ snapshot: LarkMessageDisplay, key: String, value: Bool, expected: Bool) async throws -> LarkMessageDisplay {
        struct Body: Encodable {
            var account_id: String
            var key: String
            var value: Bool
            var expected: Bool
        }
        return try await post("/api/v1/lark/message-display", body: Body(account_id: snapshot.accountID, key: key, value: value, expected: expected))
    }
}

/// Owned by one HubLink. Unknown or unverifiable account preferences always hide retained content.
@Observable final class LarkMessageDisplayModel {
    private(set) var snapshot: LarkMessageDisplay?
    private(set) var loading = false
    private(set) var saving = false
    private(set) var problem: String?
    @ObservationIgnored private var pendingRefresh: APIClient?
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    var policy: LarkMessageDisplay { snapshot ?? .hidden }

    func refresh(using api: APIClient) async {
        pendingRefresh = api
        guard !saving else { return }
        if let refreshTask { await refreshTask.value; return }
        loading = true
        // The model owns this read: leaving one view must not cancel a queued live update.
        let task = Task { await readPending() }
        refreshTask = task
        await task.value
    }

    private func readPending() async {
        defer { loading = false; refreshTask = nil }
        while let api = pendingRefresh {
            pendingRefresh = nil
            do {
                let next = try await api.larkMessageDisplay()
                // A change arrived after this read began. Only publish the subsequent read.
                guard pendingRefresh == nil else { continue }
                guard !next.accountID.isEmpty else { throw LarkOutboxIdentity.Failure.unavailable }
                snapshot = next; problem = nil
            } catch {
                guard pendingRefresh == nil else { continue }
                snapshot = nil
                if LarkLoadProblem.isNotLoggedIn(error) {
                    problem = LarkLoadProblem.of(error).message
                } else {
                    problem = (error as? APIError)?.status == 404 ? "请先更新 Hub 和 lark-connector，再使用消息显示设置。" : "暂时无法读取设置，请连接后重试。"
                }
            }
        }
    }

    func set(_ key: String, value: Bool, using api: APIClient) async {
        guard let current = snapshot, !saving, !loading else { return }
        saving = true
        let expected = key == "anti_recall" ? current.antiRecall : current.showEditHistory
        do {
            let next = try await api.larkMessageDisplayChange(current, key: key, value: value, expected: expected)
            guard !next.accountID.isEmpty else { throw LarkOutboxIdentity.Failure.unavailable }
            if pendingRefresh == nil { snapshot = next; problem = nil }
        } catch {
            snapshot = nil
            if LarkLoadProblem.isNotLoggedIn(error) {
                problem = LarkLoadProblem.of(error).message
            } else {
                switch (error as? APIError)?.status {
                case 409: problem = "设置或账号已变化，请重新读取后再修改。"
                case 403: problem = "当前连接没有修改权限。"
                case 404: problem = "请先更新 lark-connector，再修改这些设置。"
                default: problem = "暂时无法确认保存结果，请重新读取设置。"
                }
            }
        }
        saving = false
        if let api = pendingRefresh { await refresh(using: api) }
    }
}

struct LarkMessageDisplaySettingsView: View {
    @Environment(AppModel.self) private var app
    var body: some View {
        Group {
            if let link = app.hub { LarkMessageDisplaySettingsContent(link: link).id(ObjectIdentifier(link)) }
            else { ContentUnavailableView("请先连接 Hub", systemImage: Theme.Icon.connectors) }
        }
        .navigationTitle("风灵月影").navigationBarTitleDisplayMode(.inline)
    }
}

private struct LarkMessageDisplaySettingsContent: View {
    let link: HubLink
    private var state: LarkMessageDisplayModel { link.larkDisplay }
    var body: some View {
        List {
            Section {
                setting("防撤回", key: "anti_recall", value: state.policy.antiRecall)
            } footer: { Text("查看 lark-connector 已留存的撤回内容，保留「已撤回」标记。未留存的内容无法恢复。") }
            Section {
                setting("编辑历史", key: "show_edit_history", value: state.policy.showEditHistory)
            } footer: { Text("点击消息下方的「已编辑」，查看已留存的旧版本。") }
            Section {
                if state.loading || state.saving { ProgressView(state.saving ? "正在保存…" : "正在读取…") }
                if let problem = state.problem {
                    Text(problem).font(Theme.Font.label).foregroundStyle(.secondary)
                    Button("重新读取") { Task { await state.refresh(using: link.api) } }.lineLimit(1)
                }
            } footer: { Text("与 lark-connector 网页共用当前飞书账号的设置。") }
        }
        .themedPage()
        .task { await state.refresh(using: link.api) }
    }
    private func setting(_ title: String, key: String, value: Bool) -> some View {
        Toggle(title, isOn: Binding(get: { value }, set: { next in Task { await state.set(key, value: next, using: link.api) } }))
            .disabled(state.snapshot == nil || state.loading || state.saving)
            .accessibilityIdentifier("lark.display." + key)
    }
}

struct LarkEditHistoryView: View {
    var message: LarkMessage
    @Environment(\.dismiss) private var dismiss
    @Environment(\.larkDisplay) private var display
    var body: some View {
        NavigationStack {
            List {
                if display.showsHistory(of: message) {
                    ForEach(Array((message.editHistory ?? []).enumerated()), id: \.offset) { _, version in
                        VStack(alignment: .leading, spacing: Theme.Space.s) {
                            if let at = JSON.parseDate(version.editedAt) {
                                Text(at.formatted(date: .abbreviated, time: .shortened)).font(Theme.Font.caption).foregroundStyle(.secondary)
                            }
                            if let encryption = version.olk { LarkEncryptedContent(encryption: encryption) }
                            else if let parts = version.parts, !parts.isEmpty { LarkPartsView(parts: parts) }
                            else { Text(version.text).font(Theme.Font.body) }
                        }
                    }
                } else { Text("编辑历史已隐藏").foregroundStyle(.secondary) }
            }
            .themedPage().navigationTitle("编辑历史").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() }.lineLimit(1) } }
        }
    }
}
