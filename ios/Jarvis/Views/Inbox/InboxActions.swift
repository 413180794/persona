import SwiftUI

// What can be done with one inbox item besides reading it (docs/inbox.md): AI 草拟回复 (a reply in the user's voice,
// edited and sent from here), 交给 AI (the item as <jarvis-context> to the assistant or an agent), 已处理.

/// The strip under the navigation bar of an opened item: the AI's summary and the two actions.
struct InboxActionBar: View {
    var item: InboxItem
    var draft: () -> Void
    var handOff: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let s = item.summary, !s.isEmpty {
                (Text(Image(systemName: "sparkles")).foregroundStyle(Theme.ai) + Text(" " + s))
                    .font(.subheadline)
                    .lineLimit(3)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack(spacing: 8) {
                action(item.agentDraft == nil ? "AI 草拟回复" : "查看草稿", symbol: "pencil.and.scribble", prominent: true, perform: draft)
                action("交给 AI", symbol: "paperplane", prominent: false, perform: handOff)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
    }

    private func action(_ title: String, symbol: String, prominent: Bool, perform: @escaping () -> Void) -> some View {
        Button(action: perform) {
            Label(title, systemImage: symbol)
                .font(.subheadline.weight(.semibold))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)
                .foregroundStyle(prominent ? Color.white : Theme.accent)
                .background(prominent ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(Theme.accent.opacity(0.12)), in: .capsule)
        }
        .buttonStyle(.plain)
    }
}

// MARK: - AI 草拟回复

/// The drafted reply, editable; 重写 asks again with an instruction; 发送 replies in the Feishu chat or by mail and
/// files the item as done.
struct InboxDraftSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let item: InboxItem
    var accounts: [MailAccount]
    @State private var text = ""
    @State private var instruction = ""
    @State private var loading = false
    @State private var sending = false
    @State private var error: String?
    @FocusState private var editing: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let a = item.agent, item.agentDraft != nil {
                    agentNote(a)
                }
                ZStack(alignment: .topLeading) {
                    TextEditor(text: $text)
                        .focused($editing)
                        .scrollContentBackground(.hidden)
                        .padding(.horizontal, 12)
                        .padding(.top, 8)
                        .disabled(loading)
                        .opacity(loading ? 0.4 : 1)
                    if loading {
                        HStack(spacing: 8) {
                            ProgressView()
                            Text("AI 正在起草…").foregroundStyle(.secondary)
                        }
                        .padding(20)
                    }
                }
                if let error {
                    Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 16).padding(.bottom, 6)
                }
                Divider()
                HStack(spacing: 8) {
                    TextField("让 AI 改：语气、要点…", text: $instruction, axis: .vertical)
                        .lineLimit(1...3)
                        .padding(.horizontal, 12).padding(.vertical, 8)
                        .background(Color.secondary.opacity(0.12), in: .rect(cornerRadius: 18))
                        .onSubmit { Task { await load() } }
                    Button { Task { await load() } } label: {
                        Label("重写", systemImage: "arrow.clockwise").labelStyle(.titleAndIcon).lineLimit(1)
                    }
                    .buttonStyle(.bordered)
                    .disabled(loading)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
            }
            .themedPage()
            .navigationTitle(item.isLark ? "回复 " + item.title : "回复邮件")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await send() } } label: {
                        if sending { ProgressView() } else { Text("发送").lineLimit(1) }
                    }
                    .disabled(sending || loading || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
            .task {
                guard text.isEmpty else { return }
                if let d = item.agentDraft { text = d } else { await load() }
            }
        }
        .presentationDetents([.medium, .large])
    }

    /// What the AI 分身 found, and a way into its conversation.
    private func agentNote(_ a: InboxItem.Agent) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if let note = a.note, !note.isEmpty {
                (Text(Image(systemName: "sparkles")).foregroundStyle(Theme.accent) + Text(" " + note))
                    .font(.footnote).foregroundStyle(.secondary).lineLimit(4)
            }
            if let key = a.sessionKey {
                Button {
                    model.assistantCurrent = key
                    model.selectedTab = .assistant
                    dismiss()
                } label: {
                    Label("查看 AI 分身的处理过程", systemImage: "list.bullet.rectangle").font(.footnote.weight(.medium)).lineLimit(1)
                }
                .buttonStyle(.borderless)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Theme.accent.opacity(0.06))
    }

    private func load() async {
        guard let api = model.api else { return }
        loading = true
        defer { loading = false }
        do {
            text = try await api.inboxDraft(item.id, instruction: instruction)
            instruction = ""
            error = nil
        } catch {
            if let shown = error.shown { self.error = shown }
        }
    }

    private func send() async {
        guard let api = model.api else { return }
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        sending = true
        defer { sending = false }
        do {
            try await api.inboxReply(item.id, text: body) // the hub sends it and files the item
            InboxStore.shared.setDone(item, true, haptic: false)
            Haptics.success()
            model.toast = Toast("已发送，标为已处理", style: .success)
            dismiss()
        } catch {
            if let shown = error.shown { self.error = shown }
        }
    }
}

// MARK: - 交给 AI

/// Hands the item to the assistant (a new conversation in 对话) or to a running agent, as a <jarvis-context> plus
/// what to do with it.
struct InboxHandOffSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let item: InboxItem
    @State private var instruction = ""
    @State private var target: SessionKey? // nil = the assistant, new conversation
    @State private var markDone = true
    @State private var sending = false
    @State private var error: String?

    private static let presets = ["总结一下，告诉我要做什么，给出建议的回复", "查清楚背景再告诉我", "直接帮我处理，回复前先问我"]

    private var agents: [Session] {
        model.allSessions.filter { !$0.assistant }
            .sorted { ($0.updatedAt ?? .distantPast) > ($1.updatedAt ?? .distantPast) }
            .prefix(8).map { $0 }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("要它做什么") {
                    TextField(Self.presets[0], text: $instruction, axis: .vertical).lineLimit(2...6)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(Self.presets, id: \.self) { p in
                                Button { instruction = p } label: {
                                    Text(p).font(.footnote).lineLimit(1)
                                        .padding(.horizontal, 10).padding(.vertical, 5)
                                        .background(Color.secondary.opacity(0.12), in: .capsule)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                Section("交给谁") {
                    Picker("交给谁", selection: $target) {
                        Label("Jarvis（新对话）", systemImage: "sparkles").tag(SessionKey?.none)
                        ForEach(agents) { s in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(s.displayTitle).lineLimit(1)
                                Text(model.machine(s.machineID)?.name ?? s.machineID).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            .tag(SessionKey?.some(s.key))
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                }
                Section {
                    Toggle("同时标为已处理", isOn: $markDone)
                }
                if let error { Text(error).foregroundStyle(Theme.danger).font(.footnote) }
            }
            .navigationTitle("交给 AI")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await send() } } label: {
                        if sending { ProgressView() } else { Text("发送").lineLimit(1) }
                    }
                    .disabled(sending)
                }
            }
        }
    }

    private func send() async {
        guard let api = model.api else { return }
        sending = true
        defer { sending = false }
        do {
            let full = try await api.inboxItem(item.id)
            let ask = instruction.trimmingCharacters(in: .whitespacesAndNewlines)
            let text = Self.message(full, ask: ask.isEmpty ? Self.presets[0] : ask)
            if let key = target {
                model.sendNow(key, text: text)
                model.openChat = key
                model.selectedTab = .chats
            } else {
                let s = try await api.startConversation(prompt: text)
                model.assistantCurrent = s.key
                model.selectedTab = .assistant
            }
            if markDone { InboxStore.shared.setDone(item, true, haptic: false) }
            Haptics.success()
            dismiss()
        } catch {
            if let shown = error.shown { self.error = shown }
        }
    }

    /// The item as context the chat shows as a chip, then the ask.
    static func message(_ it: InboxItem, ask: String) -> String {
        let title = (it.isLark ? "飞书 · " : "邮件 · ") + it.title
        var text = it.context?.isEmpty == false ? it.context! : [it.sender ?? "", it.preview].filter { !$0.isEmpty }.joined(separator: "：")
        if let l = it.lark { text += "\n\n（飞书会话 feedId \(l.feedID)，可用飞书工具读更多上下文或回复）" }
        if let m = it.mail, let uid = m.uids.last {
            text += "\n\n（邮件 account_id \(m.accountID)，mailbox \(m.mailbox)，uid \(uid)，可用 mail_read 读全文、mail_send 回复）"
        }
        let ref = ContextRef(kind: "context", title: title, url: "", text: text)
        return ref.xml + "\n\n" + ask
    }
}
