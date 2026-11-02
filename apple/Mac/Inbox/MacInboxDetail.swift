import SwiftUI

/// The detail column: what the list has selected. One item opens as on the iPhone (the Feishu chat at its first new
/// message, the mail, the cloud document's rows); an agent row shows what it waits for with the quick answers;
/// several selected offer to file them all.
struct MacInboxDetail: View {
    @Environment(AppModel.self) private var model
    var state = MacInboxState.shared

    var body: some View {
        let sel = state.selection
        if sel.count > 1 {
            VStack(spacing: Theme.Space.l) {
                EmptyStateView(symbol: Theme.Icon.inbox, title: "已选 \(sel.count) 件", message: "按 E 把它们都标为已处理。")
                Button("全部标为已处理") { state.toggleDone(sel, model: model) }
                    .controlSize(.large)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .navigationTitle("收件箱")
        } else if case .item(let id)? = sel.first, let item = state.item(id) {
            MacInboxItemDetail(item: item, accounts: state.accounts)
                .id(item.id)
        } else if case .agent(let id)? = sel.first, let ix = model.pendingInteractions.first(where: { $0.id == id }) {
            ScrollView {
                InteractionPreviewCard(interaction: ix) { state.open(ix, model: model) }
                    .frame(maxWidth: Theme.Size.readable)
                    .padding(Theme.Space.xl)
            }
            .frame(maxWidth: .infinity)
            .navigationTitle(ix.sessionTitle ?? ix.title)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { state.open(ix, model: model) } label: { Label("打开对话", systemImage: Theme.Icon.conversation) }
                        .help("打开这个 Agent 的对话")
                }
            }
        } else {
            SplitPlaceholder(symbol: Theme.Icon.inbox, title: "选一件事", message: "左边列表里选一件事，详情会在这里打开。")
                .navigationTitle("收件箱")
        }
    }
}

/// One inbox item opened, in the detail column or a window of its own: the AI's summary on top, the content under it,
/// and the item's actions in the toolbar (AI 草拟回复 / 查看草稿, 交给 AI, 已处理 / 移回). Links in it open in the panel at
/// the trailing edge; Feishu pages (a profile, another chat) push in the column's own stack.
struct MacInboxItemDetail: View {
    @Environment(AppModel.self) private var model
    var item: InboxItem
    var accounts: [MailAccount]
    @State private var drafting = false
    @State private var handingOff = false
    @State private var link: LinkTarget?
    @State private var path = NavigationPath()

    private var current: InboxItem { InboxStore.shared.items.first { $0.id == item.id } ?? item }

    var body: some View {
        NavigationStack(path: $path) {
            content
                .feishuRoutes()
                .navigationDestination(for: InboxItem.self) { MacInboxItemDetail(item: $0, accounts: accounts) }
        }
        .safeAreaInset(edge: .top, spacing: 0) { summary }
        .environment(\.feishuOpen) { route in
            // Documents read beside the item; people and chats replace it (back returns).
            if case .doc(let url, _) = route, let u = URL(string: url) { link = LinkTarget(url: u) } else { path.append(route) }
        }
        .macOpensLinksInPanel($link)
        .macLinkPanel($link)
        .navigationTitle(current.title)
        .navigationSubtitle(subtitle)
        .toolbar { toolbar }
        .sheet(isPresented: $drafting) { InboxDraftSheet(item: current, accounts: accounts).macSheet() }
        .sheet(isPresented: $handingOff) { InboxHandOffSheet(item: current).macSheet() }
        .onChange(of: MacInboxState.shared.drafting, initial: true) { _, d in
            if d?.id == item.id { drafting = true; MacInboxState.shared.drafting = nil }
        }
        .onChange(of: MacInboxState.shared.handingOff, initial: true) { _, d in
            if d?.id == item.id { handingOff = true; MacInboxState.shared.handingOff = nil }
        }
        #if DEBUG
        // `-showInboxAction draft|handoff`: open that sheet; `-openInboxLink <url>`: the link panel (screenshots).
        .task {
            switch UserDefaults.standard.string(forKey: "showInboxAction") {
            case "draft": drafting = true
            case "handoff": handingOff = true
            default: break
            }
            if let s = UserDefaults.standard.string(forKey: "openInboxLink"), let u = URL(string: s) { link = LinkTarget(url: u) }
        }
        #endif
    }

    private var subtitle: String {
        if current.doc != nil { return current.doc?.typeName ?? "" }
        if current.isLark { return current.lark?.isGroup == true ? "飞书群聊" : "飞书" }
        return current.accountName
    }

    @ViewBuilder private var content: some View {
        if current.doc != nil {
            InboxDocDetailView(item: current)
        } else if let c = item.larkConversation {
            LarkChatView(conversation: c)
        } else if let m = item.mailSummary {
            MailDetailView(summary: m, accounts: accounts)
        } else {
            EmptyStateView(symbol: Theme.Icon.info, title: item.title, message: item.preview)
        }
    }

    /// The AI's summary of the item, a line over the content (the iPhone's action bar, whose buttons are the toolbar's
    /// here); the AI 分身 at work, when it is.
    @ViewBuilder private var summary: some View {
        let s = current.summary ?? ""
        if !s.isEmpty || current.agentWorking {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                if !s.isEmpty {
                    (Text(Image(systemName: Theme.Icon.ai)).foregroundStyle(Theme.ai) + Text(" " + s))
                        .font(Theme.Font.secondary)
                        .lineLimit(3)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                if current.agentWorking {
                    HStack(spacing: Theme.Space.sm) {
                        ProgressView().controlSize(.mini)
                        Text(current.agent?.state == "queued" ? "AI 分身排队中" : "AI 分身正在处理" + (current.agent?.task.map { "：" + $0 } ?? ""))
                            .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.s)
            .background(.bar)
            .overlay(alignment: .bottom) { Divider() }
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if !current.isDone {
                Button { drafting = true } label: {
                    Label(current.agentDraft == nil ? "AI 草拟回复" : "查看草稿", systemImage: Theme.Icon.draftReply)
                }
                .help(current.agentDraft == nil ? "让 AI 用你的口吻草拟一条回复" : "AI 分身草拟好了回复，看看再发")
                Button { handingOff = true } label: { Label("交给 AI", systemImage: Theme.Icon.handToAI) }
                    .help("把这件事交给 Jarvis 或一个 Agent")
            }
            if current.isDone {
                Button { InboxStore.shared.setDone(current, false) } label: { Label("移回", systemImage: Theme.Icon.undo) }
                    .help("移回收件箱")
            } else {
                Button { MacInboxState.shared.toggleDone([.item(current.id)], model: model) } label: { Label("已处理", systemImage: Theme.Icon.done) }
                    .help("标为已处理（列表里按 E）")
            }
        }
    }
}

/// An inbox item in a window of its own (⏎ or a double click in the list, 「在新窗口打开」).
struct MacInboxItemWindow: View {
    static let id = "inbox-item"
    @Environment(AppModel.self) private var model
    var itemID: String?

    var body: some View {
        Group {
            if let itemID, let item = InboxStore.shared.items.first(where: { $0.id == itemID }) {
                MacInboxItemDetail(item: item, accounts: MacInboxState.shared.accounts)
            } else {
                EmptyStateView(symbol: Theme.Icon.inbox, title: "这件事不在收件箱里了", message: "可能已经过期，回到主窗口看看。")
            }
        }
        .macLook(model)
        .frame(minWidth: Theme.Size.macSheetMinWidth, minHeight: Theme.Size.macSheetMinHeight)
    }
}

/// The inbox page until the window's three columns take the pieces over (docs/mac.md「三栏接口」): the filters, the
/// list and the detail side by side, the list's width dragged and remembered.
struct MacInboxPage: View {
    @AppStorage("mac.inbox.listWidth") private var listWidth: Double = Theme.Size.macInboxListIdeal
    @State private var dragging: Double?

    var body: some View {
        HStack(spacing: 0) {
            // On the window's white, as the lists beside the sections are on the other pages.
            MacInboxFilters()
                .frame(width: Theme.Size.macInboxFiltersWidth)
                .frame(maxHeight: .infinity)
            Divider()
            MacInboxList()
                .frame(width: min(max(dragging ?? listWidth, Theme.Size.macInboxListMin), Theme.Size.macInboxListMax))
            MacPanelResizeHandle { delta in
                dragging = min(max((dragging ?? listWidth) + delta, Theme.Size.macInboxListMin), Theme.Size.macInboxListMax)
            } ended: {
                if let dragging { listWidth = dragging }
                dragging = nil
            }
            MacInboxDetail()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        // The pages' color, as Agents and Jarvis have: the window's own took the wallpaper's tint in dark mode, brown
        // beside the other pages' gray (2026-11-02).
        .background(Theme.pageBackground)
    }
}
