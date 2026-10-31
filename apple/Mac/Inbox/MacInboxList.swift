import SwiftUI

// The Mac inbox (docs/inbox.md; docs/mac.md「主窗口」): the sidebar's inbox entries (`MacInboxFilters`), the list column
// (`MacInboxList`) and the detail column (`MacInboxDetail`). The list is the iPhone's: the same rows, sections 需要你 /
// 知悉即可 / 已折叠 / 已处理, the AI 分身's draft cards. Mac ways on top: system selection (⇧ / ⌘ for several), ↑↓ to move,
// E or ⌫ files the selected as 已处理 and selects the next, ⏎ or a double click opens it in a window of its own (an
// agent row: its chat), a right-click menu with everything the row can do, swipe left on the trackpad for 已处理.

/// The inbox's entries in the window's sidebar: sources (全部, 飞书, each mail account) and kinds (@我, 私聊, 群聊, 邮件,
/// Agent), each with what still waits in it. A `Section` of rows for a sidebar `List`; picking one sets the list's
/// filter. Rows are tagged with `MacInboxFilter`, so a sidebar whose selection is that type selects them natively.
struct MacInboxFilters: View {
    @Environment(AppModel.self) private var model
    var state = MacInboxState.shared

    var body: some View {
        Section("收件箱") {
            ForEach(state.sources) { row($0) }
        }
        Section("类型") {
            ForEach(state.kinds) { row($0) }
        }
    }

    private func row(_ f: MacInboxFilter) -> some View {
        let n = state.count(f, model: model)
        return Label(f.title, systemImage: f.icon)
            .lineLimit(1)
            .badge(n > 0 ? Text("\(n)") : nil)
            .tag(f)
            .help(f.title)
            .accessibilityIdentifier("mac.inbox.filter.\(f.id)")
    }
}

/// The list column.
struct MacInboxList: View {
    @Environment(AppModel.self) private var model
    @Environment(\.openWindow) private var openWindow
    @Bindable var state = MacInboxState.shared
    private var store: InboxStore { .shared }

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("MacInboxList")
        #endif
        let sections = state.sections(model)
        List(selection: $state.selection) {
            if let error = store.error, store.items.isEmpty {
                Label(error, systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
            ForEach(sections.tiers, id: \.tier) { t in
                Section {
                    if state.expanded.contains(t.tier.rawValue) {
                        ForEach(t.agents) { agentRow($0) }
                        ForEach(t.items) { row($0) }
                    }
                } header: {
                    header(t.tier.title, count: t.items.count + t.agents.count, key: t.tier.rawValue,
                           clear: t.tier == .action ? nil : (t.tier == .fyi ? "全部看过" : "全部清除", { store.setDone(t.items, true) }))
                }
            }
            if !sections.done.isEmpty {
                Section {
                    if state.expanded.contains("done") { ForEach(sections.done) { row($0) } }
                } header: {
                    header("已处理", count: sections.done.count, key: "done", clear: nil)
                }
            }
        }
        .listStyle(.inset)
        .alternatingRowBackgrounds(.disabled)
        .overlay {
            if sections.isEmpty && !store.loading {
                EmptyStateView(symbol: Theme.Icon.success, title: state.query.isEmpty ? "都处理完了" : "没有匹配的",
                               message: state.query.isEmpty ? "新的飞书消息和邮件会出现在这里。" : "换个关键词试试。")
            } else if store.loading && store.items.isEmpty {
                ProgressView()
            }
        }
        .contextMenu(forSelectionType: MacInboxSelection.self) { ids in
            menu(ids)
        } primaryAction: { ids in
            openAll(ids)
        }
        .onKeyPress(characters: ["e", "E"], phases: .down) { _ in
            guard !state.selection.isEmpty else { return .ignored }
            state.toggleDone(state.selection, model: model)
            return .handled
        }
        .onDeleteCommand { state.toggleDone(state.selection, model: model) }
        .searchable(text: $state.query, placement: .toolbar, prompt: "搜索标题、发件人、内容")
        .toolbar { toolbar }
        .sheet(isPresented: $state.showAccounts, onDismiss: { Task { await state.loadAccounts(model) } }) {
            MailAccountsView().macSheet()
        }
        .sheet(item: $state.composing) { d in ComposeView(draft: d, accounts: state.accounts).macSheet() }
        .sheet(item: $state.editingDraft) { InboxDraftSheet(item: $0, accounts: state.accounts).macSheet() }
        .task {
            store.reload()
            await state.loadAccounts(model)
            #if DEBUG
            // `-openInboxFirst YES`: select the first open item; `-openInboxMatch <text>`: the first whose title has the
            // text (detail screenshots); `-addMail YES`: 管理邮箱.
            if UserDefaults.standard.bool(forKey: "addMail") { state.showAccounts = true }
            let match = UserDefaults.standard.string(forKey: "openInboxMatch")
            if UserDefaults.standard.bool(forKey: "openInboxFirst") || match != nil {
                for _ in 0..<20 where store.open.isEmpty { try? await Task.sleep(for: .milliseconds(500)) }
                if let first = store.items.first(where: { match == nil ? !$0.isDone : $0.title.contains(match!) }) {
                    state.selection = [.item(first.id)]
                }
            }
            #endif
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button { Task { await store.refresh() } } label: { Label("刷新", systemImage: Theme.Icon.retry) }
                .keyboardShortcut("r", modifiers: .command)
                .help("刷新（⌘R）")
            Button { state.composing = ComposeDraft(accountID: state.accounts.first?.id ?? "") } label: {
                Label("写邮件", systemImage: Theme.Icon.compose)
            }
            .disabled(state.accounts.isEmpty)
            .keyboardShortcut("n", modifiers: [.command, .option])
            .help(state.accounts.isEmpty ? "先在「管理邮箱」里添加一个邮箱" : "写邮件（⌥⌘N）")
            Button { state.showAccounts = true } label: { Label("管理邮箱", systemImage: Theme.Icon.mailAccounts) }
                .help("管理邮箱")
        }
    }

    /// A section title that opens / closes the section, with its count, and an optional action on the right.
    private func header(_ title: String, count: Int, key: String, clear: (String, () -> Void)?) -> some View {
        HStack(spacing: Theme.Space.sm) {
            Button {
                withAnimation(Theme.Motion.standard) { state.toggle(key) }
            } label: {
                HStack(spacing: Theme.Space.xs) {
                    Image(systemName: Theme.Icon.disclosure).font(Theme.Font.labelHeavy).foregroundStyle(.secondary)
                        .rotationEffect(.degrees(state.expanded.contains(key) ? 90 : 0))
                    Text(title).foregroundStyle(key == "action" ? Color.primary : .secondary)
                    Text("\(count)").foregroundStyle(.secondary).monospacedDigit()
                    Spacer(minLength: 0)
                }
                .font(Theme.Font.sectionHeader)
                .lineLimit(1)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .help(state.expanded.contains(key) ? "收起" : "展开")
            if let clear {
                Button(clear.0) { withAnimation(Theme.Motion.standard) { clear.1() } }
                    .font(Theme.Font.secondary)
                    .lineLimit(1)
                    .buttonStyle(.link)
            }
        }
        .textCase(nil)
    }

    private func agentRow(_ ix: Interaction) -> some View {
        InboxAgentRow(interaction: ix, machine: model.machine(ix.machineID), machineName: model.machine(ix.machineID)?.name ?? ix.machineID)
            .tag(MacInboxSelection.agent(ix.id))
            .macHoverRow()
    }

    private func row(_ it: InboxItem) -> some View {
        InboxRow(item: it, showAccount: state.accounts.count > 1, sending: store.sending.contains(it.id),
                 send: { text in Task { await store.send(it, text: text, changes: it.agent?.changes ?? []) } },
                 edit: { state.editingDraft = it })
            .tag(MacInboxSelection.item(it.id))
            .macHoverRow()
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                if it.isDone {
                    Button { store.setDone(it, false) } label: { Label("移回", systemImage: Theme.Icon.undo) }.tint(Theme.accent)
                } else {
                    Button { state.toggleDone([.item(it.id)], model: model) } label: { Label("已处理", systemImage: Theme.Icon.done) }.tint(Theme.positive)
                }
            }
    }

    // MARK: Menu

    @ViewBuilder private func menu(_ ids: Set<MacInboxSelection>) -> some View {
        let items = ids.compactMap { s -> InboxItem? in if case .item(let id) = s { return state.item(id) } else { return nil } }
        let agents = ids.compactMap { s -> Interaction? in
            if case .agent(let id) = s { return model.pendingInteractions.first { $0.id == id } } else { return nil }
        }
        if items.count == 1, let it = items.first {
            Button { openWindow(id: MacInboxItemWindow.id, value: it.id) } label: { Label("在新窗口打开", systemImage: Theme.Icon.newWindow) }
            Divider()
            if !it.isDone {
                Button { state.drafting = it; state.selection = [.item(it.id)] } label: {
                    Label(it.agentDraft == nil ? "AI 草拟回复" : "查看草稿", systemImage: Theme.Icon.draftReply)
                }
                Button { state.handingOff = it; state.selection = [.item(it.id)] } label: { Label("交给 AI", systemImage: Theme.Icon.handToAI) }
                Divider()
            }
        }
        if !items.isEmpty {
            let allDone = items.allSatisfy(\.isDone)
            Button { state.toggleDone(Set(items.map { .item($0.id) }), model: model) } label: {
                Label(allDone ? "移回收件箱" : items.count > 1 ? "\(items.count) 件都标为已处理" : "已处理",
                      systemImage: allDone ? Theme.Icon.undo : Theme.Icon.done)
            }
        }
        if items.count == 1, let it = items.first {
            Divider()
            Button { copy(it.title + "\n" + (it.summary ?? it.preview)) } label: { Label("拷贝内容", systemImage: Theme.Icon.copy) }
            if let url = it.doc.map { $0.link ?? $0.url }, let u = URL(string: url) {
                Button { NSWorkspace.shared.open(u) } label: { Label("在飞书中打开", systemImage: Theme.Icon.openExternal) }
            }
        }
        if agents.count == 1, let ix = agents.first {
            Button { state.open(ix, model: model) } label: { Label("打开对话", systemImage: Theme.Icon.conversation) }
        }
    }

    private func openAll(_ ids: Set<MacInboxSelection>) {
        for id in ids {
            switch id {
            case .item(let i): openWindow(id: MacInboxItemWindow.id, value: i)
            case .agent(let a):
                if let ix = model.pendingInteractions.first(where: { $0.id == a }) { state.open(ix, model: model) }
            }
        }
    }

    private func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
        model.toast = Toast("已拷贝", style: .success)
    }
}

extension View {
    /// The iOS sheets (forms) in a Mac sheet: a size to start from, as Mail's compose sheet has.
    /// Forms in them are grouped (System Settings' look): the Mac's default two-column form put the iPhone's section
    /// rows (presets, pickers) in the label column and cut them off.
    func macSheet() -> some View {
        formStyle(.grouped)
            .frame(minWidth: Theme.Size.macSheetMinWidth, minHeight: Theme.Size.macSheetMinHeight)
    }

    /// A list row that reacts to the pointer (a light fill under it), as Mac lists with row actions do.
    func macHoverRow() -> some View { modifier(MacHoverRow()) }
}

private struct MacHoverRow: ViewModifier {
    @State private var hovering = false

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, Theme.Space.xs)
            .background(hovering ? Theme.Fill.well : Color.clear, in: .rect(cornerRadius: Theme.Radius.control))
            .onHover { hovering = $0 }
    }
}
