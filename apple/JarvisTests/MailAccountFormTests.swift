import Foundation
import Testing
@testable import Jarvis

/// 添加邮箱's form (`MailAccountForm`): the request it makes, and what a recognized paste fills in.
@MainActor struct MailAccountFormTests {
    private func guess(_ account: String, missing: [String] = [], note: String? = nil) -> APIClient.MailGuess {
        let list = "[" + missing.map { "\"\($0)\"" }.joined(separator: ",") + "]"
        let json = #"{"account":\#(account),"missing":\#(list),"note":\#(note.map { "\"\($0)\"" } ?? "null")}"#
        return try! JSON.decoder.decode(APIClient.MailGuess.self, from: Data(json.utf8))
    }

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

    @Test func aRecognizedPasteFillsTheFormAndSaysWhatsNext() {
        var f = MailAccountForm()
        let full = f.fill(from: guess(#"{"name":"a","email":"me@example.com","provider":"imap","password":"p","imap_host":"imap.example.com","imap_port":993}"#))
        #expect(full == .save)
        #expect(f.kind == .imap && f.imapHost == "imap.example.com" && f.smtpPort == "465")
        let gmail = f.fill(from: guess(#"{"name":"a","email":"me@example.com","provider":"gmail"}"#))
        if case .signIn = gmail {} else { Issue.record("Gmail signs in with Google, got \(gmail)") }
        let partial = f.fill(from: guess(#"{"name":"a","email":"me@example.com","provider":"feishu"}"#, missing: ["password"], note: "飞书邮箱"))
        #expect(partial == .ask(note: "飞书邮箱 还缺：密码"))
        #expect(f.kind == .feishu)
    }
}
