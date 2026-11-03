import BeautifulUI
import SwiftUI

@MainActor final class InboxAccountState: ObservableObject {
    @Published var accounts = StateCache.load("mail-accounts", as: [MailAccount].self) ?? []
}

// MARK: - 收件箱

/// The unified inbox (docs/inbox.md): Feishu messages and mail in one list, one row per thing (the new messages in a
/// chat, a mail thread), newest first. No per-chat browsing: what is here is what came in. Filters narrow it by source
/// (飞书, one mail account) and kind (私聊, @我, 群聊); a row swipes away as 已处理, and the done ones stay at the bottom
/// for a few days in case of a slip.
struct InboxView: View {
    @State private var path = NavigationPath()
    @StateObject private var accountState = InboxAccountState()

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("InboxView")
        #endif
        SplitNavigation(path: $path, destinations: InboxDestinations(accounts: accountState.accounts)) {
            InboxPage(path: $path, accountState: accountState)
        } empty: {
            SplitPlaceholder(symbol: Theme.Icon.inbox, title: "选一件事", message: "左边列表里点一件事，详情会在这里打开。")
        }
        .environment(\.feishuOpen) { path.append($0) }
        .feishuHandlesLinks { path.append($0) }
    }
}

extension InboxView {
    /// A mail account's short name in the filters (the Mac's inbox uses it too).
    static func short(_ email: String) -> String { InboxPage.short(email) }
}

/// The inbox's page itself, in whatever stack shows it: 收件箱's own (InboxView), or the shell's 助手 page (pushed from
/// the drawer, docs/ios.md「新外壳」), which passes `embedded` so the page brings its destinations and link handling.
struct InboxPage: View {
    @Environment(AppModel.self) private var model
    @Binding var path: NavigationPath
    @ObservedObject var accountState: InboxAccountState
    var embedded = false
    @AppStorage("inbox.source") private var source = "" // "" all, "feishu", or a mail account id
    @AppStorage("inbox.kind") private var kind = ""     // "" all, agent, dm, mention, group, mail
    @State private var query = ""
    private var accounts: [MailAccount] {
        get { accountState.accounts }
        nonmutating set { accountState.accounts = newValue }
    }
    /// Sections shown open: tiers (action, fyi, noise) and "done". 已折叠 and 已处理 start closed.
    @State private var expanded: Set<String> = ["action", "fyi"]
    @State private var composing: ComposeDraft?
    @State private var editingDraft: InboxItem? // 修改 on the AI 分身's draft
    @State private var showingMutes = false     // 已静音
    private var store: InboxStore { .shared }

    private func matches(_ it: InboxItem) -> Bool {
        if !source.isEmpty && it.account != source { return false }
        if !kind.isEmpty && !(it.kind == kind || kind == "group" && it.kind == "bot" || kind == "mention" && (it.kind == "doc" || it.kind == "moment" && it.kindLabel == "@我")) { return false }
        if query.isEmpty { return true }
        return [it.title, it.sender ?? "", it.preview, it.summary ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
    }

    private var openItems: [InboxItem] { store.items.filter { !$0.isDone && matches($0) } }

    /// The hub's connectors with items in the list (GitHub…), one filter each; none, no chip.
    private var connectorSources: [(id: String, name: String)] {
        var seen = Set<String>()
        return store.items.compactMap { it in
            guard it.connector != nil, seen.insert(it.account).inserted else { return nil }
            return (it.account, it.accountName)
        }
    }

    /// Agents (and the assistant) waiting for an answer, an approval or a plan review: they need the user too, so they
    /// head 需要你. Opening one goes to its chat; the Agents tab stays as it is.
    private var waitingAgents: [Interaction] {
        guard source.isEmpty, kind.isEmpty || kind == "agent" else { return [] }
        return model.pendingInteractions.filter { ix in
            !model.isMuted(ix.sessionKey) && (query.isEmpty || [ix.sessionTitle ?? "", ix.summary ?? "", ix.title].contains { $0.localizedCaseInsensitiveContains(query) })
        }
    }

    var body: some View {
        if embedded { // its destinations are the shell's (ShellDestinations)
            page
                .environment(\.feishuOpen) { path.append($0) }
                .feishuHandlesLinks { path.append($0) }
        } else {
            page
        }
    }

    private var page: some View {
            list
                .navigationTitle("收件箱")
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $query, prompt: "搜索标题、发件人、内容")
                .toolbar {
                    ToolbarItem(placement: .principal) { ConnectionTitle(title: "收件箱") }
                    if model.isMultiHub {
                        // Each hub has its own inbox (docs/multi-hub.md「hub 级页面」): whose this is, and the others.
                        ToolbarItem(placement: .topBarLeading) { HubSwitcher(compact: true) }
                    }
                    ToolbarItem(placement: .topBarTrailing) {

                        // Mail accounts are managed in 设置 › 连接器 › 邮箱 (2026-11-01, with this device's data and 飞书).
                        Menu {
                            Button { composing = ComposeDraft(accountID: accounts.first?.id ?? "") } label: { Label("写邮件", systemImage: Theme.Icon.compose) }
                                .disabled(accounts.isEmpty)
                            Button { showingMutes = true } label: { Label("已静音", systemImage: Theme.Icon.mute) }
                        } label: { Image(systemName: Theme.Icon.moreMenu) }
                        .accessibilityLabel("更多")
                    }
                }
                .sheet(item: $composing) { d in ComposeView(draft: d, accounts: accounts) }
                .sheet(item: $editingDraft) { InboxDraftSheet(item: $0, accounts: accounts) }
                .sheet(isPresented: $showingMutes) { InboxMutesView() }
                .task {
                    store.reload()
                    await loadAccounts()
                    #if DEBUG
                    // `-showInboxMutes YES`: the 已静音 list (screenshots).
                    if UserDefaults.standard.bool(forKey: "showInboxMutes") { showingMutes = true }
                    // `-openInboxFirst YES`: open the first open item (detail screenshots).
                    if UserDefaults.standard.bool(forKey: "openInboxFirst") {
                        for _ in 0..<20 where store.open.isEmpty { try? await Task.sleep(for: .milliseconds(500)) }
                        if let first = openItems.first { path = NavigationPath([first]) }
                    }
                    // `-openInboxItem <id>`: open that item (a kind's detail, e.g. a removed one).
                    if let id = UserDefaults.standard.string(forKey: "openInboxItem") {
                        for _ in 0..<20 where !store.items.contains(where: { $0.id == id }) { try? await Task.sleep(for: .milliseconds(500)) }
                        if let it = store.items.first(where: { $0.id == id }) { path = NavigationPath([it]) }
                    }
                    #endif
                }
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
                Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
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
        HStack(spacing: Theme.Space.sm) {
            Button {
                withAnimation(Theme.Motion.standard) { if expanded.contains(key) { expanded.remove(key) } else { expanded.insert(key) } }
            } label: {
                HStack(spacing: Theme.Space.sm) {
                    Text("\(title) · \(count)").font(Theme.Font.sectionHeader).foregroundStyle(key == "action" ? Color.primary : .secondary)
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelHeavy).foregroundStyle(.secondary)
                        .rotationEffect(.degrees(expanded.contains(key) ? 90 : 0))
                    Spacer(minLength: 0)
                }
                .lineLimit(1)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if let clear {
                Button(clear.0) { withAnimation(Theme.Motion.standard) { clear.1() } }
                    .font(Theme.Font.secondary)
                    .lineLimit(1)
                    .buttonStyle(.borderless)
            }
        }
        .textCase(nil)
        .padding(.vertical, Theme.Space.xxs)
    }

    private func agentRow(_ ix: Interaction) -> some View {
        Button { open(ix) } label: {
            InboxAgentRow(interaction: ix, machine: model.machine(ix.machineRef), machineName: model.machine(ix.machineRef)?.title ?? ix.machineID)
        }
        .buttonStyle(.plain)
        .modifier(InboxRowLook())
        .accessibilityIdentifier("inbox.agentRow")
    }

    /// The agent's chat (the assistant's in 对话), where the question / plan / approval waits.
    private func open(_ ix: Interaction) {
        model.open(ix.sessionKey) // in its own group (another group's Persona: that group's)
    }

    private func row(_ it: InboxItem) -> some View {
        InboxRow(item: it, showAccount: accounts.count > 1, sending: store.sending.contains(it.id),
                 send: { text in Task { await store.send(it, text: text, changes: it.agent?.changes ?? []) } }, edit: { editingDraft = it })
            .background { NavigationLink(value: it) { EmptyView() }.opacity(0) }
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                if it.isDone {
                    Button { store.setDone(it, false) } label: { Label("移回", systemImage: Theme.Icon.undo) }.tint(Theme.accent)
                } else {
                    Button { store.setDone(it, true) } label: { Label("已处理", systemImage: Theme.Icon.done) }.tint(Theme.positive)
                }
            }
            .contextMenu { rowMenu(it) }
            .modifier(InboxRowLook())
    }

    /// A row's long-press menu: 已处理 (or 移回), then what the item can be muted by — its repository, chat or sender,
    /// the name under each (docs/inbox.md「静音」).
    @ViewBuilder private func rowMenu(_ it: InboxItem) -> some View {
        if it.isDone {
            Button { store.setDone(it, false) } label: { Label("移回", systemImage: Theme.Icon.undo) }
        } else {
            Button { store.setDone(it, true) } label: { Label("已处理", systemImage: Theme.Icon.done) }
        }
        if let mutes = it.mutes, !mutes.isEmpty {
            Section {
                ForEach(mutes, id: \.self) { m in
                    Button { store.mute(it, m) } label: {
                        Label(m.title, systemImage: Theme.Icon.mute)
                        Text(m.shortName)
                    }
                }
            }
        }
    }

    // MARK: Filters

    /// Sources (all, 飞书, each mail account) then kinds, as one row of chips; in the BeautifulUI style its filter chips,
    /// wrapping to a second row (no sideways scrolling at the top of a page, like the Agents list).
    @ViewBuilder private var filters: some View {
        if AppStyle.current.usesBeautifulUI {
            LarkFlowLayout(spacing: Theme.Space.xs, lineSpacing: Theme.Space.sm) {
                filterChip("全部", on: source.isEmpty && kind.isEmpty) { source = ""; kind = "" }
                filterChip("飞书", on: source == "feishu") { source = source == "feishu" ? "" : "feishu" }
                ForEach(accounts) { a in
                    filterChip(Self.short(a.email), on: source == a.id) { source = source == a.id ? "" : a.id }
                }
                ForEach(connectorSources, id: \.id) { c in
                    filterChip(c.name, on: source == c.id) { source = source == c.id ? "" : c.id }
                }
                ForEach([("mention", "@我"), ("dm", "私聊"), ("group", "群聊"), ("mail", "邮件"), ("agent", "Agent")], id: \.0) { k, title in
                    filterChip(title, on: kind == k) { kind = kind == k ? "" : k }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Theme.Space.l)
        } else {
            plainFilters
        }
    }

    private func filterChip(_ title: String, on: Bool, action: @escaping () -> Void) -> some View {
        BUIFilterChip(title, isActive: on) {
            withAnimation(Theme.Motion.standard) { action() }
            Haptics.tap()
        }
    }

    private var plainFilters: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.s) {
                chip("全部", on: source.isEmpty && kind.isEmpty) { source = ""; kind = "" }
                chip("飞书", symbol: Theme.Icon.feishuChats, on: source == "feishu") { source = source == "feishu" ? "" : "feishu" }
                ForEach(accounts) { a in
                    chip(Self.short(a.email), symbol: Theme.Icon.mail, on: source == a.id) { source = source == a.id ? "" : a.id }
                }
                ForEach(connectorSources, id: \.id) { c in
                    chip(c.name, symbol: Theme.Icon.connectors, on: source == c.id) { source = source == c.id ? "" : c.id }
                }
                Divider().scaledFrame(height: Theme.Size.badge)
                ForEach([("mention", "@我"), ("dm", "私聊"), ("group", "群聊"), ("mail", "邮件"), ("agent", "Agent")], id: \.0) { k, title in
                    chip(title, on: kind == k) { kind = kind == k ? "" : k }
                }
            }
            .padding(.horizontal, Theme.Space.l)
        }
    }

    private func chip(_ title: String, symbol: String? = nil, on: Bool, action: @escaping () -> Void) -> some View {
        Button {
            withAnimation(Theme.Motion.standard) { action() }
            Haptics.tap()
        } label: {
            HStack(spacing: Theme.Space.xs) {
                if let symbol { Image(systemName: symbol).font(Theme.Font.label) }
                Text(title)
            }
            .font(on ? Theme.Font.secondaryStrong : Theme.Font.secondary)
            .lineLimit(1)
            .padding(.horizontal, Theme.Space.m)
            .padding(.vertical, Theme.Space.sm)
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

    @AppStorage(TranslationPrefs.targetKey) private var translateTarget = TranslationPrefs.defaultTarget
    @AppStorage(TranslationPrefs.enabledKey) private var translating = false

    /// 设置 › 翻译: the hub's translation of foreign mail, done as it came in (subject and preview).
    private var translated: InboxItem.Translated? { translating && TranslationPrefs.mail ? item.translated(translateTarget) : nil }

    /// Where the text column starts inside the row (40 avatar + 12): the separator lines up with it.
    static let textColumn: CGFloat = 52

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            avatar
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(translated.map(\.subject).flatMap { $0.isEmpty ? nil : $0 } ?? item.title).font(item.isDone ? Theme.Font.body : Theme.Font.heading).foregroundStyle(item.isDone ? .secondary : .primary).lineLimit(1)
                    if translated != nil {
                        Image(systemName: Theme.Icon.translate).font(Theme.Font.label).foregroundStyle(.tertiary)
                            .accessibilityLabel("已翻译")
                    }
                    if let label = item.kindLabel {
                        Chip(text: label, look: item.kind == "mention" || item.kind == "doc" && label == "@我" ? .tint(Theme.danger) : .neutral)
                    }
                    Spacer(minLength: 4)
                    Text(LarkTime.short(item.updatedAt)).font(Theme.Font.caption).foregroundStyle(.secondary).fixedSize()
                }
                HStack(alignment: .top, spacing: Theme.Space.sm) {
                    if let summary = item.summary, !summary.isEmpty {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            (Text(Image(systemName: Theme.Icon.ai)).foregroundStyle(Theme.ai) + Text(" " + summary))
                                // The hierarchical styles, not `Color.primary`: a Mac list's selected row turns that
                                // white, where the row sets the words' color for these (MacHoverRow).
                                .font(Theme.Font.secondary)
                                .foregroundStyle(item.isDone ? AnyShapeStyle(.secondary) : AnyShapeStyle(.primary.opacity(0.9))).lineLimit(2)
                            Text(from).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    } else {
                        Text(from).font(Theme.Font.secondary).lineHeight(.secondary).foregroundStyle(.secondary).lineLimit(2)
                    }
                    Spacer(minLength: 0)
                    if item.count > 1 && !item.isDone {
                        // @我 is the accent (it is about you); any other count is neutral.
                        CountBadge(count: item.count, look: .solid(item.kind == "mention" ? Theme.accent : Theme.countBadge))
                    }
                }
                if item.doc != nil && !item.isDone {
                    InboxDocRowsPreview(item: item).padding(.top, Theme.Space.xxs)
                }
                if showAccount && !item.isLark {
                    Text(item.accountName).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                if !item.isDone {
                    if let draft = item.agentDraft, let send {
                        InboxDraftCard(draft: draft, changes: (item.agent?.changes ?? []).map { DocChangeText.line($0, in: item.doc) },
                                       sending: sending, send: { send(draft) }, edit: edit)
                            .padding(.top, Theme.Space.sm)
                    } else if item.agentWorking {
                        HStack(spacing: Theme.Space.sm) {
                            if AppStyle.current.usesBeautifulUI { BUISpinner(size: Theme.Size.dotXL) } else { ProgressView().controlSize(.mini) }
                            Text(item.agent?.state == "queued" ? "AI 分身排队中" : "AI 分身正在处理" + (item.agent?.task.map { "：" + $0 } ?? ""))
                                .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                        .padding(.top, Theme.Space.xxs)
                    }
                }
            }
        }
        .padding(.vertical, Theme.Space.m)
        .contentShape(.rect)
    }

    /// Who wrote what (under the AI's summary once it has one).
    private var from: String {
        if let doc = item.doc { return doc.typeName + " · " + item.preview } // the preview names who: 李雷提及了你
        if item.moment != nil { return (item.sender ?? "") + "：" + item.preview } // who commented, on the post in the title
        if let c = item.connector { // example-org/example-repo#12 · bob：请看一下
            let who = [item.sender ?? "", item.preview].filter { !$0.isEmpty }.joined(separator: "：")
            return [c.reference ?? "", who].filter { !$0.isEmpty }.joined(separator: " · ")
        }
        if let t = translated?.preview, !t.isEmpty, let sender = item.sender, !sender.isEmpty { return sender + "：" + t }
        guard let sender = item.sender, !sender.isEmpty, item.lark?.isGroup != false || !item.isLark else { return item.preview }
        return sender + "：" + item.preview
    }

    private var avatar: some View {
        ZStack(alignment: .bottomTrailing) {
            Group {
            if let doc = item.doc {
                InboxDocIcon(doc: doc)
            } else if item.moment != nil {
                RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    .fill((LarkColor.text("orange") ?? Theme.accent).opacity(0.15))
                    .scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
                    .overlay(Image(systemName: Theme.Icon.moments).font(Theme.Font.bodyStrong).foregroundStyle(LarkColor.text("orange") ?? Theme.accent))
            } else if item.isLark {
                LarkAvatarImage(name: item.title, url: item.avatar, isGroup: item.lark?.isGroup ?? false, size: Theme.Size.avatar)
            } else {
                Monogram(name: item.sender ?? "", fill: Theme.monogramFill)
            }
            }
            .opacity(item.isDone ? 0.6 : 1)
            // The source: 飞书's (or a connector's: GitHub's, a template image, in black) own mark on white; mail, or a
            // connector without a mark, a neutral gray, not the accent.
            Group {
                if let brand = item.isLark ? Theme.Brand.lark : item.connector != nil ? ConnectorText.brand(item.source) : nil {
                    Image(brand).resizable().scaledToFit().foregroundStyle(.black).padding(1.5).background(.white, in: .circle)
                } else {
                    Image(systemName: item.connector != nil ? Theme.Icon.connectors : Theme.Icon.mailFilled)
                        .font(Theme.Font.labelHeavy).imageScale(.small).dynamicTypeSize(...DynamicTypeSize.large)
                        .foregroundStyle(.white)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Color(uiColor: .systemGray), in: .circle)
                }
            }
            .scaledFrame(width: Theme.Size.badge, height: Theme.Size.badge)
            .overlay(Circle().stroke(Theme.pageBackground, lineWidth: 1.5))
            .offset(x: 4, y: 4)
        }
        .scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
    }
}

/// The AI 分身's reply under an inbox row: one tap sends it as it is, 修改 opens it in the draft sheet.
struct InboxDraftCard: View {
    var draft: String
    /// Status moves made with it (document items), one line each.
    var changes: [String] = []
    var sending: Bool
    var send: () -> Void
    var edit: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Label("AI 分身草拟的回复", systemImage: Theme.Icon.ai)
                .font(Theme.Font.labelStrong)
                .foregroundStyle(Theme.ai)
                .lineLimit(1)
            Text(draft)
                .font(Theme.Font.secondary)
                .foregroundStyle(.primary)
                .lineLimit(4)
                .frame(maxWidth: .infinity, alignment: .leading)
            ForEach(changes, id: \.self) { c in
                Label(c, systemImage: Theme.Icon.inProgress)
                    .font(Theme.Font.captionMedium).foregroundStyle(.secondary).lineLimit(1)
            }
            if AppStyle.current.usesBeautifulUI { beautifulActions } else { plainActions }
        }
        .padding(Theme.Space.m)
        .modifier(DraftSurface())
    }

    /// BeautifulUI's RecommendationCard actions: 修改 secondary, 发送 the accent.
    private var beautifulActions: some View {
        HStack(spacing: Theme.Space.s) {
            Spacer(minLength: 0)
            if let edit {
                Button("修改", action: edit).buttonStyle(.bui(.secondary)).lineLimit(1)
            }
            Button(action: send) {
                HStack(spacing: Theme.Space.xs) {
                    if sending { BUISpinner(size: Theme.Size.dotXL) }
                    Text("发送").lineLimit(1)
                }
            }
            .buttonStyle(.bui(.accent))
            .disabled(sending)
            .accessibilityLabel("发送 AI 分身草拟的回复")
        }
    }

    private var plainActions: some View {
        HStack(spacing: Theme.Space.s) {
            Spacer(minLength: 0)
            if let edit {
                Button(action: edit) {
                    Text("修改").font(Theme.Font.secondaryMedium).lineLimit(1)
                        .padding(.horizontal, Theme.Space.l).padding(.vertical, Theme.Space.sm)
                        .foregroundStyle(Theme.accent)
                        .background(Theme.accent.opacity(0.12), in: .capsule)
                }
                .buttonStyle(.borderless)
            }
            Button(action: send) {
                HStack(spacing: Theme.Space.xs) {
                    if sending { ProgressView().controlSize(.mini).tint(.white) } else { Image(systemName: Theme.Icon.sendMail).font(Theme.Font.label) }
                    Text("发送")
                }
                .font(Theme.Font.secondaryStrong).lineLimit(1)
                .padding(.horizontal, Theme.Space.l).padding(.vertical, Theme.Space.sm)
                .foregroundStyle(.white)
                .background(Theme.accent, in: .capsule)
            }
            .buttonStyle(.borderless)
            .disabled(sending)
            .accessibilityLabel("发送 AI 分身草拟的回复")
        }
    }
}

/// The draft's card: the accent's tint with a hairline; in the BeautifulUI style the field gray (RecommendationCard).
private struct DraftSurface: ViewModifier {
    func body(content: Content) -> some View {
        if AppStyle.current.usesBeautifulUI {
            content.background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        } else {
            content
                .background(Theme.accent.opacity(0.07), in: .rect(cornerRadius: Theme.Radius.card))
                .overlay(RoundedRectangle(cornerRadius: Theme.Radius.card).stroke(Theme.accent.opacity(0.18), lineWidth: 0.5))
        }
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
        HStack(alignment: .top, spacing: Theme.Space.m) {
            // The rounded square of the Agents rows (SessionAvatar), with what kind of request it is.
            SessionAvatar(machine: machine, state: .needsYou, size: 40, symbol: ix.kind.symbol, dot: false)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(ix.sessionTitle?.isEmpty == false ? ix.sessionTitle! : (ix.cwd.map { URL(fileURLWithPath: $0).lastPathComponent } ?? "Agent"))
                        .font(Theme.Font.heading).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(LarkTime.short(ix.createdAt)).font(Theme.Font.caption).foregroundStyle(.secondary).fixedSize()
                }
                if let asks, !asks.isEmpty {
                    Text(asks).font(Theme.Font.secondary).foregroundStyle(.primary.opacity(0.9)).lineLimit(2) // see InboxRow
                    (Text(ix.title).fontWeight(.semibold).foregroundStyle(StatusPalette.needsYou.text)
                        + Text(" · " + machineName).foregroundStyle(.secondary))
                        .font(Theme.Font.caption).lineLimit(1)
                } else {
                    Text(ix.title).font(Theme.Font.secondaryStrong).foregroundStyle(StatusPalette.needsYou.text).lineLimit(1)
                    Text(machineName).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
        .padding(.vertical, Theme.Space.m)
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
                        Button { InboxStore.shared.setDone(current, false) } label: { Label("移回", systemImage: Theme.Icon.undo) }
                    } else {
                        Button {
                            InboxStore.shared.setDone(current, true)
                            dismiss()
                        } label: { Label("已处理", systemImage: Theme.Icon.done) }
                    }
                }
            }
    }

    @ViewBuilder private var content: some View {
        if current.doc != nil {
            InboxDocDetailView(item: current)
        } else if current.connector != nil {
            InboxConnectorDetailView(item: current)
        } else if let m = current.moment {
            LarkMomentsPostView(postID: m.postID, pushed: true) // the post and its comments, as a shared removed card opens
        } else if let c = item.larkConversation {
            LarkChatView(conversation: c)
        } else if let m = item.mailSummary {
            MailDetailView(summary: m, accounts: accounts)
        } else {
            EmptyStateView(symbol: "questionmark", title: item.title, message: item.preview)
        }
    }
}

/// The pages the inbox opens, in either layout (`SplitNavigation`).
struct InboxDestinations: ViewModifier {
    var accounts: [MailAccount]
    @Environment(\.feishuOpen) private var feishuOpen

    // Links on these pages open in the app (feishuHandlesLinks) wherever the stack is: the shell pushes them onto its
    // own stack, where the inbox page's handler does not reach.
    func body(content: Content) -> some View {
        content
            .navigationDestination(for: InboxItem.self) {
                InboxDetailView(item: $0, accounts: accounts).splitDetailPage().feishuHandlesLinks(feishuOpen)
            }
            .navigationDestination(for: FeishuRoute.self) { FeishuRouteView(route: $0).splitDetailPage().feishuHandlesLinks(feishuOpen) }
    }
}

/// An inbox row on the list: in the BeautifulUI style a card of its own on the page (its surface), like the Agents
/// list's capsules, no list background or line; otherwise the plain row with its line from the text's column. The
/// swipe actions stay the list row's either way.
private struct InboxRowLook: ViewModifier {
    func body(content: Content) -> some View {
        if AppStyle.current.usesBeautifulUI {
            content
                .padding(.horizontal, Theme.Space.m)
                .card(padding: 0)
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
                .listRowInsets(EdgeInsets(top: Theme.Space.xs, leading: Theme.Space.l, bottom: Theme.Space.xs, trailing: Theme.Space.l))
        } else {
            content
                .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
                .themedRow()
                .alignmentGuide(.listRowSeparatorLeading) { _ in InboxRow.textColumn }
        }
    }
}
