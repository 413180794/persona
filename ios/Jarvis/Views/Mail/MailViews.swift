import SwiftUI
import WebKit

extension APIClient {
    struct AccountsResponse: Decodable { var accounts: [MailAccount] }
    struct MessagesResponse: Decodable {
        var messages: [MailSummary]
        var hasMore: Bool
        enum CodingKeys: String, CodingKey { case messages; case hasMore = "has_more" }
    }
    struct MessageResponse: Decodable { var message: MailMessage }
    struct MailboxesResponse: Decodable { var mailboxes: [Mailbox] }

    func mailAccounts() async throws -> [MailAccount] { try await get("/api/v1/mail/accounts", as: AccountsResponse.self).accounts }
    func mailMessages(account: String?, mailbox: String = "INBOX", beforeUID: UInt32? = nil, query: String = "") async throws -> MessagesResponse {
        try await get("/api/v1/mail/messages", query: ["account_id": account ?? "", "mailbox": mailbox, "limit": "40",
                                                         "before_uid": beforeUID.map { String($0) } ?? "", "q": query])
    }
    func mailMessage(account: String, uid: UInt32, mailbox: String) async throws -> MailMessage {
        try await get("/api/v1/mail/messages/\(account)/\(uid)", query: ["mailbox": mailbox], as: MessageResponse.self).message
    }
    struct FlagsBody: Encodable { var mailbox: String; var seen: Bool?; var flagged: Bool? }
    func mailFlags(account: String, uid: UInt32, _ body: FlagsBody) async throws {
        try await post("/api/v1/mail/messages/\(account)/\(uid)/flags", body: body)
    }
    struct SendMail: Encodable {
        var accountID: String
        var to: [MailAddress]
        var cc: [MailAddress]
        var subject: String
        var text: String
        var inReplyTo: String?
        var references: [String]?
        enum CodingKeys: String, CodingKey {
            case to, cc, subject, text, references
            case accountID = "account_id", inReplyTo = "in_reply_to"
        }
    }
    func sendMail(_ m: SendMail) async throws { try await post("/api/v1/mail/send", body: m, timeout: 60) }
    struct AddAccount: Encodable {
        struct OAuth: Encodable {
            var clientID: String, clientSecret: String, refreshToken: String
            enum CodingKeys: String, CodingKey { case clientID = "client_id", clientSecret = "client_secret", refreshToken = "refresh_token" }
        }
        var name: String
        var email: String
        var provider: String?
        var auth: String
        var username: String?
        var password: String?
        var oauth: OAuth?
        var imapHost: String?
        var imapPort: Int?
        var smtpHost: String?
        var smtpPort: Int?
        var smtpSecurity: String?
        enum CodingKeys: String, CodingKey {
            case name, email, provider, auth, username, password, oauth
            case imapHost = "imap_host", imapPort = "imap_port", smtpHost = "smtp_host", smtpPort = "smtp_port", smtpSecurity = "smtp_security"
        }
    }
    struct AccountResponse: Decodable { var account: MailAccount }
    func addMailAccount(_ a: AddAccount) async throws -> MailAccount {
        try await post("/api/v1/mail/accounts", body: a, timeout: 60, as: AccountResponse.self).account
    }
    func deleteMailAccount(_ id: String) async throws { try await delete("/api/v1/mail/accounts/\(id)") }
}

struct MailView: View {
    @Environment(AppModel.self) private var model
    @State private var accounts: [MailAccount] = []
    @State private var account: String?
    @State private var messages: [MailSummary] = []
    @State private var hasMore = false
    @State private var loading = false
    @State private var error: String?
    @State private var query = ""
    @State private var showAccounts = false
    @State private var composing: ComposeDraft?
    @State private var loadedAccounts = false

    var body: some View {
        NavigationStack {
            Group {
                if loadedAccounts && accounts.isEmpty {
                    EmptyStateView(symbol: "envelope.badge", title: "添加邮箱",
                                   message: "Gmail 或任意 IMAP/SMTP 邮箱。凭据加密保存在 example-machine 上，Claude 也能帮你查信、回信（发信前会在手机上确认）。",
                                   action: ("添加邮箱", { showAccounts = true }))
                } else {
                    list
                }
            }
            .navigationTitle(account.flatMap { id in accounts.first { $0.id == id }?.email } ?? "收件箱")
            .navigationBarTitleDisplayMode(.large)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Menu {
                        Picker("邮箱", selection: $account) {
                            Label("全部收件箱", systemImage: "tray.2").tag(String?.none)
                            ForEach(accounts) { a in Label(a.email, systemImage: "envelope").tag(Optional(a.id)) }
                        }
                        Divider()
                        Button { showAccounts = true } label: { Label("管理邮箱", systemImage: "gearshape") }
                    } label: { Image(systemName: "tray.2") }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { composing = ComposeDraft(accountID: account ?? accounts.first?.id ?? "") } label: {
                        Image(systemName: "square.and.pencil")
                    }
                    .disabled(accounts.isEmpty)
                }
            }
            .navigationDestination(for: MailSummary.self) { m in MailDetailView(summary: m, accounts: accounts) }
            .sheet(isPresented: $showAccounts, onDismiss: { Task { await loadAccounts() } }) { MailAccountsView() }
            .sheet(item: $composing) { d in ComposeView(draft: d, accounts: accounts) }
            .task { await loadAccounts() }
            .task(id: account) { await reload() }
        }
    }

    private var list: some View {
        List {
            if let error {
                Label(error, systemImage: "exclamationmark.triangle").font(.footnote).foregroundStyle(Theme.danger)
            }
            ForEach(messages) { m in
                NavigationLink(value: m) { MailRow(message: m, showAccount: account == nil && accounts.count > 1, accounts: accounts) }
                    .swipeActions(edge: .leading) {
                        Button { Task { await toggleSeen(m) } } label: {
                            Label(m.seen ? "未读" : "已读", systemImage: m.seen ? "envelope.badge" : "envelope.open")
                        }
                        .tint(Theme.done)
                    }
            }
            if hasMore {
                ProgressView().frame(maxWidth: .infinity).task { await loadMore() }
            }
            if loading && messages.isEmpty {
                ForEach(0..<6, id: \.self) { _ in MailRowPlaceholder() }
            }
        }
        .listStyle(.plain)
        .searchable(text: $query, prompt: "搜索邮件")
        .onSubmit(of: .search) { Task { await reload() } }
        .refreshable { await reload() }
    }

    private func loadAccounts() async {
        guard let api = model.api else { return }
        accounts = (try? await api.mailAccounts()) ?? accounts
        loadedAccounts = true
    }

    private func reload() async {
        guard let api = model.api else { return }
        loading = true
        defer { loading = false }
        do {
            let r = try await api.mailMessages(account: account, query: query)
            messages = r.messages
            hasMore = r.hasMore && account != nil
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func loadMore() async {
        guard let api = model.api, let last = messages.last, account != nil else { return }
        if let r = try? await api.mailMessages(account: account, beforeUID: last.uid, query: query) {
            messages += r.messages
            hasMore = r.hasMore
        } else { hasMore = false }
    }

    private func toggleSeen(_ m: MailSummary) async {
        guard let api = model.api else { return }
        try? await api.mailFlags(account: m.accountID, uid: m.uid, .init(mailbox: m.mailbox, seen: !m.seen))
        if let i = messages.firstIndex(of: m) { messages[i].seen.toggle() }
    }
}

struct MailRow: View {
    var message: MailSummary
    var showAccount: Bool
    var accounts: [MailAccount]

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle().fill(message.seen ? Color.clear : Theme.done).frame(width: 9, height: 9).padding(.top, 6)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(message.from?.display ?? "（无发件人）")
                        .font(.body.weight(message.seen ? .regular : .semibold))
                        .lineLimit(1)
                    Spacer()
                    if message.hasAttachments { Image(systemName: "paperclip").font(.caption).foregroundStyle(.secondary) }
                    if message.flagged { Image(systemName: "flag.fill").font(.caption).foregroundStyle(Theme.attention) }
                    Text(Fmt.relative(message.date)).font(.caption).foregroundStyle(.secondary)
                }
                Text(message.subject?.isEmpty == false ? message.subject! : "（无主题）")
                    .font(.subheadline.weight(message.seen ? .regular : .medium))
                    .lineLimit(1)
                if let s = message.snippet, !s.isEmpty {
                    Text(s).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }
                if showAccount, let a = accounts.first(where: { $0.id == message.accountID }) {
                    Chip(text: a.email, symbol: "envelope")
                }
            }
        }
        .padding(.vertical, 2)
    }
}

struct MailRowPlaceholder: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            RoundedRectangle(cornerRadius: 4).fill(Color.primary.opacity(0.08)).frame(width: 140, height: 12)
            RoundedRectangle(cornerRadius: 4).fill(Color.primary.opacity(0.06)).frame(height: 10)
            RoundedRectangle(cornerRadius: 4).fill(Color.primary.opacity(0.05)).frame(width: 220, height: 10)
        }
        .padding(.vertical, 6)
        .redacted(reason: .placeholder)
    }
}

struct MailDetailView: View {
    @Environment(AppModel.self) private var model
    var summary: MailSummary
    var accounts: [MailAccount]
    @State private var message: MailMessage?
    @State private var error: String?
    @State private var reply: ComposeDraft?
    @State private var askClaude = false
    @State private var webHeight: CGFloat = 300

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text(summary.subject?.isEmpty == false ? summary.subject! : "（无主题）").font(.title2.weight(.bold))
                HStack(alignment: .top, spacing: 12) {
                    Circle().fill(Theme.accent.opacity(0.15)).frame(width: 40, height: 40)
                        .overlay(Text(String((summary.from?.display ?? "?").prefix(1))).font(.headline).foregroundStyle(Theme.accent))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(summary.from?.display ?? "").font(.subheadline.weight(.semibold))
                        Text(summary.from?.email ?? "").font(.caption).foregroundStyle(.secondary)
                        if let to = message?.to, !to.isEmpty {
                            Text("收件人：" + to.map(\.display).joined(separator: "，")).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    Spacer()
                    Text(summary.date.map { $0.formatted(date: .abbreviated, time: .shortened) } ?? "").font(.caption).foregroundStyle(.secondary)
                }
                Divider()
                if let m = message {
                    if let text = m.text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Text(text).font(.body).textSelection(.enabled)
                    } else if let html = m.html, !html.isEmpty {
                        HTMLView(html: html, height: $webHeight).frame(height: webHeight)
                    }
                    if let atts = m.attachments, !atts.isEmpty {
                        VStack(alignment: .leading, spacing: 6) {
                            ForEach(Array(atts.enumerated()), id: \.offset) { _, a in
                                Label("\(a.filename ?? "附件")  \(ByteCountFormatter.string(fromByteCount: Int64(a.size ?? 0), countStyle: .file))", systemImage: "paperclip")
                                    .font(.footnote)
                            }
                        }
                        .card(padding: 12, radius: 14)
                    }
                } else if let error {
                    Text(error).foregroundStyle(Theme.danger)
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(40)
                }
            }
            .padding(20)
        }
        .toolbar {
            ToolbarItemGroup(placement: .bottomBar) {
                Button { askClaude = true } label: { Label("让 Claude 处理", systemImage: "sparkles") }
                Spacer()
                Button {
                    reply = ComposeDraft.reply(to: summary, message: message)
                } label: { Image(systemName: "arrowshape.turn.up.left") }
                    .disabled(message == nil)
            }
        }
        .sheet(item: $reply) { d in ComposeView(draft: d, accounts: accounts) }
        .sheet(isPresented: $askClaude) {
            NewChatPrefilled(prompt: "请用 mail_read 读取邮件（account_id: \(summary.accountID)，mailbox: \(summary.mailbox)，uid: \(summary.uid)，主题：\(summary.subject ?? "")），总结要点，并给出建议的回复草稿。")
        }
        .task {
            guard let api = model.api else { return }
            do {
                message = try await api.mailMessage(account: summary.accountID, uid: summary.uid, mailbox: summary.mailbox)
                if !summary.seen { try? await api.mailFlags(account: summary.accountID, uid: summary.uid, .init(mailbox: summary.mailbox, seen: true)) }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// New chat sheet with the first message filled in (used by mail's "让 Claude 处理").
struct NewChatPrefilled: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    var prompt: String

    var body: some View {
        NewChatView(initialPrompt: prompt) { s in
            dismiss()
            model.selectedTab = .chats
            model.openChat = s.key
        }
    }
}

struct HTMLView: UIViewRepresentable {
    var html: String
    @Binding var height: CGFloat

    func makeCoordinator() -> Coordinator { Coordinator(height: $height) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        let v = WKWebView(frame: .zero, configuration: config)
        v.scrollView.isScrollEnabled = false
        v.isOpaque = false
        v.backgroundColor = .clear
        v.navigationDelegate = context.coordinator
        return v
    }

    func updateUIView(_ v: WKWebView, context: Context) {
        let page = """
        <html><head><meta name="viewport" content="width=device-width, initial-scale=1">
        <style>body{font:-apple-system-body;margin:0;color:CanvasText;background:transparent;word-wrap:break-word}
        img{max-width:100%;height:auto} table{max-width:100%}
        @media (prefers-color-scheme: dark){body{color:#eee} a{color:#7ab}}</style></head><body>\(html)</body></html>
        """
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            v.loadHTMLString(page, baseURL: nil)
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var height: Binding<CGFloat>
        var loaded: String?
        init(height: Binding<CGFloat>) { self.height = height }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            webView.evaluateJavaScript("document.body.scrollHeight") { value, _ in
                if let h = value as? CGFloat { Task { @MainActor in self.height.wrappedValue = h + 20 } }
            }
        }

        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
            if action.navigationType == .linkActivated, let url = action.request.url {
                await UIApplication.shared.open(url)
                return .cancel
            }
            return .allow
        }
    }
}

struct ComposeDraft: Identifiable {
    let id = UUID()
    var accountID: String
    var to = ""
    var cc = ""
    var subject = ""
    var body = ""
    var inReplyTo: String?
    var references: [String]?

    static func reply(to s: MailSummary, message: MailMessage?) -> ComposeDraft {
        var d = ComposeDraft(accountID: s.accountID)
        d.to = s.from?.email ?? ""
        let subj = s.subject ?? ""
        d.subject = subj.lowercased().hasPrefix("re:") ? subj : "Re: " + subj
        d.inReplyTo = message?.messageID ?? s.messageID
        d.references = (message?.references ?? []) + [message?.messageID ?? s.messageID].compactMap { $0 }
        let quoted = (message?.text ?? "").split(separator: "\n", omittingEmptySubsequences: false).prefix(40).map { "> " + $0 }.joined(separator: "\n")
        d.body = "\n\n\(s.date.map { $0.formatted() } ?? "")，\(s.from?.display ?? "") 写道：\n" + quoted
        return d
    }
}

struct ComposeView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State var draft: ComposeDraft
    var accounts: [MailAccount]
    @State private var sending = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Picker("发件人", selection: $draft.accountID) {
                    ForEach(accounts) { a in Text(a.email).tag(a.id) }
                }
                TextField("收件人（逗号分隔）", text: $draft.to).keyboardType(.emailAddress).textInputAutocapitalization(.never)
                TextField("抄送", text: $draft.cc).keyboardType(.emailAddress).textInputAutocapitalization(.never)
                TextField("主题", text: $draft.subject)
                Section {
                    TextField("正文", text: $draft.body, axis: .vertical).lineLimit(10...40)
                }
                if let error { Text(error).foregroundStyle(Theme.danger).font(.footnote) }
            }
            .navigationTitle("写邮件")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await send() } } label: {
                        if sending { ProgressView() } else { Image(systemName: "paperplane.fill") }
                    }
                    .disabled(sending || draft.to.isEmpty)
                }
            }
        }
    }

    private func addresses(_ s: String) -> [MailAddress] {
        s.split(whereSeparator: { $0 == "," || $0 == "，" || $0 == ";" })
            .map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
            .map { MailAddress(name: nil, email: $0) }
    }

    private func send() async {
        guard let api = model.api else { return }
        sending = true
        defer { sending = false }
        do {
            try await api.sendMail(.init(accountID: draft.accountID, to: addresses(draft.to), cc: addresses(draft.cc),
                                         subject: draft.subject, text: draft.body, inReplyTo: draft.inReplyTo, references: draft.references))
            Haptics.success()
            model.toast = Toast("已发送", style: .success)
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct MailAccountsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var accounts: [MailAccount] = []
    @State private var adding = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(accounts) { a in
                        HStack {
                            Image(systemName: a.provider == "gmail" ? "g.circle.fill" : "envelope.circle.fill")
                                .font(.title2).foregroundStyle(Theme.accent)
                            VStack(alignment: .leading) {
                                Text(a.email).font(.body.weight(.medium))
                                Text(a.status == "error" ? (a.error ?? "错误") : "\(a.imapHost ?? "") · \(a.auth == "oauth2" ? "OAuth" : "密码")")
                                    .font(.caption).foregroundStyle(a.status == "error" ? Theme.danger : .secondary)
                            }
                        }
                    }
                    .onDelete { idx in
                        let ids = idx.map { accounts[$0].id }
                        Task {
                            for id in ids { try? await model.api?.deleteMailAccount(id) }
                            await load()
                        }
                    }
                }
                Section {
                    Button { adding = true } label: { Label("添加邮箱", systemImage: "plus.circle.fill") }
                }
            }
            .navigationTitle("邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } }
            .sheet(isPresented: $adding, onDismiss: { Task { await load() } }) { AddMailAccountView() }
            .task { await load() }
        }
    }

    private func load() async { accounts = (try? await model.api?.mailAccounts()) ?? [] }
}

struct AddMailAccountView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    enum Kind: String, CaseIterable, Identifiable { case gmail = "Gmail", gmailOAuth = "Gmail OAuth", imap = "其他 IMAP"; var id: String { rawValue } }
    @State private var kind: Kind = .gmail
    @State private var email = ""
    @State private var password = ""
    @State private var clientID = ""
    @State private var clientSecret = ""
    @State private var refreshToken = ""
    @State private var imapHost = ""
    @State private var imapPort = "993"
    @State private var smtpHost = ""
    @State private var smtpPort = "465"
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Picker("类型", selection: $kind) { ForEach(Kind.allCases) { Text($0.rawValue).tag($0) } }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                Section {
                    TextField("邮箱地址", text: $email).keyboardType(.emailAddress).textInputAutocapitalization(.never).autocorrectionDisabled()
                    switch kind {
                    case .gmail, .imap:
                        SecureField(kind == .gmail ? "应用专用密码（16 位）" : "密码 / 授权码", text: $password)
                    case .gmailOAuth:
                        TextField("Client ID", text: $clientID).textInputAutocapitalization(.never).autocorrectionDisabled()
                        SecureField("Client Secret", text: $clientSecret)
                        SecureField("Refresh Token", text: $refreshToken)
                    }
                } footer: {
                    switch kind {
                    case .gmail: Text("Google 账号开启两步验证后，在 myaccount.google.com/apppasswords 生成应用专用密码。")
                    case .gmailOAuth: Text("在 Google Cloud 建 OAuth 客户端并取得 refresh token（scope https://mail.google.com/），步骤见 docs/mail.md。")
                    case .imap: Text("QQ / 163 / Outlook / iCloud 等会自动填服务器；国内邮箱一般要用「授权码」而不是登录密码。")
                    }
                }
                if kind == .imap {
                    Section("服务器（可留空自动识别）") {
                        TextField("IMAP 服务器", text: $imapHost).textInputAutocapitalization(.never).autocorrectionDisabled()
                        TextField("IMAP 端口", text: $imapPort).keyboardType(.numberPad)
                        TextField("SMTP 服务器", text: $smtpHost).textInputAutocapitalization(.never).autocorrectionDisabled()
                        TextField("SMTP 端口", text: $smtpPort).keyboardType(.numberPad)
                    }
                }
                if let error { Section { Text(error).foregroundStyle(Theme.danger).font(.footnote) } }
            }
            .navigationTitle("添加邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await save() } } label: { if saving { ProgressView() } else { Text("连接") } }
                        .disabled(saving || email.isEmpty)
                }
            }
        }
    }

    private func save() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        var req = APIClient.AddAccount(name: email, email: email, auth: kind == .gmailOAuth ? "oauth2" : "password")
        switch kind {
        case .gmail:
            req.provider = "gmail"
            req.password = password.replacingOccurrences(of: " ", with: "")
        case .gmailOAuth:
            req.provider = "gmail"
            req.oauth = .init(clientID: clientID, clientSecret: clientSecret, refreshToken: refreshToken)
        case .imap:
            req.provider = "imap"
            req.password = password
            if !imapHost.isEmpty { req.imapHost = imapHost; req.imapPort = Int(imapPort) }
            if !smtpHost.isEmpty { req.smtpHost = smtpHost; req.smtpPort = Int(smtpPort); req.smtpSecurity = smtpPort == "587" ? "starttls" : "tls" }
        }
        do {
            _ = try await api.addMailAccount(req)
            Haptics.success()
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }
}
