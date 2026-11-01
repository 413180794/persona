import AuthenticationServices
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
    struct AddAccount: Codable {
        var name: String
        var email: String
        var provider: String?
        var auth: String?
        var username: String?
        var password: String?
        var imapHost: String?
        var imapPort: Int?
        var smtpHost: String?
        var smtpPort: Int?
        var smtpSecurity: String?
        enum CodingKeys: String, CodingKey {
            case name, email, provider, auth, username, password
            case imapHost = "imap_host", imapPort = "imap_port", smtpHost = "smtp_host", smtpPort = "smtp_port", smtpSecurity = "smtp_security"
        }
    }
    struct AccountResponse: Decodable { var account: MailAccount }
    func addMailAccount(_ a: AddAccount) async throws -> MailAccount {
        try await post("/api/v1/mail/accounts", body: a, timeout: 60, as: AccountResponse.self).account
    }
    struct GoogleStart: Decodable {
        var authURL: URL
        var callbackScheme: String
        var state: String
        enum CodingKeys: String, CodingKey { case authURL = "auth_url", callbackScheme = "callback_scheme", state }
    }
    struct GoogleFinish: Encodable { var state: String; var code: String?; var error: String? }
    /// Begins Gmail's "Sign in with Google": the hub builds the consent URL (PKCE) for the app to open.
    func googleSignInStart() async throws -> GoogleStart {
        try await post("/api/v1/mail/oauth/google/start", as: GoogleStart.self)
    }
    /// Hands the callback's code to the hub, which exchanges it, checks IMAP / SMTP and saves the account.
    func googleSignInFinish(_ f: GoogleFinish) async throws -> MailAccount {
        try await post("/api/v1/mail/oauth/google/finish", body: f, timeout: 60, as: AccountResponse.self).account
    }
    func deleteMailAccount(_ id: String) async throws { try await delete("/api/v1/mail/accounts/\(id)") }

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
    /// 设置 › 翻译: the mail in the target language (shown instead of the original until 显示原文).
    @State private var translation: MailTranslation?
    @State private var showOriginal = false
    @State private var translating = false
    @State private var translateError: String?
    @AppStorage(TranslationPrefs.targetKey) private var target = TranslationPrefs.defaultTarget
    private var translated: MailTranslation? { showOriginal ? nil : translation }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.l) {
                Text(translated.flatMap { $0.subject.isEmpty ? nil : $0.subject } ?? (summary.subject?.isEmpty == false ? summary.subject! : "（无主题）"))
                    .font(Theme.Font.title)
                HStack(alignment: .top, spacing: Theme.Space.m) {
                    Monogram(name: summary.from?.display ?? "", fill: Theme.accent.opacity(0.15), ink: Theme.accent)
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text(summary.from?.display ?? "").font(Theme.Font.secondaryStrong)
                        Text(summary.from?.email ?? "").font(Theme.Font.label).foregroundStyle(.secondary)
                        if let to = message?.to, !to.isEmpty {
                            Text("收件人：" + to.map(\.display).joined(separator: "，")).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                        }
                    }
                    Spacer()
                    Text(summary.date.map { $0.formatted(date: .abbreviated, time: .shortened) } ?? "").font(Theme.Font.label).foregroundStyle(.secondary)
                }
                Divider()
                translationBar
                if let m = message {
                    if let t = translated {
                        Text(t.text).font(Theme.Font.body).lineHeight(.body).textSelection(.enabled)
                        if t.clipped == true {
                            Text("邮件很长，只翻译了前面一部分；全文看原文。").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    } else if let text = m.text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Text(text).font(Theme.Font.body).lineHeight(.body).textSelection(.enabled)
                    } else if let html = m.html, !html.isEmpty {
                        HTMLView(html: html, height: $webHeight).frame(height: webHeight)
                    }
                    if let atts = m.attachments, !atts.isEmpty {
                        VStack(alignment: .leading, spacing: Theme.Space.sm) {
                            ForEach(Array(atts.enumerated()), id: \.offset) { _, a in
                                Label("\(a.filename ?? "附件")  \(ByteCountFormatter.string(fromByteCount: Int64(a.size ?? 0), countStyle: .file))", systemImage: "paperclip")
                                    .font(Theme.Font.caption)
                            }
                        }
                        .card(padding: 12)
                    }
                } else if let error {
                    Text(error).foregroundStyle(Theme.danger)
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(40)
                }
            }
            .padding(Theme.Space.xl)
        }
        .toolbar {
            ToolbarItemGroup(placement: .bottomBar) {
                Button { askClaude = true } label: { Label("让 Claude 处理", systemImage: Theme.Icon.ai) }
                Spacer()
                Button {
                    reply = ComposeDraft.reply(to: summary, message: message)
                } label: { Image(systemName: Theme.Icon.reply) }
                    .disabled(message == nil)
            }
        }
        .sheet(item: $reply) { d in ComposeView(draft: d, accounts: accounts) }
        .sheet(isPresented: $askClaude) {
            NewChatPrefilled(prompt: "请用 mail_read 读取邮件（account_id: \(summary.accountID)，mailbox: \(summary.mailbox)，uid: \(summary.uid)，主题：\(summary.subject ?? "")），总结要点，并给出建议的回复草稿。")
        }
        #if DEBUG && os(macOS)
        // `-showMailAskClaude YES` (with an inbox mail open): the 「让 Claude 处理」 sheet.
        .onAppear { if UserDefaults.standard.bool(forKey: "showMailAskClaude") { askClaude = true } }
        #endif
        .task {
            guard let api = model.api else { return }
            do {
                message = try await api.mailMessage(account: summary.accountID, uid: summary.uid, mailbox: summary.mailbox)
                if let message { await translateIfNeeded(message) }
                if !summary.seen { try? await api.mailFlags(account: summary.accountID, uid: summary.uid, .init(mailbox: summary.mailbox, seen: true)) }
            } catch {
                self.error = error.shown
            }
        }
    }
}

extension MailDetailView {
    /// While translating, or once translated: what happened and the way back to the original (or to the translation).
    @ViewBuilder var translationBar: some View {
        if translating {
            HStack(spacing: Theme.Space.s) {
                ProgressView().controlSize(.small)
                Text("正在翻译成\(TranslationPrefs.name(of: target))…").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        } else if translation != nil {
            HStack(spacing: Theme.Space.s) {
                Label(showOriginal ? "原文" : "已翻译成\(TranslationPrefs.name(of: target))", systemImage: Theme.Icon.translate)
                    .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: Theme.Space.s)
                Button(showOriginal ? "显示译文" : "显示原文") {
                    withAnimation(Theme.Motion.state) { showOriginal.toggle() }
                }
                .font(Theme.Font.captionMedium)
                .lineLimit(1)
                .buttonStyle(.borderless)
            }
        } else if let translateError {
            Label("没能翻译：\(translateError)", systemImage: Theme.Icon.error).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(2)
        }
    }

    /// 设置 › 翻译 is on for mail and this one is in another language: ask the hub.
    func translateIfNeeded(_ m: MailMessage) async {
        guard TranslationPrefs.translatesMail, translation == nil else { return }
        let body = m.text?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? m.text! : MailText.stripped(m.html ?? "")
        guard Translator.shared.isForeign((summary.subject ?? "") + "\n" + body, target: target) else { return }
        translating = true
        defer { translating = false }
        do {
            translation = try await Translator.shared.mail(account: summary.accountID, uid: summary.uid, mailbox: summary.mailbox, to: target)
            translateError = nil
        } catch {
            translateError = error.shown
        }
    }
}

/// A mail's HTML as rough text, enough to tell its language.
nonisolated enum MailText {
    static func stripped(_ html: String) -> String {
        var s = html
        for block in ["style", "script", "head"] {
            s = s.replacingOccurrences(of: "<\(block)[^>]*>[\\s\\S]*?</\(block)>", with: " ", options: [.regularExpression, .caseInsensitive])
        }
        s = s.replacingOccurrences(of: "<[^>]+>", with: " ", options: .regularExpression)
        return s.replacingOccurrences(of: "&nbsp;", with: " ")
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

#if os(iOS)
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
#endif

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
                    #if os(macOS)
                    // A grouped Mac form shows a titled field as label + trailing value: the body took the right half.
                    TextField("正文", text: $draft.body, prompt: Text("正文"), axis: .vertical).labelsHidden().lineLimit(10...40)
                    #else
                    TextField("正文", text: $draft.body, axis: .vertical).lineLimit(10...40)
                    #endif
                }
                if let error { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) }
            }
            .themedPage()
            .navigationTitle("写邮件")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button { Task { await send() } } label: {
                        if sending { ProgressView() } else { Label("发送", systemImage: Theme.Icon.sendMail) }
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
            self.error = error.shown
        }
    }
}

struct MailAccountsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var accounts: [MailAccount] = []
    @State private var adding = false
    /// A sheet of its own (with 完成), or a page pushed in 设置 › 连接器 › 邮箱.
    var inSheet = true

    var body: some View {
        if inSheet {
            NavigationStack { page.toolbar { ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } } } }
        } else {
            page
        }
    }

    private var page: some View {
            List {
                Section {
                    ForEach(accounts) { a in
                        HStack {
                            MailProviderMark(provider: a.provider ?? "")
                            VStack(alignment: .leading) {
                                Text(a.email).font(Theme.Font.heading)
                                Text(a.status == "error" ? (a.error ?? "错误") : a.auth == "oauth2" ? "Gmail · Google 账号登录" : "\(a.provider == "feishu" ? "飞书邮箱" : (a.imapHost ?? ""))")
                                    .font(Theme.Font.label).foregroundStyle(a.status == "error" ? Theme.danger : .secondary)
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
            .themedPage()
            .navigationTitle("邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .sheet(isPresented: $adding, onDismiss: { Task { await load() } }) { AddMailAccountView() }
            .task { await load() }
            #if DEBUG
            .onAppear { if UserDefaults.standard.bool(forKey: "addMail") { adding = true } }
            #endif
    }

    private func load() async { accounts = (try? await model.api?.mailAccounts()) ?? [] }
}

/// An account's provider: Gmail's and 飞书's own marks, an envelope for any other IMAP server.
struct MailProviderMark: View {
    var provider: String

    var body: some View {
        Group {
            switch provider {
            case "gmail": Image(Theme.Brand.gmail).resizable().scaledToFit()
            case "feishu": Image(Theme.Brand.lark).resizable().scaledToFit()
            default: Image(systemName: "envelope.circle.fill").resizable().scaledToFit().foregroundStyle(Theme.accent)
            }
        }
        .scaledFrame(width: Theme.Size.wellXS, height: Theme.Size.wellXS)
    }
}

/// Google's sign-in button as its branding guidelines draw it (developers.google.com/identity/branding-guidelines):
/// the G on white with a gray outline in light mode, on #131314 in dark mode; not the app's accent.
struct GoogleSignInLabel: View {
    @Environment(\.colorScheme) private var scheme
    var title: String
    var busy = false

    var body: some View {
        let dark = scheme == .dark
        HStack(spacing: Theme.Space.m) {
            if busy { ProgressView().controlSize(.small) } else {
                Image(Theme.Brand.google).resizable().scaledToFit().scaledFrame(width: Theme.Size.iconXS, height: Theme.Size.iconXS)
            }
            Text(title).font(Theme.Font.heading).lineLimit(1)
        }
        .foregroundStyle(Color(hex: dark ? 0xE3E3E3 : 0x1F1F1F))
        .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.avatarXL)
        .background(Color(hex: dark ? 0x131314 : 0xFFFFFF), in: .capsule)
        .overlay(Capsule().stroke(Color(hex: dark ? 0x8E918F : 0x747775), lineWidth: 1))
        .contentShape(.capsule)
    }
}

struct AddMailAccountView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(\.webAuthenticationSession) private var webAuth
    typealias Kind = MailAccountForm.Kind
    @State private var form = MailAccountForm()
    @State private var saving = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Picker("类型", selection: $form.kind) { ForEach(Kind.allCases) { Text($0.rawValue).tag($0) } }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                if form.kind == .gmail {
                    Section {
                        Button { Task { await signInWithGoogle() } } label: {
                            GoogleSignInLabel(title: saving ? "正在连接 Gmail…" : "使用 Google 账号登录", busy: saving)
                        }
                        .buttonStyle(.plain)
                        .disabled(saving)
                        .listRowBackground(Color.clear)
                        .listRowInsets(EdgeInsets())
                    } footer: {
                        Text("跳到 Google 选择账号并授权 Jarvis 收发邮件，不需要密码。授权随时可以在 Google 账号的「第三方应用和服务」里撤销；授权失效时再登录一次即可恢复。")
                    }
                } else {
                    Section {
                        TextField("邮箱地址", text: $form.email).keyboardType(.emailAddress).textInputAutocapitalization(.never).autocorrectionDisabled()
                        SecureField("密码 / 授权码", text: $form.password)
                    } footer: {
                        switch form.kind {
                        case .gmail: EmptyView()
                        case .feishu: Text("飞书邮箱：服务器固定为 imap.larkoffice.com:993、smtp.larkoffice.com:465（SSL），只填邮箱和密码。密码是飞书邮箱设置里的「第三方客户端登录密码」。")
                        case .imap: Text("QQ / 163 / Outlook / iCloud 等会自动填服务器；国内邮箱一般要用「授权码」而不是登录密码。")
                        }
                    }
                }
                if form.kind == .imap {
                    Section("服务器（可留空自动识别）") {
                        TextField("IMAP 服务器", text: $form.imapHost).textInputAutocapitalization(.never).autocorrectionDisabled()
                        TextField("IMAP 端口", text: $form.imapPort).keyboardType(.numberPad)
                        TextField("SMTP 服务器", text: $form.smtpHost).textInputAutocapitalization(.never).autocorrectionDisabled()
                        TextField("SMTP 端口", text: $form.smtpPort).keyboardType(.numberPad)
                    }
                }
                if let error { Section { Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption) } }
                Section {
                } footer: {
                    // 2026-11-01: 「AI 一键添加」 (paste and recognize) is gone; Jarvis adds accounts itself (mail_account_add).
                    Label("也可以直接在 Jarvis 里说「帮我添加邮箱」，把邮箱和密码告诉它，在手机上确认后就加好了。", systemImage: Theme.Icon.ai)
                }
            }
            .themedPage()
            .navigationTitle("添加邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                if form.kind != .gmail {
                    ToolbarItem(placement: .confirmationAction) {
                        Button { Task { await save() } } label: { if saving { ProgressView() } else { Text("连接") } }
                            .disabled(saving || form.email.isEmpty || form.password.isEmpty)
                    }
                }
            }
        }
    }

    /// Gmail: Google's consent page in the system browser, then the hub swaps the code for a refresh token.
    private func signInWithGoogle() async {
        guard let api = model.api else { return }
        saving = true
        defer { saving = false }
        error = nil
        do {
            let start = try await api.googleSignInStart()
            let callback = try await webAuth.authenticate(using: start.authURL, callback: .customScheme(start.callbackScheme),
                                                          preferredBrowserSession: .shared, additionalHeaderFields: [:])
            let items = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let value = { (name: String) in items.first(where: { $0.name == name })?.value }
            if value("error") == "access_denied" { return }
            _ = try await api.googleSignInFinish(.init(state: value("state") ?? start.state, code: value("code"), error: value("error")))
            Haptics.success()
            dismiss()
        } catch let e as ASWebAuthenticationSessionError where e.code == .canceledLogin {
            return
        } catch {
            if let shown = error.shown { self.error = shown; Haptics.error() }
        }
    }

    private func save() async {
        guard let api = model.api, let req = form.request else { return }
        saving = true
        defer { saving = false }
        do {
            _ = try await api.addMailAccount(req)
            Haptics.success()
            dismiss()
        } catch {
            self.error = error.shown
        }
    }
}
