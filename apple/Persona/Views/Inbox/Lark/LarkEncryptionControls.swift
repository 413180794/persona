import SwiftUI

struct LarkEncryptionModeBar: View {
    @Binding var draft: LarkEncryptedDraft?
    /// [removed] words (#882), offered while the account's switch is on.
    @Binding var climbing: Bool
    var canEncrypt: Bool
    var canClimb = false
    var checking = false
    var selectEncrypted: () -> Void
    var pickCover: () -> Void

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Menu {
                Button { draft = nil; climbing = false } label: { Text("普通消息").lineLimit(1) }
                    .accessibilityIdentifier("lark.encryption.plain")
                Button(action: selectEncrypted) { Text("加密消息").lineLimit(1) }
                    .disabled(!canEncrypt)
                    .accessibilityIdentifier("lark.encryption.encrypted")
                if canClimb || climbing {
                    Button { draft = nil; climbing = true } label: { Text("").lineLimit(1) }
                        .disabled(!canClimb)
                        .accessibilityIdentifier("lark.encryption.braveClimb")
                }
            } label: {
                HStack(spacing: Theme.Space.xs) {
                    Label(mode.title, systemImage: mode.icon)
                    Image(systemName: Theme.Icon.expand)
                }
                .font(Theme.Font.label).lineLimit(1)
            }
            .accessibilityIdentifier("lark.encryption.mode")
            if let draft {
                Button(action: pickCover) {
                    Text(draft.cover.isEmpty ? "选择对外展示" : "对外展示：\(draft.cover)")
                        .font(Theme.Font.label).lineLimit(1).truncationMode(.tail)
                }
                .accessibilityIdentifier("lark.encryption.cover")
                .disabled(checking)
            }
            if climbing && draft == nil {
                Text(canClimb ? "可还原的编码，不是加密" : "")
                    .font(Theme.Font.label).lineLimit(1).truncationMode(.tail)
            }
            if checking { ProgressView().controlSize(.small).accessibilityLabel("正在检查加密服务") }
            Spacer(minLength: 0)
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.page)
        .padding(.top, Theme.Space.s)
    }

    private var mode: (title: String, icon: String) {
        if draft != nil { ("加密消息", Theme.Icon.locked) }
        else if climbing { ("", Theme.Icon.braveClimb) }
        else { ("普通消息", Theme.Icon.message) }
    }
}

struct LarkEncryptionCoverPicker: View {
    @Environment(\.dismiss) private var dismiss
    var api: APIClient
    var account: String
    var feed: String
    var select: (String) -> Void
    @State private var lines: [String] = []
    @State private var loading = true
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    if loading { ProgressView("加载历史内容…") }
                    else if let problem {
                        Text(problem).font(Theme.Font.secondary)
                        Button("重试") { Task { await load() } }.lineLimit(1)
                    } else if lines.isEmpty {
                        Text("暂无可用的历史内容").foregroundStyle(.secondary)
                    } else {
                        ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                            Button { select(line); dismiss() } label: {
                                Text(line).font(Theme.Font.body).lineLimit(1)
                            }
                            .accessibilityIdentifier("lark.encryption.coverChoice")
                        }
                    }
                } footer: {
                    Text("选一段历史消息，作为无法解密时看到的内容。")
                }
            }
            .themedGroupedPage()
            .navigationTitle("对外展示")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() }.lineLimit(1) } }
            .task { await load() }
        }
    }
    private func load() async {
        loading = true; problem = nil
        defer { loading = false }
        do { lines = try await api.larkEncryptionCovers(account: account, feed: feed) }
        catch { if !error.isCancellation { problem = error.shown ?? "暂时无法加载，请重试" } }
    }
}

struct LarkEncryptionUnlockSheet: View {
    @Environment(\.dismiss) private var dismiss
    var api: APIClient
    var account: String
    var unified: Bool
    var unlocked: () -> Void
    @State private var password = ""
    @State private var busy = false
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField(unified ? "lark-connector 登录密码" : "密钥库密码", text: $password)
                        .textContentType(.password)
                        .accessibilityIdentifier("lark.encryption.password")
                    if let problem { Text(problem).font(Theme.Font.secondary).foregroundStyle(Theme.danger) }
                    Button {
                        let input = password
                        password = ""
                        busy = true
                        problem = nil
                        Task {
                            defer { busy = false }
                            do {
                                let result = try await api.larkUnlock(account: account, password: input)
                                guard result.accountId == account, !result.locked else { throw LarkOutboxIdentity.Failure.changed }
                                dismiss()
                                unlocked()
                            } catch { problem = error.shown ?? "解锁失败，请重试" }
                        }
                    } label: {
                        HStack(spacing: Theme.Space.s) {
                            if busy { ProgressView().controlSize(.small) }
                            Text(busy ? "正在解锁…" : "解锁并发送").lineLimit(1)
                        }
                    }
                    .disabled(busy || password.isEmpty)
                    .accessibilityIdentifier("lark.encryption.unlock")
                } footer: { Text("密码仅用于本次解锁，不会保存。") }
            }
            .themedGroupedPage()
            .navigationTitle("解锁加密消息")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("取消") { password = ""; dismiss() }.lineLimit(1).disabled(busy) } }
            .interactiveDismissDisabled(busy)
            .onDisappear { password = "" }
        }
    }
}
