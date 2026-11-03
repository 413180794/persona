import SwiftUI

#if os(iOS)
/// One extra entry, not a second messaging implementation: destinations are the inbox's native chat.
struct LarkConversationsView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let config = model.config {
            LarkConversationsPage(config: config).id(config)
        }
    }
}

private struct LarkConversationsPage: View {
    @Environment(AppModel.self) private var model
    @Environment(\.shell) private var shell
    @Environment(\.tabIsActive) private var active
    @State private var store: LarkConversationsModel
    @State private var path: [LarkConversation] = []
    @State private var query = ""

    init(config: ServerConfig) {
        _store = State(initialValue: LarkConversationsModel(deps: .live(config)))
    }

    private var filtered: [LarkConversation] {
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return q.isEmpty ? store.conversations : store.conversations.filter {
            $0.name.localizedStandardContains(q) || $0.preview.localizedStandardContains(q)
        }
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                if store.conversations.isEmpty {
                    empty
                        .listRowSeparator(.hidden)
                } else {
                    if store.failed {
                        Button { Task { await store.refresh() } } label: {
                            Label("暂未更新 · 重试", systemImage: Theme.Icon.retry)
                                .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                        .listRowSeparator(.hidden)
                    }
                    ForEach(filtered) { conversation in
                        NavigationLink(value: conversation) { row(conversation) }
                            .accessibilityIdentifier("feishu-chat-" + conversation.id)
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
            .navigationDestination(for: LarkConversation.self) { LarkChatView(conversation: $0) }
        }
        .onChange(of: path.isEmpty, initial: true) { _, root in shell?.feishuAtRoot = root }
        .tabActiveTask(id: model.connection.shown == .online) {
            await store.refresh()
            for await _ in LarkSync.shared.changes() {
                guard !Task.isCancelled else { return }
                await store.refresh()
            }
        }

    }

    @ViewBuilder private var empty: some View {
        if store.loading {
            ProgressView("加载中…").frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xxl)
        } else if store.status?.configured == false {
            notice("还没有连接飞书", detail: "连接 Hub 上的飞书账号后，会话会显示在这里。", action: "连接设置") { openConnectionSettings() }
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
            LarkAvatarImage(name: conversation.name, url: conversation.avatar, isGroup: conversation.isGroup, size: Theme.Size.avatarL)
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
            ShellCircleButton(symbol: Theme.Icon.settings, label: "飞书设置", id: "feishu-settings") { openConnectionSettings() }
        }
        .overlay(alignment: .top) {
            ShellSwitch(tab: $model.selectedTab) { EmptyView() } workMenu: { EmptyView() }
        }
        .padding(.horizontal, Theme.Space.l).padding(.top, Theme.Space.xxs)
        .shellTopBarScaled().modifier(ShellBarAtRest(page: 2))
    }
}
#endif
