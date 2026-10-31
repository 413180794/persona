import Foundation

/// 添加邮箱's form as a value: what the fields become as a request to the hub, and what a recognized paste fills in.
/// `AddMailAccountView` binds to it and talks to the hub; `MailAccountFormTests` checks both directions.
struct MailAccountForm: Equatable {
    enum Kind: String, CaseIterable, Identifiable {
        case gmail = "Gmail", feishu = "飞书", imap = "其他 IMAP"
        var id: String { rawValue }
    }

    /// What to do after filling in a recognized paste.
    enum Next: Equatable {
        /// Gmail takes no passwords from other clients: sign in with Google instead.
        case signIn(note: String)
        /// Nothing missing: add it.
        case save
        /// Something missing: show what.
        case ask(note: String)
    }

    var kind: Kind = .gmail
    var email = ""
    var password = ""
    var imapHost = ""
    var imapPort = "993"
    var smtpHost = ""
    var smtpPort = "465"

    var canSave: Bool { !email.isEmpty && !password.isEmpty }

    /// The account to add with a password; nil for Gmail, which signs in with Google.
    var request: APIClient.AddAccount? {
        var req = APIClient.AddAccount(name: email, email: email, auth: "password")
        switch kind {
        case .gmail:
            return nil
        case .feishu:
            req.provider = "feishu"
            req.password = password
        case .imap:
            req.provider = "imap"
            req.password = password
            if !imapHost.isEmpty { req.imapHost = imapHost; req.imapPort = Int(imapPort) }
            if !smtpHost.isEmpty { req.smtpHost = smtpHost; req.smtpPort = Int(smtpPort); req.smtpSecurity = smtpPort == "587" ? "starttls" : "tls" }
        }
        return req
    }

    /// Fills the form from what the AI read in a paste and says what comes next.
    mutating func fill(from g: APIClient.MailGuess) -> Next {
        let a = g.account
        email = a.email
        password = a.password ?? ""
        switch a.provider {
        case "gmail": kind = .gmail
        case "feishu": kind = .feishu
        default:
            kind = .imap
            imapHost = a.imapHost ?? ""
            imapPort = a.imapPort.map(String.init) ?? "993"
            smtpHost = a.smtpHost ?? ""
            smtpPort = a.smtpPort.map(String.init) ?? "465"
        }
        if kind == .gmail { return .signIn(note: "Gmail 不再支持用密码登录第三方客户端，点「使用 Google 账号登录」授权。") }
        if g.missing.isEmpty { return .save }
        let missing = g.missing.map { ["email": "邮箱", "password": "密码"][$0] ?? $0 }.joined(separator: "、")
        return .ask(note: (g.note.map { $0 + " " } ?? "") + "还缺：" + missing)
    }
}
