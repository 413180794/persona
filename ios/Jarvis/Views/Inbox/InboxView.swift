import SwiftUI

// MARK: - Feishu (lark-connector) wire types

nonisolated struct LarkConversation: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String
    var isGroup: Bool
    var unread: Int
    var atMe: Bool
    var muted: Bool
    var preview: String
    var isMine: Bool
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, unread, muted, preview
        case isGroup = "is_group", atMe = "at_me", isMine = "is_mine", updatedAt = "updated_at"
    }
}

nonisolated struct LarkMessage: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var position: String
    var senderName: String
    var isMine: Bool
    var type: String
    var text: String
    var createdAt: Date?
    var recalled: Bool
    var replyCount: Int

    enum CodingKeys: String, CodingKey {
        case id, position, type, text, recalled
        case senderName = "sender_name", isMine = "is_mine", createdAt = "created_at", replyCount = "reply_count"
    }
}

extension APIClient {
    struct LarkConversations: Decodable {
        var conversations: [LarkConversation]
        var nextCursor: String?
        enum CodingKeys: String, CodingKey { case conversations; case nextCursor = "next_cursor" }
    }
    struct LarkMessages: Decodable {
        var messages: [LarkMessage]
        var hasOlder: Bool
        enum CodingKeys: String, CodingKey { case messages; case hasOlder = "has_older" }
    }
    func larkConversations(cursor: String? = nil) async throws -> LarkConversations {
        try await get("/api/v1/lark/conversations", query: ["cursor": cursor ?? "", "limit": "60"])
    }
    func larkMessages(_ id: String, before: String? = nil) async throws -> LarkMessages {
        try await get("/api/v1/lark/conversations/\(id)/messages", query: ["before": before ?? "", "limit": "40"])
    }
    struct LarkText: Encodable { var text: String }
    func larkSend(_ id: String, text: String) async throws { try await post("/api/v1/lark/conversations/\(id)/messages", body: LarkText(text: text)) }
    struct LarkRead: Encodable { var position: String }
    func larkRead(_ id: String, position: String) async throws { try await post("/api/v1/lark/conversations/\(id)/read", body: LarkRead(position: position)) }
}

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
        NavigationStack {
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
            .task { await reload() }
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
                LarkAvatar(name: c.name, isGroup: c.isGroup)
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

/// One Feishu chat: latest messages, older on demand, reply, and hand it to Jarvis.
struct LarkChatView: View {
    @Environment(AppModel.self) private var model
    var conversation: LarkConversation
    @State private var messages: [LarkMessage] = []
    @State private var hasOlder = false
    @State private var error: String?
    @State private var text = ""
    @State private var sending = false
    @State private var position = ScrollPosition(edge: .bottom)

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 10) {
                if hasOlder {
                    Button("加载更早的消息") { Task { await loadOlder() } }
                        .font(.caption.weight(.medium))
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 6)
                }
                if let error { Label(error, systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(Theme.danger) }
                ForEach(Array(messages.enumerated()), id: \.element.id) { i, m in
                    bubble(m, showName: i == 0 || messages[i - 1].senderName != m.senderName)
                }
            }
            .padding(16)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .safeAreaInset(edge: .bottom) {
            HStack(alignment: .bottom, spacing: 8) {
                TextField("回复 \(conversation.name)", text: $text, axis: .vertical)
                    .lineLimit(1...5)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 20, style: .continuous))
                Button { Task { await send() } } label: {
                    Group { if sending { ProgressView().tint(.white) } else { Image(systemName: "arrow.up").font(.body.weight(.bold)) } }
                        .frame(width: 36, height: 36)
                }
                .buttonStyle(.glassProminent)
                .clipShape(.circle)
                .disabled(sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
        }
        .navigationTitle(conversation.name)
        .navigationSubtitle("飞书")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await askJarvis() } } label: { Label("让 Jarvis 总结", systemImage: "sparkles") }
            }
        }
        .task {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                await refreshLatest()
            }
        }
    }

    @ViewBuilder private func bubble(_ m: LarkMessage, showName: Bool) -> some View {
        if m.type == "SYSTEM" {
            Text(m.text).font(.caption).foregroundStyle(.secondary).frame(maxWidth: .infinity)
        } else {
            VStack(alignment: m.isMine ? .trailing : .leading, spacing: 3) {
                if showName && !m.isMine {
                    Text(m.senderName).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                }
                Text(m.recalled ? "（消息已撤回）" : (m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text))
                    .font(.body)
                    .foregroundStyle(m.isMine ? .white : .primary)
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(m.isMine ? AnyShapeStyle(Color(hex: 0x3370FF).gradient) : AnyShapeStyle(Color.primary.opacity(0.06)),
                                in: .rect(cornerRadius: 16, style: .continuous))
                    .contextMenu { Button { UIPasteboard.general.string = m.text } label: { Label("拷贝", systemImage: "doc.on.doc") } }
                HStack(spacing: 6) {
                    if m.type != "TEXT" && m.type != "POST" { Text(m.type.lowercased()).font(.caption2.monospaced()) }
                    if m.replyCount > 0 { Label("\(m.replyCount)", systemImage: "arrowshape.turn.up.left").font(.caption2) }
                    Text(m.createdAt.map { $0.formatted(date: .omitted, time: .shortened) } ?? "").font(.caption2)
                }
                .foregroundStyle(.tertiary)
            }
            .frame(maxWidth: .infinity, alignment: m.isMine ? .trailing : .leading)
            .padding(m.isMine ? .leading : .trailing, 48)
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            let r = try await api.larkMessages(conversation.id)
            messages = r.messages
            hasOlder = r.hasOlder
            error = nil
            if let last = messages.last { try? await api.larkRead(conversation.id, position: last.position) }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func refreshLatest() async {
        guard let api = model.api, let r = try? await api.larkMessages(conversation.id) else { return }
        let fresh = r.messages.filter { m in !messages.contains { $0.id == m.id } }
        guard !fresh.isEmpty else { return }
        withAnimation(.snappy) { messages += fresh }
        position.scrollTo(edge: .bottom)
        if let last = messages.last { try? await api.larkRead(conversation.id, position: last.position) }
    }

    private func loadOlder() async {
        guard let api = model.api, let first = messages.first,
              let r = try? await api.larkMessages(conversation.id, before: first.position) else { return }
        messages = r.messages.filter { m in !messages.contains { $0.id == m.id } } + messages
        hasOlder = r.hasOlder
    }

    private func send() async {
        guard let api = model.api else { return }
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        sending = true
        defer { sending = false }
        do {
            try await api.larkSend(conversation.id, text: t)
            text = ""
            Haptics.success()
            try? await Task.sleep(for: .milliseconds(800))
            await refreshLatest()
        } catch {
            model.show(error)
        }
    }

    private func askJarvis() async {
        guard let api = model.api else { return }
        let prompt = "读取我飞书会话「\(conversation.name)」（lark-connector feedId \(conversation.id)）最近的消息，总结要点，列出需要我回复或处理的事，并给出建议的回复。"
        do {
            let s = try await api.startConversation(prompt: prompt)
            model.assistantCurrent = s.key
            model.selectedTab = .assistant
        } catch {
            model.show(error)
        }
    }
}
