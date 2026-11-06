import Foundation

/// Add Mail Account's form as a value: what the fields become as a request to the hub.
/// `AddMailAccountView` binds to it and talks to the hub; `MailAccountFormTests` checks it.
struct MailAccountForm: Equatable {
    enum Kind: String, CaseIterable, Identifiable {
        case gmail = "Gmail", outlook = "Outlook", feishu = "飞书", imap = "其他 IMAP" // i18n-ignore: ids; shown via `title`
        var id: String { rawValue }
        /// The segment's label (the raw value is the id).
        var title: String {
            switch self {
            case .gmail: "Gmail"
            case .outlook: "Outlook"
            case .feishu: String(localized: "飞书")
            case .imap: String(localized: "其他 IMAP")
            }
        }
    }

    var kind: Kind = .gmail
    var email = ""
    var password = ""
    var imapHost = ""
    var imapPort = "993"
    var smtpHost = ""
    var smtpPort = "465"

    var canSave: Bool { !email.isEmpty && !password.isEmpty }

    /// The account to add with a password; nil for Gmail and Outlook, which sign in with Google / Microsoft.
    var request: APIClient.AddAccount? {
        var req = APIClient.AddAccount(name: email, email: email, auth: "password")
        switch kind {
        case .gmail, .outlook:
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

}
