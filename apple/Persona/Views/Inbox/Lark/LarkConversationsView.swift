import SwiftUI

#if os(iOS)
/// One extra entry, not a second messaging implementation: destinations are the inbox's native chat.
struct LarkConversationsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let config = model.config {
            LarkAccountsPage(config: config).id(config)
        }
    }
}

/// The Feishu accounts on the hub's lark-connector (docs/feishu.md「多账号」): with more than one signed in, chips over the
/// list pick whose chats it shows; each account keeps its own list and cache.
private struct LarkAccountsPage: View {
    @State private var choice: LarkAccountChoice

    init(config: ServerConfig) {
        _choice = State(initialValue: LarkAccountChoice(deps: .live(config)))
    }

    var body: some View {
        LarkConversationsPage(config: choice.config, choice: choice).id(choice.requestAccount)
    }
}

private struct LarkConversationsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.shell) private var shell
    @Environment(\.tabIsActive) private var active
    @State private var store: LarkConversationsModel
    @State private var path: [LarkConversation] = []
    @State private var query = ""
    @State private var pins = LarkPinnedInteraction()
    @State private var canExpandPins = false
    @State private var scroll = LarkConversationScroll()
    var choice: LarkAccountChoice

    init(config: ServerConfig, choice: LarkAccountChoice) {
        self.choice = choice
        _store = State(initialValue: LarkConversationsModel(deps: .live(config, account: choice.requestAccount,
                                                                          accountName: choice.selectedName)))
    }

    private var filtered: [LarkConversation] {
        LarkConversationsModel.listed(store.conversations, pinned: store.pinned, query: query)
    }

    var body: some View {
        NavigationStack(path: $path) {
            ScrollViewReader { proxy in
            List {
                if query.isEmpty, choice.accounts.count > 1 {
                    accountChips
                        .listRowInsets(EdgeInsets(top: Theme.Space.s, leading: 0, bottom: Theme.Space.s, trailing: 0))
                        .listRowSeparator(.hidden)
                        .themedRow()
                }
                if query.isEmpty, !store.pinned.isEmpty {
                    LarkPinnedShelf(conversations: store.pinned, interaction: $pins, expandable: $canExpandPins) { path.append($0) }
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { scroll.shelfChanged($0) }
                        .background(LarkConversationScroll.Finder(scroll: scroll))
                        .listRowInsets(EdgeInsets(top: 0, leading: Theme.Space.page, bottom: 0, trailing: Theme.Space.page))
                        .listRowSeparator(.hidden)
                        .themedRow()
                }
                if store.conversations.isEmpty {
                    empty
                        .listRowSeparator(.hidden)
                } else {
                    if store.failed {
                        Button { Task { await store.refresh() } } label: {
                            Label(store.needsUpgrade ? "更新 Hub 后可同步置顶 · 重新检查" : "暂未更新 · 重试", systemImage: Theme.Icon.retry)
                                .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                        .listRowSeparator(.hidden)
                    }
                    ForEach(filtered) { conversation in
                        NavigationLink(value: conversation) { row(conversation) }
                            .accessibilityIdentifier("feishu-chat-" + conversation.id)
                            .id(conversation.id)
                            .onGeometryChange(for: CGFloat.self) { $0.frame(in: .global).minY } action: { scroll.reader.row(conversation.id, top: $0) }
                            .onDisappear { scroll.reader.gone(conversation.id) }
                            .listRowInsets(EdgeInsets(top: 0, leading: Theme.Space.page, bottom: 0, trailing: Theme.Space.page))
                            .themedRow()
                    }
                    if store.nextCursor != nil, query.isEmpty {
                        Group {
                            if store.pageFailed {
                                Button("重试加载") { Task { await store.more() } }.lineLimit(1)
                            } else {
                                ProgressView().frame(maxWidth: .infinity)
                                    .task(id: store.nextCursor) { await store.more() }
                            }
                        }
                        .listRowSeparator(.hidden)
                    }
                }
            }
            .listStyle(.plain)
            .accessibilityIdentifier("feishu-conversations")
            .onAppear {
                store.willChange = { [weak scroll] in scroll?.reader.willChange() }
                scroll.reader.reveal = { id, at in proxy.scrollTo(id, anchor: UnitPoint(x: 0.5, y: at)) }
            }
            .onScrollGeometryChange(for: Double.self) { Double($0.contentOffset.y + $0.contentInsets.top) } action: { _, offset in
                updatePins(offset)
            }
            .onScrollPhaseChange { old, phase, context in
                let offset = Double(context.geometry.contentOffset.y + context.geometry.contentInsets.top)
                if phase != .idle { scroll.reader.cancel() }
                store.holdScrolling(phase != .idle)
                if (phase == .tracking || phase == .interacting) && old != .tracking && old != .interacting {
                    pins.beginDrag(scrollOffset: offset, shelfPassed: shelfPassed(offset), canExpand: canExpandPins && query.isEmpty)
                }
                if old == .interacting && phase != .interacting && phase != .tracking {
                    updatePins(offset)
                    withAnimation(Theme.Motion.standard) { _ = pins.endDrag(canExpand: canExpandPins && query.isEmpty) }
                } else if old == .tracking && phase != .interacting { pins.cancelDrag() }
            }
            .themedPage()
            .searchable(text: $query, prompt: "搜索已加载的会话")
            .navigationTitle("消息")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(shell == nil ? .visible : .hidden, for: .navigationBar)
            .safeAreaInset(edge: .top, spacing: 0) {
                if let shell { topBar(shell) }
            }
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { openConnectionSettings() } label: { Label("飞书设置", systemImage: Theme.Icon.settings) }
                }
            }
            }
            .navigationDestination(for: LarkConversation.self) { LarkChatView(conversation: $0, messageComposer: true) }
        }
        .onChange(of: store.accountID) { _, _ in path = []; pins = LarkPinnedInteraction() }
        .onChange(of: query) { _, _ in pins = LarkPinnedInteraction(); scroll.resetShelf() }
        .onChange(of: store.pinned.isEmpty) { _, empty in
            if empty { pins = LarkPinnedInteraction(); scroll.resetShelf() }
        }
        .onDisappear { pins.cancelDrag(); store.holdScrolling(false); store.willChange = {} }
        .onChange(of: path.isEmpty, initial: true) { _, root in shell?.feishuAtRoot = root }
        .tabActiveTask(id: model.connection.shown == .online) {
            let changes = LarkSync.shared.changes()
            async let accounts: Void = choice.load()
            await store.refresh()
            await accounts
            for await _ in changes {
                guard !Task.isCancelled else { return }
                async let accounts: Void = choice.load()
                await store.refresh()
                await accounts
            }
        }

    }

    /// One chip per signed-in account, its unread count beside the name; the chosen one is filled.
    private var accountChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: Theme.Space.s) {
                ForEach(choice.accounts) { account in
                    FilterChip(title: account.title, count: account.unread > 0 ? account.unread : nil,
                               on: account.id == choice.selectedID) { choice.select(account.id) }
                        .accessibilityIdentifier("feishu-account-\(account.id)")
                }
            }
            .padding(.horizontal, Theme.Space.page)
        }
        .accessibilityLabel("飞书账号")
    }

    private func shelfPassed(_ offset: Double) -> Bool {
        scroll.shelfHeight > 0 && offset >= Double(scroll.shelfHeight) - LarkPinnedInteraction.topTolerance
    }

    private func updatePins(_ offset: Double) {
        let expanded = pins.isExpanded
        pins.update(scrollOffset: offset, shelfPassed: shelfPassed(offset))
        if expanded && !pins.isExpanded { scroll.compensateCollapse = true }
    }

    @ViewBuilder private var empty: some View {
        if store.loading {
            ProgressView("加载中…").frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xxl)
        } else if store.status?.configured == false {
            notice("还没有连接飞书", detail: "连接 Hub 上的飞书账号后，会话会显示在这里。", action: "连接设置") { openConnectionSettings() }
        } else if store.needsUpgrade {
            notice("需要更新 Hub", detail: "更新后可显示置顶会话，并同步当前飞书账号。", action: "重新检查") { Task { await store.refresh() } }
        } else if store.failed {
            notice("暂时无法加载会话", detail: "请检查飞书连接，稍后重试。", action: "重试") { Task { await store.refresh() } }
        } else {
            Text("暂无会话").font(Theme.Font.body).foregroundStyle(.secondary)
                .frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xxl)
        }
    }

    private func notice(_ title: String, detail: String, action: String, perform: @escaping () -> Void) -> some View {
        VStack(spacing: Theme.Space.m) {
            Text(title).font(Theme.Font.heading)
            Text(detail).font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
            Button(action, action: perform).lineLimit(1).themedButton()
        }
        .frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xxl)
    }

    private func row(_ conversation: LarkConversation) -> some View {
        HStack(spacing: Theme.Space.m) {
            LarkAvatarImage(name: conversation.name, url: conversation.avatar, isGroup: conversation.isGroup, size: Theme.Size.avatarXL)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.s) {
                    Text(conversation.name).font(Theme.Font.bodyStrong).foregroundStyle(.primary).lineLimit(1)
                    Spacer(minLength: 0)
                    if let date = conversation.updatedAt {
                        Text(Fmt.relative(date)).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                HStack(spacing: Theme.Space.s) {
                    Text(conversation.preview).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                    Spacer(minLength: 0)
                    if conversation.badge > 0 { CountBadge(count: conversation.badge) }
                    if conversation.muted { Image(systemName: Theme.Icon.muted).font(Theme.Font.caption).foregroundStyle(.secondary) }
                }
            }
        }
        .padding(.vertical, Theme.Space.s)
    }

    private func openConnectionSettings() {
        model.settingsPath = NavigationPath([SettingsPage.feishu])
        if shell != nil { model.shellSettings = true }
        else { model.selectedTab = .settings }
    }

    private func topBar(_ shell: ShellState) -> some View {
        @Bindable var model = model
        return HStack(alignment: .top, spacing: Theme.Space.s) {
            ShellPersonaButton(id: "feishu-sidebar") { shell.drawer.open() }
            Spacer(minLength: 0)
            ViewThatFits(in: .horizontal) {
                ShellSwitch(tab: $model.selectedTab) { EmptyView() } workMenu: { EmptyView() }
                ShellSwitch(tab: $model.selectedTab) { EmptyView() } workMenu: { EmptyView() }
                    .dynamicTypeSize(...Theme.Shell.compactSwitchLargestTypeSize)
            }
            Spacer(minLength: 0)
            ShellCircleButton(symbol: Theme.Icon.settings, label: "飞书设置", id: "feishu-settings") { openConnectionSettings() }
        }
        .padding(.horizontal, Theme.Space.l).padding(.top, Theme.Space.xxs)
        .shellTopBarScaled().modifier(ShellBarAtRest(page: 2))
    }
}
#endif
