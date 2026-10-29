import SwiftUI

@MainActor private final class InboxAccountState: ObservableObject {
    @Published var accounts = StateCache.load("mail-accounts", as: [MailAccount].self) ?? []
}

// MARK: - 收件箱

/// The unified inbox (docs/inbox.md): Feishu messages and mail in one list, one row per thing (the new messages in a
/// chat, a mail thread), newest first. No per-chat browsing: what is here is what came in. Filters narrow it by source
/// (飞书, one mail account) and kind (私聊, @我, 群聊); a row swipes away as 已处理, and the done ones stay at the bottom
/// for a few days in case of a slip.
struct InboxView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("inbox.source") private var source = "" // "" all, "feishu", or a mail account id
    @AppStorage("inbox.kind") private var kind = ""     // "" all, agent, dm, mention, group, mail
    @State private var query = ""
    @StateObject private var accountState = InboxAccountState()
    private var accounts: [MailAccount] {
        get { accountState.accounts }
        nonmutating set { accountState.accounts = newValue }
    }
    /// Sections shown open: tiers (action, fyi, noise) and "done". 已折叠 and 已处理 start closed.
    @State private var expanded: Set<String> = ["action", "fyi"]
    @State private var showAccounts = false
    @State private var composing: ComposeDraft?
    @State private var editingDraft: InboxItem? // 修改 on the AI 分身's draft
    @State private var path = NavigationPath()
    private var store: InboxStore { .shared }

    private func matches(_ it: InboxItem) -> Bool {
        if !source.isEmpty && it.account != source { return false }
        if !kind.isEmpty && !(it.kind == kind || kind == "group" && it.kind == "bot") { return false }
        if query.isEmpty { return true }
        return [it.title, it.sender ?? "", it.preview, it.summary ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
    }

    private var openItems: [InboxItem] { store.items.filter { !$0.isDone && matches($0) } }

    /// Agents (and the assistant) waiting for an answer, an approval or a plan review: they need the user too, so they
    /// head 需要你. Opening one goes to its chat; the Agents tab stays as it is.
    private var waitingAgents: [Interaction] {
        guard source.isEmpty, kind.isEmpty || kind == "agent" else { return [] }
        return model.pendingInteractions.filter { ix in
            !model.isMuted(ix.sessionKey) && (query.isEmpty || [ix.sessionTitle ?? "", ix.summary ?? "", ix.title].contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    var body: some View {
        #if PERF_SIGNPOST
        let _ = PerfTrace.event("InboxView.body")
        #endif
        NavigationStack(path: $path) {
            list
                .navigationTitle("收件箱")
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $query, prompt: "搜索标题、发件人、内容")
                .toolbar {
                    ToolbarItem(placement: .principal) { ConnectionTitle(title: "收件箱") }
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Button { composing = ComposeDraft(accountID: accounts.first?.id ?? "") } label: { Label("写邮件", systemImage: "square.and.pencil") }
                                .disabled(accounts.isEmpty)
                            Button { showAccounts = true } label: { Label("管理邮箱", systemImage: "envelope.badge") }
                        } label: { Image(systemName: "ellipsis.circle") }
                        .accessibilityLabel("更多")
                    }
                }
                .navigationDestination(for: InboxItem.self) { InboxDetailView(item: $0, accounts: accounts) }
                .feishuRoutes()
                .sheet(isPresented: $showAccounts, onDismiss: { Task { await loadAccounts() } }) { MailAccountsView() }
                .sheet(item: $composing) { d in ComposeView(draft: d, accounts: accounts) }
                .sheet(item: $editingDraft) { InboxDraftSheet(item: $0, accounts: accounts) }
                #if DEBUG
                // `-addMail YES`: open 管理邮箱 › 添加邮箱 for screenshots.
                .onAppear { if UserDefaults.standard.bool(forKey: "addMail") { showAccounts = true } }
                #endif
                .task {
                    store.reload()
                    await loadAccounts()
                    #if DEBUG
                    // `-openInboxFirst YES`: open the first open item (detail screenshots).
                    if UserDefaults.standard.bool(forKey: "openInboxFirst") {
                        for _ in 0..<20 where store.open.isEmpty { try? await Task.sleep(for: .milliseconds(500)) }
                        if let first = openItems.first { path.append(first) }
                    }
                    #endif
                }
        }
        .environment(\.feishuOpen) { path.append($0) }
        .feishuHandlesLinks { path.append($0) }
    }

    private var list: some View {
        let partition = Dictionary(grouping: store.items.filter(matches), by: \.isDone)
        let openItems = partition[false] ?? []
        let byTier = Dictionary(grouping: openItems, by: \.triage)
        let doneItems = partition[true] ?? []
        let shownDone = expanded.contains("done")
            ? doneItems.sorted { ($0.doneAt ?? .distantPast) > ($1.doneAt ?? .distantPast) } : []
        let waitingAgents = waitingAgents
        return List {
            filters
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 8, trailing: 0))
                .listRowSeparator(.hidden)
                .themedRow()
            if let error = store.error, store.items.isEmpty {
                Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                    .themedRow()
            }
            if openItems.isEmpty && waitingAgents.isEmpty && !store.loading {
                EmptyStateView(symbol: "checkmark.seal", title: query.isEmpty ? "都处理完了" : "没有匹配的",
                               message: query.isEmpty ? "新的飞书消息和邮件会出现在这里。" : "换个关键词试试。")
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
            // The AI's triage: 需要你 (and what it has not looked at yet), 知悉即可, 已折叠 (noise).
            ForEach(InboxItem.Tier.allCases, id: \.self) { tier in
                let items = kind == "agent" ? [] : byTier[tier] ?? []
                let agents = tier == .action ? waitingAgents : []
                if !items.isEmpty || !agents.isEmpty {
                    Section {
                        if expanded.contains(tier.rawValue) {
                            ForEach(agents) { agentRow($0) }
                            ForEach(items) { row($0) }
                        }
                    } header: {
                        sectionHeader(tier.title, count: items.count + agents.count, key: tier.rawValue,
                                      clear: tier == .action ? nil : (tier == .fyi ? "全部看过" : "全部清除", { store.setDone(items, true) }))
                    }
                }
            }
            if !doneItems.isEmpty {
                Section {
                    if expanded.contains("done") { ForEach(shownDone) { row($0) } }
                } header: {
                    sectionHeader("已处理", count: doneItems.count, key: "done", clear: nil)
                }
            }
        }
        .listStyle(.plain)
        .themedPage()
        .refreshable { await store.refresh() }
        .overlay { if store.loading && store.items.isEmpty { ProgressView() } }
    }

    /// A section title that opens / closes the section, with an optional action on the right (List headers take no
    /// context menu, so it is a visible button).
    private func sectionHeader(_ title: String, count: Int, key: String, clear: (String, () -> Void)?) -> some View {
        HStack(spacing: 6) {
            Button {
                withAnimation(.snappy) { if expanded.contains(key) { expanded.remove(key) } else { expanded.insert(key) } }
            } label: {
                HStack(spacing: 6) {
                    Text("\(title) · \(count)").font(.subheadline.weight(.semibold)).foregroundStyle(key == "action" ? Color.primary : .secondary)
                    Image(systemName: "chevron.right").font(.caption2.weight(.bold)).foregroundStyle(.secondary)
                        .rotationEffect(.degrees(expanded.contains(key) ? 90 : 0))
                    Spacer(minLength: 0)
                }
                .lineLimit(1)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if let clear {
                Button(clear.0) { withAnimation(.snappy) { clear.1() } }
                    .font(.subheadline)
                    .lineLimit(1)
                    .buttonStyle(.borderless)
            }
        }
        .textCase(nil)
        .padding(.vertical, 2)
    }

    private func agentRow(_ ix: Interaction) -> some View {
        Button { open(ix) } label: {
            InboxAgentRow(interaction: ix, machine: model.machine(ix.machineID), machineName: model.machine(ix.machineID)?.name ?? ix.machineID)
        }
        .buttonStyle(.plain)
        .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
        .themedRow()
        .alignmentGuide(.listRowSeparatorLeading) { _ in InboxRow.textColumn }
    }

    /// The agent's chat (the assistant's in 对话), where the question / plan / approval waits.
    private func open(_ ix: Interaction) {
        if model.session(ix.sessionKey)?.assistant == true {
            model.assistantCurrent = ix.sessionKey
            model.selectedTab = .assistant
        } else {
            model.openChat = ix.sessionKey
            model.selectedTab = .chats
        }
    }

    private func row(_ it: InboxItem) -> some View {
        InboxRow(item: it, showAccount: accounts.count > 1, sending: store.sending.contains(it.id),
                 send: { text in Task { await store.send(it, text: text) } }, edit: { editingDraft = it })
            .background { NavigationLink(value: it) { EmptyView() }.opacity(0) }
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                if it.isDone {
                    Button { store.setDone(it, false) } label: { Label("移回", systemImage: "arrow.uturn.backward") }.tint(Theme.accent)
                } else {
                    Button { store.setDone(it, true) } label: { Label("已处理", systemImage: "checkmark") }.tint(Theme.positive)
                }
            }
            .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
            .themedRow()
            .alignmentGuide(.listRowSeparatorLeading) { _ in InboxRow.textColumn }
    }

    // MARK: Filters

    /// Sources (all, 飞书, each mail account) then kinds, as one row of chips.
    private var filters: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                chip("全部", on: source.isEmpty && kind.isEmpty) { source = ""; kind = "" }
                chip("飞书", symbol: "bubble.left.and.bubble.right", on: source == "feishu") { source = source == "feishu" ? "" : "feishu" }
                ForEach(accounts) { a in
                    chip(Self.short(a.email), symbol: "envelope", on: source == a.id) { source = source == a.id ? "" : a.id }
                }
                Divider().frame(height: 18)
                ForEach([("mention", "@我"), ("dm", "私聊"), ("group", "群聊"), ("mail", "邮件"), ("agent", "Agent")], id: \.0) { k, title in
                    chip(title, on: kind == k) { kind = kind == k ? "" : k }
                }
            }
            .padding(.horizontal, 16)
        }
    }

    private func chip(_ title: String, symbol: String? = nil, on: Bool, action: @escaping () -> Void) -> some View {
        Button {
            withAnimation(.snappy) { action() }
            Haptics.tap()
        } label: {
            HStack(spacing: 4) {
                if let symbol { Image(systemName: symbol).font(.caption) }
                Text(title)
            }
            .font(.subheadline.weight(on ? .semibold : .regular))
            .lineLimit(1)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .foregroundStyle(on ? Color.white : Color.primary)
            .background(on ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(Color.secondary.opacity(0.12)), in: .capsule)
        }
        .buttonStyle(.plain)
    }

    /// The part of an address before the @ (chips stay short), unless that alone says nothing ("c@…").
    static func short(_ email: String) -> String {
        let name = String(email.split(separator: "@").first ?? Substring(email))
        return name.count >= 3 ? name : email
    }

    private func loadAccounts() async {
        guard let api = model.api, let a = try? await api.mailAccounts() else { return }
        accounts = a
        StateCache.save("mail-accounts", a)
    }
}

// MARK: - Row

struct InboxRow: View {
    var item: InboxItem
    var showAccount = false
    var sending = false
    /// 发送 / 修改 on the AI 分身's draft (the list only).
    var send: ((String) -> Void)?
    var edit: (() -> Void)?

    /// Where the text column starts inside the row (40 avatar + 12): the separator lines up with it.
    static let textColumn: CGFloat = 52

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            avatar
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.title).font(item.isDone ? .body : .headline).foregroundStyle(item.isDone ? .secondary : .primary).lineLimit(1)
                    if let label = item.kindLabel {
                        Chip(text: label, look: item.kind == "mention" ? .tint(Theme.danger) : .neutral)
                    }
                    Spacer(minLength: 4)
                    Text(LarkTime.short(item.updatedAt)).font(.footnote).foregroundStyle(.secondary).fixedSize()
                }
                HStack(alignment: .top, spacing: 6) {
                    if let summary = item.summary, !summary.isEmpty {
                        VStack(alignment: .leading, spacing: 2) {
                            (Text(Image(systemName: "sparkles")).foregroundStyle(Theme.ai) + Text(" " + summary))
                                .font(.subheadline).foregroundStyle(item.isDone ? Color.secondary : Color.primary.opacity(0.9)).lineLimit(2)
                            Text(from).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                        }
                    } else {
                        Text(from).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                    }
                    Spacer(minLength: 0)
                    if item.count > 1 && !item.isDone {
                        // @我 is the accent (it is about you); any other count is neutral.
                        CountBadge(count: item.count, look: .solid(item.kind == "mention" ? Theme.accent : Theme.countBadge))
                    }
                }
                if showAccount && !item.isLark {
                    Text(item.accountName).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                }
                if !item.isDone {
                    if let draft = item.agentDraft, let send {
                        InboxDraftCard(draft: draft, sending: sending, send: { send(draft) }, edit: edit)
                            .padding(.top, 5)
                    } else if item.agentWorking {
                        HStack(spacing: 6) {
                            ProgressView().controlSize(.mini)
                            Text(item.agent?.state == "queued" ? "AI 分身排队中" : "AI 分身正在处理" + (item.agent?.task.map { "：" + $0 } ?? ""))
                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        .padding(.top, 2)
                    }
                }
            }
        }
        .padding(.vertical, 10)
        .contentShape(.rect)
    }

    /// Who wrote what (under the AI's summary once it has one).
    private var from: String {
        guard let sender = item.sender, !sender.isEmpty, item.lark?.isGroup != false || !item.isLark else { return item.preview }
        return sender + "：" + item.preview
    }

    private var avatar: some View {
        ZStack(alignment: .bottomTrailing) {
            Group {
            if item.isLark {
                LarkAvatarImage(name: item.title, url: item.avatar, isGroup: item.lark?.isGroup ?? false, size: 40)
            } else {
                Circle().fill(Theme.monogramFill).frame(width: 40, height: 40)
                    .overlay(Text(String((item.sender ?? "?").prefix(1))).font(.headline).foregroundStyle(.primary))
            }
            }
            .opacity(item.isDone ? 0.6 : 1)
            // The source: Feishu's own blue (the same in both appearances), mail a neutral gray, not the accent.
            Image(systemName: item.isLark ? "bubble.left.fill" : "envelope.fill")
                .font(.caption2.weight(.bold)).imageScale(.small).dynamicTypeSize(...DynamicTypeSize.large)
                .foregroundStyle(.white)
                .frame(width: 18, height: 18)
                .background(item.isLark ? Color(hex: 0x3370FF) : Color(uiColor: .systemGray), in: .circle)
                .overlay(Circle().stroke(Theme.pageBackground, lineWidth: 1.5))
                .offset(x: 4, y: 4)
        }
        .frame(width: 40, height: 40)
    }
}

/// The AI 分身's reply under an inbox row: one tap sends it as it is, 修改 opens it in the draft sheet.
struct InboxDraftCard: View {
    var draft: String
    var sending: Bool
    var send: () -> Void
    var edit: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("AI 分身草拟的回复", systemImage: "sparkles")
                .font(.caption.weight(.semibold))
                .foregroundStyle(Theme.ai)
                .lineLimit(1)
            Text(draft)
                .font(.subheadline)
                .foregroundStyle(.primary)
                .lineLimit(4)
                .frame(maxWidth: .infinity, alignment: .leading)
            HStack(spacing: 8) {
                Spacer(minLength: 0)
                if let edit {
                    Button(action: edit) {
                        Text("修改").font(.subheadline.weight(.medium)).lineLimit(1)
                            .padding(.horizontal, 14).padding(.vertical, 6)
                            .foregroundStyle(Theme.accent)
                            .background(Theme.accent.opacity(0.12), in: .capsule)
                    }
                    .buttonStyle(.borderless)
                }
                Button(action: send) {
                    HStack(spacing: 4) {
                        if sending { ProgressView().controlSize(.mini).tint(.white) } else { Image(systemName: "paperplane.fill").font(.caption) }
                        Text("发送")
                    }
                    .font(.subheadline.weight(.semibold)).lineLimit(1)
                    .padding(.horizontal, 14).padding(.vertical, 6)
                    .foregroundStyle(.white)
                    .background(Theme.accent, in: .capsule)
                }
                .buttonStyle(.borderless)
                .disabled(sending)
                .accessibilityLabel("发送 AI 分身草拟的回复")
            }
        }
        .padding(10)
        .background(Theme.accent.opacity(0.07), in: .rect(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(Theme.accent.opacity(0.18), lineWidth: 0.5))
    }
}

/// An agent waiting on the user, among the inbox rows: which agent, what it asks, then what it waits for (red, as
/// 需要你 is everywhere) and on which machine. The label sits under the title so the title keeps the width.
struct InboxAgentRow: View {
    var interaction: Interaction
    var machine: Machine?
    var machineName: String

    var body: some View {
        let ix = interaction
        let asks = ix.summary ?? ix.questions?.first?.question
        HStack(alignment: .top, spacing: 12) {
            // The rounded square of the Agents rows (SessionAvatar), with what kind of request it is.
            SessionAvatar(machine: machine, state: .needsYou, size: 40, symbol: ix.kind.symbol, dot: false)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(ix.sessionTitle?.isEmpty == false ? ix.sessionTitle! : (ix.cwd.map { URL(fileURLWithPath: $0).lastPathComponent } ?? "Agent"))
                        .font(.headline).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(LarkTime.short(ix.createdAt)).font(.footnote).foregroundStyle(.secondary).fixedSize()
                }
                if let asks, !asks.isEmpty {
                    Text(asks).font(.subheadline).foregroundStyle(Color.primary.opacity(0.9)).lineLimit(2)
                    (Text(ix.title).fontWeight(.semibold).foregroundStyle(StatusPalette.needsYou.text)
                        + Text(" · " + machineName).foregroundStyle(.secondary))
                        .font(.footnote).lineLimit(1)
                } else {
                    Text(ix.title).font(.subheadline.weight(.semibold)).foregroundStyle(StatusPalette.needsYou.text).lineLimit(1)
                    Text(machineName).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
        .padding(.vertical, 10)
        .contentShape(.rect)
    }
}

// MARK: - Detail

/// One inbox item opened: the Feishu chat at its first new message, or the mail; the AI's summary with AI 草拟回复 /
/// 交给 AI on top, 已处理 in the toolbar.
struct InboxDetailView: View {
    var item: InboxItem
    var accounts: [MailAccount]
    @Environment(\.dismiss) private var dismiss
    @State private var drafting = false
    @State private var handingOff = false

    private var current: InboxItem { InboxStore.shared.items.first { $0.id == item.id } ?? item }

    var body: some View {
        content
            .safeAreaInset(edge: .top, spacing: 0) {
                InboxActionBar(item: current, draft: { drafting = true }, handOff: { handingOff = true })
            }
            .sheet(isPresented: $drafting) { InboxDraftSheet(item: current, accounts: accounts) }
            .sheet(isPresented: $handingOff) { InboxHandOffSheet(item: current) }
            #if DEBUG
            // `-showInboxAction draft|handoff`: open that sheet (screenshots).
            .task {
                switch UserDefaults.standard.string(forKey: "showInboxAction") {
                case "draft": drafting = true
                case "handoff": handingOff = true
                default: break
                }
            }
            #endif
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    if current.isDone {
                        Button { InboxStore.shared.setDone(current, false) } label: { Label("移回", systemImage: "arrow.uturn.backward") }
                    } else {
                        Button {
                            InboxStore.shared.setDone(current, true)
                            dismiss()
                        } label: { Label("已处理", systemImage: "checkmark") }
                    }
                }
            }
    }

    @ViewBuilder private var content: some View {
        if let c = item.larkConversation {
            LarkChatView(conversation: c)
        } else if let m = item.mailSummary {
            MailDetailView(summary: m, accounts: accounts)
        } else {
            EmptyStateView(symbol: "questionmark", title: item.title, message: item.preview)
        }
    }
}
