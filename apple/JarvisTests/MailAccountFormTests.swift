import Foundation
import Testing
@testable import Jarvis

/// 添加邮箱's form (`MailAccountForm`): the request it makes.
@MainActor struct MailAccountFormTests {
    @Test func imapOn587IsStartTLSAndEmptyHostsAreLeftToTheHub() {
        var f = MailAccountForm(kind: .imap, email: "me@example.com", password: "p")
        f.smtpHost = "smtp.example.com"
        f.smtpPort = "587"
        let r = f.request
        #expect(r?.provider == "imap" && r?.password == "p")
        #expect(r?.smtpSecurity == "starttls" && r?.smtpPort == 587)
        #expect(r?.imapHost == nil, "no host typed: the hub finds it")
        f.smtpPort = "465"
        #expect(f.request?.smtpSecurity == "tls")
    }

    @Test func gmailMakesNoPasswordRequest() {
        #expect(MailAccountForm(kind: .gmail, email: "me@example.com", password: "p").request == nil)
        #expect(MailAccountForm(kind: .feishu, email: "me@example.com", password: "p").request?.provider == "feishu")
    }
}
