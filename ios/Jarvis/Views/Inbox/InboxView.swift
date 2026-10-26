import SwiftUI

// MARK: - 信息

/// Feishu's 消息 page (through lark-connector next to the hub): pinned chats across the top, 消息 / 未读 / 标记 tabs,
/// a 分组 drawer with 已完成, 标记 and every label, Feishu's swipe and long-press actions; e-mail is one more group.
struct InboxView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("inbox.group") private var groupKey = "messages"
    @AppStorage("inbox.tab") private var tab: LarkTab = .all
    @State private var chats: [LarkConversation] = []
    @State private var cursor: String?
    @State private var pinned: [LarkConversation] = []
    @State private var labels: [LarkLabel] = []
    @State private var unreadTotal = 0
    @State private var flaggedChats: [LarkConversation] = []
    @State private var flaggedMessages: [LarkFlaggedMessage] = []
    @State private var larkError: String?
    @State private var mails: [MailSummary] = []
    @State private var mailError: String?
    @State private var accounts: [MailAccount] = []
    @State private var loading = false
    @State private var loadedKey = ""
    @State private var query = ""
    @State private var drawer = false
    @State private var showAccounts = false
    @State private var composing: ComposeDraft?
    @State private var editingLabels: LarkConversation?
    @State private var path = NavigationPath()

    private var group: LarkGroup { LarkGroup(key: groupKey) }
    private var groupBinding: Binding<LarkGroup> { Binding(get: { group }, set: { groupKey = $0.key }) }
    /// What the list currently shows; a change reloads it.
    private var listKey: String { group == .messages ? "messages/\(tab.rawValue)" : group.key }
    private var showsFlags: Bool { group == .flagged || (group == .messages && tab == .flagged) }

    private var scope: String {
        switch group {
        case .done: "done"
        case .label(let id, _): "label:" + id
        default: tab == .unread ? "unread" : "chats"
        }
    }

    private func matches(_ c: LarkConversation) -> Bool {
        query.isEmpty || c.name.localizedCaseInsensitiveContains(query) || c.preview.localizedCaseInsensitiveContains(query)
    }

    /// The rows under the tabs. In 消息 the pinned chats sit in the strip above, so the list leaves them out.
    private var rows: [LarkConversation] {
        if showsFlags { return flaggedChats.filter(matches) }
        var list = chats
        if group == .messages && tab == .unread { list = list.filter { $0.badge > 0 && !$0.muted && !$0.isDone } }
        if group == .messages && tab == .all {
            if query.isEmpty { list.removeAll { $0.isPinned } } else { list += pinned.filter { p in !list.contains { $0.id == p.id } } }
        }
        return list.filter(matches)
    }

    var body: some View {
        NavigationStack(path: $path) {
            Group {
                if group == .mail { mailList } else { larkList }
            }
            .navigationTitle(group.title)
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $query, prompt: group == .mail ? "搜索发件人、主题、内容" : "搜索会话")
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button { withAnimation(.snappy(duration: 0.28)) { drawer = true } } label: { Image(systemName: "line.3.horizontal") }
                        .accessibilityLabel("分组")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { path.append(FeishuRoute.search(query: nil, feedID: nil)) } label: { Image(systemName: "magnifyingglass") }
                        .accessibilityLabel("搜索")
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button { composing = ComposeDraft(accountID: accounts.first?.id ?? "") } label: { Label("写邮件", systemImage: "square.and.pencil") }
                            .disabled(accounts.isEmpty)
                        Button { showAccounts = true } label: { Label("管理邮箱", systemImage: "envelope.badge") }
                    } label: { Image(systemName: "ellipsis.circle") }
                }
            }
            .navigationDestination(for: LarkConversation.self) { c in LarkChatView(conversation: c) }
            .feishuRoutes()
            .navigationDestination(for: MailSummary.self) { m in MailDetailView(summary: m, accounts: accounts) }
            .sheet(isPresented: $showAccounts, onDismiss: { Task { await reloadMail() } }) { MailAccountsView() }
            #if DEBUG
            // `-addMail YES`: open 管理邮箱 › 添加邮箱 for screenshots.
            .onAppear { if UserDefaults.standard.bool(forKey: "addMail") { showAccounts = true } }
            #endif
            .sheet(item: $composing) { d in ComposeView(draft: d, accounts: accounts) }
            .sheet(item: $editingLabels) { c in LarkLabelPicker(conversation: c) { Task { await reload(quiet: true) } } }
            .task(id: listKey) {
                // Paint what was last seen for this list, then refresh quietly (a spinner only when nothing is known).
                if loadedKey != listKey { restore(listKey) }
                await reload(quiet: !chats.isEmpty || !flaggedChats.isEmpty)
                #if DEBUG
                if UserDefaults.standard.bool(forKey: "inboxDrawer") { drawer = true }
                // Simulator automation: `-lark-connector <feedId>` opens that Feishu chat (as listed, so with its unread state).
                if path.isEmpty, let id = UserDefaults.standard.string(forKey: "lark-connector") {
                    var c = (pinned + chats).first { $0.id == id } ?? LarkConversation(id: id, name: UserDefaults.standard.string(forKey: "lark-connectorName") ?? "飞书",
                                                                                       isGroup: true, isTopic: UserDefaults.standard.bool(forKey: "lark-connectorTopic"))
                    // `-lark-connectorAt <position>` opens it at that message.
                    if let at = UserDefaults.standard.string(forKey: "lark-connectorAt") { c.focus = at }
                    path.append(c)
                }
                #endif
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(30))
                    if path.isEmpty { await reload(quiet: true) }
                }
            }
            .onChange(of: path.count) { old, new in
                // Back from a chat: its unread count and place in the list have changed.
                if new < old { Task { await reload(quiet: true) } }
            }
        }
        .environment(\.feishuOpen) { path.append($0) }
        .feishuHandlesLinks { path.append($0) }
        .overlay { drawerOverlay }
    }

    // MARK: Lists

    private var larkList: some View {
        List {
            if group == .messages {
                if tab == .all && query.isEmpty && !pinned.isEmpty {
                    LarkPinnedStrip(pinned: pinned) { path.append($0) }
                        .listRowInsets(EdgeInsets())
                        .themedRow()
                        .listRowSeparator(.hidden)
                }
                Picker("视图", selection: $tab) {
                    ForEach(LarkTab.allCases) { t in
                        Text(t == .unread && unreadTotal > 0 ? "未读 \(unreadTotal)" : t.rawValue).tag(t)
                    }
                }
                .pickerStyle(.segmented)
                .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                .themedRow()
                .listRowSeparator(.hidden)
            }
            if let larkError {
                Label(larkError, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
            }
            ForEach(rows) { c in
                // Feishu rows have no disclosure chevron: the link sits invisibly behind the row.
                LarkFeedRow(conversation: c)
                    .background { NavigationLink(value: c) { EmptyView() }.opacity(0) }
                    .modifier(LarkFeedRowActions(conversation: c, perform: perform, editLabels: { editingLabels = $0 }))
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
                    .themedRow()
                    .alignmentGuide(.listRowSeparatorLeading) { _ in 62 }
            }
            if showsFlags {
                ForEach(flaggedMessages.filter { query.isEmpty || $0.chatName.localizedCaseInsensitiveContains(query) || $0.message.text.localizedCaseInsensitiveContains(query) }) { f in
                    LarkFlaggedMessageRow(item: f)
                        .background {
                            NavigationLink(value: LarkConversation(id: f.feedID, name: f.chatName, avatar: f.chatAvatar, isGroup: true, focus: f.message.position)) { EmptyView() }
                                .opacity(0)
                        }
                    .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
                    .themedRow()
                }
            }
            if cursor != nil && !showsFlags && query.isEmpty {
                ProgressView().frame(maxWidth: .infinity).listRowSeparator(.hidden).task { await loadMore() }
            }
            if rows.isEmpty && (!showsFlags || flaggedMessages.isEmpty) && !loading && larkError == nil {
                EmptyStateView(symbol: emptySymbol, title: emptyTitle, message: query.isEmpty ? "下拉刷新试试。" : "换个关键词试试。")
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
        }
        .listStyle(.plain)
        .themedPage()
        .refreshable { await reload(quiet: true) }
        .overlay { if loading && rows.isEmpty { ProgressView() } }
    }

    private var emptyTitle: String {
        if showsFlags { return "没有标记" }
        switch group {
        case .done: return "没有已完成的会话"
        case .label: return "这个标签下没有会话"
        default: return tab == .unread ? "没有未读" : "没有消息"
        }
    }

    private var emptySymbol: String {
        if showsFlags { return "flag" }
        return group == .done ? "checkmark.circle" : (tab == .unread ? "checkmark.seal" : "tray")
    }

    private var mailList: some View {
        let list = mails.filter { m in query.isEmpty || [m.subject ?? "", m.from?.display ?? "", m.snippet ?? ""].contains { $0.localizedCaseInsensitiveContains(query) } }
        return List {
            if let mailError { Label(mailError, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger) }
            ForEach(list) { m in
                NavigationLink(value: m) { MailRow(message: m, showAccount: accounts.count > 1, accounts: accounts) }
            }
            if list.isEmpty && !loading {
                EmptyStateView(symbol: "envelope", title: "没有邮件", message: accounts.isEmpty ? "在右上角菜单添加邮箱。" : "下拉刷新试试。")
                    .listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .themedPage()
        .refreshable { await reloadMail() }
        .task {
            if mails.isEmpty { mails = InboxCache.shared.snapshot.mails; accounts = InboxCache.shared.snapshot.accounts }
            await reloadMail() // quietly: the cached list stays until the fresh one replaces it
        }
    }

    // MARK: Drawer

    @ViewBuilder private var drawerOverlay: some View {
        if drawer {
            ZStack(alignment: .leading) {
                Color.black.opacity(0.32).ignoresSafeArea()
                    .onTapGesture { closeDrawer() }
                    .transition(.opacity)
                LarkGroupsDrawer(group: groupBinding, labels: labels, unread: unreadTotal, close: closeDrawer)
                    .frame(width: 300)
                    .frame(maxHeight: .infinity, alignment: .top)
                    .background(Theme.pageBackground.ignoresSafeArea())
                    .transition(.move(edge: .leading))
                    .gesture(DragGesture().onEnded { if $0.translation.width < -60 { closeDrawer() } })
            }
            .zIndex(1)
        }
    }

    private func closeDrawer() { withAnimation(.snappy(duration: 0.25)) { drawer = false } }

    // MARK: Loading

    private func restore(_ key: String) {
        let c = InboxCache.shared.snapshot
        chats = c.lists[key] ?? []
        cursor = c.cursors[key]
        if pinned.isEmpty { pinned = c.pinned }
        if labels.isEmpty { labels = c.labels }
        if unreadTotal == 0 { unreadTotal = c.unread }
        if flaggedChats.isEmpty { flaggedChats = c.flaggedChats; flaggedMessages = c.flaggedMessages }
        if mails.isEmpty { mails = c.mails }
        if accounts.isEmpty { accounts = c.accounts }
        if c.lists[key] != nil { loadedKey = key }
    }

    /// Writes what is on screen back to the cache.
    private func remember() {
        let key = listKey, chats = chats, cursor = cursor, pinned = pinned, labels = labels, unread = unreadTotal
        let fc = flaggedChats, fm = flaggedMessages, mails = mails, accounts = accounts
        InboxCache.shared.update { s in
            if !chats.isEmpty { s.lists[key] = Array(chats.prefix(200)) }
            s.cursors[key] = cursor
            s.pinned = pinned
            s.labels = labels
            s.unread = unread
            s.flaggedChats = fc
            s.flaggedMessages = fm
            s.mails = Array(mails.prefix(100))
            s.accounts = accounts
        }
    }

    private func reload(quiet: Bool) async {
        guard let api = model.api, group != .mail else { return }
        let key = listKey
        if !quiet { loading = true }
        defer { loading = false }
        async let labelsTask = api.larkLabels()
        async let unreadTask = api.larkUnreadCount()
        async let pinnedTask = group == .messages ? api.larkPinned() : []
        do {
            if showsFlags {
                let f = try await api.larkFlags()
                flaggedChats = f.conversations
                flaggedMessages = f.messages
                cursor = nil
            } else {
                let r = try await api.larkConversations(scope: scope)
                guard key == listKey else { return }
                if quiet && loadedKey == key && chats.count > r.conversations.count {
                    // A quiet refresh keeps the pages already scrolled through below the first.
                    let fresh = Set(r.conversations.map(\.id))
                    chats = r.conversations + chats.dropFirst(r.conversations.count).filter { !fresh.contains($0.id) }
                } else {
                    chats = r.conversations
                    cursor = r.nextCursor
                }
            }
            loadedKey = key
            larkError = nil
        } catch {
            if !(error is CancellationError) { larkError = error.shown }
        }
        if let p = try? await pinnedTask, group == .messages { pinned = p }
        if let l = try? await labelsTask { labels = l }
        if let n = try? await unreadTask { unreadTotal = n }
        remember()
    }

    private func loadMore() async {
        guard let api = model.api, let c = cursor else { return }
        guard let r = try? await api.larkConversations(scope: scope, cursor: c) else { cursor = nil; return }
        chats += r.conversations.filter { n in !chats.contains { $0.id == n.id } }
        cursor = r.nextCursor
        remember()
    }

    private func reloadMail() async {
        guard let api = model.api else { return }
        async let m = api.mailMessages(account: nil)
        async let a = api.mailAccounts()
        do { mails = try await m.messages; mailError = nil } catch { mailError = error.shown }
        accounts = (try? await a) ?? accounts
        remember()
    }

    /// Applies a chat action at once, then asks lark-connector; a failure reloads the true state.
    private func perform(_ c: LarkConversation, _ action: LarkFeedAction) {
        withAnimation(.snappy) {
            for i in chats.indices where chats[i].id == c.id { action.apply(to: &chats[i]) }
            for i in pinned.indices where pinned[i].id == c.id { action.apply(to: &pinned[i]) }
            for i in flaggedChats.indices where flaggedChats[i].id == c.id { action.apply(to: &flaggedChats[i]) }
            switch action {
            case .done(let v):
                // 完成 moves the chat out of 消息 / a label into 已完成, and back.
                if (group == .done) != v { chats.removeAll { $0.id == c.id } }
            case .pin(let v):
                if v, var p = chats.first(where: { $0.id == c.id }) { p.pinned = true; pinned.insert(p, at: 0) }
                if !v { pinned.removeAll { $0.id == c.id } }
            case .flag(false):
                if showsFlags { flaggedChats.removeAll { $0.id == c.id } }
            default: break
            }
        }
        Haptics.tap()
        remember()
        Task {
            guard let api = model.api else { return }
            do {
                if case .unread(let v) = action { try await api.larkSetUnread(c.id, unread: v) }
                else if let s = action.settings { try await api.larkUpdate(c.id, s) }
                AppModel.shared.toast = Toast(action.done, style: .success)
                await reload(quiet: true)
            } catch {
                model.show(error)
                await reload(quiet: true)
            }
        }
    }
}

struct LarkAvatar: View {
    var name: String
    var isGroup: Bool
    var size: CGFloat = 44

    var body: some View {
        let palette: [Color] = [Color(hex: 0x3370FF), Color(hex: 0x00B42A), Color(hex: 0xFF7D00), Color(hex: 0x7B61FF), Color(hex: 0x14C9C9), Color(hex: 0xF54A45)]
        let color = palette[abs(name.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }) % palette.count]
        Circle()
            .fill(color.gradient)
            .frame(width: size, height: size)
            .overlay {
                if isGroup {
                    Image(systemName: "person.2.fill").font(.system(size: size * 0.38)).foregroundStyle(.white)
                } else {
                    Text(String(name.prefix(1))).font(.system(size: size * 0.42, weight: .semibold)).foregroundStyle(.white)
                }
            }
    }
}

/// The inbox's last known lists (per 消息 tab / group), pinned chats, labels, flags and mail, kept in memory and on
/// disk: switching to 信息, a cold start or no network show them at once; refreshes replace them quietly.
@MainActor
final class InboxCache {
    static let shared = InboxCache()

    nonisolated struct Snapshot: Codable, Sendable {
        var lists: [String: [LarkConversation]] = [:]
        var cursors: [String: String] = [:]
        var pinned: [LarkConversation] = []
        var labels: [LarkLabel] = []
        var unread = 0
        var flaggedChats: [LarkConversation] = []
        var flaggedMessages: [LarkFlaggedMessage] = []
        var mails: [MailSummary] = []
        var accounts: [MailAccount] = []
    }

    var snapshot: Snapshot
    private let writer = Debouncer(.seconds(1))

    private init() { snapshot = StateCache.load("inbox", as: Snapshot.self) ?? Snapshot() }

    func update(_ change: (inout Snapshot) -> Void) {
        change(&snapshot)
        let snap = snapshot
        writer.run { StateCache.save("inbox", snap) }
    }
}

