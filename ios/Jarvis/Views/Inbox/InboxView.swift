import SwiftUI

// MARK: - 信息

enum InboxSource: String, CaseIterable, Identifiable {
    case all = "全部", lark = "飞书", mail = "邮件"
    var id: String { rawValue }
}

enum InboxEntry: Identifiable {
    case lark(LarkConversation)
    case mail(MailSummary)

    var id: String {
        switch self {
        case .lark(let c): "lark/" + c.id
        case .mail(let m): "mail/" + m.id
        }
    }

    var date: Date {
        switch self {
        case .lark(let c): c.updatedAt ?? .distantPast
        case .mail(let m): m.date ?? .distantPast
        }
    }

    var isUnread: Bool {
        switch self {
        case .lark(let c): c.unread > 0 && !c.muted || c.atMe
        case .mail(let m): !m.seen
        }
    }
}

/// Feishu chats (through lark-connector on example-machine) and e-mail in one place.
struct InboxView: View {
    @Environment(AppModel.self) private var model
    @AppStorage("inbox.source") private var source: InboxSource = .all
    @AppStorage("inbox.unreadOnly") private var unreadOnly = false
    @State private var lark: [LarkConversation] = []
    @State private var larkCursor: String?
    @State private var larkError: String?
    @State private var mails: [MailSummary] = []
    @State private var mailError: String?
    @State private var accounts: [MailAccount] = []
    @State private var loading = false
    @State private var query = ""
    @State private var showAccounts = false
    @State private var composing: ComposeDraft?
    @State private var path = NavigationPath()

    private var entries: [InboxEntry] {
        var out: [InboxEntry] = []
        if source != .mail { out += lark.map { .lark($0) } }
        if source != .lark { out += mails.map { .mail($0) } }
        return out
            .filter { !unreadOnly || $0.isUnread }
            .filter { e in
                guard !query.isEmpty else { return true }
                switch e {
                case .lark(let c): return c.name.localizedCaseInsensitiveContains(query) || c.preview.localizedCaseInsensitiveContains(query)
                case .mail(let m): return [m.subject ?? "", m.from?.display ?? "", m.snippet ?? ""].contains { $0.localizedCaseInsensitiveContains(query) }
                }
            }
            .sorted { $0.date > $1.date }
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                Section {
                    Picker("来源", selection: $source) {
                        ForEach(InboxSource.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
                }
                if let larkError, source != .mail {
                    Label("飞书：\(larkError)", systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                }
                if let mailError, source != .lark {
                    Label("邮件：\(mailError)", systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
                }
                ForEach(entries) { e in
                    switch e {
                    case .lark(let c):
                        NavigationLink(value: c) { LarkRow(conversation: c) }
                    case .mail(let m):
                        NavigationLink(value: m) { MailRow(message: m, showAccount: accounts.count > 1, accounts: accounts) }
                    }
                }
                if source != .mail, larkCursor != nil, !unreadOnly, query.isEmpty {
                    ProgressView().frame(maxWidth: .infinity).task { await loadMoreLark() }
                }
                if entries.isEmpty && !loading {
                    EmptyStateView(symbol: "tray", title: unreadOnly ? "没有未读" : "没有信息",
                                   message: accounts.isEmpty && source != .lark ? "在右上角菜单添加邮箱。" : "下拉刷新试试。")
                        .listRowBackground(Color.clear)
                }
            }
            .listStyle(.plain)
            .navigationTitle("信息")
            .searchable(text: $query, prompt: "搜索会话、发件人、内容")
            .refreshable { await reload() }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button { withAnimation { unreadOnly.toggle() } } label: {
                        Image(systemName: unreadOnly ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
                    }
                    .accessibilityLabel(unreadOnly ? "显示全部" : "只看未读")
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
            .navigationDestination(for: MailSummary.self) { m in MailDetailView(summary: m, accounts: accounts) }
            .sheet(isPresented: $showAccounts, onDismiss: { Task { await reload() } }) { MailAccountsView() }
            .sheet(item: $composing) { d in ComposeView(draft: d, accounts: accounts) }
            .task {
                #if DEBUG
                // Simulator automation: `-lark-connector <feedId>` opens that Feishu chat.
                if path.isEmpty, let id = UserDefaults.standard.string(forKey: "lark-connector") {
                    path.append(LarkConversation(id: id, name: UserDefaults.standard.string(forKey: "lark-connectorName") ?? "飞书", isGroup: true,
                                                 isTopic: UserDefaults.standard.bool(forKey: "lark-connectorTopic"), unread: 0, atMe: false, muted: false, preview: "", isMine: false))
                }
                #endif
                await reload()
            }
        }
    }

    private func reload() async {
        guard let api = model.api else { return }
        loading = true
        defer { loading = false }
        async let l = api.larkConversations()
        async let m = api.mailMessages(account: nil)
        async let a = api.mailAccounts()
        do { let r = try await l; lark = r.conversations; larkCursor = r.nextCursor; larkError = nil } catch { larkError = error.localizedDescription }
        do { mails = try await m.messages; mailError = nil } catch { mailError = error.localizedDescription }
        accounts = (try? await a) ?? accounts
    }

    private func loadMoreLark() async {
        guard let api = model.api, let cursor = larkCursor else { return }
        if let r = try? await api.larkConversations(cursor: cursor) {
            lark += r.conversations.filter { c in !lark.contains { $0.id == c.id } }
            larkCursor = r.nextCursor
        } else {
            larkCursor = nil
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

struct LarkRow: View {
    var conversation: LarkConversation

    var body: some View {
        let c = conversation
        HStack(alignment: .top, spacing: 12) {
            ZStack(alignment: .bottomTrailing) {
                LarkAvatarImage(name: c.name, url: c.avatar, isGroup: c.isGroup, size: 44)
                Image(systemName: "paperplane.circle.fill")
                    .font(.system(size: 15))
                    .foregroundStyle(Color(hex: 0x3370FF))
                    .background(Circle().fill(Color(.systemBackground)))
                    .offset(x: 3, y: 3)
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(c.name).font(.body.weight(c.unread > 0 && !c.muted ? .semibold : .regular)).lineLimit(1)
                    if c.muted { Image(systemName: "bell.slash").font(.caption2).foregroundStyle(.tertiary) }
                    Spacer(minLength: 6)
                    Text(Fmt.relative(c.updatedAt)).font(.caption).foregroundStyle(.secondary).fixedSize()
                }
                HStack(alignment: .top) {
                    (c.atMe ? Text("[有人@我] ").foregroundStyle(Theme.needsYou) : Text(""))
                        + Text(c.isMine ? "我：" + c.preview : c.preview).foregroundStyle(.secondary)
                    Spacer(minLength: 6)
                    if c.unread > 0 {
                        Text(c.unread > 99 ? "99+" : "\(c.unread)")
                            .font(.caption2.weight(.bold).monospacedDigit())
                            .foregroundStyle(.white)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(c.muted ? Color.secondary.opacity(0.5) : Theme.needsYou, in: .capsule)
                            .fixedSize()
                    }
                }
                .font(.subheadline)
                .lineLimit(2)
            }
        }
        .padding(.vertical, 3)
    }
}
