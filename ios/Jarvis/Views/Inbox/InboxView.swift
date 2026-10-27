import SwiftUI

// MARK: - 收件箱

/// The unified inbox (docs/inbox.md): Feishu messages and mail in one list, one row per thing (the new messages in a
/// chat, a mail thread), newest first. No per-chat browsing: what is here is what came in. Filters narrow it by source
/// (飞书, one mail account) and kind (私聊, @我, 群聊); a row swipes away as 已处理, and the done ones stay at the bottom
/// for a few days in case of a slip.
struct InboxView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("inbox.source") private var source = "" // "" all, "feishu", or a mail account id
    @AppStorage("inbox.kind") private var kind = ""     // "" all, dm, mention, group, mail
    @State private var query = ""
    @State private var accounts: [MailAccount] = StateCache.load("mail-accounts", as: [MailAccount].self) ?? []
    /// Sections shown open: tiers (action, fyi, noise) and "done". 已折叠 and 已处理 start closed.
    @State private var expanded: Set<String> = ["action", "fyi"]
    @State private var showAccounts = false
    @State private var composing: ComposeDraft?
    @State private var path = NavigationPath()
    private var store: InboxStore { .shared }

    private func matches(_ it: InboxItem) -> Bool {
        if !source.isEmpty && it.account != source { return false }
        if !kind.isEmpty && !(it.kind == kind || kind == "group" && it.kind == "bot") { return false }
        if query.isEmpty { return true }
        return [it.title, it.sender ?? "", it.preview, it.summary ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
    }

    private var openItems: [InboxItem] { store.items.filter { !$0.isDone && matches($0) } }
    private func openItems(_ tier: InboxItem.Tier) -> [InboxItem] { openItems.filter { $0.triage == tier } }
    private var doneItems: [InboxItem] {
        store.items.filter { $0.isDone && matches($0) }.sorted { ($0.doneAt ?? .distantPast) > ($1.doneAt ?? .distantPast) }
    }

    var body: some View {
        NavigationStack(path: $path) {
            list
                .navigationTitle("收件箱")
                .navigationBarTitleDisplayMode(.inline)
                .searchable(text: $query, prompt: "搜索标题、发件人、内容")
                .toolbar {
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
        List {
            filters
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 8, trailing: 0))
                .listRowSeparator(.hidden)
                .themedRow()
            if let error = store.error, store.items.isEmpty {
                Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                    .themedRow()
            }
            if openItems.isEmpty && !store.loading {
                EmptyStateView(symbol: "checkmark.seal", title: query.isEmpty ? "都处理完了" : "没有匹配的",
                               message: query.isEmpty ? "新的飞书消息和邮件会出现在这里。" : "换个关键词试试。")
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
            // The AI's triage: 需要你 (and what it has not looked at yet), 知悉即可, 已折叠 (noise).
            ForEach(InboxItem.Tier.allCases, id: \.self) { tier in
                let items = openItems(tier)
                if !items.isEmpty {
                    Section {
                        if expanded.contains(tier.rawValue) { ForEach(items) { row($0) } }
                    } header: {
                        sectionHeader(tier.title, count: items.count, key: tier.rawValue,
                                      clear: tier == .action ? nil : (tier == .fyi ? "全部看过" : "全部清除", { store.setDone(items, true) }))
                    }
                }
            }
            if !doneItems.isEmpty {
                Section {
                    if expanded.contains("done") { ForEach(doneItems) { row($0) } }
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

    private func row(_ it: InboxItem) -> some View {
        InboxRow(item: it, showAccount: accounts.count > 1)
            .background { NavigationLink(value: it) { EmptyView() }.opacity(0) }
            .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                if it.isDone {
                    Button { store.setDone(it, false) } label: { Label("移回", systemImage: "arrow.uturn.backward") }.tint(Theme.accent)
                } else {
                    Button { store.setDone(it, true) } label: { Label("已处理", systemImage: "checkmark") }.tint(Theme.idle)
                }
            }
            .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 12))
            .themedRow()
            .alignmentGuide(.listRowSeparatorLeading) { _ in 62 }
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
                ForEach([("mention", "@我"), ("dm", "私聊"), ("group", "群聊"), ("mail", "邮件")], id: \.0) { k, title in
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

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            avatar
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(item.title).font(.body.weight(item.isDone ? .regular : .semibold)).lineLimit(1)
                    if let label = item.kindLabel {
                        Text(label)
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(item.kind == "mention" ? Theme.danger : .secondary)
                            .padding(.horizontal, 5).padding(.vertical, 1)
                            .background((item.kind == "mention" ? Theme.danger : Color.secondary).opacity(0.12), in: .capsule)
                            .lineLimit(1)
                            .fixedSize()
                    }
                    Spacer(minLength: 4)
                    Text(LarkTime.short(item.updatedAt)).font(.caption).foregroundStyle(.secondary).fixedSize()
                }
                HStack(alignment: .top, spacing: 6) {
                    if let summary = item.summary, !summary.isEmpty {
                        VStack(alignment: .leading, spacing: 2) {
                            (Text(Image(systemName: "sparkles")).foregroundStyle(Theme.accent) + Text(" " + summary))
                                .font(.subheadline).foregroundStyle(.primary.opacity(item.isDone ? 0.7 : 0.9)).lineLimit(2)
                            Text(from).font(.caption).foregroundStyle(.tertiary).lineLimit(1)
                        }
                    } else {
                        Text(from).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                    }
                    Spacer(minLength: 0)
                    if item.count > 1 && !item.isDone {
                        Text(item.count > 99 ? "99+" : "\(item.count)")
                            .font(.caption2.weight(.bold).monospacedDigit())
                            .foregroundStyle(.white)
                            .padding(.horizontal, 6).padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.6), in: .capsule)
                    }
                }
                if showAccount && !item.isLark {
                    Text(item.accountName).font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
        }
        .padding(.vertical, 10)
        .opacity(item.isDone ? 0.55 : 1)
        .contentShape(.rect)
    }

    /// Who wrote what (under the AI's summary once it has one).
    private var from: String {
        guard let sender = item.sender, !sender.isEmpty, item.lark?.isGroup != false || !item.isLark else { return item.preview }
        return sender + "：" + item.preview
    }

    private var avatar: some View {
        ZStack(alignment: .bottomTrailing) {
            if item.isLark {
                LarkAvatarImage(name: item.title, url: item.avatar, isGroup: item.lark?.isGroup ?? false, size: 40)
            } else {
                Circle().fill(Theme.accent.opacity(0.15)).frame(width: 40, height: 40)
                    .overlay(Text(String((item.sender ?? "?").prefix(1))).font(.headline).foregroundStyle(Theme.accent))
            }
            Image(systemName: item.isLark ? "bubble.left.fill" : "envelope.fill")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.white)
                .frame(width: 16, height: 16)
                .background(item.isLark ? Color(hex: 0x3370FF) : Theme.accent, in: .circle)
                .overlay(Circle().stroke(Theme.pageBackground, lineWidth: 1.5))
                .offset(x: 3, y: 3)
        }
        .frame(width: 40, height: 40)
    }
}

// MARK: - Detail

/// One inbox item opened: the Feishu chat at its first new message, or the mail; 已处理 in the toolbar.
struct InboxDetailView: View {
    var item: InboxItem
    var accounts: [MailAccount]
    @Environment(\.dismiss) private var dismiss

    private var current: InboxItem { InboxStore.shared.items.first { $0.id == item.id } ?? item }

    var body: some View {
        content
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
