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
            .pageToolbar {
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

/// One inbox item opened, in the detail column or a window of its own: the content (what a plugin draws for its source
/// when one does, placement inbox_item), and beside it a column with the AI's summary and the item's actions as
/// labelled buttons (`MacInboxActions`: AI 草拟回复 / 查看草稿, 交给 AI, 已处理 / 移回; over the item when there is no
/// room). Links in it open in the panel at the trailing edge; Feishu pages (a profile, another chat) push in the
/// column's own stack.
struct MacInboxItemDetail: View {
    @Environment(AppModel.self) private var model
    var item: InboxItem
    var accounts: [MailAccount]
    @State private var drafting = false
    @State private var handingOff = false
    @State private var link: LinkTarget?
    @State private var path = NavigationPath()
    @State private var slot: InboxItemSlotModel

    init(item: InboxItem, accounts: [MailAccount]) {
        self.item = item
        self.accounts = accounts
        _slot = State(initialValue: InboxItemSlotModel(item: item))
    }

    private var current: InboxItem { InboxStore.shared.items.first { $0.id == item.id } ?? item }

    var body: some View {
        NavigationStack(path: $path) {
            InboxItemSlotContent(item: current, slot: slot) { content }
                .feishuRoutes()
                .navigationDestination(for: InboxItem.self) { MacInboxItemDetail(item: $0, accounts: accounts) }
        }
        .environment(\.feishuOpen) { route in
            // Documents read beside the item; people and chats replace it (back returns).
            if case .doc(let url, _) = route, let u = URL(string: url) { link = LinkTarget(url: u) } else { path.append(route) }
        }
        .macOpensLinksInPanel($link)
        .macLinkPanel($link, wide: true)
        // Outside the link panel: the actions stay at the window's edge, the document opens between them and the item.
        // While a document is open they are a row over the item, and the filters and the list fold away (`reading`).
        .macInboxActions(current, reading: link != nil, draft: { drafting = true }, handOff: { handingOff = true })
        .onChange(of: link != nil, initial: true) { _, open in MacInboxState.shared.reading = open }
        .onDisappear { if link != nil { MacInboxState.shared.reading = false } }
        .navigationTitle(current.title)
        .navigationSubtitle(subtitle)
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
        } else if current.connector != nil {
            // GitHub, Telegram…: the thread as the connector draws it, as on iOS and the web (#2611).
            InboxConnectorDetailView(item: current)
        } else if let m = current.moment {
            LarkMomentsPostView(postID: m.postID, pushed: true) // the 公司圈 post and its comments
        } else if let c = item.larkConversation {
            LarkChatView(conversation: c)
        } else if let m = item.mailSummary {
            MailDetailView(summary: m, accounts: accounts)
        } else {
            EmptyStateView(symbol: Theme.Icon.info, title: item.title, message: item.preview)
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
    /// The page's whole width: filters, list and detail.
    @State private var total: CGFloat = 0
    private var state: MacInboxState { .shared }
    private var listFolded: Bool { state.reading && !state.listWhileReading && state.query.isEmpty }

    /// The list's width: the one dragged to, narrowed (down to its least) while the detail would get less than
    /// `macDetailMin` (#1200: in the narrowest window the detail was cut off at the window's edge).
    private var listShown: CGFloat {
        // Reading a document: the list at its least, the room is the document's (#2113).
        let reading = MacInboxState.shared.reading
        let wanted = reading ? Theme.Size.macInboxListMin : min(max(dragging ?? listWidth, Theme.Size.macInboxListMin), Theme.Size.macInboxListMax)
        guard total > 0 else { return wanted }
        let filters = reading ? 0 : Theme.Size.macInboxFiltersWidth + Theme.Size.line
        let room = total - filters - Theme.Size.macResizeHitWidth - Theme.Size.macDetailMin
        return max(Theme.Size.macInboxListMin, min(wanted, room))
    }

    var body: some View {
        HStack(spacing: 0) {
            // On the window's white, as the lists beside the sections are on the other pages.
            // Below the title bar's row: the page's title is drawn there over the filters (#1243). Not the list beside
            // them: the title is not over it. Its section headers scroll with the rows (`MacInlineListHeaders`),
            // without AppKit's separately colored floating group banner (#3080).
            // Folded away while a document is read beside the item: the page is the document's (#2113).
            if !MacInboxState.shared.reading {
                MacInboxFilters()
                    .modifier(MacBelowTitleRow())
                    .frame(width: Theme.Size.macInboxFiltersWidth)
                    .frame(maxHeight: .infinity)
                    .transition(.move(edge: .leading).combined(with: .opacity))
                // Up into the title bar's row, as the shell's divider: the list reaches up there and the filters do not,
                // so a divider stopping under the row left a step between them (#1736, as #830).
                Divider().ignoresSafeArea(.container, edges: .top)
            }
            // Folded too while a document is read (2026-11-05 user: 选 B), back with the actions row's 显示列表, a
            // search, or the document closing. Narrowed to nothing rather than left out: the window's toolbar and
            // search field are the list's, and they stay.
            MacInboxList()
                .frame(width: listFolded ? 0 : listShown)
                .clipped()
                .opacity(listFolded ? 0 : 1)
                .allowsHitTesting(!listFolded)
                .accessibilityHidden(listFolded)
            if !listFolded {
                MacPanelResizeHandle { delta in
                    dragging = min(max((dragging ?? listWidth) + delta, Theme.Size.macInboxListMin), Theme.Size.macInboxListMax)
                } ended: {
                    if let dragging { listWidth = dragging }
                    dragging = nil
                }
            }
            MacInboxDetail()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { total = $0 }
        .animation(Theme.Motion.layout, value: state.reading)
        .animation(Theme.Motion.layout, value: listFolded)
        // The pages' color, as Agents and Persona have: the window's own took the wallpaper's tint in dark mode, brown
        // beside the other pages' gray (2026-11-02).
        .background(Theme.pageBackground)
    }
}
