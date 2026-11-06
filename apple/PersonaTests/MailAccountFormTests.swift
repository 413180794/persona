import Foundation
import Testing
@testable import Jarvis

/// Add Mail Account's form (`MailAccountForm`): the request it makes.
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
        #expect(MailAccountForm(kind: .outlook, email: "me@example.com", password: "p").request == nil) // signs in with Microsoft
        #expect(MailAccountForm(kind: .feishu, email: "me@example.com", password: "p").request?.provider == "feishu")
    }

    /// QQ / NetEase addresses get where to make their authorization code (#5123); other addresses and kinds get none.
    @Test func qqAndNetEaseAddressesGetTheCodeGuide() {
        var f = MailAccountForm(kind: .imap, email: "me@QQ.com")
        #expect(f.codeGuide?.provider == .qq && f.codeGuide?.webMail.absoluteString == "https://mail.qq.com")
        f.email = "me@foxmail.com"
        #expect(f.codeGuide?.provider == .qq)
        f.email = "me@126.com"
        #expect(f.codeGuide?.provider == .netease && f.codeGuide?.webMail.absoluteString == "https://mail.126.com")
        f.email = "me@vip.163.com"
        #expect(f.codeGuide?.webMail.absoluteString == "https://vip.163.com")
        f.email = "me@188.com"
        #expect(f.codeGuide?.webMail.absoluteString == "https://www.188.com")
        f.email = "me@example.com"
        #expect(f.codeGuide == nil)
        f.email = "me"
        #expect(f.codeGuide == nil)
        #expect(MailAccountForm(kind: .feishu, email: "me@qq.com").codeGuide == nil)
        f.email = "me@qq.com"
        f.imapHost = "imap.example.com"
        #expect(f.codeGuide == nil, "servers typed: they may not be QQ's")
    }
}
