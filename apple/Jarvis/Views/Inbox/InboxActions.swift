import SwiftUI

// What can be done with one inbox item besides reading it (docs/inbox.md): AI 草拟回复 (a reply in the user's voice,
// edited and sent from here), 交给 AI (the item as <jarvis-context> to the assistant or an agent), 已处理.

/// The strip under the navigation bar of an opened item: the AI's summary and the two actions.
struct InboxActionBar: View {
    var item: InboxItem
    var draft: () -> Void
    var handOff: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let s = item.summary, !s.isEmpty {
                (Text(Image(systemName: Theme.Icon.ai)).foregroundStyle(Theme.ai) + Text(" " + s))
                    .font(Theme.Font.secondary)
                    .lineLimit(3)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            HStack(spacing: Theme.Space.s) {
                action(item.agentDraft == nil ? "AI 草拟回复" : "查看草稿", symbol: "pencil.and.scribble", prominent: true, perform: draft)
                action("交给 AI", symbol: "paperplane", prominent: false, perform: handOff)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        .background(.bar)
        .overlay(alignment: .bottom) { Divider() }
    }

    private func action(_ title: String, symbol: String, prominent: Bool, perform: @escaping () -> Void) -> some View {
        Button(action: perform) {
            Label(title, systemImage: symbol)
                .font(Theme.Font.secondaryStrong)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Space.s)
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
    @State private var error: String?
    @FocusState private var editing: Bool
    /// The AI 分身's proposed status moves the user keeps (all of them to start with).
    @State private var kept: Set<InboxItem.DocChange>?
    private var proposed: [InboxItem.DocChange] { item.agentDraft == nil ? [] : item.agent?.changes ?? [] }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if let a = item.agent, item.agentDraft != nil {
                    agentNote(a)
                }
                if !proposed.isEmpty { changesList }
                ZStack(alignment: .topLeading) {
                    TextEditor(text: $text)
                        .focused($editing)
                        .scrollContentBackground(.hidden)
                        .padding(.horizontal, Theme.Space.m)
                        .padding(.top, Theme.Space.s)
                        .disabled(loading)
                        .opacity(loading ? 0.4 : 1)
                    if loading {
                        HStack(spacing: Theme.Space.s) {
                            ProgressView()
                            Text("AI 正在起草…").foregroundStyle(.secondary)
                        }
                        .padding(Theme.Space.xl)
                    }
                }
                if let error {
                    Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, Theme.Space.l).padding(.bottom, Theme.Space.sm)
                }
                Divider()
                HStack(spacing: Theme.Space.s) {
                    TextField("让 AI 改：语气、要点…", text: $instruction, axis: .vertical)
                        .lineLimit(1...3)
                        .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.s)
                        .background(Color.secondary.opacity(0.12), in: .rect(cornerRadius: Theme.Radius.card))
                        .onSubmit { Task { await load() } }
                    Button { Task { await load() } } label: {
                        Label("重写", systemImage: Theme.Icon.retry).labelStyle(.titleAndIcon).lineLimit(1)
                    }
                    .buttonStyle(.bordered)
                    .disabled(loading)
                }
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.m)
            }
            .themedPage()
            .navigationTitle(item.doc != nil ? "评论 " + item.title : item.isLark ? "回复 " + item.title : "回复邮件")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await send() } } label: {
                        Text("发送").lineLimit(1)
                    }
                    .disabled(loading || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && keptChanges.isEmpty)
                }
            }
            .task {
                guard text.isEmpty else { return }
                if let u = InboxStore.shared.unsentReplies[item.id] { text = u } else if let d = item.agentDraft { text = d } else { await load() }
            }
        }
        .presentationDetents([.medium, .large])
    }

    /// What the AI 分身 found, and a way into its conversation.
    private func agentNote(_ a: InboxItem.Agent) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            if let note = a.note, !note.isEmpty {
                (Text(Image(systemName: Theme.Icon.ai)).foregroundStyle(Theme.accent) + Text(" " + note))
                    .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(4)
            }
            if let key = a.sessionKey {
                Button {
                    model.assistantCurrent = key
                    model.selectedTab = .assistant
                    dismiss()
                } label: {
                    Label("查看 AI 分身的处理过程", systemImage: Theme.Icon.list).font(Theme.Font.captionMedium).lineLimit(1)
                }
                .buttonStyle(.borderless)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.m)
        .background(Theme.accent.opacity(0.06))
    }

    private var keptChanges: [InboxItem.DocChange] { proposed.filter { (kept ?? Set(proposed)).contains($0) } }

    /// The status moves the AI 分身 proposes, each one a toggle; the kept ones are made when the reply is sent.
    private var changesList: some View {
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            Text("发送时一起改").font(Theme.Font.labelStrong).foregroundStyle(.secondary)
            ForEach(proposed, id: \.self) { c in
                let on = (kept ?? Set(proposed)).contains(c)
                Button {
                    var k = kept ?? Set(proposed)
                    if on { k.remove(c) } else { k.insert(c) }
                    kept = k
                } label: {
                    Label(DocChangeText.line(c, in: item.doc), systemImage: on ? Theme.Icon.successFilled : Theme.Icon.unchecked)
                        .font(Theme.Font.caption).lineLimit(1)
                        .foregroundStyle(on ? Color.primary : Color.secondary)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.s)
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

    /// Closes at once: the store files the item and sends in the background (a failure puts it back, with this text).
    private func send() async {
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let it = item, changes = keptChanges
        Task { await InboxStore.shared.send(it, text: body, changes: changes) }
        dismiss()
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
                        HStack(spacing: Theme.Space.s) {
                            ForEach(Self.presets, id: \.self) { p in
                                Button { instruction = p } label: {
                                    Text(p).font(Theme.Font.caption).lineLimit(1)
                                        .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.sm)
                                        .background(Color.secondary.opacity(0.12), in: .capsule)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                Section("交给谁") {
                    Picker("交给谁", selection: $target) {
                        Label("Jarvis（新对话）", systemImage: Theme.Icon.ai).tag(SessionKey?.none)
                        ForEach(agents) { s in
                            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                                Text(s.displayTitle).lineLimit(1)
                                Text(model.machine(s.machineID)?.name ?? s.machineID).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
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
                if let error { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) }
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
